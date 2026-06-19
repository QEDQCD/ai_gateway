package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/example/ai_gateway/gateway/internal/secret"
	"github.com/jackc/pgx/v5"
)

type postgresMemberConsoleService struct {
	db                 consoleDB
	principalOverride  ConsolePrincipal
	secretService      platformAPIKeySecretService
	pointsDivisorValue int64
}

func NewPostgresMemberConsoleService(db consoleDB, principalOverride ConsolePrincipal, pointsDivisor int64, secretCodecs ...*secret.Codec) MemberConsoleService {
	if db == nil {
		return NewUnavailableMemberConsoleService()
	}
	var secretCodec *secret.Codec
	if len(secretCodecs) > 0 {
		secretCodec = secretCodecs[0]
	}
	return postgresMemberConsoleService{
		db:                 db,
		principalOverride:  principalOverride,
		secretService:      newPlatformAPIKeySecretService(secretCodec),
		pointsDivisorValue: NormalizePointsDivisor(pointsDivisor),
	}
}

func (s postgresMemberConsoleService) Overview(ctx context.Context) (MemberOverviewPageData, error) {
	principal, err := s.resolvePrincipal(ctx)
	if err != nil {
		return MemberOverviewPageData{}, err
	}

	var payload MemberOverviewPageData
	if err := s.db.QueryRow(ctx, `
		with managed_keys as (
			select distinct target_id
			from audit_events
			where tenant_id = $1
			  and event_type = 'api_key_created'
			  and target_type = 'platform_api_key'
		)
		select
			t.id,
			t.name,
			coalesce(count(p.id) filter (where p.status = 'active' and p.id in (select target_id from managed_keys)), 0)
		from tenants t
		left join platform_api_keys p on p.tenant_id = t.id
		where t.id = $1
		group by t.id, t.name;
	`, principal.TenantID).Scan(&payload.TenantID, &payload.TenantName, &payload.ActiveAPIKeys); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MemberOverviewPageData{}, StatusError{
				Code:    http.StatusNotFound,
				Message: "tenant not found",
				Err:     err,
			}
		}
		return MemberOverviewPageData{}, err
	}

	quota, err := loadTenantQuotaSummary(ctx, s.db, payload.TenantID, time.Now())
	if err != nil {
		return MemberOverviewPageData{}, err
	}
	payload.Quota = quota

	return payload, nil
}

func (s postgresMemberConsoleService) APIKeys(ctx context.Context) (MemberAPIKeysPageData, error) {
	principal, err := s.resolvePrincipal(ctx)
	if err != nil {
		return MemberAPIKeysPageData{}, err
	}

	rows, err := s.db.Query(ctx, `
		with owned_keys as (
			select distinct on (e.target_id)
				e.target_id,
				e.actor_user_id
			from audit_events e
			where e.tenant_id = $1
			  and e.actor_user_id = $2
			  and e.event_type = 'api_key_created'
			  and e.target_type = 'platform_api_key'
			order by e.target_id, e.created_at asc, e.id asc
		)
		select
			p.id,
			p.name,
			p.tenant_id,
			p.status,
			p.scopes,
			coalesce(p.last_used_at, p.created_at),
			o.actor_user_id,
			coalesce(p.expires_at, p.created_at + interval '30 days'),
			p.secret_recoverable
		from platform_api_keys p
		join owned_keys o on o.target_id = p.id
		where p.tenant_id = $1
		order by p.created_at asc, p.id asc;
	`, principal.TenantID, principal.UserID)
	if err != nil {
		return MemberAPIKeysPageData{}, err
	}
	defer rows.Close()

	items := make([]MemberAPIKeyItem, 0)
	for rows.Next() {
		var item MemberAPIKeyItem
		var status string
		var lastUsedAt time.Time
		var expiresAt time.Time
		var secretRecoverable bool
		if err := rows.Scan(&item.ID, &item.Name, &item.Tenant, &status, &item.Scopes, &lastUsedAt, &item.OwnerUserID, &expiresAt, &secretRecoverable); err != nil {
			return MemberAPIKeysPageData{}, err
		}
		item.Status = translateLifecycleStatus(status, expiresAt, time.Now())
		item.LastUsedAt = lastUsedAt.In(shanghaiLocation()).Format(time.RFC3339)
		item.CreatedByUserID = item.OwnerUserID
		item.ExpiresAt = expiresAt.In(shanghaiLocation()).Format(time.RFC3339)
		item.Revealable = secretRecoverable
		item.LegacyUnrecoverable = !secretRecoverable
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return MemberAPIKeysPageData{}, err
	}

	return MemberAPIKeysPageData{Items: items}, nil
}

