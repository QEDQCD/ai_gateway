package store_test

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

	"github.com/example/ai_gateway/gateway/internal/domain"
	"github.com/example/ai_gateway/gateway/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestLookupPlatformAPIKeyByHash(t *testing.T) {
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

	if _, err := conn.Exec(ctx, `insert into tenants (id, name, status) values ('tenant_demo', 'Demo', 'active');`); err != nil {
		t.Fatalf("insert tenant failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into platform_api_keys (id, tenant_id, name, key_hash, status) values ('key_demo', 'tenant_demo', 'demo', 'sha256:demo', 'active');`); err != nil {
		t.Fatalf("insert platform_api_key failed: %v", err)
	}

	queries := store.New(conn)
	apiKey, err := queries.GetPlatformAPIKeyByHash(ctx, "sha256:demo")
	if err != nil {
		t.Fatalf("GetPlatformAPIKeyByHash failed: %v", err)
	}
	if apiKey.TenantID != "tenant_demo" {
		t.Fatalf("expected tenant_id %q, got %q", "tenant_demo", apiKey.TenantID)
	}
	if apiKey.Status != string(domain.StatusActive) {
		t.Fatalf("expected status %q, got %q", domain.StatusActive, apiKey.Status)
	}
}

func TestListActiveProviderCredentialsReturnsSupportedModelsInDeterministicOrder(t *testing.T) {
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

	if _, err := conn.Exec(ctx, `
		insert into provider_credentials (id, provider, display_name, supported_models, base_url, encrypted_secret, status)
		values
			('pc_b', 'openai', 'OpenAI Secondary', '{"gpt-4o"}', 'https://secondary.example/v1', 'enc-b', 'active'),
			('pc_a', 'openai', 'OpenAI Primary', '{"gpt-4o-mini","text-embedding-3-small"}', 'https://primary.example/v1', 'enc-a', 'active'),
			('pc_disabled', 'anthropic', 'Anthropic Disabled', '{"claude-3-5-sonnet"}', 'https://disabled.example/v1', 'enc-c', 'disabled');
	`); err != nil {
		t.Fatalf("insert provider_credentials failed: %v", err)
	}

	queries := store.New(conn)
	credentials, err := queries.ListActiveProviderCredentials(ctx)
	if err != nil {
		t.Fatalf("ListActiveProviderCredentials failed: %v", err)
	}
	if len(credentials) != 2 {
		t.Fatalf("expected 2 active credentials, got %d", len(credentials))
	}

	if credentials[0].ID != "pc_a" {
		t.Fatalf("expected first credential %q, got %q", "pc_a", credentials[0].ID)
	}
	if credentials[1].ID != "pc_b" {
		t.Fatalf("expected second credential %q, got %q", "pc_b", credentials[1].ID)
	}
	if got := strings.Join(credentials[0].SupportedModels, ","); got != "gpt-4o-mini,text-embedding-3-small" {
		t.Fatalf("expected supported models to round-trip, got %q", got)
	}
	if credentials[0].BaseUrl != "https://primary.example/v1" {
		t.Fatalf("expected base URL %q, got %q", "https://primary.example/v1", credentials[0].BaseUrl)
	}
	if got := strings.Join(credentials[1].SupportedModels, ","); got != "gpt-4o" {
		t.Fatalf("expected supported models to round-trip, got %q", got)
	}
	if credentials[1].BaseUrl != "https://secondary.example/v1" {
		t.Fatalf("expected base URL %q, got %q", "https://secondary.example/v1", credentials[1].BaseUrl)
	}
}

