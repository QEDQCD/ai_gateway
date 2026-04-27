package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/example/ai_gateway/gateway/internal/secret"
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/example/ai_gateway/gateway/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestSeedDemoDataEncryptsProviderSecrets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(ctx, t)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	for _, migration := range readMigrations(t) {
		if _, err := conn.Exec(ctx, migration); err != nil {
			t.Fatalf("conn.Exec migration failed: %v", err)
		}
	}

	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("secret.NewCodec failed: %v", err)
	}

	if err := SeedDemoData(ctx, conn, SeedConfig{
		PlatformAPIKey:      "platform-live-key",
		ProviderBaseURL:     "https://dashscope.aliyuncs.com/compatible-mode/v1",
		ProviderAPIKey:      "seed-provider-key",
		Provider:            "dashscope",
		ProviderDisplayName: "DashScope Primary",
		SecretCodec:         codec,
	}); err != nil {
		t.Fatalf("SeedDemoData failed: %v", err)
	}

	var encryptedSecret string
	if err := conn.QueryRow(ctx, `select encrypted_secret from provider_credentials where id = 'provider_dashscope_primary';`).Scan(&encryptedSecret); err != nil {
		t.Fatalf("QueryRow failed: %v", err)
	}
	if encryptedSecret == "seed-provider-key" {
		t.Fatal("expected encrypted_secret to be ciphertext")
	}
	if !strings.HasPrefix(encryptedSecret, secret.EncryptedSecretPrefix) {
		t.Fatalf("expected encrypted secret prefix %q, got %q", secret.EncryptedSecretPrefix, encryptedSecret)
	}

	queries := store.New(conn)
	repository := store.NewAuthRepository(queries, codec)
	credentials, err := repository.ListActiveProviderCredentials(ctx)
	if err != nil {
		t.Fatalf("ListActiveProviderCredentials failed: %v", err)
	}

	var providerSecret string
	for _, credential := range credentials {
		if credential.ID == "provider_dashscope_primary" {
			providerSecret = credential.APIKey
			break
		}
	}
	if providerSecret != "seed-provider-key" {
		t.Fatalf("expected decrypted provider key %q, got %q", "seed-provider-key", providerSecret)
	}
}

func TestSeedDemoDataAlignsProviderCredentialAndRouteSemantics(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(ctx, t)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	for _, migration := range readMigrations(t) {
		if _, err := conn.Exec(ctx, migration); err != nil {
			t.Fatalf("conn.Exec migration failed: %v", err)
		}
	}

	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("secret.NewCodec failed: %v", err)
	}

	cfg := SeedConfig{
		PlatformAPIKey:      "platform-live-key",
		ProviderBaseURL:     "https://api.openai.example/v1",
		ProviderAPIKey:      "seed-provider-key",
		Provider:            "openai",
		ProviderDisplayName: "OpenAI Primary",
		SecretCodec:         codec,
	}
	if err := SeedDemoData(ctx, conn, cfg); err != nil {
		t.Fatalf("SeedDemoData failed: %v", err)
	}

	wantCredentialID := "provider_openai_primary"
	wantChatRouteID := service.RouteIDForCredential(wantCredentialID, []string{"gpt-4o-mini", "text-embedding-3-small"}, "gpt-4o-mini")
	wantEmbeddingRouteID := service.RouteIDForCredential(wantCredentialID, []string{"gpt-4o-mini", "text-embedding-3-small"}, "text-embedding-3-small")

	var provider string
	var displayName string
	if err := conn.QueryRow(ctx, `
		select provider, display_name
		from provider_credentials
		where id = $1
	`, wantCredentialID).Scan(&provider, &displayName); err != nil {
		t.Fatalf("QueryRow provider_credentials failed: %v", err)
	}
	if provider != cfg.Provider {
		t.Fatalf("expected provider %q, got %q", cfg.Provider, provider)
	}
	if displayName != cfg.ProviderDisplayName {
		t.Fatalf("expected display_name %q, got %q", cfg.ProviderDisplayName, displayName)
	}

	rows, err := conn.Query(ctx, `
		select id, requested_model, resolved_provider, provider_credential_id
		from route_catalog
		where provider_credential_id = $1
		order by requested_model
	`, wantCredentialID)
	if err != nil {
		t.Fatalf("Query route_catalog failed: %v", err)
	}
	defer rows.Close()

	gotRoutes := map[string]string{}
	for rows.Next() {
		var id string
		var requestedModel string
		var resolvedProvider string
		var providerCredentialID string
		if err := rows.Scan(&id, &requestedModel, &resolvedProvider, &providerCredentialID); err != nil {
			t.Fatalf("rows.Scan failed: %v", err)
		}
		if providerCredentialID != wantCredentialID {
			t.Fatalf("expected provider_credential_id %q, got %q", wantCredentialID, providerCredentialID)
		}
		if resolvedProvider != cfg.ProviderDisplayName {
			t.Fatalf("expected resolved_provider %q, got %q", cfg.ProviderDisplayName, resolvedProvider)
		}
		gotRoutes[requestedModel] = id
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err failed: %v", err)
	}

	if gotRoutes["gpt-4o-mini"] != wantChatRouteID {
		t.Fatalf("expected chat route_id %q, got %q", wantChatRouteID, gotRoutes["gpt-4o-mini"])
	}
	if gotRoutes["text-embedding-3-small"] != wantEmbeddingRouteID {
		t.Fatalf("expected embedding route_id %q, got %q", wantEmbeddingRouteID, gotRoutes["text-embedding-3-small"])
	}
}

