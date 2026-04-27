package service_test

import (
	"context"
	"testing"
	"time"

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

func newUsageMemberConsoleService(t *testing.T, ctx context.Context, principal service.ConsolePrincipal) (service.MemberConsoleService, *pgx.Conn) {
	t.Helper()

	_, conn := newUsageConsoleService(t, ctx)
	member := service.NewPostgresMemberConsoleService(conn, principal)
	return member, conn
}