func TestListActiveProviderCredentialsReturnsSecretRefMode(t *testing.T) {
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

	if _, err := conn.Exec(ctx, `
		insert into provider_credentials (
			id, provider, display_name, supported_models, base_url,
			encrypted_secret, secret_ref, credential_mode, status
		) values (
			'provider_qwen', 'qwen', 'Qwen', '{"qwen-flash"}',
			'https://dashscope.aliyuncs.com/compatible-mode/v1',
			'', 'dashscope_api_key', 'secret_ref', 'active'
		);
	`); err != nil {
		t.Fatalf("seed provider failed: %v", err)
	}

	queries := store.New(conn)
	items, err := queries.ListActiveProviderCredentials(ctx)
	if err != nil {
		t.Fatalf("ListActiveProviderCredentials failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(items))
	}
	if items[0].SecretRef != "dashscope_api_key" {
		t.Fatalf("expected secret_ref dashscope_api_key, got %q", items[0].SecretRef)
	}
	if items[0].CredentialMode != "secret_ref" {
		t.Fatalf("expected credential_mode secret_ref, got %q", items[0].CredentialMode)
	}

	if _, err := conn.Exec(ctx, `
		insert into route_catalog (
			id, requested_model, resolved_provider, provider_credential_id,
			endpoint, health_status, request_mode
		) values (
			'route_qwen_flash', 'qwen-flash', 'qwen', 'provider_qwen',
			'/chat/completions', 'healthy', 'sync'
		);
	`); err != nil {
		t.Fatalf("seed route_catalog failed: %v", err)
	}

	var (
		status                   string
		healthcheckEnabled       bool
		healthcheckAssertionType string
		lastHealthCheckedAt      pgtype.Timestamptz
		lastHealthError          string
		firstTokenLatencyMs      int32
	)
	if err := conn.QueryRow(ctx, `
		select status, healthcheck_enabled, healthcheck_assertion_type, last_health_checked_at, last_health_error, first_token_latency_ms
		from route_catalog
		where id = 'route_qwen_flash'
	`).Scan(&status, &healthcheckEnabled, &healthcheckAssertionType, &lastHealthCheckedAt, &lastHealthError, &firstTokenLatencyMs); err != nil {
		t.Fatalf("query route_catalog defaults failed: %v", err)
	}
	if status != "active" {
		t.Fatalf("expected route_catalog status %q, got %q", "active", status)
	}
	if healthcheckEnabled {
		t.Fatal("expected route_catalog healthcheck_enabled to default false")
	}
	if healthcheckAssertionType != "non_empty" {
		t.Fatalf("expected route_catalog healthcheck_assertion_type %q, got %q", "non_empty", healthcheckAssertionType)
	}
	if lastHealthCheckedAt.Valid {
		t.Fatalf("expected route_catalog last_health_checked_at to default NULL, got %v", lastHealthCheckedAt.Time)
	}
	if lastHealthError != "" {
		t.Fatalf("expected route_catalog last_health_error %q, got %q", "", lastHealthError)
	}
	if firstTokenLatencyMs != 0 {
		t.Fatalf("expected route_catalog first_token_latency_ms %d, got %d", 0, firstTokenLatencyMs)
	}
}

func TestAuthRepositoryListActiveProviderCredentialsResolvesSecretRefAPIKey(t *testing.T) {
	t.Setenv("DASHSCOPE_API_KEY_TEST", "dashscope-real-secret")

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

	if _, err := conn.Exec(ctx, `
		insert into provider_credentials (
			id, provider, display_name, supported_models, base_url,
			encrypted_secret, secret_ref, credential_mode, status
		) values (
			'provider_qwen', 'qwen', 'Qwen', '{"qwen-flash"}',
			'https://dashscope.aliyuncs.com/compatible-mode/v1',
			'', 'DASHSCOPE_API_KEY_TEST', 'secret_ref', 'active'
		);
	`); err != nil {
		t.Fatalf("insert provider_credentials failed: %v", err)
	}

	repo := store.NewAuthRepository(store.New(conn))
	items, err := repo.ListActiveProviderCredentials(ctx)
	if err != nil {
		t.Fatalf("ListActiveProviderCredentials failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(items))
	}
	if items[0].APIKey != "dashscope-real-secret" {
		t.Fatalf("expected resolved API key %q, got %q", "dashscope-real-secret", items[0].APIKey)
	}
}

func TestAuthRepositoryListActiveProviderCredentialsResolvesSecretRefAPIKeyFromFile(t *testing.T) {
	tempDir := t.TempDir()
	secretPath := filepath.Join(tempDir, "dashscope.key")
	if err := os.WriteFile(secretPath, []byte("dashscope-file-secret\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile failed: %v", err)
	}

	t.Setenv("DASHSCOPE_API_KEY_FILE_TEST", "")
	t.Setenv("DASHSCOPE_API_KEY_FILE_TEST_FILE", secretPath)

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

	if _, err := conn.Exec(ctx, `
		insert into provider_credentials (
			id, provider, display_name, supported_models, base_url,
			encrypted_secret, secret_ref, credential_mode, status
		) values (
			'provider_qwen_file', 'qwen', 'Qwen File', '{"qwen-flash"}',
			'https://dashscope.aliyuncs.com/compatible-mode/v1',
			'', 'DASHSCOPE_API_KEY_FILE_TEST', 'secret_ref', 'active'
		);
	`); err != nil {
		t.Fatalf("insert provider_credentials failed: %v", err)
	}

	repo := store.NewAuthRepository(store.New(conn))
	items, err := repo.ListActiveProviderCredentials(ctx)
	if err != nil {
		t.Fatalf("ListActiveProviderCredentials failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(items))
	}
	if items[0].APIKey != "dashscope-file-secret" {
		t.Fatalf("expected resolved API key %q, got %q", "dashscope-file-secret", items[0].APIKey)
	}
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

	migrationsDir := filepath.Join(filepath.Dir(filename), "..", "..", "db", "migrations")
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