func TestRuntimeMigrationsCreateUsageObservabilityTablesWithDemoData(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(ctx, t)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	for _, migration := range readMigrations(t) {
		if _, err := conn.Exec(ctx, migration); err != nil {
			t.Fatalf("conn.Exec migration failed: %v", err)
		}
	}

	for _, statement := range RuntimeSeedStatements() {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("conn.Exec seed failed: %v", err)
		}
	}

	for _, tableName := range []string{"llm_request_logs", "llm_request_events", "llm_usage_agg_hourly"} {
		var found string
		if err := conn.QueryRow(ctx, `select coalesce(to_regclass($1)::text, '')`, tableName).Scan(&found); err != nil {
			t.Fatalf("QueryRow table existence failed: %v", err)
		}
		if found != tableName {
			t.Fatalf("expected table %q to exist, got %q", tableName, found)
		}
	}

	var seedCount int
	if err := conn.QueryRow(ctx, `select count(*) from llm_request_logs`).Scan(&seedCount); err != nil {
		t.Fatalf("QueryRow count failed: %v", err)
	}
	if seedCount < 2 {
		t.Fatalf("expected at least 2 llm_request_logs seed rows, got %d", seedCount)
	}

	var eventCount int
	if err := conn.QueryRow(ctx, `select count(*) from llm_request_events`).Scan(&eventCount); err != nil {
		t.Fatalf("QueryRow event count failed: %v", err)
	}
	if eventCount < 2 {
		t.Fatalf("expected at least 2 llm_request_events seed rows, got %d", eventCount)
	}

	var aggCount int
	if err := conn.QueryRow(ctx, `select count(*) from llm_usage_agg_hourly`).Scan(&aggCount); err != nil {
		t.Fatalf("QueryRow aggregate count failed: %v", err)
	}
	if aggCount < 1 {
		t.Fatalf("expected at least 1 llm_usage_agg_hourly seed row, got %d", aggCount)
	}

	var routeID string
	var platformAPIKeyName string
	var usageSource string
	var usageStatus string
	if err := conn.QueryRow(ctx, `
		select route_id, platform_api_key_name, usage_source, usage_status
		from llm_request_logs
		order by created_at asc
		limit 1
	`).Scan(&routeID, &platformAPIKeyName, &usageSource, &usageStatus); err != nil {
		t.Fatalf("QueryRow demo log failed: %v", err)
	}
	if routeID != "route:provider_openai_demo:default" {
		t.Fatalf("expected demo llm_request_logs row to use bootstrap route_id, got %q", routeID)
	}
	if platformAPIKeyName == "" {
		t.Fatal("expected demo llm_request_logs row to include platform_api_key_name")
	}
	if !containsString(service.AllUsageSources(), usageSource) {
		t.Fatalf("expected demo llm_request_logs row to use new usage source vocabulary, got %q", usageSource)
	}
	if !containsString(service.AllUsageStatuses(), usageStatus) {
		t.Fatalf("expected demo llm_request_logs row to use supported status vocabulary, got %q", usageStatus)
	}
}

