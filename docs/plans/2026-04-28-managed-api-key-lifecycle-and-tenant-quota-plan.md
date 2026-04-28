# Managed API Key Lifecycle And Tenant Quota Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把平台 API Key 从“只存 hash、创建时瞬时返回明文”的演示能力，升级为“可加密托管、可受控回显、可审计、可按租户做请求数与 Token 双限额治理”的完整生产能力。

**Architecture:** 后端继续以 Go + PostgreSQL 为主线，复用现有 `secret.Codec` 做平台密钥密文存储，鉴权仍然只走 `key_hash`。额度治理新增“租户策略表 + 月周期用量表 + 数据库额度守卫”，前端控制台围绕“选中密钥的稳定详情区”和“租户额度卡”完成 admin/member 两个视角的联动。

**Tech Stack:** Go 1.22, Fiber, PostgreSQL, sqlc, React 18, Vite, Vitest, Docker Compose

---

## File Map

### 数据库与启动编排

- Create: `gateway/db/migrations/0010_add_managed_api_key_lifecycle_and_tenant_quota.sql`
- Modify: `gateway/db/runtime.go`
- Modify: `gateway/db/runtime_test.go`
- Modify: `gateway/internal/config/config.go`
- Modify: `gateway/internal/config/config_test.go`
- Modify: `gateway/cmd/server/main.go`

### 后端密钥生命周期与额度治理

- Modify: `gateway/internal/service/console_service.go`
- Modify: `gateway/internal/service/member_console_service.go`
- Create: `gateway/internal/service/platform_api_key_secret.go`
- Create: `gateway/internal/service/platform_api_key_secret_test.go`
- Create: `gateway/internal/service/tenant_quota_guard.go`
- Create: `gateway/internal/service/tenant_quota_guard_test.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/service/postgres_console_service_test.go`
- Modify: `gateway/internal/service/postgres_member_console_service.go`
- Modify: `gateway/internal/service/postgres_member_console_service_test.go`
- Modify: `gateway/internal/service/auth_service.go`
- Modify: `gateway/internal/service/auth_service_test.go`
- Modify: `gateway/internal/service/usage_aggregator.go`
- Modify: `gateway/internal/service/usage_aggregator_test.go`
- Modify: `gateway/internal/store/auth_repository.go`
- Modify: `gateway/internal/store/auth_repository_test.go`
- Modify: `gateway/db/query/api_keys.sql`
- Modify: `gateway/internal/store/api_keys.sql.go`
- Modify: `gateway/internal/store/models.go`

### HTTP 接口与前端 API

- Modify: `gateway/internal/http/handlers/admin.go`
- Modify: `gateway/internal/http/handlers/member.go`
- Modify: `gateway/internal/http/router.go`
- Modify: `gateway/internal/http/router_test.go`
- Modify: `web/src/lib/console-api.ts`

### 控制台页面

- Modify: `web/src/pages/api-keys.tsx`
- Modify: `web/src/pages/member-overview.tsx`
- Modify: `web/src/pages/admin-tenants.tsx`
- Modify: `web/src/styles.css`
- Modify: `web/src/test/router.test.tsx`

### 文档与部署验证

- Modify: `README.md`
- Modify: `docs/specs/2026-04-28-managed-api-key-lifecycle-and-tenant-quota-design.md`

## Task 1: 扩展 Schema、Seed 与密钥编解码注入

**Files:**
- Create: `gateway/db/migrations/0010_add_managed_api_key_lifecycle_and_tenant_quota.sql`
- Modify: `gateway/db/runtime.go`
- Modify: `gateway/db/runtime_test.go`
- Modify: `gateway/internal/config/config.go`
- Modify: `gateway/internal/config/config_test.go`
- Modify: `gateway/cmd/server/main.go`

- [ ] **Step 1: 先写配置与 seed 失败测试**

```go
func TestLoadReadsPlatformAPIKeySecretKeyFromFile(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "platform_api_key_secret")
	const expected = "0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(secretPath, []byte(expected+"\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile failed: %v", err)
	}

	t.Setenv("GATEWAY_PLATFORM_API_KEY_SECRET_KEY_FILE", secretPath)

	cfg := Load()
	if cfg.PlatformAPIKeySecretKey != expected {
		t.Fatalf("expected PlatformAPIKeySecretKey %q, got %q", expected, cfg.PlatformAPIKeySecretKey)
	}
}

func TestSeedDemoDataEncryptsPlatformAPIKeys(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(ctx, t)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

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
		PlatformKeyCodec:    codec,
	}); err != nil {
		t.Fatalf("SeedDemoData failed: %v", err)
	}

	var ciphertext string
	var recoverable bool
	if err := conn.QueryRow(ctx, `
		select key_ciphertext, secret_recoverable
		from platform_api_keys
		where id = 'pak_live_console'
	`).Scan(&ciphertext, &recoverable); err != nil {
		t.Fatalf("QueryRow failed: %v", err)
	}
	if !strings.HasPrefix(ciphertext, secret.EncryptedSecretPrefix) {
		t.Fatalf("expected encrypted key prefix %q, got %q", secret.EncryptedSecretPrefix, ciphertext)
	}
	if !recoverable {
		t.Fatal("expected seeded platform api key to be recoverable")
	}
}
```

- [ ] **Step 2: 运行测试，确认当前实现失败**

Run:

```bash
cd gateway && go test ./internal/config -run TestLoadReadsPlatformAPIKeySecretKeyFromFile -v
cd gateway && go test ./db -run TestSeedDemoDataEncryptsPlatformAPIKeys -v
```

Expected:

```text
FAIL: Config does not expose PlatformAPIKeySecretKey
FAIL: SeedConfig has no PlatformKeyCodec / platform_api_keys has no key_ciphertext
```

- [ ] **Step 3: 实现 migration、配置项和启动注入**

```sql
alter table platform_api_keys
  add column key_ciphertext text not null default '',
  add column key_kek_version text not null default 'v1',
  add column created_by_user_id text references users(id),
  add column expires_at timestamptz,
  add column rotated_from_key_id text references platform_api_keys(id),
  add column disabled_at timestamptz,
  add column disabled_reason text not null default '',
  add column secret_recoverable boolean not null default false;

update platform_api_keys
set expires_at = created_at + interval '30 days'
where expires_at is null;

create table tenant_quota_policies (
  tenant_id text primary key references tenants(id),
  period_type text not null check (period_type in ('monthly')),
  request_limit bigint not null check (request_limit > 0),
  token_limit bigint not null check (token_limit > 0),
  effective_from timestamptz not null default now(),
  created_by text references users(id),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table tenant_quota_usage_periods (
  tenant_id text not null references tenants(id),
  period_start timestamptz not null,
  period_end timestamptz not null,
  requests_used bigint not null default 0 check (requests_used >= 0),
  tokens_used bigint not null default 0 check (tokens_used >= 0),
  last_aggregated_at timestamptz not null default now(),
  primary key (tenant_id, period_start),
  check (period_end > period_start)
);

create table api_key_secret_access_logs (
  id text primary key,
  api_key_id text not null references platform_api_keys(id) on delete cascade,
  tenant_id text not null references tenants(id),
  actor_user_id text references users(id),
  actor_role text not null check (actor_role in ('admin', 'member')),
  action text not null check (action in ('reveal', 'copy')),
  access_result text not null check (access_result in ('allowed', 'denied')),
  ip_address text not null default '',
  user_agent text not null default '',
  created_at timestamptz not null default now()
);
```

```go
type Config struct {
	ProviderSecretKey       string
	PlatformAPIKeySecretKey string
}

return Config{
	ProviderSecretKey:       lookupEnv("GATEWAY_PROVIDER_SECRET_KEY"),
	PlatformAPIKeySecretKey: lookupEnv("GATEWAY_PLATFORM_API_KEY_SECRET_KEY"),
}
```

```go
type SeedConfig struct {
	SecretCodec     *secret.Codec
	PlatformKeyCodec *secret.Codec
}

platformKeyCodec := mustNewPlatformAPIKeySecretCodec(cfg)

if err := gatewaydb.SeedDemoData(ctx, pool, gatewaydb.SeedConfig{
	PlatformAPIKey:   cfg.SeedPlatformAPIKey,
	SecretCodec:      providerSecretCodec,
	PlatformKeyCodec: platformKeyCodec,
}); err != nil {
	panic(err)
}

consoleService := service.NewPostgresConsoleService(pool, authService, chatProxy, ragProxy, cfg.SeedPlatformAPIKey, platformKeyCodec)
memberConsoleService := service.NewPostgresMemberConsoleService(pool, service.ConsolePrincipal{}, platformKeyCodec)
```

- [ ] **Step 4: 重新运行测试**

Run:

```bash
cd gateway && go test ./internal/config -run TestLoadReadsPlatformAPIKeySecretKeyFromFile -v
cd gateway && go test ./db -run 'TestSeedDemoData(EncryptsProviderSecrets|EncryptsPlatformAPIKeys)' -v
```

Expected:

```text
PASS
ok  	github.com/example/ai_gateway/gateway/internal/config
ok  	github.com/example/ai_gateway/gateway/db
```

- [ ] **Step 5: 提交本任务**

```bash
git add gateway/db/migrations/0010_add_managed_api_key_lifecycle_and_tenant_quota.sql gateway/db/runtime.go gateway/db/runtime_test.go gateway/internal/config/config.go gateway/internal/config/config_test.go gateway/cmd/server/main.go
git commit -m "feat: add managed api key schema and secret codec wiring"
```

## Task 2: 落地平台密钥托管、回显与兼容旧 Key 的服务层

**Files:**
- Modify: `gateway/internal/service/console_service.go`
- Modify: `gateway/internal/service/member_console_service.go`
- Create: `gateway/internal/service/platform_api_key_secret.go`
- Create: `gateway/internal/service/platform_api_key_secret_test.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/service/postgres_console_service_test.go`
- Modify: `gateway/internal/service/postgres_member_console_service.go`
- Modify: `gateway/internal/service/postgres_member_console_service_test.go`

- [ ] **Step 1: 先写 member/admin 的回显与兼容测试**

```go
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

	secretView, err := member.RevealAPIKeySecret(ctx, created.Item.ID)
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

func TestPostgresConsoleServiceRevealLegacyKeyMarksUnrecoverable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)
	if _, err := conn.Exec(ctx, `
		insert into platform_api_keys (
			id, tenant_id, name, key_hash, status, scopes, created_at, expires_at, secret_recoverable
		) values (
			'pak_legacy_only_hash',
			'tenant_demo',
			'legacy-only-hash',
			'sha256:legacy-only-hash',
			'active',
			ARRAY['chat'],
			now(),
			now() + interval '30 days',
			false
		);
	`); err != nil {
		t.Fatalf("seed legacy key failed: %v", err)
	}

	secretView, err := console.RevealAPIKeySecret(ctx, "pak_legacy_only_hash")
	if err != nil {
		t.Fatalf("RevealAPIKeySecret failed: %v", err)
	}
	if secretView.Revealable {
		t.Fatal("expected legacy key to be unrecoverable")
	}
	if !secretView.LegacyUnrecoverable {
		t.Fatal("expected LegacyUnrecoverable to be true")
	}
}
```