func (s postgresMemberConsoleService) CreateAPIKey(ctx context.Context, req CreateAPIKeyRequest) (APIKeyMutationResult, error) {
	principal, err := s.resolvePrincipal(ctx)
	if err != nil {
		return APIKeyMutationResult{}, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return APIKeyMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "name is required",
		}
	}
	if tenantID := strings.TrimSpace(req.TenantID); tenantID != "" && tenantID != principal.TenantID {
		return APIKeyMutationResult{}, StatusError{
			Code:    http.StatusForbidden,
			Message: "forbidden",
		}
	}

	rawKey := newPlatformAPIKeySecret()
	ciphertext, recoverable, err := s.secretService.Encrypt(rawKey)
	if err != nil {
		return APIKeyMutationResult{}, err
	}
	keyID := newPlatformAPIKeyID()
	var item APIKeyItem
	var status string
	var lastUsedAt time.Time
	var expiresAt time.Time
	if err := s.db.QueryRow(ctx, `
		with inserted as (
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
			values ($1, $2, $3, $4, $5, $6, 'active', $7, $8, now(), now() + interval '30 days', $9)
			returning id, name, tenant_id, status, scopes, created_at, expires_at
		),
		audited as (
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
				$10,
				'member',
				$11,
				$2,
				'api_key_created',
				'platform_api_key',
				id,
				$12
			from inserted
		)
		select id, name, tenant_id, status, scopes, created_at, expires_at
		from inserted;
	`, keyID, principal.TenantID, strings.TrimSpace(req.Name), hashPlatformAPIKey(rawKey), ciphertext, platformAPIKeyKEKVersionV1, sanitizeScopes(req.Scopes), principal.UserID, recoverable, newAuditEventID(), principal.UserID, "member created api key").Scan(&item.ID, &item.Name, &item.Tenant, &status, &item.Scopes, &lastUsedAt, &expiresAt); err != nil {
		return APIKeyMutationResult{}, mapAPIKeyMutationError(err, "tenant not found")
	}

	item.Status = translateLifecycleStatus(status, expiresAt, time.Now())
	item.LastUsedAt = lastUsedAt.In(shanghaiLocation()).Format(time.RFC3339)
	item.CreatedByUserID = principal.UserID
	item.ExpiresAt = expiresAt.In(shanghaiLocation()).Format(time.RFC3339)
	item.Revealable = recoverable
	item.LegacyUnrecoverable = !recoverable
	return APIKeyMutationResult{
		Item:   item,
		RawKey: rawKey,
	}, nil
}

