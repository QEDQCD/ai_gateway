package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/example/ai_gateway/gateway/internal/domain"
	"github.com/example/ai_gateway/gateway/internal/secret"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

type consoleDB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type postgresConsoleService struct {
	db              consoleDB
	authService     AuthService
	chatProxy       ChatProxyService
	ragProxy        RAGProxyService
	seedPlatformKey string
	secretService   platformAPIKeySecretService
}

func NewPostgresConsoleService(
	db consoleDB,
	authService AuthService,
	chatProxy ChatProxyService,
	ragProxy RAGProxyService,
	seedPlatformKey string,
	secretCodecs ...*secret.Codec,
) ConsoleService {
	if db == nil {
		return NewUnavailableConsoleService()
	}
	var secretCodec *secret.Codec
	if len(secretCodecs) > 0 {
		secretCodec = secretCodecs[0]
	}
	return postgresConsoleService{
		db:              db,
		authService:     authService,
		chatProxy:       chatProxy,
		ragProxy:        ragProxy,
		seedPlatformKey: seedPlatformKey,
		secretService:   newPlatformAPIKeySecretService(secretCodec),
	}
}

func (s postgresConsoleService) Overview(ctx context.Context) (OverviewPageData, error) {
	var requests24h int
	var successRate float64
	var activeAPIKeys int

	if err := s.db.QueryRow(ctx, `
		with recent as (
			select status_code
			from audit_logs
			where created_at >= now() - interval '24 hours'
		),
		managed_keys as (
			select distinct target_id
			from audit_events
			where event_type = 'api_key_created'
			  and target_type = 'platform_api_key'
		)
		select
			(select count(*) from recent),
			coalesce((select avg(case when status_code between 200 and 399 then 100.0 else 0 end) from recent), 0),
			(
				select count(*)
				from platform_api_keys
				where status = 'active'
				  and id in (select target_id from managed_keys)
			);
	`).Scan(&requests24h, &successRate, &activeAPIKeys); err != nil {
		return OverviewPageData{}, err
	}

	quotaSummary, err := loadAggregateTenantQuotaSummary(ctx, s.db, time.Now())
	if err != nil {
		return OverviewPageData{}, err
	}
	quotaUsage := 0.0
	if quotaSummary.Configured && quotaSummary.RequestLimit > 0 {
		quotaUsage = math.Min(100, (float64(quotaSummary.RequestsUsed)*100.0)/float64(quotaSummary.RequestLimit))
	}

	routeHealthRows, err := s.collectTableRows(ctx, `
		with recent as (
			select
				request_model,
				max(provider_credential_id) as provider_credential_id,
				round(avg(latency_ms))::integer as avg_latency_ms,
				sum(case when usage_status = 'success' then 1 else 0 end) as success_count,
				count(*) as total_count
			from llm_request_logs
			where request_started_at >= now() - interval '24 hours'
			group by request_model
		)
		select
			recent.request_model,
			coalesce(provider_credentials.display_name, '-'),
			recent.avg_latency_ms::text || ' ms',
			case
				when recent.success_count = recent.total_count then '健康'
				when recent.success_count = 0 then '降级'
				else '告警'
			end
		from recent
		left join provider_credentials on provider_credentials.id = recent.provider_credential_id
		order by recent.total_count desc, recent.request_model asc
		limit 3;
	`)
	if err != nil {
		return OverviewPageData{}, err
	}
	for index := range routeHealthRows {
		if len(routeHealthRows[index].Columns) > 1 {
			routeHealthRows[index].Columns[1] = neutralizeConsoleRouteLabel(routeHealthRows[index].Columns[1])
		}
	}

	topModelsRows, err := s.collectTableRows(ctx, `
		with total as (
			select count(*)::numeric as total_requests
			from llm_request_logs
		)
		select
			request_model,
			count(*)::text,
			coalesce(round((count(*) * 100.0) / nullif((select total_requests from total), 0), 2), 0)::text || '%',
			case
				when max(request_path) = '/v1/chat/completions' then '聊天'
				when max(request_path) = '/v1/embeddings' then '向量'
				else '检索'
			end
		from llm_request_logs
		group by request_model
		order by count(*) desc, request_model asc
		limit 3;
	`)
	if err != nil {
		return OverviewPageData{}, err
	}

	recentAlertRows, err := s.collectTableRows(ctx, `
		select
			to_char(created_at at time zone 'Asia/Shanghai', 'HH24:MI'),
			case alert_type
				when 'quota_warning' then '配额告警'
				when 'route_fallback' then '路由回退'
				when 'latency_spike' then '延迟抖动'
				else alert_type
			end,
			scope
		from operational_alerts
		order by created_at desc
		limit 3;
	`)
	if err != nil {
		return OverviewPageData{}, err
	}

	auditSnapshotRows, err := s.collectTableRows(ctx, `
		select tenant_id, endpoint, status_code::text
		from audit_logs
		order by created_at desc
		limit 3;
	`)
	if err != nil {
		return OverviewPageData{}, err
	}

	return OverviewPageData{
		Stats: []KeyMetric{
			{Label: "24 小时请求量", Value: formatLargeNumber(requests24h)},
			{Label: "成功率", Value: formatPercentage(successRate)},
			{Label: "配额使用率", Value: formatPercentage(quotaUsage)},
			{Label: "活跃 API 密钥", Value: fmt.Sprintf("%d", activeAPIKeys)},
		},
		RouteHealth:   routeHealthRows,
		TopModels:     topModelsRows,
		RecentAlerts:  recentAlertRows,
		AuditSnapshot: auditSnapshotRows,
		QuotaSummary:  quotaSummary,
	}, nil
}

func (s postgresConsoleService) SystemStatus(ctx context.Context) (ConsoleSystemStatus, error) {
	var unhealthyRoutes int
	var quotaEnabledTenants int
	if err := s.db.QueryRow(ctx, `
		select
			(select count(*) from route_catalog where health_status <> 'healthy'),
			(select count(*) from tenant_quota_policies);
	`).Scan(&unhealthyRoutes, &quotaEnabledTenants); err != nil {
		return ConsoleSystemStatus{}, err
	}

	return ConsoleSystemStatus{
		ConsoleStage:     "控制台预览版",
		RunMode:          s.lookupSetting(ctx, "routing_mode", "数据库模式"),
		GatewayHealth:    mapBool(unhealthyRoutes == 0, "健康", "告警"),
		QuotaProtection:  mapBool(quotaEnabledTenants > 0, "已启用", "未启用"),
		ConsoleEntry:     "31873",
		GatewayAdminAPI:  "32658",
		InternalServices: []string{"internal-search"},
		HiddenModules:    []string{"内部检索能力", "高级路由设置"},
	}, nil
}

func (s postgresConsoleService) Applications(ctx context.Context) (ApplicationsPageData, error) {
	rows, err := s.db.Query(ctx, `
		select id, email, name, company_name, use_case, status, created_at
		from account_applications
		order by created_at desc, id asc;
	`)
	if err != nil {
		return ApplicationsPageData{}, err
	}
	defer rows.Close()

	items := make([]ApplicationItem, 0)
	for rows.Next() {
		item, err := scanApplicationItem(rows)
		if err != nil {
			return ApplicationsPageData{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ApplicationsPageData{}, err
	}

	return ApplicationsPageData{Items: items}, nil
}

func (s postgresConsoleService) CreateApplication(ctx context.Context, req CreateApplicationRequest) (ApplicationMutationResult, error) {
	email := strings.TrimSpace(req.Email)
	emailNormalized := strings.ToLower(email)
	name := strings.TrimSpace(req.Name)
	companyName := strings.TrimSpace(req.CompanyName)
	useCase := strings.TrimSpace(req.UseCase)
	password := req.Password
	captchaPassToken := strings.TrimSpace(req.CaptchaPassToken)

	if email == "" {
		return ApplicationMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "email is required",
		}
	}
	if !strings.Contains(email, "@") {
		return ApplicationMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "邮箱格式不合法，请输入包含 @ 的邮箱地址。",
		}
	}
	if name == "" {
		return ApplicationMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "name is required",
		}
	}
	if companyName == "" {
		return ApplicationMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "company_name is required",
		}
	}
	if useCase == "" {
		return ApplicationMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "use_case is required",
		}
	}
	if strings.TrimSpace(password) == "" {
		return ApplicationMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "password is required",
		}
	}
	if captchaPassToken == "" {
		return ApplicationMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "captcha_pass_token is required",
		}
	}

	if err := validateConsolePassword(password); err != nil {
		return ApplicationMutationResult{}, err
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return ApplicationMutationResult{}, err
	}

	var consumedCaptchaID string
	if err := s.db.QueryRow(ctx, `
		update captcha_challenges
		set
			status = 'consumed',
			consumed_at = now(),
			updated_at = now()
		where pass_token_hash = $1
		  and status = 'verified'
		  and expires_at > now()
		returning id
	`, hashCaptchaValue(captchaPassToken)).Scan(&consumedCaptchaID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ApplicationMutationResult{}, StatusError{
				Code:    http.StatusBadRequest,
				Message: "captcha_pass_token is invalid",
			}
		}
		return ApplicationMutationResult{}, err
	}

	var activeUserExists bool
	if err := s.db.QueryRow(ctx, `
		select exists(
			select 1
			from users
			where lower(email) = $1
			  and status = 'active'
		)
	`, emailNormalized).Scan(&activeUserExists); err != nil {
		return ApplicationMutationResult{}, err
	}
	if activeUserExists {
		return ApplicationMutationResult{}, StatusError{
			Code:    http.StatusConflict,
			Message: "email already has an active account",
		}
	}

	var pendingExists bool
	if err := s.db.QueryRow(ctx, `
		select exists(
			select 1
			from account_applications
			where email_normalized = $1
			  and status = 'pending'
		)
	`, emailNormalized).Scan(&pendingExists); err != nil {
		return ApplicationMutationResult{}, err
	}
	if pendingExists {
		return ApplicationMutationResult{}, StatusError{
			Code:    http.StatusConflict,
			Message: "email already has a pending application",
		}
	}

	row := s.db.QueryRow(ctx, `
		insert into account_applications (
			id,
			email,
			email_normalized,
			name,
			company_name,
			use_case,
			password_hash,
			status
		) values (
			$1, $2, $3, $4, $5, $6, $7, 'pending'
		)
		returning id, email, name, company_name, use_case, status, created_at
	`, newApplicationID(), email, emailNormalized, name, companyName, useCase, string(passwordHash))

	item, err := scanApplicationItem(row)
	if err != nil {
		return ApplicationMutationResult{}, err
	}

	return ApplicationMutationResult{Item: item}, nil
}