func TestRuntimeMigrationsValidateUsageStatusAndSource(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(ctx, t)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	for _, migration := range readMigrations(t) {
		if _, err := conn.Exec(ctx, migration); err != nil {
			t.Fatalf("conn.Exec migration failed: %v", err)
		}
	}

	for _, statement := range RuntimeSeedStatements() {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("conn.Exec seed failed: %v", err)
		}
	}

	if _, err := conn.Exec(ctx, invalidUsageLogInsertSQL("unknown", "upstream")); err == nil {
		t.Fatal("expected invalid usage_status insert to fail")
	}

	if _, err := conn.Exec(ctx, invalidUsageLogInsertSQL("success", "unknown")); err == nil {
		t.Fatal("expected invalid usage_source insert to fail")
	}

	for _, status := range service.AllUsageStatuses() {
		if _, err := conn.Exec(ctx, validUsageLogInsertSQL("llm_demo_"+status, status, "upstream", "tenant_demo", "pak_demo")); err != nil {
			t.Fatalf("expected usage_status %q to be accepted: %v", status, err)
		}
	}

	for _, source := range service.AllUsageSources() {
		if _, err := conn.Exec(ctx, validUsageLogInsertSQL("llm_demo_"+source, "success", source, "tenant_demo", "pak_demo")); err != nil {
			t.Fatalf("expected usage_source %q to be accepted: %v", source, err)
		}
	}
}

func TestRuntimeMigrationsCreateUsageRequestStartedAtIndexes(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(ctx, t)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	for _, migration := range readMigrations(t) {
		if _, err := conn.Exec(ctx, migration); err != nil {
			t.Fatalf("conn.Exec migration failed: %v", err)
		}
	}

	for _, indexName := range []string{
		"idx_llm_request_logs_tenant_request_started_at",
		"idx_llm_request_logs_platform_api_key_request_started_at",
		"idx_llm_request_logs_provider_credential_request_started_at",
	} {
		var found string
		if err := conn.QueryRow(ctx, `select coalesce(to_regclass($1)::text, '')`, indexName).Scan(&found); err != nil {
			t.Fatalf("QueryRow index existence failed: %v", err)
		}
		if found != indexName {
			t.Fatalf("expected index %q to exist, got %q", indexName, found)
		}
	}
}

func TestUsageVocabularyRegression(t *testing.T) {
	t.Parallel()

	wantStatuses := []string{"success", "failed", "timeout", "rate_limited", "auth_failed", "upstream_error"}
	gotStatuses := service.AllUsageStatuses()
	if fmt.Sprintf("%q", gotStatuses) != fmt.Sprintf("%q", wantStatuses) {
		t.Fatalf("expected statuses %v, got %v", wantStatuses, gotStatuses)
	}

	wantSources := []string{"upstream", "estimated"}
	gotSources := service.AllUsageSources()
	if fmt.Sprintf("%q", gotSources) != fmt.Sprintf("%q", wantSources) {
		t.Fatalf("expected sources %v, got %v", wantSources, gotSources)
	}
}

func TestRuntimeMigrationsRejectCrossTenantUsageRows(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(ctx, t)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	for _, migration := range readMigrations(t) {
		if _, err := conn.Exec(ctx, migration); err != nil {
			t.Fatalf("conn.Exec migration failed: %v", err)
		}
	}
	for _, statement := range RuntimeSeedStatements() {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("conn.Exec seed failed: %v", err)
		}
	}
	if _, err := conn.Exec(ctx, `
		insert into tenants (id, name, status) values ('tenant_other', 'Other Tenant', 'active');
		insert into platform_api_keys (id, tenant_id, name, key_hash, status) values ('pak_other', 'tenant_other', 'other key', 'sha256:other', 'active');
	`); err != nil {
		t.Fatalf("conn.Exec cross-tenant fixture failed: %v", err)
	}

	if _, err := conn.Exec(ctx, validUsageLogInsertSQL("llm_cross_tenant_log", "success", "upstream", "tenant_demo", "pak_other")); err == nil {
		t.Fatal("expected cross-tenant llm_request_logs insert to fail")
	}

	if _, err := conn.Exec(ctx, `
		insert into llm_request_events (
			id, request_log_id, tenant_id, event_type, usage_source, usage_status, status_code, detail, created_at
		) values (
			'llmevt_cross_tenant',
			'llmreq_demo_001',
			'tenant_other',
			'response_received',
			'upstream',
			'success',
			200,
			'wrong tenant',
			timestamptz '2026-04-24T10:00:00Z'
		)
	`); err == nil {
		t.Fatal("expected cross-tenant llm_request_events insert to fail")
	}

	if _, err := conn.Exec(ctx, `
		insert into llm_usage_agg_hourly (
			bucket_start, tenant_id, platform_api_key_id, provider_credential_id, route_id, request_path, usage_source, usage_status, request_count, prompt_tokens, completion_tokens, total_tokens
		) values (
			timestamptz '2026-04-24T11:00:00Z',
			'tenant_demo',
			'pak_other',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'upstream',
			'success',
			1,
			10,
			2,
			12
		)
	`); err == nil {
		t.Fatal("expected cross-tenant llm_usage_agg_hourly insert to fail")
	}
}

