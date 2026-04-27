package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type postgresMemberConsoleService struct {
	db                consoleDB
	principalOverride ConsolePrincipal
}

func NewPostgresMemberConsoleService(db consoleDB, principalOverride ConsolePrincipal) MemberConsoleService {
	if db == nil {
		return NewUnavailableMemberConsoleService()
	}
	return postgresMemberConsoleService{
		db:                db,
		principalOverride: principalOverride,
	}
}

func (s postgresMemberConsoleService) Overview(ctx context.Context) (MemberOverviewPageData, error) {
	principal, err := s.resolvePrincipal(ctx)
	if err != nil {
		return MemberOverviewPageData{}, err
	}

	var payload MemberOverviewPageData
	if err := s.db.QueryRow(ctx, `
		select
			t.id,
			t.name,
			coalesce(count(p.id) filter (where p.status = 'active'), 0)
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
			o.actor_user_id
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
		if err := rows.Scan(&item.ID, &item.Name, &item.Tenant, &status, &item.Scopes, &lastUsedAt, &item.OwnerUserID); err != nil {
			return MemberAPIKeysPageData{}, err
		}
		item.Status = translateLifecycleStatus(status)
		item.LastUsedAt = lastUsedAt.In(shanghaiLocation()).Format(time.RFC3339)
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
	keyID := newPlatformAPIKeyID()
	var item APIKeyItem
	var status string
	var lastUsedAt time.Time
	if err := s.db.QueryRow(ctx, `
		with inserted as (
			insert into platform_api_keys (id, tenant_id, name, key_hash, status, scopes, created_at)
			values ($1, $2, $3, $4, 'active', $5, now())
			returning id, name, tenant_id, status, scopes, created_at
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
				$6,
				'member',
				$7,
				$2,
				'api_key_created',
				'platform_api_key',
				id,
				$8
			from inserted
		)
		select id, name, tenant_id, status, scopes, created_at
		from inserted;
	`, keyID, principal.TenantID, strings.TrimSpace(req.Name), hashPlatformAPIKey(rawKey), sanitizeScopes(req.Scopes), newAuditEventID(), principal.UserID, "member created api key").Scan(&item.ID, &item.Name, &item.Tenant, &status, &item.Scopes, &lastUsedAt); err != nil {
		return APIKeyMutationResult{}, mapAPIKeyMutationError(err, "tenant not found")
	}

	item.Status = translateLifecycleStatus(status)
	item.LastUsedAt = lastUsedAt.In(shanghaiLocation()).Format(time.RFC3339)
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
	newKeyID := newPlatformAPIKeyID()
	var item APIKeyItem
	var status string
	var lastUsedAt time.Time
	if err := s.db.QueryRow(ctx, `
		with owned as (
			select
				p.tenant_id,
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
			insert into platform_api_keys (id, tenant_id, name, key_hash, status, scopes, created_at)
			select $6, tenant_id, next_name, $7, 'active', next_scopes, now()
			from owned
			returning id, name, tenant_id, status, scopes, created_at
		),
		disabled as (
			update platform_api_keys
			set status = 'disabled'
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
				$8,
				'member',
				$2,
				$3,
				'api_key_created',
				'platform_api_key',
				id,
				$9
			from inserted
		)
		select id, name, tenant_id, status, scopes, created_at
		from inserted;
	`, strings.TrimSpace(id), principal.UserID, principal.TenantID, strings.TrimSpace(req.Name), req.Scopes, newKeyID, hashPlatformAPIKey(newRawKey), newAuditEventID(), "member rotated api key").Scan(&item.ID, &item.Name, &item.Tenant, &status, &item.Scopes, &lastUsedAt); err != nil {
		return APIKeyMutationResult{}, mapAPIKeyMutationError(err, "api key not found")
	}

	item.Status = translateLifecycleStatus(status)
	item.LastUsedAt = lastUsedAt.In(shanghaiLocation()).Format(time.RFC3339)
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
	if err := s.db.QueryRow(ctx, `
		with updated as (
			update platform_api_keys p
			set status = 'disabled'
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
			returning p.id, p.name, p.tenant_id, p.status, p.scopes, coalesce(p.last_used_at, p.created_at) as last_used_at
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
		select id, name, tenant_id, status, scopes, last_used_at
		from updated;
	`, strings.TrimSpace(id), principal.UserID, principal.TenantID, newAuditEventID(), "member deactivated api key").Scan(&item.ID, &item.Name, &item.Tenant, &status, &item.Scopes, &lastUsedAt); err != nil {
		return APIKeyMutationResult{}, mapAPIKeyMutationError(err, "api key not found")
	}

	item.Status = translateLifecycleStatus(status)
	item.LastUsedAt = lastUsedAt.In(shanghaiLocation()).Format(time.RFC3339)
	return APIKeyMutationResult{Item: item}, nil
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
	return (postgresConsoleService{db: s.db}).UsageOverview(ctx, query)
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
	return (postgresConsoleService{db: s.db}).UsageRequests(ctx, query)
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
	payload, err := (postgresConsoleService{db: s.db}).UsageFailures(ctx, query)
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