- [ ] **Step 2: 运行测试，确认接口当前不存在**

Run:

```bash
cd gateway && go test ./internal/service -run 'TestPostgres(MemberConsoleServiceRevealAPIKeySecretReturnsOwnedSecret|ConsoleServiceRevealLegacyKeyMarksUnrecoverable)' -v
```

Expected:

```text
FAIL: CreateAPIKey persists only key_hash
FAIL: postgres console services do not expose RevealAPIKeySecret
```

- [ ] **Step 3: 加入密钥视图模型、加密存储和回显服务**

```go
type APIKeyItem struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Tenant             string   `json:"tenant"`
	Status             string   `json:"status"`
	Scopes             []string `json:"scopes"`
	LastUsedAt         string   `json:"last_used_at"`
	CreatedByUserID    string   `json:"created_by_user_id,omitempty"`
	ExpiresAt          string   `json:"expires_at,omitempty"`
	Revealable         bool     `json:"revealable"`
	LegacyUnrecoverable bool    `json:"legacy_unrecoverable"`
}

type APIKeySecretView struct {
	APIKeyID            string `json:"api_key_id"`
	MaskedKey           string `json:"masked_key"`
	FullKey             string `json:"full_key,omitempty"`
	Revealable          bool   `json:"revealable"`
	LegacyUnrecoverable bool   `json:"legacy_unrecoverable"`
	ExpiresAt           string `json:"expires_at,omitempty"`
}
```

```go
type platformAPIKeySecretService struct {
	codec *secret.Codec
}

func (s platformAPIKeySecretService) Encrypt(rawKey string) (string, bool, error) {
	if s.codec == nil || strings.TrimSpace(rawKey) == "" {
		return "", false, nil
	}
	ciphertext, err := s.codec.Encrypt(rawKey)
	if err != nil {
		return "", false, err
	}
	return ciphertext, true, nil
}

func (s platformAPIKeySecretService) Reveal(ciphertext string, recoverable bool) (string, error) {
	if !recoverable || strings.TrimSpace(ciphertext) == "" {
		return "", nil
	}
	return s.codec.Decrypt(ciphertext)
}

type managedAPIKeySecretRecord struct {
	ID          string
	TenantID    string
	FullKey     string
	Recoverable bool
	ExpiresAt   time.Time
}

func buildAPIKeySecretView(apiKeyID string, fullKey string, recoverable bool, expiresAt time.Time) APIKeySecretView {
	return APIKeySecretView{
		APIKeyID:            apiKeyID,
		MaskedKey:           maskManagedAPIKey(fullKey),
		FullKey:             fullKey,
		Revealable:          recoverable && strings.TrimSpace(fullKey) != "",
		LegacyUnrecoverable: !recoverable,
		ExpiresAt:           expiresAt.In(shanghaiLocation()).Format(time.RFC3339),
	}
}

func maskManagedAPIKey(rawKey string) string {
	if len(rawKey) <= 8 {
		return "••••••••"
	}
	return rawKey[:4] + "••••••••" + rawKey[len(rawKey)-4:]
}
```

```go
rawKey := newPlatformAPIKeySecret()
ciphertext, recoverable, err := s.secretService.Encrypt(rawKey)
if err != nil {
	return APIKeyMutationResult{}, err
}

insert into platform_api_keys (
	id, tenant_id, name, key_hash, key_ciphertext, key_kek_version,
	status, scopes, created_by_user_id, created_at, expires_at, secret_recoverable
) values (
	$1, $2, $3, $4, $5, 'v1',
	'active', $6, $7, now(), now() + interval '30 days', $8
)
```

```go
func (s postgresMemberConsoleService) RevealAPIKeySecret(ctx context.Context, id string) (APIKeySecretView, error) {
	principal, err := s.resolvePrincipal(ctx)
	if err != nil {
		return APIKeySecretView{}, err
	}

	row := s.db.QueryRow(ctx, `
		select id, key_ciphertext, secret_recoverable, expires_at
		from platform_api_keys
		where id = $1
		  and tenant_id = $2
		  and created_by_user_id = $3
	`, strings.TrimSpace(id), principal.TenantID, principal.UserID)

	var keyID string
	var ciphertext string
	var recoverable bool
	var expiresAt time.Time
	if err := row.Scan(&keyID, &ciphertext, &recoverable, &expiresAt); err != nil {
		return APIKeySecretView{}, mapAPIKeyMutationError(err, "api key not found")
	}

	fullKey, err := s.secretService.Reveal(ciphertext, recoverable)
	if err != nil {
		return APIKeySecretView{}, err
	}

	return buildAPIKeySecretView(keyID, fullKey, recoverable, expiresAt), nil
}

func (s postgresConsoleService) loadManagedAPIKeySecretRecord(ctx context.Context, id string) (managedAPIKeySecretRecord, error) {
	var record managedAPIKeySecretRecord
	var ciphertext string
	row := s.db.QueryRow(ctx, `
		select id, tenant_id, key_ciphertext, secret_recoverable, expires_at
		from platform_api_keys
		where id = $1
	`, strings.TrimSpace(id))
	if err := row.Scan(&record.ID, &record.TenantID, &ciphertext, &record.Recoverable, &record.ExpiresAt); err != nil {
		return managedAPIKeySecretRecord{}, mapAPIKeyMutationError(err, "api key not found")
	}
	fullKey, err := s.secretService.Reveal(ciphertext, record.Recoverable)
	if err != nil {
		return managedAPIKeySecretRecord{}, err
	}
	record.FullKey = fullKey
	return record, nil
}
```