func TestApplyMigrationsCreatesTenantGovernanceTables(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	conn := openTestPostgres(t, ctx)
	applyMigrations(t, ctx, conn)

	for _, tableName := range []string{
		"account_applications",
		"users",
		"tenant_memberships",
		"audit_events",
	} {
		assertTableExists(t, ctx, conn, tableName)
	}
}

func TestApplyMigrationsEnforceApplicationReviewIntegrity(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	conn := openTestPostgres(t, ctx)
	applyMigrations(t, ctx, conn)

	if _, err := conn.Exec(ctx, `
		insert into users (id, email, name, role, status)
		values ('user_reviewer', 'reviewer@example.com', 'Reviewer', 'admin', 'active')
	`); err != nil {
		t.Fatalf("insert reviewer failed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		insert into account_applications (
			id, email, name, company_name, use_case, status
		) values (
			'app_pending_ok', 'pending@example.com', 'Pending User', 'Demo Co', 'pending flow', 'pending'
		)
	`); err != nil {
		t.Fatalf("expected pending application without review metadata to succeed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		insert into account_applications (
			id, email, name, company_name, use_case, status, reviewer_id, review_comment, reviewed_at
		) values (
			'app_pending_reviewed', 'pending-reviewed@example.com', 'Pending Reviewed', 'Demo Co', 'bad pending state', 'pending', 'user_reviewer', 'should fail', timestamptz '2026-04-24T09:40:00Z'
		)
	`); err == nil {
		t.Fatal("expected pending application with review metadata to fail")
	}

	if _, err := conn.Exec(ctx, `
		insert into account_applications (
			id, email, name, company_name, use_case, status, review_comment, reviewed_at
		) values (
			'app_approved_missing_reviewer', 'approved-missing-reviewer@example.com', 'Approved Missing Reviewer', 'Demo Co', 'bad approved state', 'approved', 'missing reviewer', timestamptz '2026-04-24T09:41:00Z'
		)
	`); err == nil {
		t.Fatal("expected approved application without reviewer_id to fail")
	}

	if _, err := conn.Exec(ctx, `
		insert into account_applications (
			id, email, name, company_name, use_case, status, reviewer_id, review_comment
		) values (
			'app_rejected_missing_reviewed_at', 'rejected-missing-reviewed-at@example.com', 'Rejected Missing ReviewedAt', 'Demo Co', 'bad rejected state', 'rejected', 'user_reviewer', 'missing reviewed_at'
		)
	`); err == nil {
		t.Fatal("expected rejected application without reviewed_at to fail")
	}
}

func TestApplyMigrationsEnforceAuditEventActors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	conn := openTestPostgres(t, ctx)
	applyMigrations(t, ctx, conn)

	if _, err := conn.Exec(ctx, `
		insert into users (id, email, name, role, status)
		values ('user_actor_admin', 'actor-admin@example.com', 'Actor Admin', 'admin', 'active')
	`); err != nil {
		t.Fatalf("insert audit actor failed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		insert into audit_events (
			id, actor_type, actor_user_id, event_type, target_type, detail
		) values (
			'audit_system_ok', 'system', null, 'quota_warning', 'tenant', 'system event'
		)
	`); err != nil {
		t.Fatalf("expected system audit event without actor_user_id to succeed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		insert into audit_events (
			id, actor_type, actor_user_id, event_type, target_type, detail
		) values (
			'audit_admin_missing_actor', 'admin', null, 'application_approved', 'account_application', 'missing actor'
		)
	`); err == nil {
		t.Fatal("expected admin audit event without actor_user_id to fail")
	}

	if _, err := conn.Exec(ctx, `
		insert into audit_events (
			id, actor_type, actor_user_id, event_type, target_type, detail
		) values (
			'audit_member_unknown_actor', 'member', 'user_missing', 'api_key_created', 'platform_api_key', 'unknown actor'
		)
	`); err == nil {
		t.Fatal("expected member audit event with unknown actor_user_id to fail")
	}

	if _, err := conn.Exec(ctx, `
		insert into audit_events (
			id, actor_type, actor_user_id, event_type, target_type, detail
		) values (
			'audit_system_with_actor', 'system', 'user_actor_admin', 'quota_warning', 'tenant', 'system should not carry actor'
		)
	`); err == nil {
		t.Fatal("expected system audit event with actor_user_id to fail")
	}
}