func (s postgresMemberConsoleService) RotateAPIKey(ctx context.Context, id string, req RotateAPIKeyRequest) (APIKeyMutationResult, error) {
	principal, err := s.resolvePrincipal(ctx)
	if err != nil {
		return APIKeyMutationResult{}, err
	}
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
	newKeyID := newPlatformAPIKeyID()
	var item APIKeyItem
	var status string
	var lastUsedAt time.Time
	var expiresAt time.Time
	if err := s.db.QueryRow(ctx, `
		with owned as (
			select
				p.tenant_id,
				coalesce(p.created_by_user_id, $2) as created_by_user_id,
				case
					when $4 <> '' then $4
					else p.name
				end as next_name,
				case
					when coalesce(cardinality($5::text[]), 0) > 0 then $5::text[]
					else p.scopes
				end as next_scopes
			from platform_api_keys p
			where p.id = $1
			  and p.tenant_id = $3
			  and exists (
				select 1
				from audit_events e
				where e.target_id = p.id
				  and e.tenant_id = p.tenant_id
				  and e.actor_user_id = $2
				  and e.event_type = 'api_key_created'
				  and e.target_type = 'platform_api_key'
			  )
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
			select $6, tenant_id, next_name, $7, $8, $9, 'active', next_scopes, nullif(created_by_user_id, ''), now(), now() + interval '30 days', $1, $10
			from owned
			returning id, name, tenant_id, status, scopes, created_at, created_by_user_id, expires_at, secret_recoverable
		),
		disabled as (
			update platform_api_keys
			set status = 'disabled',
				disabled_at = now(),
				disabled_reason = 'rotated'
			where id = $1
			  and exists (select 1 from inserted)
		),
		audited as (
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
				$11,
				'member',
				$2,
				$3,
				'api_key_created',
				'platform_api_key',
				id,
				$12
			from inserted
		)
		select id, name, tenant_id, status, scopes, created_at, created_by_user_id, expires_at, secret_recoverable
		from inserted;
	`, strings.TrimSpace(id), principal.UserID, principal.TenantID, strings.TrimSpace(req.Name), req.Scopes, newKeyID, hashPlatformAPIKey(newRawKey), ciphertext, platformAPIKeyKEKVersionV1, recoverable, newAuditEventID(), "member rotated api key").Scan(&item.ID, &item.Name, &item.Tenant, &status, &item.Scopes, &lastUsedAt, &item.CreatedByUserID, &expiresAt, &item.Revealable); err != nil {
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

func (s postgresMemberConsoleService) DeactivateAPIKey(ctx context.Context, id string) (APIKeyMutationResult, error) {
	principal, err := s.resolvePrincipal(ctx)
	if err != nil {
		return APIKeyMutationResult{}, err
	}
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
		with updated as (
			update platform_api_keys p
			set status = 'disabled',
				disabled_at = now(),
				disabled_reason = 'deactivated'
			where p.id = $1
			  and p.tenant_id = $3
			  and exists (
				select 1
				from audit_events e
				where e.target_id = p.id
				  and e.tenant_id = p.tenant_id
				  and e.actor_user_id = $2
				  and e.event_type = 'api_key_created'
				  and e.target_type = 'platform_api_key'
			  )
			returning p.id, p.name, p.tenant_id, p.status, p.scopes, coalesce(p.last_used_at, p.created_at) as last_used_at, coalesce(p.created_by_user_id, ''), coalesce(p.expires_at, p.created_at + interval '30 days'), p.secret_recoverable
		),
		audited as (
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
				$4,
				'member',
				$2,
				$3,
				'api_key_deactivated',
				'platform_api_key',
				id,
				$5
			from updated
		)
		select id, name, tenant_id, status, scopes, last_used_at, coalesce(created_by_user_id, ''), expires_at, secret_recoverable
		from updated;
	`, strings.TrimSpace(id), principal.UserID, principal.TenantID, newAuditEventID(), "member deactivated api key").Scan(&item.ID, &item.Name, &item.Tenant, &status, &item.Scopes, &lastUsedAt, &item.CreatedByUserID, &expiresAt, &item.Revealable); err != nil {
		return APIKeyMutationResult{}, mapAPIKeyMutationError(err, "api key not found")
	}

	item.Status = translateLifecycleStatus(status, expiresAt, time.Now())
	item.LastUsedAt = lastUsedAt.In(shanghaiLocation()).Format(time.RFC3339)
	item.ExpiresAt = expiresAt.In(shanghaiLocation()).Format(time.RFC3339)
	item.LegacyUnrecoverable = !item.Revealable
	return APIKeyMutationResult{Item: item}, nil
}

func (s postgresMemberConsoleService) RevealAPIKeySecret(ctx context.Context, id string) (APIKeySecretView, error) {
	principal, err := s.resolvePrincipal(ctx)
	if err != nil {
		return APIKeySecretView{}, err
	}
	metadata := RequestAuditMetadataFromContext(ctx)

	record, owned, err := s.loadOwnedManagedAPIKeySecretRecord(ctx, id, principal)
	if err != nil {
		return APIKeySecretView{}, err
	}
	if !owned {
		if err := insertAPIKeySecretAccessLog(ctx, s.db, record.ID, record.TenantID, principal.UserID, "member", "reveal", "denied", metadata.IPAddress, metadata.UserAgent); err != nil {
			return APIKeySecretView{}, err
		}
		return APIKeySecretView{}, StatusError{
			Code:    http.StatusNotFound,
			Message: "api key not found",
		}
	}
	if err := insertAPIKeySecretAccessLog(ctx, s.db, record.ID, record.TenantID, principal.UserID, "member", "reveal", "allowed", metadata.IPAddress, metadata.UserAgent); err != nil {
		return APIKeySecretView{}, err
	}
	return buildAPIKeySecretSummaryView(record.ID, record.FullKey, record.Recoverable, record.ExpiresAt), nil
}

func (s postgresMemberConsoleService) CopyAPIKeySecret(ctx context.Context, id string, ip string, userAgent string) (APIKeySecretView, error) {
	principal, err := s.resolvePrincipal(ctx)
	if err != nil {
		return APIKeySecretView{}, err
	}

	record, owned, err := s.loadOwnedManagedAPIKeySecretRecord(ctx, id, principal)
	if err != nil {
		return APIKeySecretView{}, err
	}
	if !owned {
		if err := insertAPIKeySecretAccessLog(ctx, s.db, record.ID, record.TenantID, principal.UserID, "member", "copy", "denied", ip, userAgent); err != nil {
			return APIKeySecretView{}, err
		}
		return APIKeySecretView{}, StatusError{
			Code:    http.StatusNotFound,
			Message: "api key not found",
		}
	}
	if err := insertAPIKeySecretAccessLog(ctx, s.db, record.ID, record.TenantID, principal.UserID, "member", "copy", "allowed", ip, userAgent); err != nil {
		return APIKeySecretView{}, err
	}
	return buildAPIKeySecretCopyView(record.ID, record.FullKey, record.Recoverable, record.ExpiresAt), nil
}

func (s postgresMemberConsoleService) CreateAccountDeletionApplication(ctx context.Context, req CreateAccountDeletionApplicationRequest) (AccountDeletionApplicationMutationResult, error) {
	principal, err := s.resolvePrincipal(ctx)
	if err != nil {
		return AccountDeletionApplicationMutationResult{}, err
	}

	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return AccountDeletionApplicationMutationResult{}, StatusError{
			Code:    http.StatusBadRequest,
			Message: "reason is required",
		}
	}

	row := s.db.QueryRow(ctx, `
		with selected_user as (
			select id, email, name
			from users
			where id = $2
			  and role = 'member'
			  and status = 'active'
		),
		selected_membership as (
			select tenant_id
			from tenant_memberships
			where user_id = $2
			  and tenant_id = $3
			  and status = 'active'
		),
		inserted_application as (
			insert into account_deletion_applications (
				id,
				user_id,
				tenant_id,
				reason,
				status
			)
			select $1, selected_user.id, selected_membership.tenant_id, $4, 'pending'
			from selected_user
			join selected_membership on true
			returning id, user_id, tenant_id, reason, status, disabled_api_keys, created_at, reviewed_at
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
				$5,
				'member',
				user_id,
				tenant_id,
				'account_deletion_requested',
				'account_deletion_application',
				id,
				$4
			from inserted_application
		)
		select
			inserted_application.id,
			inserted_application.user_id,
			inserted_application.tenant_id,
			selected_user.email,
			selected_user.name,
			inserted_application.reason,
			inserted_application.status,
			inserted_application.disabled_api_keys,
			inserted_application.created_at,
			inserted_application.reviewed_at
		from inserted_application
		join selected_user on selected_user.id = inserted_application.user_id;
	`, newAccountDeletionApplicationID(), principal.UserID, principal.TenantID, reason, newAuditEventID())

	item, err := scanAccountDeletionApplicationItem(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AccountDeletionApplicationMutationResult{}, StatusError{
				Code:    http.StatusNotFound,
				Message: "active member not found",
				Err:     err,
			}
		}
		return AccountDeletionApplicationMutationResult{}, mapAccountDeletionApplicationWriteError(err)
	}

	return AccountDeletionApplicationMutationResult{Item: item}, nil
}

func (s postgresMemberConsoleService) consoleService() postgresConsoleService {
	return postgresConsoleService{
		db:                 s.db,
		pointsDivisorValue: s.pointsDivisorValue,
	}
}

func (s postgresMemberConsoleService) UsageOverview(ctx context.Context, query UsageQuery) (UsageOverviewData, error) {
	principal, err := s.resolvePrincipal(ctx)
	if err != nil {
		return UsageOverviewData{}, err
	}
	if err := s.ensureOwnedPlatformAPIKey(ctx, principal, query.PlatformAPIKeyID); err != nil {
		return UsageOverviewData{}, err
	}
	query.TenantID = principal.TenantID
	return s.consoleService().UsageOverview(ctx, query)
}

func (s postgresMemberConsoleService) UsageRequests(ctx context.Context, query UsageQuery) (UsageRequestsPageData, error) {
	principal, err := s.resolvePrincipal(ctx)
	if err != nil {
		return UsageRequestsPageData{}, err
	}
	if err := s.ensureOwnedPlatformAPIKey(ctx, principal, query.PlatformAPIKeyID); err != nil {
		return UsageRequestsPageData{}, err
	}
	query.TenantID = principal.TenantID
	return s.consoleService().UsageRequests(ctx, query)
}

func (s postgresMemberConsoleService) PointsOverview(ctx context.Context, query UsageQuery) (MemberPointsOverviewData, error) {
	principal, err := s.resolvePrincipal(ctx)
	if err != nil {
		return MemberPointsOverviewData{}, err
	}
	query.TenantID = principal.TenantID
	query.UserID = principal.UserID
	return s.consoleService().MemberPointsOverview(ctx, query)
}

func (s postgresMemberConsoleService) Failures(ctx context.Context, query UsageQuery) (MemberFailurePageData, error) {
	principal, err := s.resolvePrincipal(ctx)
	if err != nil {
		return MemberFailurePageData{}, err
	}
	if err := s.ensureOwnedPlatformAPIKey(ctx, principal, query.PlatformAPIKeyID); err != nil {
		return MemberFailurePageData{}, err
	}
	query.TenantID = principal.TenantID
	payload, err := s.consoleService().UsageFailures(ctx, query)
	if err != nil {
		return MemberFailurePageData{}, err
	}
	return MemberFailurePageData{
		Breakdown:    payload.Breakdown,
		RecentEvents: payload.RecentEvents,
	}, nil
}

func (s postgresMemberConsoleService) AuditEvents(ctx context.Context) (MemberAuditPageData, error) {
	principal, err := s.resolvePrincipal(ctx)
	if err != nil {
		return MemberAuditPageData{}, err
	}

	rows, err := s.db.Query(ctx, `
		select
			created_at,
			event_type,
			target_type,
			target_id,
			detail
		from audit_events
		where tenant_id = $1
		  and actor_user_id = $2
		order by created_at desc, id desc
		limit 20;
	`, principal.TenantID, principal.UserID)
	if err != nil {
		return MemberAuditPageData{}, err
	}
	defer rows.Close()

	items := make([]MemberAuditEventItem, 0)
	for rows.Next() {
		var item MemberAuditEventItem
		var createdAt time.Time
		if err := rows.Scan(&createdAt, &item.EventType, &item.TargetType, &item.TargetID, &item.Detail); err != nil {
			return MemberAuditPageData{}, err
		}
		item.Time = createdAt.In(shanghaiLocation()).Format(time.RFC3339)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return MemberAuditPageData{}, err
	}

	return MemberAuditPageData{Items: items}, nil
}

func (s postgresMemberConsoleService) resolvePrincipal(ctx context.Context) (ConsolePrincipal, error) {
	principal := s.principalOverride
	if principal.UserID == "" {
		var ok bool
		principal, ok = ConsolePrincipalFromContext(ctx)
		if !ok {
			return ConsolePrincipal{}, StatusError{
				Code:    http.StatusUnauthorized,
				Message: "console principal missing",
			}
		}
	}
	if principal.Role != "member" || strings.TrimSpace(principal.UserID) == "" || strings.TrimSpace(principal.TenantID) == "" {
		return ConsolePrincipal{}, StatusError{
			Code:    http.StatusUnauthorized,
			Message: "invalid console principal",
		}
	}
	return principal, nil
}

func (s postgresMemberConsoleService) ensureOwnedPlatformAPIKey(ctx context.Context, principal ConsolePrincipal, platformAPIKeyID string) error {
	platformAPIKeyID = strings.TrimSpace(platformAPIKeyID)
	if platformAPIKeyID == "" {
		return nil
	}

	var count int
	if err := s.db.QueryRow(ctx, `
		select count(*)
		from platform_api_keys p
		where p.id = $1
		  and p.tenant_id = $2
		  and exists (
			select 1
			from audit_events e
			where e.target_id = p.id
			  and e.tenant_id = p.tenant_id
			  and e.actor_user_id = $3
			  and e.event_type = 'api_key_created'
			  and e.target_type = 'platform_api_key'
		  );
	`, platformAPIKeyID, principal.TenantID, principal.UserID).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return StatusError{
			Code:    http.StatusNotFound,
			Message: "api key not found",
		}
	}
	return nil
}

func (s postgresMemberConsoleService) loadOwnedManagedAPIKeySecretRecord(ctx context.Context, id string, principal ConsolePrincipal) (managedAPIKeySecretRecord, bool, error) {
	var record managedAPIKeySecretRecord
	var ciphertext string
	var owned bool
	if err := s.db.QueryRow(ctx, `
		select
			p.id,
			p.tenant_id,
			p.key_ciphertext,
			p.secret_recoverable,
			coalesce(p.expires_at, p.created_at + interval '30 days'),
			exists (
				select 1
				from audit_events e
				where e.target_id = p.id
				  and e.tenant_id = p.tenant_id
				  and e.actor_user_id = $3
				  and e.event_type = 'api_key_created'
				  and e.target_type = 'platform_api_key'
			) as owned
		from platform_api_keys p
		where p.id = $1
		  and p.tenant_id = $2;
	`, strings.TrimSpace(id), principal.TenantID, principal.UserID).Scan(&record.ID, &record.TenantID, &ciphertext, &record.Recoverable, &record.ExpiresAt, &owned); err != nil {
		return managedAPIKeySecretRecord{}, false, mapAPIKeyMutationError(err, "api key not found")
	}
	if !owned {
		return record, false, nil
	}

	fullKey, err := s.secretService.Reveal(ciphertext, record.Recoverable)
	if err != nil {
		return managedAPIKeySecretRecord{}, false, err
	}
	record.FullKey = fullKey
	return record, true, nil
}