func (s postgresConsoleService) ApproveApplication(ctx context.Context, id string, req ApproveApplicationRequest) (ApplicationMutationResult, error) {
	applicationID := strings.TrimSpace(id)
	actorID := strings.TrimSpace(req.ActorID)
	comment := strings.TrimSpace(req.Comment)
	tenantID := strings.TrimSpace(req.TenantID)
	if applicationID == "" {
		return ApplicationMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "application id is required",
		}
	}
	if actorID == "" {
		return ApplicationMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "actor_id is required",
		}
	}
	if tenantID == "" {
		return ApplicationMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "tenant_id is required",
		}
	}

	row := s.db.QueryRow(ctx, `
		with selected_application as (
			select
				id,
				email,
				name,
				company_name,
				use_case,
				password_hash,
				created_at
			from account_applications
			where id = $1
			  and status = 'pending'
			  and password_hash is not null
			for update
		),
		updated_application as (
			update account_applications
			set
				status = 'approved',
				reviewer_id = $2,
				review_comment = $3,
				reviewed_at = now(),
				password_hash = null
			from selected_application
			where account_applications.id = selected_application.id
			returning
				account_applications.id,
				selected_application.email,
				selected_application.name,
				selected_application.company_name,
				selected_application.use_case,
				selected_application.password_hash,
				account_applications.status,
				selected_application.created_at
		),
		upserted_tenant as (
			insert into tenants (id, name, status)
			select
				$5,
				case
					when company_name <> '' then company_name
					else name
				end,
				'active'
			from updated_application
			on conflict (id) do nothing
		),
		upserted_user as (
			insert into users (id, email, name, role, status, password_hash)
			select $4, email, name, 'member', 'active', password_hash
			from updated_application
			on conflict (email) do update
			set
				name = excluded.name,
				status = 'active',
				password_hash = excluded.password_hash
			returning id
		),
		upserted_membership as (
			insert into tenant_memberships (id, tenant_id, user_id, role, status)
			select $6, $5, id, 'member', 'active'
			from upserted_user
			on conflict (tenant_id, user_id) do update
			set
				role = 'member',
				status = 'active'
		),
		inserted_audit as (
			insert into audit_events (
				id,
				actor_type,
				actor_user_id,
				tenant_id,
				event_type,
				target_type,
				target_id,
				detail
			)
			select
				$7,
				'admin',
				$2,
				$5,
				'application_approved',
				'account_application',
				id,
				$3
			from updated_application
		)
		select id, email, name, company_name, use_case, status, created_at
		from updated_application;
	`, applicationID, actorID, comment, newUserID(), tenantID, newTenantMembershipID(), newAuditEventID())

	item, err := scanApplicationItem(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ApplicationMutationResult{}, s.mapApproveApplicationPendingError(ctx, applicationID)
		}
		return ApplicationMutationResult{}, mapApproveApplicationWriteError(err)
	}

	return ApplicationMutationResult{Item: item}, nil
}

func (s postgresConsoleService) RejectApplication(ctx context.Context, id string, req RejectApplicationRequest) (ApplicationMutationResult, error) {
	applicationID := strings.TrimSpace(id)
	actorID := strings.TrimSpace(req.ActorID)
	comment := strings.TrimSpace(req.Comment)
	if applicationID == "" {
		return ApplicationMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "application id is required",
		}
	}
	if actorID == "" {
		return ApplicationMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "actor_id is required",
		}
	}

	row := s.db.QueryRow(ctx, `
		with updated_application as (
			update account_applications
			set
				status = 'rejected',
				reviewer_id = $2,
				review_comment = $3,
				reviewed_at = now(),
				password_hash = null
			where id = $1
			  and status = 'pending'
			returning id, email, name, company_name, use_case, status, created_at
		),
		inserted_audit as (
			insert into audit_events (
				id,
				actor_type,
				actor_user_id,
				event_type,
				target_type,
				target_id,
				detail
			)
			select
				$4,
				'admin',
				$2,
				'application_rejected',
				'account_application',
				id,
				$3
			from updated_application
		)
		select id, email, name, company_name, use_case, status, created_at
		from updated_application;
	`, applicationID, actorID, comment, newAuditEventID())

	item, err := scanApplicationItem(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ApplicationMutationResult{}, s.mapRejectApplicationPendingError(ctx, applicationID)
		}
		return ApplicationMutationResult{}, mapRejectApplicationWriteError(err)
	}

	return ApplicationMutationResult{Item: item}, nil
}

func (s postgresConsoleService) APIKeys(ctx context.Context) (APIKeysPageData, error) {
	rows, err := s.db.Query(ctx, `
		with managed_keys as (
			select distinct target_id
			from audit_events
			where event_type = 'api_key_created'
			  and target_type = 'platform_api_key'
		)
		select
			p.id,
			p.name,
			t.id,
			p.status,
			p.scopes,
			coalesce(p.last_used_at, p.created_at),
			coalesce(p.created_by_user_id, ''),
			coalesce(p.expires_at, p.created_at + interval '30 days'),
			p.secret_recoverable
		from platform_api_keys p
		join tenants t on t.id = p.tenant_id
		where p.id in (select target_id from managed_keys)
		order by p.created_at asc, p.id asc;
	`)
	if err != nil {
		return APIKeysPageData{}, err
	}
	defer rows.Close()

	items := make([]APIKeyItem, 0)
	for rows.Next() {
		var item APIKeyItem
		var status string
		var lastUsedAt time.Time
		var expiresAt time.Time
		var secretRecoverable bool
		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&item.Tenant,
			&status,
			&item.Scopes,
			&lastUsedAt,
			&item.CreatedByUserID,
			&expiresAt,
			&secretRecoverable,
		); err != nil {
			return APIKeysPageData{}, err
		}
		item.Status = translateLifecycleStatus(status, expiresAt, time.Now())
		item.LastUsedAt = lastUsedAt.In(shanghaiLocation()).Format(time.RFC3339)
		item.ExpiresAt = expiresAt.In(shanghaiLocation()).Format(time.RFC3339)
		item.Revealable = secretRecoverable
		item.LegacyUnrecoverable = !secretRecoverable
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return APIKeysPageData{}, err
	}

	return APIKeysPageData{
		Items:          items,
		CredentialMode: s.lookupSetting(ctx, "credential_mode", "平台密钥与上游凭据分离，支持 BYOK 扩展"),
	}, nil
}

func (s postgresConsoleService) CreateAPIKey(ctx context.Context, req CreateAPIKeyRequest) (APIKeyMutationResult, error) {
	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.Name) == "" {
		return APIKeyMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "tenant_id and name are required",
		}
	}

	rawKey := newPlatformAPIKeySecret()
	principal, _ := ConsolePrincipalFromContext(ctx)
	item, err := s.insertPlatformAPIKey(ctx, createPlatformAPIKeyInput{
		ID:              newPlatformAPIKeyID(),
		TenantID:        strings.TrimSpace(req.TenantID),
		Name:            strings.TrimSpace(req.Name),
		Scopes:          sanitizeScopes(req.Scopes),
		RawKey:          rawKey,
		CreatedByUserID: principal.UserID,
	})
	if err != nil {
		return APIKeyMutationResult{}, err
	}

	return APIKeyMutationResult{
		Item:   item,
		RawKey: rawKey,
	}, nil
}

func (s postgresConsoleService) RotateAPIKey(ctx context.Context, id string, req RotateAPIKeyRequest) (APIKeyMutationResult, error) {
	if strings.TrimSpace(id) == "" {
		return APIKeyMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "api key id is required",
		}
	}

	newRawKey := newPlatformAPIKeySecret()
	ciphertext, recoverable, err := s.secretService.Encrypt(newRawKey)
	if err != nil {
		return APIKeyMutationResult{}, err
	}
	var item APIKeyItem
	var status string
	var lastUsedAt time.Time
	var expiresAt time.Time
	row := s.db.QueryRow(ctx, `
		with previous as (
			select
				tenant_id,
				coalesce(created_by_user_id, '') as created_by_user_id,
				case
					when $3 <> '' then $3
					else name
				end as next_name,
				case
					when coalesce(cardinality($4::text[]), 0) > 0 then $4::text[]
					else scopes
				end as next_scopes
			from platform_api_keys
			where id = $1
		),
		inserted as (
			insert into platform_api_keys (
				id,
				tenant_id,
				name,
				key_hash,
				key_ciphertext,
				key_kek_version,
				status,
				scopes,
				created_by_user_id,
				created_at,
				expires_at,
				rotated_from_key_id,
				secret_recoverable
			)
			select
				$2,
				tenant_id,
				next_name,
				$5,
				$6,
				$7,
				'active',
				next_scopes,
				nullif(created_by_user_id, ''),
				now(),
				now() + interval '30 days',
				$1,
				$8
			from previous
			returning id, name, tenant_id, status, scopes, created_at, coalesce(created_by_user_id, '') as created_by_user_id, expires_at, secret_recoverable
		),
		disabled as (
			update platform_api_keys
			set status = 'disabled',
				disabled_at = now(),
				disabled_reason = 'rotated'
			where id = $1
			  and exists (select 1 from inserted)
		)
		select id, name, tenant_id, status, scopes, created_at, coalesce(created_by_user_id, ''), expires_at, secret_recoverable
		from inserted;
	`, strings.TrimSpace(id), newPlatformAPIKeyID(), strings.TrimSpace(req.Name), sanitizeScopes(req.Scopes), hashPlatformAPIKey(newRawKey), ciphertext, platformAPIKeyKEKVersionV1, recoverable)
	if err := row.Scan(&item.ID, &item.Name, &item.Tenant, &status, &item.Scopes, &lastUsedAt, &item.CreatedByUserID, &expiresAt, &item.Revealable); err != nil {
		return APIKeyMutationResult{}, mapAPIKeyMutationError(err, "api key not found")
	}

	item.Status = translateLifecycleStatus(status, expiresAt, time.Now())
	item.LastUsedAt = lastUsedAt.In(shanghaiLocation()).Format(time.RFC3339)
	item.ExpiresAt = expiresAt.In(shanghaiLocation()).Format(time.RFC3339)
	item.LegacyUnrecoverable = !item.Revealable
	return APIKeyMutationResult{
		Item:   item,
		RawKey: newRawKey,
	}, nil
}