- [ ] **Step 4: 在 create/rotate/list 中统一透出生命周期字段**

```go
func translateLifecycleStatus(status string, expiresAt time.Time, now time.Time) string {
	switch {
	case status == "disabled":
		return "已停用"
	case !expiresAt.IsZero() && !now.Before(expiresAt):
		return "已过期"
	case !expiresAt.IsZero() && expiresAt.Sub(now) <= 72*time.Hour:
		return "即将过期"
	default:
		return "生效中"
	}
}
```

```go
select
	p.id,
	p.name,
	p.tenant_id,
	p.status,
	p.scopes,
	coalesce(p.last_used_at, p.created_at) as last_used_at,
	coalesce(p.created_by_user_id, '') as created_by_user_id,
	coalesce(p.expires_at, p.created_at + interval '30 days') as expires_at,
	p.secret_recoverable
from platform_api_keys p
order by p.created_at desc
```

- [ ] **Step 5: 重新运行服务层测试**

Run:

```bash
cd gateway && go test ./internal/service -run 'TestPostgres(MemberConsoleServiceRevealAPIKeySecretReturnsOwnedSecret|ConsoleServiceRevealLegacyKeyMarksUnrecoverable)' -v
cd gateway && go test ./internal/service -run 'TestPostgres(MemberConsoleService(CreateAPIKey|RotateAPIKey)|ConsoleService(CreateAPIKey|RotateAPIKey))' -v
```

Expected:

```text
PASS
ok  	github.com/example/ai_gateway/gateway/internal/service
```

- [ ] **Step 6: 提交本任务**

```bash
git add gateway/internal/service/console_service.go gateway/internal/service/member_console_service.go gateway/internal/service/platform_api_key_secret.go gateway/internal/service/platform_api_key_secret_test.go gateway/internal/service/postgres_console_service.go gateway/internal/service/postgres_console_service_test.go gateway/internal/service/postgres_member_console_service.go gateway/internal/service/postgres_member_console_service_test.go
git commit -m "feat: add managed api key secret reveal lifecycle"
```

## Task 3: 暴露 admin/member 回显与复制审计接口

**Files:**
- Modify: `gateway/internal/http/handlers/admin.go`
- Modify: `gateway/internal/http/handlers/member.go`
- Modify: `gateway/internal/http/router.go`
- Modify: `gateway/internal/http/router_test.go`
- Modify: `web/src/lib/console-api.ts`

- [ ] **Step 1: 先写 HTTP 路由和权限测试**

```go
func TestMemberSecretRevealRouteRequiresMemberOwnership(t *testing.T) {
	app := NewRouterWithDependencies(RouterDependencies{
		ServiceAuthUsername:   "svc",
		ServiceAuthPassword:   "svc-pass",
		ConsoleSessionEnabled: true,
		AuthService: stubConsoleAuthService{
			sessionPrincipal: service.ConsolePrincipal{
				UserID:   "user_member_a",
				Email:    "member-a@example.com",
				Role:     "member",
				TenantID: "tenant_demo",
			},
		},
		MemberConsoleService:  stubMemberConsoleService{},
	})

	req := httptest.NewRequest(http.MethodGet, "/me/api-keys/pak_demo/secret", nil)
	req.SetBasicAuth("svc", "svc-pass")
	req.Header.Set("X-Console-Session", "console_session_token")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.StatusCode)
	}
}

func TestAdminSecretCopyRouteRequiresAdminRole(t *testing.T) {
	app := NewRouterWithDependencies(RouterDependencies{
		ServiceAuthUsername:   "svc",
		ServiceAuthPassword:   "svc-pass",
		ConsoleSessionEnabled: true,
		AuthService: stubConsoleAuthService{
			sessionPrincipal: service.ConsolePrincipal{
				UserID:   "user_member_a",
				Email:    "member-a@example.com",
				Role:     "member",
				TenantID: "tenant_demo",
			},
		},
		ConsoleService:        stubConsoleService{},
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/api-keys/pak_demo/secret/copy", nil)
	req.SetBasicAuth("svc", "svc-pass")
	req.Header.Set("X-Console-Session", "console_session_token")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", res.StatusCode)
	}
}
```

- [ ] **Step 2: 运行测试，确认当前没有这些接口**

Run:

```bash
cd gateway && go test ./internal/http -run 'Test(MemberSecretRevealRouteRequiresMemberOwnership|AdminSecretCopyRouteRequiresAdminRole)' -v
```

Expected:

```text
FAIL: route not found
```

- [ ] **Step 3: 添加 reveal/copy handler、router 和前端 API 客户端**

```go
func ConsoleRevealAPIKeySecret(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := console.RevealAPIKeySecret(c.UserContext(), c.Params("id"))
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleCopyAPIKeySecret(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := console.CopyAPIKeySecret(c.UserContext(), c.Params("id"), c.IP(), c.Get("User-Agent"))
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}
```

```go
admin.Get("/api-keys/:id/secret", handlers.ConsoleRevealAPIKeySecret(deps.ConsoleService))
admin.Post("/api-keys/:id/secret/copy", handlers.ConsoleCopyAPIKeySecret(deps.ConsoleService))

member.Get("/api-keys/:id/secret", handlers.MemberRevealAPIKeySecret(deps.MemberConsoleService))
member.Post("/api-keys/:id/secret/copy", handlers.MemberCopyAPIKeySecret(deps.MemberConsoleService))
```

