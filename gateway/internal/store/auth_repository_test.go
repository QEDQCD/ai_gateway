package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/example/ai_gateway/gateway/internal/domain"
	"github.com/example/ai_gateway/gateway/internal/secret"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestFindPlatformAPIKeyByHashReturnsCreatorUserAndTenantScope(t *testing.T) {
	t.Parallel()

	repo := newSeededAuthRepository(t)
	record, err := repo.FindPlatformAPIKeyByHash(context.Background(), hashPlatformAPIKey("agw_demo_key"))
	if err != nil {
		t.Fatalf("FindPlatformAPIKeyByHash failed: %v", err)
	}

	if record.UserID != "user_demo" {
		t.Fatalf("expected user_demo, got %q", record.UserID)
	}
	if record.TenantID != "tenant_demo" {
		t.Fatalf("expected tenant_demo, got %q", record.TenantID)
	}
}

func TestSQLAuthRepositoryFindPlatformAPIKeyByHashSafelyBackfillsUniqueMembershipUserID(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startAuthRepositoryPostgresContainer(ctx, t)
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

	for _, migration := range readAuthRepositoryMigrations(t) {
		if _, err := conn.Exec(ctx, migration); err != nil {
			t.Fatalf("conn.Exec migration failed: %v", err)
		}
	}

	if _, err := conn.Exec(ctx, `
		insert into tenants (id, name, status) values ('tenant_demo', 'Demo Tenant', 'active');
		insert into users (id, email, name, role, status) values ('user_demo', 'member@example.com', 'Demo Member', 'member', 'active');
		insert into tenant_memberships (id, tenant_id, user_id, role, status)
		values ('tm_demo', 'tenant_demo', 'user_demo', 'member', 'active');
		insert into platform_api_keys (id, tenant_id, name, key_hash, status)
		values ('pak_demo', 'tenant_demo', 'demo', 'sha256:demo', 'active');
	`); err != nil {
		t.Fatalf("seed auth records failed: %v", err)
	}

	repo := NewAuthRepository(New(conn))
	record, err := repo.FindPlatformAPIKeyByHash(ctx, "sha256:demo")
	if err != nil {
		t.Fatalf("FindPlatformAPIKeyByHash failed: %v", err)
	}

	if record.UserID != "user_demo" {
		t.Fatalf("expected SQL auth repository to backfill the unique active membership user, got %q", record.UserID)
	}
	if record.TenantID != "tenant_demo" {
		t.Fatalf("expected tenant_demo, got %q", record.TenantID)
	}
}

func TestSQLAuthRepositoryFindPlatformAPIKeyByHashLeavesUserIDEmptyWithoutUniqueActiveMembership(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startAuthRepositoryPostgresContainer(ctx, t)
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

	for _, migration := range readAuthRepositoryMigrations(t) {
		if _, err := conn.Exec(ctx, migration); err != nil {
			t.Fatalf("conn.Exec migration failed: %v", err)
		}
	}

	if _, err := conn.Exec(ctx, `
		insert into tenants (id, name, status)
		values
			('tenant_none', 'Tenant None', 'active'),
			('tenant_multi', 'Tenant Multi', 'active');
		insert into users (id, email, name, role, status)
		values
			('user_a', 'user-a@example.com', 'User A', 'member', 'active'),
			('user_b', 'user-b@example.com', 'User B', 'member', 'active');
		insert into tenant_memberships (id, tenant_id, user_id, role, status)
		values
			('tm_a', 'tenant_multi', 'user_a', 'member', 'active'),
			('tm_b', 'tenant_multi', 'user_b', 'member', 'active');
		insert into platform_api_keys (id, tenant_id, name, key_hash, status)
		values
			('pak_none', 'tenant_none', 'none', 'sha256:none', 'active'),
			('pak_multi', 'tenant_multi', 'multi', 'sha256:multi', 'active');
	`); err != nil {
		t.Fatalf("seed auth records failed: %v", err)
	}

	repo := NewAuthRepository(New(conn))

	testCases := []struct {
		name    string
		keyHash string
	}{
		{
			name:    "no active memberships",
			keyHash: "sha256:none",
		},
		{
			name:    "multiple active memberships",
			keyHash: "sha256:multi",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			record, err := repo.FindPlatformAPIKeyByHash(ctx, tc.keyHash)
			if err != nil {
				t.Fatalf("FindPlatformAPIKeyByHash failed: %v", err)
			}
			if record.UserID != "" {
				t.Fatalf("expected empty user id when membership scope is not uniquely attributable, got %q", record.UserID)
			}
		})
	}
}

func TestSQLAuthRepositoryResolveConsolePrincipalRejectsMultipleActiveMemberships(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startAuthRepositoryPostgresContainer(ctx, t)
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

	for _, migration := range readAuthRepositoryMigrations(t) {
		if _, err := conn.Exec(ctx, migration); err != nil {
			t.Fatalf("conn.Exec migration failed: %v", err)
		}
	}

	if _, err := conn.Exec(ctx, `
		insert into tenants (id, name, status)
		values ('tenant_a', 'Tenant A', 'active'), ('tenant_b', 'Tenant B', 'active');
		insert into users (id, email, name, role, status)
		values ('user_member', 'member@example.com', 'Demo Member', 'member', 'active');
		insert into tenant_memberships (id, tenant_id, user_id, role, status)
		values
			('tm_a', 'tenant_a', 'user_member', 'member', 'active'),
			('tm_b', 'tenant_b', 'user_member', 'member', 'active');
	`); err != nil {
		t.Fatalf("seed console principal failed: %v", err)
	}

	repo := NewAuthRepository(New(conn))
	_, err = repo.ResolveConsolePrincipal(ctx, "member@example.com")
	if !errors.Is(err, ErrAuthScopeAmbiguous) {
		t.Fatalf("expected error %v, got %v", ErrAuthScopeAmbiguous, err)
	}
}

