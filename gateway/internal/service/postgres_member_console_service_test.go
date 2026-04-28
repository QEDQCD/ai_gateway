package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/ai_gateway/gateway/internal/secret"
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/jackc/pgx/v5"
)

func TestPostgresMemberConsoleServiceOverviewIsTenantScoped(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	member, conn := newUsageMemberConsoleService(t, ctx, service.ConsolePrincipal{
		UserID:   "user_member_a",
		Email:    "member-a@example.com",
		Role:     "member",
		TenantID: "tenant_demo",
	})

	if _, err := conn.Exec(ctx, `
		insert into tenants (id, name, status, request_quota_per_day)
		values ('tenant_other', 'Other Tenant', 'active', 999999);
	`); err != nil {
		t.Fatalf("seed other tenant failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into platform_api_keys (id, tenant_id, name, key_hash, status, scopes, created_at)
		values ('pak_other_tenant', 'tenant_other', 'other key', 'sha256:other-key', 'active', ARRAY['chat'], timestamptz '2026-04-24T10:00:00Z');
	`); err != nil {
		t.Fatalf("seed other tenant key failed: %v", err)
	}

	payload, err := member.Overview(ctx)
	if err != nil {
		t.Fatalf("Overview failed: %v", err)
	}

	if payload.TenantID != "tenant_demo" {
		t.Fatalf("expected tenant_id %q, got %q", "tenant_demo", payload.TenantID)
	}
	if payload.TenantName == "" {
		t.Fatal("expected tenant name to be populated")
	}
	if payload.TenantName != "Demo Tenant" {
		t.Fatalf("expected tenant name %q, got %q", "Demo Tenant", payload.TenantName)
	}
	if payload.ActiveAPIKeys != 1 {
		t.Fatalf("expected tenant-scoped active api keys %d, got %d", 1, payload.ActiveAPIKeys)
	}
}

func TestPostgresMemberConsoleServiceAPIKeysOnlyReturnsCreatorsKeys(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	member, conn := newUsageMemberConsoleService(t, ctx, service.ConsolePrincipal{
		UserID:   "user_member_a",
		Email:    "member-a@example.com",
		Role:     "member",
		TenantID: "tenant_demo",
	})

	if _, err := conn.Exec(ctx, `
		insert into platform_api_keys (id, tenant_id, name, key_hash, status, scopes, created_at)
		values
			('pak_member_a_extra', 'tenant_demo', 'member-a-extra', 'sha256:member-a-extra', 'active', ARRAY['chat'], timestamptz '2026-04-24T11:00:00Z'),
			('pak_member_b_only', 'tenant_demo', 'member-b-only', 'sha256:member-b-only', 'active', ARRAY['chat'], timestamptz '2026-04-24T12:00:00Z');
	`); err != nil {
		t.Fatalf("seed member api keys failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into audit_events (
			id,
			actor_type,
			actor_user_id,
			tenant_id,
			event_type,
			target_type,
			target_id,
			detail,
			created_at
		) values
			('audit_evt_member_a_extra', 'member', 'user_member_a', 'tenant_demo', 'api_key_created', 'platform_api_key', 'pak_member_a_extra', 'member a key', timestamptz '2026-04-24T11:00:01Z'),
			('audit_evt_member_b_only', 'member', 'user_member_b', 'tenant_demo', 'api_key_created', 'platform_api_key', 'pak_member_b_only', 'member b key', timestamptz '2026-04-24T12:00:01Z');
	`); err != nil {
		t.Fatalf("seed member api key audit events failed: %v", err)
	}

	payload, err := member.APIKeys(ctx)
	if err != nil {
		t.Fatalf("APIKeys failed: %v", err)
	}

	if len(payload.Items) != 2 {
		t.Fatalf("expected 2 member-owned api keys, got %d", len(payload.Items))
	}
	for _, item := range payload.Items {
		if item.OwnerUserID != "user_member_a" {
			t.Fatalf("expected owner_user_id %q, got %q for key %q", "user_member_a", item.OwnerUserID, item.ID)
		}
		if item.ID == "pak_member_b_only" {
			t.Fatalf("did not expect member-b key %q in response", item.ID)
		}
	}
}

func TestPostgresMemberConsoleServiceRevealAPIKeySecretReturnsOwnedSecret(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	member, _ := newUsageMemberConsoleService(t, ctx, service.ConsolePrincipal{
		UserID:   "user_member_a",
		Email:    "member-a@example.com",
		Role:     "member",
		TenantID: "tenant_demo",
	})

	created, err := member.CreateAPIKey(ctx, service.CreateAPIKeyRequest{
		Name:   "owned-key",
		Scopes: []string{"chat"},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	revealer, ok := member.(interface {
		RevealAPIKeySecret(context.Context, string) (service.APIKeySecretView, error)
	})
	if !ok {
		t.Fatal("expected member service to implement RevealAPIKeySecret")
	}

	secretView, err := revealer.RevealAPIKeySecret(ctx, created.Item.ID)
	if err != nil {
		t.Fatalf("RevealAPIKeySecret failed: %v", err)
	}
	if !secretView.Revealable {
		t.Fatal("expected Revealable to be true")
	}
	if secretView.FullKey == "" {
		t.Fatal("expected FullKey to be populated")
	}
	if secretView.MaskedKey == secretView.FullKey {
		t.Fatal("expected masked key to differ from full key")
	}
}

func TestPostgresMemberConsoleServiceCopyAPIKeySecretWritesAllowedAuditLog(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	member, conn := newUsageMemberConsoleService(t, ctx, service.ConsolePrincipal{
		UserID:   "user_member_a",
		Email:    "member-a@example.com",
		Role:     "member",
		TenantID: "tenant_demo",
	})

	created, err := member.CreateAPIKey(ctx, service.CreateAPIKeyRequest{
		Name:   "owned-copy-key",
		Scopes: []string{"chat"},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	copier, ok := member.(interface {
		CopyAPIKeySecret(context.Context, string, string, string) (service.APIKeySecretView, error)
	})
	if !ok {
		t.Fatal("expected member service to implement CopyAPIKeySecret")
	}

	secretView, err := copier.CopyAPIKeySecret(ctx, created.Item.ID, "198.51.100.25", "member-copy-test")
	if err != nil {
		t.Fatalf("CopyAPIKeySecret failed: %v", err)
	}
	if !secretView.Revealable {
		t.Fatal("expected copied key to be revealable")
	}
	if secretView.FullKey == "" {
		t.Fatal("expected FullKey to be populated")
	}

	var actorUserID string
	var actorRole string
	var action string
	var accessResult string
	if err := conn.QueryRow(ctx, `
		select actor_user_id, actor_role, action, access_result
		from api_key_secret_access_logs
		where api_key_id = $1
		order by created_at desc, id desc
		limit 1;
	`, created.Item.ID).Scan(&actorUserID, &actorRole, &action, &accessResult); err != nil {
		t.Fatalf("QueryRow api_key_secret_access_logs failed: %v", err)
	}
	if actorUserID != "user_member_a" {
		t.Fatalf("expected actor_user_id %q, got %q", "user_member_a", actorUserID)
	}
	if actorRole != "member" {
		t.Fatalf("expected actor_role %q, got %q", "member", actorRole)
	}
	if action != "copy" {
		t.Fatalf("expected action %q, got %q", "copy", action)
	}
	if accessResult != "allowed" {
		t.Fatalf("expected access_result %q, got %q", "allowed", accessResult)
	}
}

func TestPostgresMemberConsoleServiceCopyAPIKeySecretWritesDeniedAuditLogForUnownedKey(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	member, conn := newUsageMemberConsoleService(t, ctx, service.ConsolePrincipal{
		UserID:   "user_member_a",
		Email:    "member-a@example.com",
		Role:     "member",
		TenantID: "tenant_demo",
	})

	if _, err := conn.Exec(ctx, `
		insert into platform_api_keys (
			id, tenant_id, name, key_hash, status, scopes, created_at, expires_at, secret_recoverable
		) values (
			'pak_member_b_owned_copy_only',
			'tenant_demo',
			'member-b-owned',
			'sha256:member-b-owned',
			'active',
			ARRAY['chat'],
			now(),
			now() + interval '30 days',
			false
		);
	`); err != nil {
		t.Fatalf("seed member-b platform api key failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into audit_events (
			id,
			actor_type,
			actor_user_id,
			tenant_id,
			event_type,
			target_type,
			target_id,
			detail
		) values (
			'audit_evt_member_b_owned_copy_only',
			'member',
			'user_member_b',
			'tenant_demo',
			'api_key_created',
			'platform_api_key',
			'pak_member_b_owned_copy_only',
			'member b key'
		);
	`); err != nil {
		t.Fatalf("seed member-b audit event failed: %v", err)
	}

	copier, ok := member.(interface {
		CopyAPIKeySecret(context.Context, string, string, string) (service.APIKeySecretView, error)
	})
	if !ok {
		t.Fatal("expected member service to implement CopyAPIKeySecret")
	}

	_, err := copier.CopyAPIKeySecret(ctx, "pak_member_b_owned_copy_only", "198.51.100.26", "member-copy-denied-test")
	if err == nil {
		t.Fatal("expected CopyAPIKeySecret to reject unowned key")
	}

	var statusErr service.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Code != 404 {
		t.Fatalf("expected status %d, got %d", 404, statusErr.Code)
	}

	var actorUserID string
	var actorRole string
	var action string
	var accessResult string
	if err := conn.QueryRow(ctx, `
		select actor_user_id, actor_role, action, access_result
		from api_key_secret_access_logs
		where api_key_id = $1
		order by created_at desc, id desc
		limit 1;
	`, "pak_member_b_owned_copy_only").Scan(&actorUserID, &actorRole, &action, &accessResult); err != nil {
		t.Fatalf("QueryRow denied api_key_secret_access_logs failed: %v", err)
	}
	if actorUserID != "user_member_a" {
		t.Fatalf("expected actor_user_id %q, got %q", "user_member_a", actorUserID)
	}
	if actorRole != "member" {
		t.Fatalf("expected actor_role %q, got %q", "member", actorRole)
	}
	if action != "copy" {
		t.Fatalf("expected action %q, got %q", "copy", action)
	}
	if accessResult != "denied" {
		t.Fatalf("expected access_result %q, got %q", "denied", accessResult)
	}
}

func newUsageMemberConsoleService(t *testing.T, ctx context.Context, principal service.ConsolePrincipal) (service.MemberConsoleService, *pgx.Conn) {
	t.Helper()

	_, conn := newUsageConsoleService(t, ctx)
	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("secret.NewCodec failed: %v", err)
	}
	member := service.NewPostgresMemberConsoleService(conn, principal, codec)
	return member, conn
}