```ts
export type APIKeySecretView = {
  api_key_id: string;
  masked_key: string;
  full_key?: string;
  revealable: boolean;
  legacy_unrecoverable: boolean;
  expires_at?: string;
};

export async function revealMemberAPIKeySecret(id: string) {
  return requestJson<APIKeySecretView>(`/me/api-keys/${id}/secret`);
}

export async function copyMemberAPIKeySecret(id: string) {
  return requestJson<APIKeySecretView>(`/me/api-keys/${id}/secret/copy`, {
    method: "POST",
  });
}

export async function revealAPIKeySecret(id: string) {
  return requestJson<APIKeySecretView>(`/admin/api-keys/${id}/secret`);
}

export async function copyAPIKeySecret(id: string) {
  return requestJson<APIKeySecretView>(`/admin/api-keys/${id}/secret/copy`, {
    method: "POST",
  });
}
```

- [ ] **Step 4: 在服务层写入明文访问审计**

```go
const insertAPIKeySecretAccessLogSQL = `
insert into api_key_secret_access_logs (
	id, api_key_id, tenant_id, actor_user_id, actor_role, action, access_result, ip_address, user_agent, created_at
) values (
	$1, $2, $3, $4, $5, $6, $7, $8, $9, now()
)`
```

```go
func (s postgresConsoleService) logAPIKeySecretAccess(
	ctx context.Context,
	record managedAPIKeySecretRecord,
	action string,
	result string,
	ip string,
	userAgent string,
) {
	principal, _ := ConsolePrincipalFromContext(ctx)
	_, _ = s.db.Exec(ctx, insertAPIKeySecretAccessLogSQL, newAuditEventID(), record.ID, record.TenantID, principal.UserID, principal.Role, action, result, ip, userAgent)
}

func (s postgresConsoleService) CopyAPIKeySecret(ctx context.Context, id string, ip string, userAgent string) (APIKeySecretView, error) {
	record, err := s.loadManagedAPIKeySecretRecord(ctx, id)
	if err != nil {
		s.logAPIKeySecretAccess(ctx, managedAPIKeySecretRecord{ID: id}, "copy", "denied", ip, userAgent)
		return APIKeySecretView{}, err
	}

	s.logAPIKeySecretAccess(ctx, record, "copy", "allowed", ip, userAgent)
	return buildAPIKeySecretView(record.ID, record.FullKey, record.Recoverable, record.ExpiresAt), nil
}
```

- [ ] **Step 5: 重新运行 HTTP 与客户端测试**

Run:

```bash
cd gateway && go test ./internal/http -run 'Test(MemberSecretRevealRouteRequiresMemberOwnership|AdminSecretCopyRouteRequiresAdminRole)' -v
cd web && npm test -- api-keys-page
```

Expected:

```text
PASS
```

- [ ] **Step 6: 提交本任务**

```bash
git add gateway/internal/http/handlers/admin.go gateway/internal/http/handlers/member.go gateway/internal/http/router.go gateway/internal/http/router_test.go web/src/lib/console-api.ts
git commit -m "feat: add api key secret reveal and copy endpoints"
```

## Task 4: 落地租户级双限额、月周期聚合与鉴权拦截

**Files:**
- Create: `gateway/internal/service/tenant_quota_guard.go`
- Create: `gateway/internal/service/tenant_quota_guard_test.go`
- Modify: `gateway/internal/service/usage_aggregator.go`
- Modify: `gateway/internal/service/usage_aggregator_test.go`
- Modify: `gateway/internal/service/auth_service.go`
- Modify: `gateway/internal/service/auth_service_test.go`
- Modify: `gateway/internal/store/auth_repository.go`
- Modify: `gateway/internal/store/auth_repository_test.go`
- Modify: `gateway/db/query/api_keys.sql`
- Modify: `gateway/internal/store/api_keys.sql.go`
- Modify: `gateway/internal/store/models.go`
- Modify: `gateway/internal/service/console_service.go`
- Modify: `gateway/internal/service/member_console_service.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/service/postgres_member_console_service.go`

- [ ] **Step 1: 先写额度聚合和鉴权失败测试**