func (s postgresConsoleService) DeactivateAPIKey(ctx context.Context, id string) (APIKeyMutationResult, error) {
	if strings.TrimSpace(id) == "" {
		return APIKeyMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "api key id is required",
		}
	}

	var item APIKeyItem
	var status string
	var lastUsedAt time.Time
	var expiresAt time.Time
	if err := s.db.QueryRow(ctx, `
		update platform_api_keys
		set status = 'disabled',
			disabled_at = now(),
			disabled_reason = 'deactivated'
		where id = $1
		returning id, name, tenant_id, status, scopes, coalesce(last_used_at, created_at), coalesce(created_by_user_id, ''), coalesce(expires_at, created_at + interval '30 days'), secret_recoverable;
	`, strings.TrimSpace(id)).Scan(&item.ID, &item.Name, &item.Tenant, &status, &item.Scopes, &lastUsedAt, &item.CreatedByUserID, &expiresAt, &item.Revealable); err != nil {
		return APIKeyMutationResult{}, mapAPIKeyMutationError(err, "api key not found")
	}

	item.Status = translateLifecycleStatus(status, expiresAt, time.Now())
	item.LastUsedAt = lastUsedAt.In(shanghaiLocation()).Format(time.RFC3339)
	item.ExpiresAt = expiresAt.In(shanghaiLocation()).Format(time.RFC3339)
	item.LegacyUnrecoverable = !item.Revealable
	return APIKeyMutationResult{Item: item}, nil
}

func (s postgresConsoleService) DeleteAPIKey(ctx context.Context, id string) (APIKeyMutationResult, error) {
	if strings.TrimSpace(id) == "" {
		return APIKeyMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "api key id is required",
		}
	}

	var item APIKeyItem
	var status string
	var lastUsedAt time.Time
	var expiresAt time.Time
	if err := s.db.QueryRow(ctx, `
		delete from platform_api_keys
		where id = $1
		returning id, name, tenant_id, status, scopes, coalesce(last_used_at, created_at), coalesce(created_by_user_id, ''), coalesce(expires_at, created_at + interval '30 days'), secret_recoverable;
	`, strings.TrimSpace(id)).Scan(&item.ID, &item.Name, &item.Tenant, &status, &item.Scopes, &lastUsedAt, &item.CreatedByUserID, &expiresAt, &item.Revealable); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return APIKeyMutationResult{}, StatusError{
				Code:    http.StatusNotFound,
				Message: "api key not found",
				Err:     err,
			}
		}

		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return APIKeyMutationResult{}, StatusError{
				Code:    http.StatusConflict,
				Message: "api key has request history and cannot be deleted",
				Err:     err,
			}
		}

		return APIKeyMutationResult{}, err
	}

	item.Status = "已删除"
	item.LastUsedAt = lastUsedAt.In(shanghaiLocation()).Format(time.RFC3339)
	item.ExpiresAt = expiresAt.In(shanghaiLocation()).Format(time.RFC3339)
	item.LegacyUnrecoverable = !item.Revealable
	return APIKeyMutationResult{Item: item}, nil
}

type managedAPIKeySecretRecord struct {
	ID          string
	TenantID    string
	FullKey     string
	Recoverable bool
	ExpiresAt   time.Time
}

func (s postgresConsoleService) RevealAPIKeySecret(ctx context.Context, id string) (APIKeySecretView, error) {
	metadata := RequestAuditMetadataFromContext(ctx)
	record, err := s.loadManagedAPIKeySecretRecord(ctx, id)
	if err != nil {
		return APIKeySecretView{}, err
	}
	actorUserID := ""
	if principal, ok := ConsolePrincipalFromContext(ctx); ok && principal.Role == "admin" {
		actorUserID = principal.UserID
	}
	if err := insertAPIKeySecretAccessLog(ctx, s.db, record.ID, record.TenantID, actorUserID, "admin", "reveal", "allowed", metadata.IPAddress, metadata.UserAgent); err != nil {
		return APIKeySecretView{}, err
	}
	return buildAPIKeySecretSummaryView(record.ID, record.FullKey, record.Recoverable, record.ExpiresAt), nil
}

func (s postgresConsoleService) CopyAPIKeySecret(ctx context.Context, id string, ip string, userAgent string) (APIKeySecretView, error) {
	record, err := s.loadManagedAPIKeySecretRecord(ctx, id)
	if err != nil {
		return APIKeySecretView{}, err
	}

	actorUserID := ""
	if principal, ok := ConsolePrincipalFromContext(ctx); ok && principal.Role == "admin" {
		actorUserID = principal.UserID
	}
	if err := insertAPIKeySecretAccessLog(ctx, s.db, record.ID, record.TenantID, actorUserID, "admin", "copy", "allowed", ip, userAgent); err != nil {
		return APIKeySecretView{}, err
	}

	return buildAPIKeySecretCopyView(record.ID, record.FullKey, record.Recoverable, record.ExpiresAt), nil
}

func (s postgresConsoleService) loadManagedAPIKeySecretRecord(ctx context.Context, id string) (managedAPIKeySecretRecord, error) {
	var record managedAPIKeySecretRecord
	var ciphertext string
	if err := s.db.QueryRow(ctx, `
		select id, tenant_id, key_ciphertext, secret_recoverable, coalesce(expires_at, created_at + interval '30 days')
		from platform_api_keys
		where id = $1;
	`, strings.TrimSpace(id)).Scan(&record.ID, &record.TenantID, &ciphertext, &record.Recoverable, &record.ExpiresAt); err != nil {
		return managedAPIKeySecretRecord{}, mapAPIKeyMutationError(err, "api key not found")
	}

	fullKey, err := s.secretService.Reveal(ciphertext, record.Recoverable)
	if err != nil {
		return managedAPIKeySecretRecord{}, err
	}
	record.FullKey = fullKey
	return record, nil
}

func insertAPIKeySecretAccessLog(
	ctx context.Context,
	db consoleDB,
	apiKeyID string,
	tenantID string,
	actorUserID string,
	actorRole string,
	action string,
	accessResult string,
	ip string,
	userAgent string,
) error {
	_, err := db.Exec(ctx, `
		insert into api_key_secret_access_logs (
			id,
			api_key_id,
			tenant_id,
			actor_user_id,
			actor_role,
			action,
			access_result,
			ip_address,
			user_agent
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9);
	`, newAPIKeySecretAccessLogID(), strings.TrimSpace(apiKeyID), strings.TrimSpace(tenantID), nullableText(strings.TrimSpace(actorUserID)), strings.TrimSpace(actorRole), strings.TrimSpace(action), strings.TrimSpace(accessResult), strings.TrimSpace(ip), strings.TrimSpace(userAgent))
	return err
}