func TestSeedDemoDataPopulatesTenantGovernanceDemoData(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	conn := openTestPostgres(t, ctx)
	applyMigrations(t, ctx, conn)

	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("secret.NewCodec failed: %v", err)
	}

	cfg := SeedConfig{
		PlatformAPIKey:      "platform-live-key",
		ProviderBaseURL:     "https://api.openai.example/v1",
		ProviderAPIKey:      "seed-provider-key",
		Provider:            "openai",
		ProviderDisplayName: "OpenAI Primary",
		SecretCodec:         codec,
	}
	if err := SeedDemoData(ctx, conn, cfg); err != nil {
		t.Fatalf("SeedDemoData failed: %v", err)
	}

	assertGovernanceSeedCounts(t, ctx, conn)
	assertApprovedApplicationAuditEvent(t, ctx, conn, "tenant_alpha", "app_alpha_approved", "user_admin_alpha", "seed approve")
}

func TestRuntimeSeedStatementsPopulateApprovalAndMembershipData(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	conn := openSeededRuntimeDB(t, ctx)

	assertTableCount(t, ctx, conn, "account_applications", 2)
	assertTableCount(t, ctx, conn, "users", 3)
	assertTableCount(t, ctx, conn, "tenant_memberships", 2)
	assertTableCount(t, ctx, conn, "audit_events", 3)
	assertApprovedApplicationAuditEvent(t, ctx, conn, "tenant_demo", "app_demo_approved", "user_admin_demo", "seed approve")
}

func TestRuntimeSeedRouteIDsAlignWithObservabilityRows(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(ctx, t)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	for _, migration := range readMigrations(t) {
		if _, err := conn.Exec(ctx, migration); err != nil {
			t.Fatalf("conn.Exec migration failed: %v", err)
		}
	}
	for _, statement := range RuntimeSeedStatements() {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("conn.Exec seed failed: %v", err)
		}
	}

	var logOrphans int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from llm_request_logs l
		left join route_catalog r on r.id = l.route_id
		where r.id is null
	`).Scan(&logOrphans); err != nil {
		t.Fatalf("QueryRow llm_request_logs orphan count failed: %v", err)
	}
	if logOrphans != 0 {
		t.Fatalf("expected llm_request_logs route_id values to match route_catalog ids, got %d orphans", logOrphans)
	}

	var aggOrphans int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from llm_usage_agg_hourly a
		left join route_catalog r on r.id = a.route_id
		where r.id is null
	`).Scan(&aggOrphans); err != nil {
		t.Fatalf("QueryRow llm_usage_agg_hourly orphan count failed: %v", err)
	}
	if aggOrphans != 0 {
		t.Fatalf("expected llm_usage_agg_hourly route_id values to match route_catalog ids, got %d orphans", aggOrphans)
	}
}

func TestRuntimeSeedStatementsAreIdempotent(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(ctx, t)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	for _, migration := range readMigrations(t) {
		if _, err := conn.Exec(ctx, migration); err != nil {
			t.Fatalf("conn.Exec migration failed: %v", err)
		}
	}

	for run := 1; run <= 2; run++ {
		for _, statement := range RuntimeSeedStatements() {
			if _, err := conn.Exec(ctx, statement); err != nil {
				t.Fatalf("conn.Exec seed run %d failed: %v", run, err)
			}
		}
	}

	assertTableCount(t, ctx, conn, "llm_request_logs", 2)
	assertTableCount(t, ctx, conn, "llm_request_events", 2)
	assertTableCount(t, ctx, conn, "llm_usage_agg_hourly", 2)
}

func invalidUsageLogInsertSQL(status string, source string) string {
	return validUsageLogInsertSQL("llm_demo_invalid", status, source, "tenant_demo", "pak_demo")
}

func validUsageLogInsertSQL(id string, status string, source string, tenantID string, platformAPIKeyID string) string {
	return fmt.Sprintf(`
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			request_started_at,
			request_completed_at
		) values (
			'%s',
			'%s',
			'%s',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'%s',
			'%s',
			200,
			120,
			10,
			20,
			30,
			timestamptz '2026-04-24T10:00:00Z',
			timestamptz '2026-04-24T10:00:01Z'
		)
	`, id, tenantID, platformAPIKeyID, source, status)
}