```go
func TestUsageAggregatorUpdatesTenantQuotaUsagePeriod(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(ctx, t)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	pool, err := gatewaydb.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("OpenPostgres failed: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := gatewaydb.ApplyMigrations(ctx, pool); err != nil {
		t.Fatalf("ApplyMigrations failed: %v", err)
	}
	for _, statement := range gatewaydb.RuntimeSeedStatements() {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("conn.Exec seed failed: %v", err)
		}
	}

	aggregator := NewUsageAggregator(conn)

	err = aggregator.Consume(ctx, queue.UsageEvent{
		TenantID:             "tenant_alpha",
		PlatformAPIKeyID:     "pak_live_console",
		ProviderCredentialID: "provider_dashscope_primary",
		RouteID:              "route_chat",
		Endpoint:             "/v1/chat/completions",
		Status:               "success",
		UsageSource:          "upstream",
		PromptTokens:         120,
		CompletionTokens:     80,
		TotalTokens:          200,
		OccurredAt:           time.Date(2026, 4, 28, 10, 0, 0, 0, shanghaiLocation()),
	})
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}

	var requestsUsed int64
	var tokensUsed int64
	if err := conn.QueryRow(ctx, `
		select requests_used, tokens_used
		from tenant_quota_usage_periods
		where tenant_id = 'tenant_alpha'
	`).Scan(&requestsUsed, &tokensUsed); err != nil {
		t.Fatalf("QueryRow failed: %v", err)
	}
	if requestsUsed != 1 || tokensUsed != 200 {
		t.Fatalf("expected usage (1, 200), got (%d, %d)", requestsUsed, tokensUsed)
	}
}

func TestDatabaseQuotaGuardRejectsExhaustedTenant(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(ctx, t)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	pool, err := gatewaydb.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("OpenPostgres failed: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := gatewaydb.ApplyMigrations(ctx, pool); err != nil {
		t.Fatalf("ApplyMigrations failed: %v", err)
	}
	for _, statement := range gatewaydb.RuntimeSeedStatements() {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("conn.Exec seed failed: %v", err)
		}
	}
	if _, err := conn.Exec(ctx, `
		insert into tenant_quota_policies (tenant_id, period_type, request_limit, token_limit)
		values ('tenant_alpha', 'monthly', 1, 100);
	`); err != nil {
		t.Fatalf("seed tenant_quota_policies failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into tenant_quota_usage_periods (tenant_id, period_start, period_end, requests_used, tokens_used)
		values ('tenant_alpha', timestamptz '2026-04-30T16:00:00Z', timestamptz '2026-05-31T16:00:00Z', 1, 100);
	`); err != nil {
		t.Fatalf("seed tenant_quota_usage_periods failed: %v", err)
	}

	guard := NewDatabaseQuotaGuard(conn)
	if err := guard.CheckTenantQuota(ctx, "tenant_alpha"); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded, got %v", err)
	}
}
```

- [ ] **Step 2: 运行测试，确认当前没有月周期额度能力**

Run:

```bash
cd gateway && go test ./internal/service -run 'Test(UsageAggregatorUpdatesTenantQuotaUsagePeriod|DatabaseQuotaGuardRejectsExhaustedTenant)' -v
```

Expected:

```text
FAIL: tenant_quota_usage_periods not updated
FAIL: DatabaseQuotaGuard is not implemented
```

- [ ] **Step 3: 实现月度周期计算、聚合落表与 DB 额度守卫**

```go
func currentMonthlyPeriod(now time.Time, loc *time.Location) (time.Time, time.Time) {
	localNow := now.In(loc)
	start := time.Date(localNow.Year(), localNow.Month(), 1, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 1, 0)
	return start.UTC(), end.UTC()
}
```

```go
const upsertTenantQuotaUsagePeriodSQL = `
insert into tenant_quota_usage_periods (
	tenant_id, period_start, period_end, requests_used, tokens_used, last_aggregated_at
) values (
	$1, $2, $3, $4, $5, now()
)
on conflict (tenant_id, period_start) do update set
	requests_used = tenant_quota_usage_periods.requests_used + excluded.requests_used,
	tokens_used = tenant_quota_usage_periods.tokens_used + excluded.tokens_used,
	last_aggregated_at = now()
`
```

```go
type DatabaseQuotaGuard struct {
	db       store.DBTX
	location *time.Location
}

func NewDatabaseQuotaGuard(db store.DBTX) DatabaseQuotaGuard {
	return DatabaseQuotaGuard{
		db:       db,
		location: shanghaiLocation(),
	}
}

func (g DatabaseQuotaGuard) CheckTenantQuota(ctx context.Context, tenantID string) error {
	periodStart, _ := currentMonthlyPeriod(time.Now(), g.location)
	var requestLimit int64
	var tokenLimit int64
	var requestsUsed int64
	var tokensUsed int64
	err := g.db.QueryRow(ctx, `
		select
			p.request_limit,
			p.token_limit,
			coalesce(u.requests_used, 0),
			coalesce(u.tokens_used, 0)
		from tenant_quota_policies p
		left join tenant_quota_usage_periods u
		  on u.tenant_id = p.tenant_id
		 and u.period_start = $2
		where p.tenant_id = $1
	`, tenantID, periodStart).Scan(&requestLimit, &tokenLimit, &requestsUsed, &tokensUsed)
	if err != nil {
		return err
	}
	if requestsUsed >= requestLimit || tokensUsed >= tokenLimit {
		return ErrQuotaExceeded
	}
	return nil
}
```

- [ ] **Step 4: 在 auth、overview 和 list 查询里消费额度摘要**

```go
type TenantQuotaSummary struct {
	RequestLimit      int64  `json:"request_limit"`
	RequestsUsed      int64  `json:"requests_used"`
	RequestsRemaining int64  `json:"requests_remaining"`
	TokenLimit        int64  `json:"token_limit"`
	TokensUsed        int64  `json:"tokens_used"`
	TokensRemaining   int64  `json:"tokens_remaining"`
	ResetsAt          string `json:"resets_at"`
}

type MemberOverviewPageData struct {
	TenantID      string             `json:"tenant_id"`
	TenantName    string             `json:"tenant_name"`
	ActiveAPIKeys int                `json:"active_api_keys"`
	Quota         TenantQuotaSummary `json:"quota"`
}
```

```go
func NewCompositeQuotaGuard(guards ...QuotaGuard) QuotaGuard {
	return compositeQuotaGuard{guards: guards}
}