func nullableText(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func (s postgresConsoleService) Routes(ctx context.Context) (RoutesPageData, error) {
	var activeProviders int
	var modelMappings int
	if err := s.db.QueryRow(ctx, `
		select
			(select count(*) from provider_credentials where status = 'active'),
			(select count(*) from route_catalog);
	`).Scan(&activeProviders, &modelMappings); err != nil {
		return RoutesPageData{}, err
	}

	rows, err := s.db.Query(ctx, `
		select requested_model, resolved_provider, provider_credential_id, latency_ms, health_status
		from route_catalog
		order by requested_model asc;
	`)
	if err != nil {
		return RoutesPageData{}, err
	}
	defer rows.Close()

	items := make([]RouteItem, 0)
	for rows.Next() {
		var item RouteItem
		var latency int
		var status string
		if err := rows.Scan(&item.RequestedModel, &item.RouteLabel, &item.Credential, &latency, &status); err != nil {
			return RoutesPageData{}, err
		}
		item.RouteLabel = neutralizeConsoleRouteLabel(item.RouteLabel)
		item.Credential = neutralizeConsoleCredential(item.Credential)
		item.Latency = fmt.Sprintf("%d ms", latency)
		item.Status = translateRouteHealth(status)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return RoutesPageData{}, err
	}

	return RoutesPageData{
		Stats: []RouteMetric{
			{Label: "活跃供应商", Value: fmt.Sprintf("%d", activeProviders)},
			{Label: "模型映射数", Value: fmt.Sprintf("%d", modelMappings)},
			{Label: "回退策略", Value: s.lookupSetting(ctx, "fallback_policy", "已启用")},
			{Label: "运行模式", Value: s.lookupSetting(ctx, "routing_mode", "数据库模式")},
		},
		Items: items,
		PolicySummary: []string{
			"路由模式：" + s.lookupSetting(ctx, "routing_mode", "数据库模式"),
			"模型优先解析：" + s.lookupSetting(ctx, "model_resolution_mode", "已启用"),
			"请求会先解析到平台托管凭证，再按平台路由策略回退。",
		},
	}, nil
}

func (s postgresConsoleService) Playground(ctx context.Context) (PlaygroundPageData, error) {
	rows, err := s.db.Query(ctx, `
		select requested_model
		from route_catalog
		where endpoint in ('/v1/chat/completions', '/v1/rag/query')
		order by requested_model asc;
	`)
	if err != nil {
		return PlaygroundPageData{}, err
	}
	defer rows.Close()

	models := make([]string, 0)
	for rows.Next() {
		var model string
		if err := rows.Scan(&model); err != nil {
			return PlaygroundPageData{}, err
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return PlaygroundPageData{}, err
	}

	var lastRun PlaygroundRunResponse
	var found bool
	var latencyMS int
	var statusCode int
	if err := s.db.QueryRow(ctx, `
		select resolved_provider, endpoint, latency_ms, status_code, response_excerpt, (
			select name from platform_api_keys where id = playground_runs.platform_api_key_id
		)
		from playground_runs
		order by created_at desc
		limit 1;
	`).Scan(&lastRun.RouteLabel, &lastRun.Endpoint, &latencyMS, &statusCode, &lastRun.Response, &lastRun.PlatformKey); err == nil {
		found = true
	}

	if found {
		lastRun.RouteLabel = neutralizeConsoleRouteLabel(lastRun.RouteLabel)
		lastRun.Endpoint = neutralizeConsoleEndpoint(lastRun.Endpoint)
		lastRun.Latency = fmt.Sprintf("%d ms", latencyMS)
		lastRun.Status = fmt.Sprintf("%d %s", statusCode, mapBool(statusCode < 400, "成功", "失败"))
	}

	data := PlaygroundPageData{AvailableModels: models}
	if found {
		data.LastRun = &lastRun
	}
	return data, nil
}

func (s postgresConsoleService) RunPlayground(ctx context.Context, req PlaygroundRunRequest) (PlaygroundRunResponse, error) {
	model := strings.TrimSpace(req.Model)
	prompt := strings.TrimSpace(req.Prompt)
	if model == "" || prompt == "" {
		return PlaygroundRunResponse{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "playground request invalid",
			Err:     fmt.Errorf("model and prompt are required"),
		}
	}

	resolved, err := s.authService.Resolve(ctx, s.seedPlatformKey, model)
	if err != nil {
		return PlaygroundRunResponse{}, err
	}

	var endpoint string
	var responseText string
	start := time.Now()

	row := s.db.QueryRow(ctx, `select endpoint from route_catalog where requested_model = $1 limit 1;`, model)
	if err := row.Scan(&endpoint); err != nil {
		return PlaygroundRunResponse{}, err
	}

	if endpoint == "/v1/rag/query" {
		resp, err := s.ragProxy.Query(ctx, RAGQueryRequest{
			KnowledgeBaseID: "kb_product_docs",
			Question:        prompt,
		}, resolved)
		if err != nil {
			return PlaygroundRunResponse{}, err
		}
		responseText = resp.Answer
	} else {
		resp, err := s.chatProxy.Complete(ctx, ChatRequest{
			Model: model,
			Messages: []ChatMessage{
				{Role: "user", Content: prompt},
			},
		}, resolved)
		if err != nil {
			return PlaygroundRunResponse{}, err
		}
		if len(resp.Choices) > 0 {
			responseText = resp.Choices[0].Message.Content
		}
		endpoint = "/v1/chat/completions"
	}

	latencyMS := durationMilliseconds(time.Since(start))
	if err := s.insertPlaygroundRun(ctx, resolved, model, prompt, endpoint, responseText, http.StatusOK, latencyMS); err != nil {
		return PlaygroundRunResponse{}, err
	}
	if err := s.insertAuditLog(ctx, resolved.TenantID, resolved.PlatformAPIKeyID, model, endpoint, http.StatusOK, resolved.SelectedProviderName, latencyMS); err != nil {
		return PlaygroundRunResponse{}, err
	}

	platformKeyName := s.lookupPlatformKeyName(ctx, resolved.PlatformAPIKeyID)
	return PlaygroundRunResponse{
		RouteLabel:  neutralizeConsoleRouteLabel(resolved.SelectedProviderName),
		Endpoint:    neutralizeConsoleEndpoint(endpoint),
		Latency:     fmt.Sprintf("%d ms", latencyMS),
		Status:      "200 成功",
		Response:    responseText,
		PlatformKey: platformKeyName,
	}, nil
}

func (s postgresConsoleService) StreamPlayground(ctx context.Context, req PlaygroundRunRequest) (PlaygroundStreamSession, error) {
	model := strings.TrimSpace(req.Model)
	prompt := strings.TrimSpace(req.Prompt)
	if model == "" || prompt == "" {
		return PlaygroundStreamSession{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "playground request invalid",
			Err:     fmt.Errorf("model and prompt are required"),
		}
	}

	resolved, err := s.authService.Resolve(ctx, s.seedPlatformKey, model)
	if err != nil {
		return PlaygroundStreamSession{}, err
	}

	var endpoint string
	if err := s.db.QueryRow(ctx, `select endpoint from route_catalog where requested_model = $1 limit 1;`, model).Scan(&endpoint); err != nil {
		return PlaygroundStreamSession{}, err
	}
	if endpoint == "/v1/rag/query" {
		return PlaygroundStreamSession{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "playground stream only supports chat routes",
			Err:     fmt.Errorf("requested model %q resolves to non-chat endpoint %q", model, endpoint),
		}
	}

	start := time.Now()
	stream, err := s.chatProxy.Stream(ctx, ChatRequest{
		Model: model,
		Messages: []ChatMessage{
			{Role: "user", Content: prompt},
		},
		Stream: true,
	}, resolved)
	if err != nil {
		return PlaygroundStreamSession{}, err
	}

	requestID := requestIDFromContext(ctx)
	platformKeyName := s.lookupPlatformKeyName(ctx, resolved.PlatformAPIKeyID)
	contentType := strings.TrimSpace(stream.ContentType)
	if contentType == "" {
		contentType = "text/event-stream; charset=utf-8"
	}

	return PlaygroundStreamSession{
		StatusCode:  stream.StatusCode,
		ContentType: contentType,
		Run: func(emit func([]byte) error) (PlaygroundRunResponse, error) {
			var emitErr error
			if err := emitPlaygroundStreamEvent(emit, "meta", map[string]any{
				"request_id": requestID,
				"model":      model,
				"endpoint":   "/v1/chat/completions",
			}); err != nil {
				emitErr = err
			}

			firstTokenLatencyMS := int64(0)
			firstStatsSent := false
			result, streamErr := stream.Run(func(chunk []byte) error {
				if emitErr != nil {
					return emitErr
				}
				return emitPlaygroundTokenChunk(emit, chunk)
			}, func() {
				if emitErr != nil || firstStatsSent {
					return
				}
				firstTokenLatencyMS = durationMilliseconds(time.Since(start))
				firstStatsSent = true
				if err := emitPlaygroundStreamEvent(emit, "stats", map[string]any{
					"first_token_latency_ms": firstTokenLatencyMS,
					"status":                 "streaming",
					"latency_ms":             firstTokenLatencyMS,
				}); err != nil {
					emitErr = err
				}
			})
			if streamErr == nil && emitErr != nil {
				streamErr = emitErr
			}

			latencyMS := durationMilliseconds(time.Since(start))
			statusCode := stream.StatusCode
			if statusCode == 0 {
				statusCode = http.StatusOK
			}
			if streamErr != nil {
				if result.ClientAborted && result.SawContentToken {
					statusCode = http.StatusOK
				} else if statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices {
					statusCode = http.StatusInternalServerError
				}
			}

			responseText := extractPlaygroundResponseText(result.Response)
			if err := s.insertPlaygroundRun(ctx, resolved, model, prompt, "/v1/chat/completions", responseText, statusCode, latencyMS); err != nil {
				return PlaygroundRunResponse{}, err
			}
			if err := s.insertAuditLog(ctx, resolved.TenantID, resolved.PlatformAPIKeyID, model, "/v1/chat/completions", statusCode, resolved.SelectedProviderName, latencyMS); err != nil {
				return PlaygroundRunResponse{}, err
			}

			finalResponse := PlaygroundRunResponse{
				RouteLabel:  neutralizeConsoleRouteLabel(resolved.SelectedProviderName),
				Endpoint:    neutralizeConsoleEndpoint("/v1/chat/completions"),
				Latency:     fmt.Sprintf("%d ms", latencyMS),
				Status:      fmt.Sprintf("%d %s", statusCode, mapBool(statusCode < 400, "成功", "失败")),
				Response:    responseText,
				PlatformKey: platformKeyName,
			}
			if emitErr == nil {
				_ = emitPlaygroundStreamEvent(emit, "stats", map[string]any{
					"first_token_latency_ms": firstTokenLatencyMS,
					"status":                 finalResponse.Status,
					"latency_ms":             latencyMS,
				})
				if streamErr == nil {
					_ = emitPlaygroundStreamEvent(emit, "done", map[string]any{
						"status": finalResponse.Status,
					})
				}
			}

			return finalResponse, streamErr
		},
	}, nil
}

func (s postgresConsoleService) Audit(ctx context.Context) (AuditPageData, error) {
	var totalRequests int64
	var failedRequests int64
	var rateLimitedRequests int64
	var upstreamErrors int64
	if err := s.db.QueryRow(ctx, `
		select
			count(*),
			count(*) filter (where usage_status <> 'success'),
			count(*) filter (where usage_status = 'rate_limited' or status_code = 429),
			count(*) filter (where usage_status in ('upstream_error', 'auth_failed', 'timeout'))
		from llm_request_logs
		where request_started_at >= now() - interval '24 hours';
	`).Scan(&totalRequests, &failedRequests, &rateLimitedRequests, &upstreamErrors); err != nil {
		return AuditPageData{}, err
	}

	if totalRequests == 0 {
		return s.auditFromFallbackLogs(ctx)
	}

	rows, err := s.db.Query(ctx, `
		select
			to_char(l.request_started_at at time zone 'Asia/Shanghai', 'MM-DD HH24:MI'),
			l.tenant_id,
			l.request_path,
			l.request_model,
			coalesce(nullif(l.upstream_model, ''), l.request_model),
			l.usage_status,
			coalesce(pc.display_name, l.provider_credential_id),
			l.latency_ms,
			l.first_token_latency_ms,
			l.usage_source
		from llm_request_logs l
		left join provider_credentials pc on pc.id = l.provider_credential_id
		order by l.request_started_at desc, l.id desc
		limit 12;
	`)
	if err != nil {
		return AuditPageData{}, err
	}
	defer rows.Close()

	items := make([]AuditItem, 0, 12)
	for rows.Next() {
		var item AuditItem
		var status string
		var latencyMS int64
		var firstTokenLatencyMS int64
		var usageSource string
		if err := rows.Scan(
			&item.Time,
			&item.Tenant,
			&item.Endpoint,
			&item.RequestModel,
			&item.UpstreamModel,
			&status,
			&item.RouteLabel,
			&latencyMS,
			&firstTokenLatencyMS,
			&usageSource,
		); err != nil {
			return AuditPageData{}, err
		}
		item.Endpoint = neutralizeConsoleEndpoint(item.Endpoint)
		item.RouteLabel = neutralizeConsoleRouteLabel(item.RouteLabel)
		item.Status = translateUsageStatus(status)
		item.Latency = fmt.Sprintf("%d ms", latencyMS)
		item.FirstTokenLatencyMS = firstTokenLatencyMS
		item.UsageSource = translateUsageSource(usageSource)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return AuditPageData{}, err
	}

	eventRows, err := s.db.Query(ctx, `
		select
			to_char(e.created_at at time zone 'Asia/Shanghai', 'MM-DD HH24:MI'),
			e.event_type,
			e.usage_status,
			e.detail,
			l.request_model,
			coalesce(pc.display_name, l.provider_credential_id),
			l.latency_ms,
			e.status_code,
			e.usage_source
		from llm_request_events e
		join llm_request_logs l on l.id = e.request_log_id and l.tenant_id = e.tenant_id
		left join provider_credentials pc on pc.id = l.provider_credential_id
		order by e.created_at desc
		limit 8;
	`)
	if err != nil {
		return AuditPageData{}, err
	}
	defer eventRows.Close()

	events := make([]AuditEvent, 0, 8)
	for eventRows.Next() {
		var event AuditEvent
		var status string
		var eventType string
		var requestModel string
		var provider string
		var latencyMS int64
		var statusCode int
		var detail string
		var usageSource string
		if err := eventRows.Scan(&event.Time, &eventType, &status, &detail, &requestModel, &provider, &latencyMS, &statusCode, &usageSource); err != nil {
			return AuditPageData{}, err
		}
		event.Type = translateUsageEventType(eventType)
		event.Status = translateUsageStatus(status)
		event.Detail = describeUsageEvent(detail, eventType, requestModel, provider, latencyMS, statusCode, usageSource)
		events = append(events, event)
	}
	if err := eventRows.Err(); err != nil {
		return AuditPageData{}, err
	}

	return AuditPageData{
		Metrics: []AuditMetric{
			{Label: "最近 24 小时请求", Value: formatLargeNumber(int(totalRequests))},
			{Label: "失败请求", Value: formatLargeNumber(int(failedRequests))},
			{Label: "限流次数", Value: formatLargeNumber(int(rateLimitedRequests))},
			{Label: "上游错误", Value: formatLargeNumber(int(upstreamErrors))},
		},
		Events: events,
		Items:  items,
		Summaries: []AuditSummary{
			{Title: "真实摘要", Content: fmt.Sprintf("最近 24 小时共 %d 次请求，其中 %d 次失败。", totalRequests, failedRequests)},
			{Title: "数据来源", Content: "本页优先展示 llm_request_logs 与 llm_request_events 的真实聚合结果。"},
		},
	}, nil
}

func (s postgresConsoleService) auditFromFallbackLogs(ctx context.Context) (AuditPageData, error) {
	rows, err := s.db.Query(ctx, `
		select
			to_char(created_at at time zone 'Asia/Shanghai', 'MM-DD HH24:MI'),
			tenant_id,
			endpoint,
			status_code,
			provider_display_name,
			latency_ms
		from audit_logs
		order by created_at desc
		limit 10;
	`)
	if err != nil {
		return AuditPageData{}, err
	}
	defer rows.Close()

	items := make([]AuditItem, 0, 10)
	for rows.Next() {
		var item AuditItem
		var statusCode int
		var latencyMS int64
		if err := rows.Scan(&item.Time, &item.Tenant, &item.Endpoint, &statusCode, &item.RouteLabel, &latencyMS); err != nil {
			return AuditPageData{}, err
		}
		item.Endpoint = neutralizeConsoleEndpoint(item.Endpoint)
		item.RouteLabel = neutralizeConsoleRouteLabel(item.RouteLabel)
		item.RequestModel = "-"
		item.UpstreamModel = "-"
		item.Status = fmt.Sprintf("%d", statusCode)
		item.Latency = fmt.Sprintf("%d ms", latencyMS)
		item.UsageSource = "审计回退"
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return AuditPageData{}, err
	}

	return AuditPageData{
		Metrics: []AuditMetric{
			{Label: "最近 24 小时请求", Value: formatLargeNumber(len(items))},
			{Label: "失败请求", Value: "0"},
			{Label: "限流次数", Value: "0"},
			{Label: "上游错误", Value: "0"},
		},
		Events: nil,
		Items:  items,
		Summaries: []AuditSummary{
			{Title: "真实摘要", Content: "usage 日志为空，当前展示 fallback 审计日志。"},
		},
	}, nil
}

func (s postgresConsoleService) UsageOverview(ctx context.Context, query UsageQuery) (UsageOverviewData, error) {
	var err error
	query, err = normalizeUsageQuery(query, time.Now())
	if err != nil {
		return UsageOverviewData{}, err
	}
	return s.usageOverviewFromLogs(ctx, query)
}

func (s postgresConsoleService) UsageTrends(ctx context.Context, query UsageQuery) (UsageTrendData, error) {
	var err error
	query, err = normalizeUsageQuery(query, time.Now())
	if err != nil {
		return UsageTrendData{}, err
	}
	return s.usageTrendsFromLogs(ctx, query)
}

func (s postgresConsoleService) UsageLatencyWall(ctx context.Context, query UsageQuery) (UsageLatencyWallData, error) {
	var err error
	query, err = normalizeUsageQuery(query, time.Now())
	if err != nil {
		return UsageLatencyWallData{}, err
	}

	windowLabel, bucketExpr, bucketStep, bucketLayout := usageLatencyWindowConfig(query)
	whereClause, args := buildUsageLogWhere(query, "l.request_started_at")
	rows, err := s.db.Query(ctx, `
		select
			l.request_model,
			coalesce(pc.display_name, l.provider_credential_id),
			`+bucketExpr+` as bucket_start,
			count(*) as request_count,
			count(*) filter (where l.usage_status = 'success') as success_count,
			coalesce(round(avg(l.latency_ms)), 0)::bigint as avg_latency_ms
		from llm_request_logs l
		left join route_catalog r on r.id = l.route_id
		left join provider_credentials pc on pc.id = l.provider_credential_id
		where `+whereClause+`
		group by l.request_model, coalesce(pc.display_name, l.provider_credential_id), bucket_start
		order by max(l.request_started_at) desc, l.request_model asc, bucket_start asc;
	`, args...)
	if err != nil {
		return UsageLatencyWallData{}, err
	}
	defer rows.Close()

	type bucketStat struct {
		requestCount int64
		successCount int64
		avgLatencyMS int64
	}
	type laneAccumulator struct {
		model             string
		provider          string
		totalRequests     int64
		totalSuccess      int64
		totalLatencyTimes int64
		buckets           map[time.Time]bucketStat
	}

	lanesByKey := make(map[string]*laneAccumulator)
	for rows.Next() {
		var model string
		var provider string
		var bucketStart time.Time
		var requestCount int64
		var successCount int64
		var avgLatencyMS int64
		if err := rows.Scan(&model, &provider, &bucketStart, &requestCount, &successCount, &avgLatencyMS); err != nil {
			return UsageLatencyWallData{}, err
		}

		key := model + "::" + provider
		lane := lanesByKey[key]
		if lane == nil {
			lane = &laneAccumulator{
				model:    model,
				provider: provider,
				buckets:  make(map[time.Time]bucketStat),
			}
			lanesByKey[key] = lane
		}
		lane.totalRequests += requestCount
		lane.totalSuccess += successCount
		lane.totalLatencyTimes += avgLatencyMS * requestCount
		lane.buckets[bucketStart.UTC()] = bucketStat{
			requestCount: requestCount,
			successCount: successCount,
			avgLatencyMS: avgLatencyMS,
		}
	}
	if err := rows.Err(); err != nil {
		return UsageLatencyWallData{}, err
	}

	buckets := usageLatencyBuckets(query.From, query.To, bucketStep, bucketLayout)
	result := UsageLatencyWallData{
		WindowLabel: windowLabel,
		Buckets:     buckets.labels,
		Lanes:       make([]UsageLatencyLane, 0, len(lanesByKey)),
	}
	if len(lanesByKey) == 0 {
		return result, nil
	}

	laneList := make([]*laneAccumulator, 0, len(lanesByKey))
	for _, lane := range lanesByKey {
		laneList = append(laneList, lane)
	}
	sort.Slice(laneList, func(i, j int) bool {
		if laneList[i].totalRequests == laneList[j].totalRequests {
			return laneList[i].model < laneList[j].model
		}
		return laneList[i].totalRequests > laneList[j].totalRequests
	})

	for _, lane := range laneList {
		averageLatency := "0 ms"
		if lane.totalRequests > 0 {
			averageLatency = fmt.Sprintf("%d ms", int(math.Round(float64(lane.totalLatencyTimes)/float64(lane.totalRequests))))
		}

		cells := make([]UsageLatencyCell, 0, len(buckets.times))
		for index, bucketStart := range buckets.times {
			if bucket, ok := lane.buckets[bucketStart]; ok {
				status := "健康"
				if bucket.successCount < bucket.requestCount {
					status = "失败"
				}
				cells = append(cells, UsageLatencyCell{
					BucketLabel: buckets.labels[index],
					Latency:     fmt.Sprintf("%d ms", bucket.avgLatencyMS),
					Status:      status,
					Requests:    fmt.Sprintf("%d 次", bucket.requestCount),
				})
				continue
			}

			cells = append(cells, UsageLatencyCell{
				BucketLabel: buckets.labels[index],
				Latency:     "--",
				Status:      "空窗",
				Requests:    "0 次",
			})
		}

		result.Lanes = append(result.Lanes, UsageLatencyLane{
			Model:          lane.model,
			RouteLabel:     neutralizeConsoleRouteLabel(lane.provider),
			SuccessRate:    formatPercentage(successRateForCounts(lane.totalSuccess, lane.totalRequests)),
			AverageLatency: averageLatency,
			Cells:          cells,
		})
	}

	return result, nil
}

func (s postgresConsoleService) usageOverviewFromLogs(ctx context.Context, query UsageQuery) (UsageOverviewData, error) {
	logWhere, logArgs := buildUsageLogWhere(query, "l.request_started_at")
	var totalRequests int64
	var totalTokens int64
	var successRequests int64
	var estimatedRequests int64
	var averageLatency float64
	if err := s.db.QueryRow(ctx, `
		select
			count(*),
			coalesce(sum(l.total_tokens), 0),
			coalesce(sum(case when l.usage_status = 'success' then 1 else 0 end), 0),
			coalesce(sum(case when l.usage_source = 'estimated' then 1 else 0 end), 0),
			coalesce(avg(l.latency_ms), 0)
		from llm_request_logs l
		left join route_catalog r on r.id = l.route_id
		left join provider_credentials pc on pc.id = l.provider_credential_id
		where `+logWhere+`;
	`, logArgs...).Scan(&totalRequests, &totalTokens, &successRequests, &estimatedRequests, &averageLatency); err != nil {
		return UsageOverviewData{}, err
	}

	successRate := 0.0
	estimatedShare := 0.0
	if totalRequests > 0 {
		successRate = float64(successRequests) * 100 / float64(totalRequests)
		estimatedShare = float64(estimatedRequests) * 100 / float64(totalRequests)
	}

	return UsageOverviewData{
		TotalRequests:  totalRequests,
		SuccessRate:    formatPercentage(successRate),
		TotalTokens:    formatLargeNumber(int(totalTokens)),
		AverageLatency: fmt.Sprintf("%d ms", int(math.Round(averageLatency))),
		EstimatedShare: formatPercentage(estimatedShare),
	}, nil
}

func (s postgresConsoleService) usageTrendsFromLogs(ctx context.Context, query UsageQuery) (UsageTrendData, error) {
	whereClause, args := buildUsageLogWhere(query, "l.request_started_at")
	rows, err := s.db.Query(ctx, `
		select
			date_trunc('hour', l.request_started_at) as bucket_start,
			count(*) as request_count,
			coalesce(sum(l.total_tokens), 0) as total_tokens,
			coalesce(sum(case when l.usage_status = 'success' then 1 else 0 end), 0) as success_count
		from llm_request_logs l
		left join route_catalog r on r.id = l.route_id
		left join provider_credentials pc on pc.id = l.provider_credential_id
		where `+whereClause+`
		group by date_trunc('hour', l.request_started_at)
		order by bucket_start asc;
	`, args...)
	if err != nil {
		return UsageTrendData{}, err
	}
	defer rows.Close()

	data := UsageTrendData{
		Requests: make([]UsageTrendPoint, 0),
		Tokens:   make([]UsageTrendPoint, 0),
		Success:  make([]UsageTrendPoint, 0),
	}
	for rows.Next() {
		var bucketStart time.Time
		var requestCount int64
		var totalTokens int64
		var successCount int64
		if err := rows.Scan(&bucketStart, &requestCount, &totalTokens, &successCount); err != nil {
			return UsageTrendData{}, err
		}

		label := bucketStart.In(shanghaiLocation()).Format("01-02 15:04")
		successRate := 0.0
		if requestCount > 0 {
			successRate = float64(successCount) * 100 / float64(requestCount)
		}
		data.Requests = append(data.Requests, UsageTrendPoint{Label: label, Value: formatLargeNumber(int(requestCount))})
		data.Tokens = append(data.Tokens, UsageTrendPoint{Label: label, Value: formatLargeNumber(int(totalTokens))})
		data.Success = append(data.Success, UsageTrendPoint{Label: label, Value: formatPercentage(successRate)})
	}
	if err := rows.Err(); err != nil {
		return UsageTrendData{}, err
	}

	return data, nil
}

func (s postgresConsoleService) UsageFailures(ctx context.Context, query UsageQuery) (UsageFailureData, error) {
	var err error
	query, err = normalizeUsageQuery(query, time.Now())
	if err != nil {
		return UsageFailureData{}, err
	}

	whereClause, args := buildUsageLogWhere(query, "l.request_started_at")
	rows, err := s.db.Query(ctx, `
		select
			case
				when l.error_code <> '' then l.error_code
				else l.usage_status
			end as failure_category,
			count(*) as request_count
		from llm_request_logs l
		left join route_catalog r on r.id = l.route_id
		left join provider_credentials pc on pc.id = l.provider_credential_id
		where `+whereClause+`
		  and l.usage_status <> 'success'
		group by failure_category
		order by count(*) desc, failure_category asc;
	`, args...)
	if err != nil {
		return UsageFailureData{}, err
	}
	defer rows.Close()

	breakdown := make([]UsageFailureBucket, 0)
	breakdownCounts := make(map[string]int64)
	for rows.Next() {
		var failureCategory string
		var count int64
		if err := rows.Scan(&failureCategory, &count); err != nil {
			return UsageFailureData{}, err
		}
		label := translateUsageFailureCategory(failureCategory)
		breakdownCounts[label] += count
	}
	if err := rows.Err(); err != nil {
		return UsageFailureData{}, err
	}
	if len(breakdownCounts) > 0 {
		labels := make([]string, 0, len(breakdownCounts))
		for label := range breakdownCounts {
			labels = append(labels, label)
		}
		sort.Slice(labels, func(i, j int) bool {
			left := breakdownCounts[labels[i]]
			right := breakdownCounts[labels[j]]
			if left == right {
				return labels[i] < labels[j]
			}
			return left > right
		})
		for _, label := range labels {
			breakdown = append(breakdown, UsageFailureBucket{
				Label: label,
				Value: fmt.Sprintf("%d 次", breakdownCounts[label]),
			})
		}
	}

	eventWhere, eventArgs := buildUsageEventWhere(query)
	eventRows, err := s.db.Query(ctx, `
		select
			e.created_at,
			`+usageEventFailureCategoryExpr("l", "e")+` as failure_category,
			e.event_type,
			e.status_code,
			e.detail,
			l.request_model,
			coalesce(pc.display_name, l.provider_credential_id),
			l.latency_ms,
			e.usage_source
		from llm_request_events e
		join llm_request_logs l on l.id = e.request_log_id and l.tenant_id = e.tenant_id
		left join route_catalog r on r.id = l.route_id
		left join provider_credentials pc on pc.id = l.provider_credential_id
		where `+eventWhere+`
		  and (e.usage_status <> 'success' or e.event_type = 'usage_publish_failed')
		order by e.created_at desc
		limit 5;
	`, eventArgs...)
	if err != nil {
		return UsageFailureData{}, err
	}
	defer eventRows.Close()

	recentEvents := make([]string, 0)
	for eventRows.Next() {
		var createdAt time.Time
		var failureCategory string
		var eventType string
		var statusCode int
		var detail string
		var requestModel string
		var provider string
		var latencyMS int64
		var usageSource string
		if err := eventRows.Scan(&createdAt, &failureCategory, &eventType, &statusCode, &detail, &requestModel, &provider, &latencyMS, &usageSource); err != nil {
			return UsageFailureData{}, err
		}

		message := fmt.Sprintf(
			"%s · %s · %s",
			createdAt.In(shanghaiLocation()).Format("01-02 15:04"),
			translateUsageFailureCategory(failureCategory),
			describeUsageEvent(detail, eventType, requestModel, provider, latencyMS, statusCode, usageSource),
		)
		recentEvents = append(recentEvents, message)
	}
	if err := eventRows.Err(); err != nil {
		return UsageFailureData{}, err
	}

	return UsageFailureData{
		Breakdown:    breakdown,
		RecentEvents: recentEvents,
	}, nil
}

func (s postgresConsoleService) UsageRequests(ctx context.Context, query UsageQuery) (UsageRequestsPageData, error) {
	var err error
	query, err = normalizeUsageQuery(query, time.Now())
	if err != nil {
		return UsageRequestsPageData{}, err
	}

	whereClause, args := buildUsageLogWhere(query, "l.request_started_at")
	var total int64
	if err := s.db.QueryRow(ctx, `
		select count(*)
		from llm_request_logs l
		left join route_catalog r on r.id = l.route_id
		left join provider_credentials pc on pc.id = l.provider_credential_id
		where `+whereClause+`;
	`, args...).Scan(&total); err != nil {
		return UsageRequestsPageData{}, err
	}

	args = append(args, query.Limit, query.Offset)
	limitIndex := len(args) - 1
	offsetIndex := len(args)
	rows, err := s.db.Query(ctx, `
		select
			l.id,
			l.tenant_id,
			l.request_path,
			l.request_model,
			l.usage_status,
			l.total_tokens,
			l.latency_ms,
			l.first_token_latency_ms,
			l.usage_source
		from llm_request_logs l
		left join route_catalog r on r.id = l.route_id
		left join provider_credentials pc on pc.id = l.provider_credential_id
		where `+whereClause+`
		order by l.request_started_at desc, l.id desc
		limit $`+fmt.Sprintf("%d", limitIndex)+` offset $`+fmt.Sprintf("%d", offsetIndex)+`;
	`, args...)
	if err != nil {
		return UsageRequestsPageData{}, err
	}
	defer rows.Close()

	items := make([]UsageRequestItem, 0)
	for rows.Next() {
		var item UsageRequestItem
		var status string
		var totalTokens int
		var latencyMS int64
		var firstTokenLatencyMS int64
		var usageSource string
		if err := rows.Scan(&item.RequestID, &item.Tenant, &item.Endpoint, &item.Model, &status, &totalTokens, &latencyMS, &firstTokenLatencyMS, &usageSource); err != nil {
			return UsageRequestsPageData{}, err
		}
		item.Endpoint = neutralizeConsoleEndpoint(item.Endpoint)
		item.Status = translateUsageStatus(status)
		item.TotalTokens = formatLargeNumber(totalTokens)
		item.Latency = fmt.Sprintf("%d ms", latencyMS)
		item.FirstTokenLatencyMS = firstTokenLatencyMS
		item.UsageSource = translateUsageSource(usageSource)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return UsageRequestsPageData{}, err
	}

	return UsageRequestsPageData{
		Items:  items,
		Total:  total,
		Limit:  query.Limit,
		Offset: query.Offset,
	}, nil
}

func (s postgresConsoleService) collectTableRows(ctx context.Context, sql string) ([]TableRow, error) {
	rows, err := s.db.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TableRow, 0)
	for rows.Next() {
		rowValues, err := rows.Values()
		if err != nil {
			return nil, err
		}
		columns := make([]string, 0, len(rowValues))
		for _, value := range rowValues {
			columns = append(columns, fmt.Sprint(value))
		}
		items = append(items, TableRow{Columns: columns})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s postgresConsoleService) lookupSetting(ctx context.Context, key string, fallback string) string {
	var value string
	if err := s.db.QueryRow(ctx, `select value from system_settings where key = $1;`, key).Scan(&value); err != nil {
		return fallback
	}
	return value
}

func (s postgresConsoleService) lookupPlatformKeyName(ctx context.Context, keyID string) string {
	var name string
	if err := s.db.QueryRow(ctx, `select name from platform_api_keys where id = $1;`, keyID).Scan(&name); err != nil {
		return keyID
	}
	return name
}

func (s postgresConsoleService) insertPlaygroundRun(ctx context.Context, requestContext domain.RequestContext, model string, prompt string, endpoint string, responseText string, statusCode int, latencyMS int64) error {
	_, err := s.db.Exec(ctx, `
		insert into playground_runs (
			tenant_id,
			platform_api_key_id,
			requested_model,
			prompt,
			response_excerpt,
			endpoint,
			resolved_provider,
			status_code,
			latency_ms
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9);
	`, requestContext.TenantID, requestContext.PlatformAPIKeyID, model, prompt, truncateText(responseText, 240), endpoint, requestContext.SelectedProviderName, statusCode, latencyMS)
	return err
}

func emitPlaygroundStreamEvent(emit func([]byte) error, event string, payload any) error {
	if emit == nil {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return emit([]byte("event: " + event + "\ndata: " + string(body) + "\n\n"))
}

func emitPlaygroundTokenChunk(emit func([]byte) error, chunk []byte) error {
	if emit == nil {
		return nil
	}
	payload := strings.TrimSpace(string(chunk))
	if payload == "" || payload == "data: [DONE]" {
		return nil
	}
	if !strings.HasPrefix(payload, "data: ") {
		return nil
	}
	return emit([]byte("event: token\n" + payload + "\n\n"))
}

func extractPlaygroundResponseText(resp ChatResponse) string {
	if len(resp.Choices) == 0 {
		return ""
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content)
}

func (s postgresConsoleService) insertAuditLog(ctx context.Context, tenantID string, platformAPIKeyID string, model string, endpoint string, statusCode int, provider string, latencyMS int64) error {
	_, err := s.db.Exec(ctx, `
		insert into audit_logs (
			tenant_id,
			platform_api_key_id,
			requested_model,
			endpoint,
			status_code,
			provider_display_name,
			latency_ms
		) values ($1, $2, $3, $4, $5, $6, $7);
	`, tenantID, platformAPIKeyID, model, endpoint, statusCode, provider, latencyMS)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `update platform_api_keys set last_used_at = now() where id = $1;`, platformAPIKeyID)
	return err
}

func translateLifecycleStatus(status string, expiresAt time.Time, now time.Time) string {
	switch {
	case status == "disabled":
		return "停用"
	case !expiresAt.IsZero() && !now.Before(expiresAt):
		return "已过期"
	case !expiresAt.IsZero() && expiresAt.Sub(now) <= apiKeyExpiryWarningWindow:
		return "即将过期"
	default:
		return "启用"
	}
}

func translateRouteHealth(status string) string {
	switch status {
	case "healthy":
		return "健康"
	case "warning":
		return "告警"
	default:
		return "降级"
	}
}

func translateKnowledgeStatus(status string) string {
	switch status {
	case "ready":
		return "就绪"
	case "indexing":
		return "索引中"
	default:
		return "失败"
	}
}

func truncateText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func formatLargeNumber(value int) string {
	if value >= 10000 {
		return fmt.Sprintf("%.1f 万", float64(value)/10000.0)
	}
	return fmt.Sprintf("%d", value)
}

func formatPercentage(value float64) string {
	return fmt.Sprintf("%.2f%%", math.Round(value*100)/100)
}

func mapBool(condition bool, yes string, no string) string {
	if condition {
		return yes
	}
	return no
}

func shanghaiLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*60*60)
	}
	return location
}