func openTestPostgres(t *testing.T, ctx context.Context) *pgx.Conn {
	t.Helper()

	container, dsn := startPostgresContainer(ctx, t)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	return conn
}

func applyMigrations(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()

	for _, migration := range readMigrations(t) {
		if _, err := conn.Exec(ctx, migration); err != nil {
			t.Fatalf("conn.Exec migration failed: %v", err)
		}
	}
}

func openSeededRuntimeDB(t *testing.T, ctx context.Context) *pgx.Conn {
	t.Helper()

	conn := openTestPostgres(t, ctx)
	applyMigrations(t, ctx, conn)

	for _, statement := range RuntimeSeedStatements() {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("conn.Exec seed failed: %v", err)
		}
	}

	return conn
}

func assertTableExists(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string) {
	t.Helper()

	var found string
	if err := conn.QueryRow(ctx, `select coalesce(to_regclass($1)::text, '')`, tableName).Scan(&found); err != nil {
		t.Fatalf("QueryRow table existence failed: %v", err)
	}
	if found != tableName {
		t.Fatalf("expected table %q to exist, got %q", tableName, found)
	}
}

func assertTableCount(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string, want int) {
	t.Helper()

	var got int
	if err := conn.QueryRow(ctx, `select count(*) from `+tableName).Scan(&got); err != nil {
		t.Fatalf("QueryRow %s count failed: %v", tableName, err)
	}
	if got != want {
		t.Fatalf("expected %s count %d, got %d", tableName, want, got)
	}
}

func assertGovernanceSeedCounts(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()

	assertTableCount(t, ctx, conn, "account_applications", 2)
	assertTableCount(t, ctx, conn, "users", 3)
	assertTableCount(t, ctx, conn, "tenant_memberships", 2)
	assertTableCount(t, ctx, conn, "audit_events", 3)
}

func assertApprovedApplicationAuditEvent(t *testing.T, ctx context.Context, conn *pgx.Conn, wantTenantID string, wantApplicationID string, wantReviewerID string, wantDetail string) {
	t.Helper()

	var matchingEvents int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from audit_events
		where actor_type = 'admin'
			and event_type = 'application_approved'
			and target_type = 'account_application'
			and target_id = $1
			and coalesce(tenant_id, '') = $2
			and actor_user_id = $3
			and detail = $4
	`, wantApplicationID, wantTenantID, wantReviewerID, wantDetail).Scan(&matchingEvents); err != nil {
		t.Fatalf("QueryRow approved audit event count failed: %v", err)
	}
	if matchingEvents != 1 {
		t.Fatalf("expected exactly 1 matching approved audit event, got %d", matchingEvents)
	}

	var status string
	var reviewerID string
	var reviewComment string
	var reviewedAt time.Time
	if err := conn.QueryRow(ctx, `
		select
			status,
			reviewer_id,
			review_comment,
			reviewed_at
		from account_applications
		where id = $1
	`, wantApplicationID).Scan(&status, &reviewerID, &reviewComment, &reviewedAt); err != nil {
		t.Fatalf("QueryRow approved application failed: %v", err)
	}
	if status != "approved" {
		t.Fatalf("expected approved application status %q, got %q", "approved", status)
	}
	if reviewerID != wantReviewerID {
		t.Fatalf("expected approved application reviewer_id %q, got %q", wantReviewerID, reviewerID)
	}
	if reviewComment != wantDetail {
		t.Fatalf("expected approved application review_comment %q, got %q", wantDetail, reviewComment)
	}
	if reviewedAt.IsZero() {
		t.Fatal("expected approved application reviewed_at to be set")
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func startPostgresContainer(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
	t.Helper()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       "gateway_test",
				"POSTGRES_USER":     "postgres",
				"POSTGRES_PASSWORD": "postgres",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("GenericContainer failed: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container.Host failed: %v", err)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("container.MappedPort failed: %v", err)
	}

	dsn := fmt.Sprintf("postgres://postgres:postgres@%s:%s/gateway_test?sslmode=disable", host, port.Port())
	return container, dsn
}

func readMigrations(t *testing.T) []string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	migrationsDir := filepath.Join(filepath.Dir(filename), "migrations")
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("os.ReadDir failed: %v", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	migrations := make([]string, 0, len(names))
	for _, name := range names {
		sqlBytes, err := os.ReadFile(filepath.Join(migrationsDir, name))
		if err != nil {
			t.Fatalf("os.ReadFile failed: %v", err)
		}
		migrations = append(migrations, string(sqlBytes))
	}

	return migrations
}