func (g compositeQuotaGuard) CheckTenantQuota(ctx context.Context, tenantID string) error {
	for _, guard := range g.guards {
		if guard == nil {
			continue
		}
		if err := guard.CheckTenantQuota(ctx, tenantID); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 5: 重新生成 sqlc 代码并运行后端测试**

Run:

```bash
cd gateway && sqlc generate
cd gateway && go test ./internal/store -run 'Test(FindPlatformAPIKeyByHash|AuthenticateConsoleUser)' -v
cd gateway && go test ./internal/service -run 'Test(UsageAggregatorUpdatesTenantQuotaUsagePeriod|AuthServiceRejectsExpiredOrQuotaExhaustedPlatformKey)' -v
```

Expected:

```text
PASS
```

- [ ] **Step 6: 提交本任务**

```bash
git add gateway/internal/service/tenant_quota_guard.go gateway/internal/service/tenant_quota_guard_test.go gateway/internal/service/usage_aggregator.go gateway/internal/service/usage_aggregator_test.go gateway/internal/service/auth_service.go gateway/internal/service/auth_service_test.go gateway/internal/store/auth_repository.go gateway/internal/store/auth_repository_test.go gateway/db/query/api_keys.sql gateway/internal/store/api_keys.sql.go gateway/internal/store/models.go gateway/internal/service/console_service.go gateway/internal/service/member_console_service.go gateway/internal/service/postgres_console_service.go gateway/internal/service/postgres_member_console_service.go
git commit -m "feat: enforce tenant request and token quotas"
```

## Task 5: 完成控制台联动，做成稳定详情区而不是一次性弹窗

**Files:**
- Modify: `web/src/lib/console-api.ts`
- Modify: `web/src/pages/api-keys.tsx`
- Modify: `web/src/pages/member-overview.tsx`
- Modify: `web/src/pages/admin-tenants.tsx`
- Modify: `web/src/styles.css`
- Modify: `web/src/test/router.test.tsx`

- [ ] **Step 1: 先写前端交互测试**

```tsx
it("shows stable selected key detail area and copies full key", async () => {
	vi.spyOn(globalThis.navigator.clipboard, "writeText").mockResolvedValue(undefined);

mockSession({
  role: "member",
  user_id: "user_member_a",
  email: "member-a@example.com",
  name: "租户用户 A",
});
mockFetch({
  "/api/me/api-keys": {
    items: [{ id: "pak_live_console", name: "prod-gateway", tenant: "tenant_demo", status: "生效中", scopes: ["chat"], last_used_at: "2026-04-28T10:00:00+08:00", revealable: true, legacy_unrecoverable: false }],
  },
  "/api/me/api-keys/pak_live_console/secret": {
    api_key_id: "pak_live_console",
    masked_key: "sk-gw••••••••cret",
    full_key: "sk-gw-live-secret",
    revealable: true,
    legacy_unrecoverable: false,
  },
  "/api/me/api-keys/pak_live_console/secret/copy": {
    api_key_id: "pak_live_console",
    masked_key: "sk-gw••••••••cret",
    full_key: "sk-gw-live-secret",
    revealable: true,
    legacy_unrecoverable: false,
  },
});
renderRoute("/api-keys");

	expect(await screen.findByText("prod-gateway")).toBeInTheDocument();
	await userEvent.click(screen.getByRole("button", { name: "显示完整密钥" }));
	expect(await screen.findByText(/sk-gw/)).toBeInTheDocument();

	await userEvent.click(screen.getByRole("button", { name: "复制完整密钥" }));
	expect(navigator.clipboard.writeText).toHaveBeenCalledWith("sk-gw-live-secret");
	expect(await screen.findByText("完整密钥已复制")).toBeInTheDocument();
});

it("renders tenant quota summary for member overview", async () => {
mockSession({
  role: "member",
  user_id: "user_member_a",
  email: "member-a@example.com",
  name: "租户用户 A",
});
mockFetch({
  "/api/me/overview": {
    tenant_id: "tenant_demo",
    tenant_name: "Demo Tenant",
    active_api_keys: 2,
    quota: {
      request_limit: 500000,
      requests_used: 120000,
      requests_remaining: 380000,
      token_limit: 10000000,
      tokens_used: 2400000,
      tokens_remaining: 7600000,
      resets_at: "2026-05-01T00:00:00+08:00",
    },
  },
});
renderRoute("/me");

	expect(await screen.findByText("本月请求额度")).toBeInTheDocument();
	expect(screen.getByText("120000 / 500000")).toBeInTheDocument();
	expect(screen.getByText("2,400,000 / 10,000,000")).toBeInTheDocument();
});
```

- [ ] **Step 2: 运行测试，确认现有页面还依赖 raw_key 临时反馈**

Run:

```bash
cd web && npm test -- api-keys-page member-overview-page
```

Expected:

```text
FAIL: API key page only renders transient creation result
FAIL: member overview has no quota section
```

- [ ] **Step 3: 改 API 客户端和 API Key 页面状态模型**

```ts
const [selectedSecret, setSelectedSecret] = useState<APIKeySecretView | null>(null);
const [secretLoading, setSecretLoading] = useState(false);

async function handleRevealSecret() {
  if (!selectedItem) return;
  setSecretLoading(true);
  setCopyNotice(null);
  try {
    const nextSecret = isAdmin
      ? await revealAPIKeySecret(selectedItem.id)
      : await revealMemberAPIKeySecret(selectedItem.id);
    setSelectedSecret(nextSecret);
  } finally {
    setSecretLoading(false);
  }
}

async function handleCopySecret() {
  if (!selectedItem) return;
  const secretView = isAdmin
    ? await copyAPIKeySecret(selectedItem.id)
    : await copyMemberAPIKeySecret(selectedItem.id);
  await copyTextWithFallback(secretView.full_key ?? "");
  setSelectedSecret(secretView);
  setCopyNotice("完整密钥已复制");
}
```

```tsx
<aside className="api-key-detail-card">
  <p className="api-key-detail-card__eyebrow">已选密钥</p>
  <h3>{selectedItem?.name ?? "未选择密钥"}</h3>
  <dl>
    <div><dt>状态</dt><dd>{selectedItem?.status ?? "-"}</dd></div>
    <div><dt>到期时间</dt><dd>{selectedItem?.expires_at ?? "-"}</dd></div>
    <div><dt>权限范围</dt><dd>{selectedItem?.scopes.join(" / ") ?? "-"}</dd></div>
    <div><dt>密钥摘要</dt><dd>{selectedSecret?.masked_key ?? "点击显示完整密钥后加载"}</dd></div>
  </dl>
  <div className="api-key-detail-card__actions">
    <button type="button" onClick={handleRevealSecret}>显示完整密钥</button>
    <button type="button" onClick={handleCopySecret}>复制完整密钥</button>
  </div>
</aside>
```

- [ ] **Step 4: 增加 member/admin 的额度展示**

```tsx
<section className="quota-summary-grid">
  <article className="quota-card">
    <span>本月请求额度</span>
    <strong>{formatNumber(data.quota.requests_used)} / {formatNumber(data.quota.request_limit)}</strong>
    <p>剩余 {formatNumber(data.quota.requests_remaining)}，重置时间 {data.quota.resets_at}</p>
  </article>
  <article className="quota-card">
    <span>本月 Token 额度</span>
    <strong>{formatNumber(data.quota.tokens_used)} / {formatNumber(data.quota.token_limit)}</strong>
    <p>剩余 {formatNumber(data.quota.tokens_remaining)}</p>
  </article>
</section>
```

```css
.api-key-detail-card {
  border: 1px solid rgba(138, 169, 196, 0.24);
  background: linear-gradient(180deg, rgba(7, 16, 28, 0.92), rgba(9, 22, 36, 0.88));
  box-shadow: 0 24px 80px rgba(4, 12, 24, 0.32);
  border-radius: 24px;
  padding: 24px;
}

.quota-card {
  background: radial-gradient(circle at top left, rgba(80, 174, 255, 0.18), rgba(6, 18, 30, 0.94));
  border: 1px solid rgba(115, 157, 198, 0.22);
  border-radius: 20px;
  padding: 20px;
}
```

- [ ] **Step 5: 重新运行前端测试与构建**

Run:

```bash
cd web && npm test -- api-keys-page member-overview-page
cd web && npm run build
```

Expected:

```text
PASS
vite v5.x building for production...
✓ built in ...
```

- [ ] **Step 6: 提交本任务**

```bash
git add web/src/lib/console-api.ts web/src/pages/api-keys.tsx web/src/pages/member-overview.tsx web/src/pages/admin-tenants.tsx web/src/styles.css web/src/test/router.test.tsx
git commit -m "feat: add managed key detail console and tenant quota cards"
```

## Task 6: 回写文档、补充执行约束并完成端到端验证

**Files:**
- Modify: `README.md`
- Modify: `docs/specs/2026-04-28-managed-api-key-lifecycle-and-tenant-quota-design.md`

- [ ] **Step 1: 先写文档与启动验证清单**

```md
## 执行约束

- 每完成一个独立开发任务，必须立即提交一次 Git commit
- 任何 API Key、控制台密码、数据库密码仅允许通过文件或环境变量注入
- 平台 API Key 明文只允许在受控回显接口返回，禁止落日志、禁止写入前端静态资源
```

```md
## 新增环境变量

- `GATEWAY_PLATFORM_API_KEY_SECRET_KEY`
- `GATEWAY_PLATFORM_API_KEY_SECRET_KEY_FILE`

## 新增控制台能力

- 历史平台 API Key 受控回显
- 历史平台 API Key 复制审计
- 租户月度请求额度 / Token 额度
- 到期前 3 天预警，到期自动拒绝鉴权
```

- [ ] **Step 2: 更新 README 和规格文档**

```md
### 平台 API Key 托管

网关现在会同时保存：

- `key_hash`：仅用于鉴权
- `key_ciphertext`：仅用于受控回显

如果某条旧数据只有 `key_hash` 没有 `key_ciphertext`，控制台会显示“历史密钥不可回显，请轮换”。
```

```md
### 开发流程约束

本项目的功能开发必须按任务粒度提交：

1. 先补失败测试
2. 实现最小代码
3. 通过测试
4. 立刻提交单独 commit
```

- [ ] **Step 3: 运行端到端验证**

Run:

```bash
cd gateway && go test ./...
cd web && npm test
cd web && npm run build
./scripts/test.sh
docker compose --env-file deploy/compose/.env.local -f deploy/compose/compose.yml up -d --build
docker compose --env-file deploy/compose/.env.local -f deploy/compose/compose.yml ps
curl http://127.0.0.1:32658/healthz
```

Expected:

```text
PASS
all containers are running
{"status":"ok"}
```

- [ ] **Step 4: 提交本任务**

```bash
git add README.md docs/specs/2026-04-28-managed-api-key-lifecycle-and-tenant-quota-design.md
git commit -m "docs: document managed key lifecycle execution rules"
```

## Self-Review

### Spec Coverage

- 历史密钥可解密回显：Task 2, Task 3, Task 5
- member 只能看自己创建的密钥：admin 可看全部：Task 2, Task 3
- 默认 30 天有效期、3 天预警、到期自动失效：Task 2, Task 4, Task 5
- 租户级请求数 + Token 双限额：Task 4, Task 5
- 审计 reveal / copy 行为：Task 3
- 兼容 legacy unrecoverable 旧 key：Task 2, Task 5
- 控制台稳定详情区与租户额度卡：Task 5
- 每完成一项任务单独 commit：Task 1-6 的提交步骤 + Task 6 文档回写

### Placeholder Scan

- 本计划没有使用 `TODO`、`TBD`、`implement later`
- 每个改动任务都包含了文件路径、测试入口、实现代码片段、验证命令、提交命令

### Type Consistency

- API 密钥详情统一使用 `APIKeySecretView`
- 租户额度统一使用 `TenantQuotaSummary`
- 回显/复制动作统一落到 `api_key_secret_access_logs`
- 生命周期兼容字段统一使用 `secret_recoverable` 和 `LegacyUnrecoverable`