type createPlatformAPIKeyInput struct {
	ID              string
	TenantID        string
	Name            string
	Scopes          []string
	RawKey          string
	CreatedByUserID string
}

func (s postgresConsoleService) insertPlatformAPIKey(ctx context.Context, input createPlatformAPIKeyInput) (APIKeyItem, error) {
	ciphertext, recoverable, err := s.secretService.Encrypt(input.RawKey)
	if err != nil {
		return APIKeyItem{}, err
	}

	var item APIKeyItem
	var status string
	var lastUsedAt time.Time
	var expiresAt time.Time
	if err := s.db.QueryRow(ctx, `
		insert into platform_api_keys (
			id,
			tenant_id,
			name,
			key_hash,
			key_ciphertext,
			key_kek_version,
			status,
			scopes,
			created_by_user_id,
			created_at,
			expires_at,
			secret_recoverable
		)
		values ($1, $2, $3, $4, $5, $6, 'active', $7, nullif($8, ''), now(), now() + interval '30 days', $9)
		returning id, name, tenant_id, status, scopes, created_at, expires_at;
	`, input.ID, input.TenantID, input.Name, hashPlatformAPIKey(input.RawKey), ciphertext, platformAPIKeyKEKVersionV1, input.Scopes, input.CreatedByUserID, recoverable).Scan(&item.ID, &item.Name, &item.Tenant, &status, &item.Scopes, &lastUsedAt, &expiresAt); err != nil {
		return APIKeyItem{}, mapAPIKeyMutationError(err, "tenant not found")
	}

	item.Status = translateLifecycleStatus(status, expiresAt, time.Now())
	item.LastUsedAt = lastUsedAt.In(shanghaiLocation()).Format(time.RFC3339)
	item.CreatedByUserID = input.CreatedByUserID
	item.ExpiresAt = expiresAt.In(shanghaiLocation()).Format(time.RFC3339)
	item.Revealable = recoverable
	item.LegacyUnrecoverable = !recoverable
	return item, nil
}

