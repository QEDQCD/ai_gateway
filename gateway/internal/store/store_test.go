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

	"github.com/jackc/pgx/v5"
	"github.com/liwenjian/ai_gateway/gateway/internal/domain"
	"github.com/liwenjian/ai_gateway/gateway/internal/store"
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
		insert into provider_credentials (id, provider, display_name, supported_models, encrypted_secret, status)
		values
			('pc_b', 'openai', 'OpenAI Secondary', '{"gpt-4o"}', 'enc-b', 'active'),
			('pc_a', 'openai', 'OpenAI Primary', '{"gpt-4o-mini","text-embedding-3-small"}', 'enc-a', 'active'),
			('pc_disabled', 'anthropic', 'Anthropic Disabled', '{"claude-3-5-sonnet"}', 'enc-c', 'disabled');
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
	if got := strings.Join(credentials[1].SupportedModels, ","); got != "gpt-4o" {
		t.Fatalf("expected supported models to round-trip, got %q", got)
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