func TestSQLAuthRepositoryListActiveProviderCredentialsMapsSupportedModels(t *testing.T) {
	t.Parallel()

	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("NewCodec returned error: %v", err)
	}
	encryptedSecret, err := codec.Encrypt("provider-secret")
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	queries := fakeAuthQueries{
		providerCredentials: []ListActiveProviderCredentialsRow{
			{
				ID:              "pc_123",
				Provider:        "dashscope",
				DisplayName:     "DashScope Primary",
				SupportedModels: []string{"qwen-flash"},
				BaseURL:         "https://dashscope.aliyuncs.com/compatible-mode/v1",
				EncryptedSecret: encryptedSecret,
				Status:          string(domain.StatusActive),
			},
		},
	}

	repo := NewAuthRepository(queries, codec)

	credentials, err := repo.ListActiveProviderCredentials(context.Background())
	if err != nil {
		t.Fatalf("ListActiveProviderCredentials returned error: %v", err)
	}
	if len(credentials) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(credentials))
	}

	got := credentials[0]
	if got.ID != "pc_123" {
		t.Fatalf("expected credential id %q, got %q", "pc_123", got.ID)
	}
	if got.Provider != "dashscope" {
		t.Fatalf("expected provider %q, got %q", "dashscope", got.Provider)
	}
	if got.DisplayName != "DashScope Primary" {
		t.Fatalf("expected display name %q, got %q", "DashScope Primary", got.DisplayName)
	}
	if got.Status != domain.StatusActive {
		t.Fatalf("expected status %q, got %q", domain.StatusActive, got.Status)
	}
	if got.BaseURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("expected base URL %q, got %q", "https://dashscope.aliyuncs.com/compatible-mode/v1", got.BaseURL)
	}
	if len(got.SupportedModels) != 1 {
		t.Fatalf("expected 1 supported model, got %d", len(got.SupportedModels))
	}
	if got.SupportedModels[0] != "qwen-flash" {
		t.Fatalf("expected supported models to round-trip, got %#v", got.SupportedModels)
	}
	if got.APIKey != "provider-secret" {
		t.Fatalf("expected decrypted api key %q, got %q", "provider-secret", got.APIKey)
	}
}

func TestSQLAuthRepositoryListActiveProviderCredentialsDecryptsSecrets(t *testing.T) {
	t.Parallel()

	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("NewCodec returned error: %v", err)
	}
	encryptedSecret, err := codec.Encrypt("provider-secret-key")
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	queries := fakeAuthQueries{
		providerCredentials: []ListActiveProviderCredentialsRow{
			{
				ID:              "pc_123",
				Provider:        "dashscope",
				DisplayName:     "DashScope Primary",
				EncryptedSecret: encryptedSecret,
				SupportedModels: []string{"qwen-flash"},
				Status:          string(domain.StatusActive),
			},
		},
	}

	repo := NewAuthRepository(queries, codec)

	credentials, err := repo.ListActiveProviderCredentials(context.Background())
	if err != nil {
		t.Fatalf("ListActiveProviderCredentials returned error: %v", err)
	}
	if len(credentials) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(credentials))
	}
	if credentials[0].APIKey != "provider-secret-key" {
		t.Fatalf("expected decrypted api key, got %q", credentials[0].APIKey)
	}
}

type fakeAuthQueries struct {
	platformKey         GetPlatformAPIKeyByHashRow
	tenant              GetTenantByIDRow
	providerCredentials []ListActiveProviderCredentialsRow
}

func (f fakeAuthQueries) GetPlatformAPIKeyByHash(context.Context, string) (GetPlatformAPIKeyByHashRow, error) {
	return f.platformKey, nil
}

func (f fakeAuthQueries) GetTenantByID(context.Context, string) (GetTenantByIDRow, error) {
	return f.tenant, nil
}

func (f fakeAuthQueries) ListActiveProviderCredentials(context.Context) ([]ListActiveProviderCredentialsRow, error) {
	return f.providerCredentials, nil
}

func newSeededAuthRepository(t *testing.T) *BootstrapAuthRepository {
	t.Helper()

	return NewBootstrapAuthRepository(BootstrapAuthConfig{
		RawPlatformAPIKey:    "agw_demo_key",
		PlatformAPIKeyID:     "pak_demo",
		PlatformAPIKeyUserID: "user_demo",
		PlatformAPIKeyName:   "demo",
		TenantID:             "tenant_demo",
		TenantName:           "Demo Tenant",
	})
}

func startAuthRepositoryPostgresContainer(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
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

func readAuthRepositoryMigrations(t *testing.T) []string {
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