type applicationScanner interface {
	Scan(dest ...any) error
}

func scanApplicationItem(scanner applicationScanner) (ApplicationItem, error) {
	var item ApplicationItem
	var createdAt time.Time
	if err := scanner.Scan(
		&item.ID,
		&item.Email,
		&item.Name,
		&item.CompanyName,
		&item.UseCase,
		&item.Status,
		&createdAt,
	); err != nil {
		return ApplicationItem{}, err
	}

	item.CreatedAt = createdAt.In(shanghaiLocation()).Format(time.RFC3339)
	return item, nil
}

func (s postgresConsoleService) mapApproveApplicationPendingError(ctx context.Context, id string) error {
	var status string
	if err := s.db.QueryRow(ctx, `
		select status
		from account_applications
		where id = $1;
	`, id).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StatusError{
				Code:    http.StatusNotFound,
				Message: "application not found",
				Err:     err,
			}
		}
		return err
	}

	return StatusError{
		Code:    http.StatusConflict,
		Message: "application is not pending",
	}
}

func (s postgresConsoleService) mapRejectApplicationPendingError(ctx context.Context, id string) error {
	return s.mapApproveApplicationPendingError(ctx, id)
}

func mapApproveApplicationWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503":
			return StatusError{
				Code:    http.StatusBadRequest,
				Message: "actor_id or tenant_id not found",
				Err:     err,
			}
		case "23514":
			return StatusError{
				Code:    http.StatusBadRequest,
				Message: "actor_id must reference an admin user",
				Err:     err,
			}
		}
	}

	return err
}

func mapRejectApplicationWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503":
			return StatusError{
				Code:    http.StatusBadRequest,
				Message: "actor_id not found",
				Err:     err,
			}
		case "23514":
			return StatusError{
				Code:    http.StatusBadRequest,
				Message: "actor_id must reference an admin user",
				Err:     err,
			}
		}
	}

	return err
}

func mapAPIKeyMutationError(err error, notFoundMessage string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return StatusError{
			Code:    http.StatusNotFound,
			Message: notFoundMessage,
			Err:     err,
		}
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503":
			return StatusError{
				Code:    http.StatusBadRequest,
				Message: "tenant not found",
				Err:     err,
			}
		case "23505":
			return StatusError{
				Code:    http.StatusConflict,
				Message: "api key already exists",
				Err:     err,
			}
		}
	}

	return err
}

func newPlatformAPIKeyID() string {
	return "pak_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func newApplicationID() string {
	return "app_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func newUserID() string {
	return "user_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func newTenantMembershipID() string {
	return "tm_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func newAuditEventID() string {
	return "audit_evt_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func newAPIKeySecretAccessLogID() string {
	return "aksal_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func newPlatformAPIKeySecret() string {
	return "agw_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func sanitizeScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{"chat"}
	}
	return append([]string(nil), scopes...)
}

type usageWhereBuilder struct {
	conditions []string
	args       []any
}

func newUsageWhereBuilder(from time.Time, to time.Time, timeColumn string) *usageWhereBuilder {
	builder := &usageWhereBuilder{}
	builder.add(fmt.Sprintf("%s >= $%%d", timeColumn), from)
	builder.add(fmt.Sprintf("%s < $%%d", timeColumn), to)
	return builder
}

func (b *usageWhereBuilder) add(template string, value any) {
	b.args = append(b.args, value)
	b.conditions = append(b.conditions, fmt.Sprintf(template, len(b.args)))
}

func (b *usageWhereBuilder) addProvider(value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	b.args = append(b.args, strings.TrimSpace(value))
	index := len(b.args)
	b.conditions = append(b.conditions, fmt.Sprintf("(pc.provider = $%d or pc.display_name = $%d or r.resolved_provider = $%d)", index, index, index))
}

func (b *usageWhereBuilder) addLogModel(value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	b.args = append(b.args, strings.TrimSpace(value))
	index := len(b.args)
	b.conditions = append(b.conditions, fmt.Sprintf("(l.request_model = $%d or l.upstream_model = $%d or r.requested_model = $%d)", index, index, index))
}

func (b *usageWhereBuilder) addAggregateModel(value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	b.args = append(b.args, strings.TrimSpace(value))
	index := len(b.args)
	b.conditions = append(b.conditions, fmt.Sprintf("r.requested_model = $%d", index))
}

func (b *usageWhereBuilder) addFailureCategory(expr string, value string) {
	values := usageFailureFilterValues(value)
	if len(values) == 0 {
		return
	}

	placeholders := make([]string, 0, len(values))
	for _, item := range values {
		b.args = append(b.args, item)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(b.args)))
	}
	b.conditions = append(b.conditions, fmt.Sprintf("%s in (%s)", expr, strings.Join(placeholders, ", ")))
}

func (b *usageWhereBuilder) whereClause() string {
	return strings.Join(b.conditions, " and ")
}

func buildUsageAggregateWhere(query UsageQuery) (string, []any) {
	builder := newUsageWhereBuilder(query.From, query.To, "a.bucket_start")
	builder.addIfNotEmpty("a.tenant_id = $%d", query.TenantID)
	builder.addIfNotEmpty("a.platform_api_key_id = $%d", query.PlatformAPIKeyID)
	builder.addIfNotEmpty("a.route_id = $%d", query.RouteID)
	builder.addIfNotEmpty("a.request_path = $%d", query.RequestPath)
	builder.addIfNotEmpty("a.usage_status = $%d", query.Status)
	builder.addIfNotEmpty("a.usage_source = $%d", query.UsageSource)
	builder.addProvider(query.Provider)
	builder.addAggregateModel(query.Model)
	return builder.whereClause(), builder.args
}

func buildUsageLogWhere(query UsageQuery, timeColumn string) (string, []any) {
	builder := newUsageWhereBuilder(query.From, query.To, timeColumn)
	builder.addIfNotEmpty("l.tenant_id = $%d", query.TenantID)
	builder.addIfNotEmpty("l.platform_api_key_id = $%d", query.PlatformAPIKeyID)
	builder.addIfNotEmpty("l.route_id = $%d", query.RouteID)
	builder.addIfNotEmpty("l.request_path = $%d", query.RequestPath)
	builder.addIfNotEmpty("l.usage_status = $%d", query.Status)
	builder.addIfNotEmpty("l.usage_source = $%d", query.UsageSource)
	builder.addFailureCategory(usageFailureCategoryExpr("l"), query.ErrorCategory)
	builder.addProvider(query.Provider)
	builder.addLogModel(query.Model)
	return builder.whereClause(), builder.args
}

func buildUsageEventWhere(query UsageQuery) (string, []any) {
	builder := newUsageWhereBuilder(query.From, query.To, "l.request_started_at")
	builder.addIfNotEmpty("l.tenant_id = $%d", query.TenantID)
	builder.addIfNotEmpty("l.platform_api_key_id = $%d", query.PlatformAPIKeyID)
	builder.addIfNotEmpty("l.route_id = $%d", query.RouteID)
	builder.addIfNotEmpty("l.request_path = $%d", query.RequestPath)
	builder.addIfNotEmpty("e.usage_status = $%d", query.Status)
	builder.addIfNotEmpty("e.usage_source = $%d", query.UsageSource)
	builder.addFailureCategory(usageEventFailureCategoryExpr("l", "e"), query.ErrorCategory)
	builder.addProvider(query.Provider)
	builder.addLogModel(query.Model)
	return builder.whereClause(), builder.args
}

func (b *usageWhereBuilder) addIfNotEmpty(template string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	b.add(template, value)
}

func normalizeUsageQuery(query UsageQuery, now time.Time) (UsageQuery, error) {
	query.Window = strings.TrimSpace(query.Window)
	query.TenantID = strings.TrimSpace(query.TenantID)
	query.PlatformAPIKeyID = strings.TrimSpace(query.PlatformAPIKeyID)
	query.Provider = strings.TrimSpace(query.Provider)
	query.Model = strings.TrimSpace(query.Model)
	query.RouteID = strings.TrimSpace(query.RouteID)
	query.RequestPath = strings.TrimSpace(query.RequestPath)
	query.Status = strings.TrimSpace(query.Status)
	query.ErrorCategory = strings.TrimSpace(query.ErrorCategory)
	query.UsageSource = strings.TrimSpace(query.UsageSource)

	if query.To.IsZero() {
		query.To = now.UTC()
	} else {
		query.To = query.To.UTC()
	}
	if query.From.IsZero() {
		windowDuration, err := parseUsageWindow(query.Window)
		if err != nil {
			return UsageQuery{}, err
		}
		if windowDuration == 0 {
			windowDuration = 24 * time.Hour
		}
		query.From = query.To.Add(-windowDuration)
	} else {
		query.From = query.From.UTC()
	}
	if !query.To.After(query.From) {
		return UsageQuery{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "invalid time range",
			Err:     fmt.Errorf("usage query to must be after from"),
		}
	}
	if query.Limit <= 0 {
		query.Limit = 20
	}
	if query.Limit > 200 {
		query.Limit = 200
	}
	if query.Offset < 0 {
		query.Offset = 0
	}
	return query, nil
}

func parseUsageWindow(window string) (time.Duration, error) {
	switch strings.TrimSpace(window) {
	case "", "24h":
		return 24 * time.Hour, nil
	case "6h":
		return 6 * time.Hour, nil
	case "7d":
		return 7 * 24 * time.Hour, nil
	default:
		return 0, StatusError{
			Code:    http.StatusBadRequest,
			Message: "invalid usage window",
			Err:     fmt.Errorf("unsupported usage window %q", window),
		}
	}
}

func usageLatencyWindowConfig(query UsageQuery) (string, string, time.Duration, string) {
	duration := query.To.Sub(query.From)
	if duration > 48*time.Hour {
		return "最近 7 天", "date_trunc('day', l.request_started_at)", 24 * time.Hour, "01-02"
	}
	if strings.TrimSpace(query.Window) == "6h" {
		return "最近 6 小时", "date_trunc('hour', l.request_started_at)", time.Hour, "01-02 15:04"
	}
	return "最近 24 小时", "date_trunc('hour', l.request_started_at)", time.Hour, "01-02 15:04"
}

type usageLatencyBucketWindow struct {
	times  []time.Time
	labels []string
}

func usageLatencyBuckets(from time.Time, to time.Time, step time.Duration, labelLayout string) usageLatencyBucketWindow {
	start := floorUsageBucket(from.In(time.UTC), step)
	end := floorUsageBucket(to.In(time.UTC), step)

	times := make([]time.Time, 0)
	labels := make([]string, 0)
	for bucket := start; !bucket.After(end); bucket = bucket.Add(step) {
		times = append(times, bucket)
		labels = append(labels, bucket.In(shanghaiLocation()).Format(labelLayout))
	}
	return usageLatencyBucketWindow{times: times, labels: labels}
}

func floorUsageBucket(value time.Time, step time.Duration) time.Time {
	if step == 24*time.Hour {
		return value.Truncate(24 * time.Hour)
	}
	return value.Truncate(step)
}

func successRateForCounts(successCount int64, totalCount int64) float64 {
	if totalCount <= 0 {
		return 0
	}
	return float64(successCount) * 100 / float64(totalCount)
}

func translateUsageStatus(status string) string {
	switch status {
	case "success":
		return "成功"
	case "failed":
		return "失败"
	case "timeout":
		return "超时"
	case "rate_limited":
		return "限流"
	case "auth_failed":
		return "鉴权失败"
	case "upstream_error":
		return "上游错误"
	default:
		return status
	}
}

func neutralizeConsoleRouteLabel(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return ""
	}
	if strings.Contains(normalized, "backup") || strings.Contains(normalized, "fallback") || strings.Contains(normalized, "standby") || strings.Contains(normalized, "secondary") || strings.Contains(normalized, "备用") || strings.Contains(normalized, "回退") {
		return "backup-route"
	}
	if strings.Contains(normalized, "default") || strings.Contains(normalized, "primary") || strings.Contains(normalized, "main") || strings.Contains(normalized, "主") || strings.Contains(normalized, "默认") {
		return "default-route"
	}
	return "shared-route"
}

func neutralizeConsoleCredential(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "platform-managed-credential"
}

func neutralizeConsoleEndpoint(value string) string {
	switch strings.TrimSpace(value) {
	case "/v1/rag/query", "/v1/internal-search":
		return "/v1/internal-search"
	default:
		return value
	}
}

func neutralizeConsoleNarrative(value string) string {
	replacements := strings.NewReplacer(
		"provider_qwen_"+"primary", "platform-managed-credential",
		"provider_rag_"+"service", "platform-managed-credential",
		"/v1/rag/query", "/v1/internal-search",
		"知"+"识库", "内部检索能力",
		"RAG", "内部检索能力",
	)
	out := replacements.Replace(value)
	if strings.Contains(out, "OpenAI") || strings.Contains(out, "DashScope") || strings.Contains(out, "Anthropic") || strings.Contains(out, "Claude") || strings.Contains(out, "DeepSeek") || strings.Contains(out, "Gemini") {
		out = strings.ReplaceAll(out, "OpenAI"+" Primary", "default-route")
		out = strings.ReplaceAll(out, "DashScope"+" Primary", "default-route")
		out = strings.ReplaceAll(out, "DashScope 主路由", "default-route")
		out = strings.ReplaceAll(out, "OpenAI 主线路由", "default-route")
		out = strings.ReplaceAll(out, "OpenAI 备用线路", "backup-route")
	}
	return out
}

func translateUsageSource(source string) string {
	switch source {
	case "upstream":
		return "上游返回"
	case "estimated":
		return "估算"
	default:
		return source
	}
}

func translateUsageEventType(eventType string) string {
	switch eventType {
	case "response_received":
		return "LLM 已返回"
	case "request_failed":
		return "LLM 调用失败"
	case "usage_estimated":
		return "计量已估算"
	case "usage_publish_failed":
		return "计量事件投递失败"
	default:
		return eventType
	}
}

func describeUsageEvent(detail string, eventType string, requestModel string, provider string, latencyMS int64, statusCode int, usageSource string) string {
	model := strings.TrimSpace(requestModel)
	if model == "" {
		model = "当前模型"
	}
	provider = strings.TrimSpace(provider)

	switch eventType {
	case "response_received":
		sourceText := "上游已回传"
		if strings.TrimSpace(usageSource) == "estimated" {
			sourceText = "网关已估算 Token"
		}
		if provider != "" {
			return fmt.Sprintf("用户调用 %s，%s，平台线路 %s，耗时 %d ms。", model, sourceText, neutralizeConsoleRouteLabel(provider), latencyMS)
		}
		return fmt.Sprintf("用户调用 %s，%s，耗时 %d ms。", model, sourceText, latencyMS)
	case "usage_estimated":
		return fmt.Sprintf("用户调用 %s 已完成，当前 Token 由网关估算。", model)
	case "request_failed":
		reason := strings.TrimSpace(neutralizeConsoleNarrative(detail))
		if reason == "" || reason == "request failed" {
			reason = "请求失败"
		}
		if statusCode > 0 {
			return fmt.Sprintf("用户调用 %s 失败，状态码 %d，原因：%s。", model, statusCode, reason)
		}
		return fmt.Sprintf("用户调用 %s 失败，原因：%s。", model, reason)
	case "usage_publish_failed":
		reason := strings.TrimSpace(neutralizeConsoleNarrative(detail))
		if reason == "" {
			reason = "usage 事件写入失败"
		}
		return fmt.Sprintf("用户调用 %s 已完成，但计量事件投递失败：%s。", model, reason)
	default:
		if strings.TrimSpace(detail) != "" {
			return neutralizeConsoleNarrative(detail)
		}
		return translateUsageEventType(eventType)
	}
}

func translateUsageFailureCategory(category string) string {
	switch category {
	case "":
		return "未知错误"
	case "rate_limit", "rate_limited":
		return "限流"
	case "upstream_rate_limited":
		return "上游限流"
	case "bad_gateway", "upstream_server_error":
		return "上游服务异常"
	case "upstream_auth_failed":
		return "上游鉴权失败"
	case "upstream_timeout", "timeout":
		return "上游超时"
	case "network_error":
		return "网络异常"
	case "internal_error":
		return "网关内部错误"
	case "bad_request":
		return "请求参数错误"
	default:
		return translateUsageStatus(category)
	}
}

func usageFailureCategoryExpr(logAlias string) string {
	return fmt.Sprintf(
		"case when %s.error_code <> '' then %s.error_code else %s.usage_status end",
		logAlias,
		logAlias,
		logAlias,
	)
}

func usageEventFailureCategoryExpr(logAlias string, eventAlias string) string {
	return fmt.Sprintf(
		"case when %s.event_type = 'usage_publish_failed' then 'internal_error' else %s end",
		eventAlias,
		usageFailureCategoryExpr(logAlias),
	)
}

func usageFailureFilterValues(value string) []string {
	switch normalizeUsageFailureFilterKey(value) {
	case "rate_limited":
		return []string{"rate_limit", "rate_limited"}
	case "upstream_service_error":
		return []string{"bad_gateway", "upstream_server_error"}
	case "upstream_rate_limited":
		return []string{"upstream_rate_limited"}
	case "upstream_auth_failed":
		return []string{"upstream_auth_failed"}
	case "auth_failed":
		return []string{"auth_failed"}
	case "timeout":
		return []string{"timeout"}
	case "upstream_timeout":
		return []string{"timeout", "upstream_timeout"}
	case "upstream_error":
		return []string{"upstream_error"}
	case "network_error":
		return []string{"network_error"}
	case "internal_error":
		return []string{"internal_error"}
	case "bad_request":
		return []string{"bad_request"}
	case "failed":
		return []string{"failed"}
	default:
		value = strings.TrimSpace(value)
		if value == "" {
			return nil
		}
		return []string{value}
	}
}

func normalizeUsageFailureFilterKey(value string) string {
	switch strings.TrimSpace(value) {
	case "rate_limit", "rate_limited", "限流":
		return "rate_limited"
	case "bad_gateway", "upstream_server_error", "上游服务异常":
		return "upstream_service_error"
	case "upstream_rate_limited", "上游限流":
		return "upstream_rate_limited"
	case "upstream_auth_failed", "上游鉴权失败":
		return "upstream_auth_failed"
	case "auth_failed", "鉴权失败":
		return "auth_failed"
	case "timeout", "超时":
		return "timeout"
	case "upstream_timeout", "上游超时":
		return "upstream_timeout"
	case "upstream_error", "上游错误":
		return "upstream_error"
	case "network_error", "网络异常":
		return "network_error"
	case "internal_error", "网关内部错误":
		return "internal_error"
	case "bad_request", "请求参数错误":
		return "bad_request"
	case "failed", "失败":
		return "failed"
	default:
		return strings.TrimSpace(value)
	}
}
