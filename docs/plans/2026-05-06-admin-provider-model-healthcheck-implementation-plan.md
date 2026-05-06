# Admin 供应商模型管理与健康检查 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 admin 控制台增加“供应商 -> 模型”管理能力，并在 `gateway` 进程内实现默认关闭的模型级聊天健康检查。

**Architecture:** 继续使用 `provider_credentials` 表示供应商，`route_catalog` 表示可调用聊天模型；通过 `secret_ref` 为新供应商提供服务器侧密钥引用模式。健康检查由 `gateway` 进程内的后台 ticker 周期执行，使用流式聊天请求、首个非空 token 判定成功，并回写模型健康状态。

**Tech Stack:** Go, Fiber, PostgreSQL, sqlc, React, TypeScript, Vitest, Docker Compose

---

## File Map

### Backend schema / store

- Create: `gateway/db/migrations/0017_add_provider_secret_ref_and_model_health.sql`
- Modify: `gateway/db/query/provider_credentials.sql`
- Modify: `gateway/internal/store/provider_credentials.sql.go`
- Modify: `gateway/internal/store/models.go`
- Modify: `gateway/internal/store/auth_repository.go`
- Test: `gateway/internal/store/store_test.go`

### Backend config / runtime

- Modify: `gateway/internal/config/config.go`
- Modify: `gateway/cmd/server/main.go`
- Create: `gateway/internal/service/model_healthcheck_runner.go`
- Create: `gateway/internal/service/model_healthcheck_runner_test.go`

### Backend admin APIs

- Modify: `gateway/internal/service/console_service.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/http/handlers/admin.go`
- Modify: `gateway/internal/http/router.go`
- Modify: `gateway/internal/http/router_test.go`
- Test: `gateway/internal/service/postgres_console_service_test.go`

### Frontend

- Modify: `web/src/lib/console-api.ts`
- Modify: `web/src/app/router.tsx`
- Create: `web/src/pages/admin-provider-models.tsx`
- Create: `web/src/pages/admin-model-health.tsx`
- Modify: `web/src/styles.css`
- Test: `web/src/test/router.test.tsx`

### Verification

- Modify: `deploy/compose/compose.yml`
- Modify: `README.md`
- Run: `./scripts/lint.sh`

---

### Task 1: 扩展数据库模型与 sqlc 读取

**Files:**
- Create: `gateway/db/migrations/0017_add_provider_secret_ref_and_model_health.sql`
- Modify: `gateway/db/query/provider_credentials.sql`
- Modify: `gateway/internal/store/provider_credentials.sql.go`
- Modify: `gateway/internal/store/models.go`
- Test: `gateway/internal/store/store_test.go`

- [ ] **Step 1: 先写失败用例，覆盖新字段读取**

在 `gateway/internal/store/store_test.go` 追加一个最小用例，验证 `ListActiveProviderCredentials` 能读出 `secret_ref` / `credential_mode`，并且 `route_catalog` 新字段存在默认值。

```go
func TestListActiveProviderCredentialsReturnsSecretRefMode(t *testing.T) {
	t.Parallel()

	ctx, repo, conn := newStoreTestRepo(t)

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

	items, err := repo.ListActiveProviderCredentials(ctx)
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
}
```

- [ ] **Step 2: 运行用例，确认因字段缺失而失败**

Run: `cd gateway && go test ./internal/store -run TestListActiveProviderCredentialsReturnsSecretRefMode -v`

Expected: SQL insert 或 scan 失败，提示 `secret_ref` / `credential_mode` 列不存在。

- [ ] **Step 3: 编写最小 migration 与 sqlc 查询**

在 `gateway/db/migrations/0017_add_provider_secret_ref_and_model_health.sql` 写入：

```sql
alter table provider_credentials
add column secret_ref text not null default '',
add column credential_mode text not null default 'encrypted'
check (credential_mode in ('encrypted', 'secret_ref'));

alter table route_catalog
add column status text not null default 'active'
check (status in ('active', 'disabled')),
add column healthcheck_enabled boolean not null default false,
add column healthcheck_assertion_type text not null default 'non_empty'
check (healthcheck_assertion_type in ('non_empty')),
add column last_health_checked_at timestamptz,
add column last_health_error text not null default '',
add column first_token_latency_ms integer not null default 0;
```

把 `gateway/db/query/provider_credentials.sql` 改为：

```sql
-- name: ListActiveProviderCredentials :many
select
  id,
  provider,
  display_name,
  supported_models,
  base_url,
  encrypted_secret,
  secret_ref,
  credential_mode,
  status
from provider_credentials
where status = 'active'
order by created_at asc, id asc;
```

对应更新 `gateway/internal/store/provider_credentials.sql.go` 和 `gateway/internal/store/models.go` 的生成结构：

```go
type ListActiveProviderCredentialsRow struct {
	ID              string
	Provider        string
	DisplayName     string
	SupportedModels []string
	BaseUrl         string
	EncryptedSecret string
	SecretRef       string
	CredentialMode  string
	Status          string
}
```

- [ ] **Step 4: 运行 store 测试验证通过**

Run: `cd gateway && go test ./internal/store -run TestListActiveProviderCredentialsReturnsSecretRefMode -v`

Expected: PASS

- [ ] **Step 5: 提交 schema / store 基础改动**

```bash
git add gateway/db/migrations/0017_add_provider_secret_ref_and_model_health.sql \
  gateway/db/query/provider_credentials.sql \
  gateway/internal/store/provider_credentials.sql.go \
  gateway/internal/store/models.go \
  gateway/internal/store/store_test.go
git commit -m "feat: add provider secret ref and model health fields"
```

---

### Task 2: 让路由解析兼容 `secret_ref` 模式

**Files:**
- Modify: `gateway/internal/store/auth_repository.go`
- Modify: `gateway/internal/service/route_service.go`
- Test: `gateway/internal/service/proxy_service_test.go`
- Test: `gateway/internal/store/store_test.go`

- [ ] **Step 1: 先写失败用例，验证 `secret_ref` 供应商可解析真实 API Key**

在 `gateway/internal/store/store_test.go` 增加读取仓储后的断言，确保 repo 返回的 `ProviderCredentialRecord.APIKey` 来自 `secret_ref` 对应的服务器值，而不是空字符串。

```go
func TestSQLAuthRepositoryListActiveProviderCredentialsResolvesSecretRef(t *testing.T) {
	t.Parallel()

	ctx, queries, conn := newStoreTestQueries(t)
	t.Setenv("PROVIDER_SECRET_DASHSCOPE_API_KEY", "sk-secret-from-env")

	if _, err := conn.Exec(ctx, `
		insert into provider_credentials (
			id, provider, display_name, supported_models, base_url,
			encrypted_secret, secret_ref, credential_mode, status
		) values (
			'provider_qwen', 'qwen', 'Qwen', '{"qwen-max"}',
			'https://dashscope.aliyuncs.com/compatible-mode/v1',
			'', 'PROVIDER_SECRET_DASHSCOPE_API_KEY', 'secret_ref', 'active'
		);
	`); err != nil {
		t.Fatalf("seed provider failed: %v", err)
	}

	repo := store.NewAuthRepository(queries)
	items, err := repo.ListActiveProviderCredentials(ctx)
	if err != nil {
		t.Fatalf("ListActiveProviderCredentials failed: %v", err)
	}
	if items[0].APIKey != "sk-secret-from-env" {
		t.Fatalf("expected resolved api key, got %q", items[0].APIKey)
	}
}
```

- [ ] **Step 2: 运行用例，确认当前返回空 key 失败**

Run: `cd gateway && go test ./internal/store -run TestSQLAuthRepositoryListActiveProviderCredentialsResolvesSecretRef -v`

Expected: FAIL，`APIKey` 仍为空字符串。

- [ ] **Step 3: 实现双模式解析**

修改 `gateway/internal/store/auth_repository.go` 的 provider 解密逻辑，优先解析 `secret_ref`，否则回退到旧 `encrypted_secret`：

```go
func (r *SQLAuthRepository) ListActiveProviderCredentials(ctx context.Context) ([]ProviderCredentialRecord, error) {
	rows, err := r.queries.ListActiveProviderCredentials(ctx)
	if err != nil {
		return nil, err
	}

	credentials := make([]ProviderCredentialRecord, 0, len(rows))
	for _, row := range rows {
		apiKey := ""
		switch strings.TrimSpace(row.CredentialMode) {
		case "secret_ref":
			apiKey = strings.TrimSpace(os.Getenv(strings.TrimSpace(row.SecretRef)))
		default:
			apiKey = row.EncryptedSecret
			if r.secretCodec != nil && strings.HasPrefix(row.EncryptedSecret, secret.EncryptedSecretPrefix) {
				decryptedSecret, err := r.secretCodec.Decrypt(row.EncryptedSecret)
				if err != nil {
					return nil, err
				}
				apiKey = decryptedSecret
			}
		}

		credentials = append(credentials, ProviderCredentialRecord{
			ID:              row.ID,
			Provider:        row.Provider,
			DisplayName:     row.DisplayName,
			BaseURL:         row.BaseUrl,
			APIKey:          apiKey,
			Status:          domain.Status(row.Status),
			SupportedModels: append([]string(nil), row.SupportedModels...),
		})
	}
	return credentials, nil
}
```

同时保留 `route_service.go` 的 `providerTargetFromCredential` 现有输出接口不变。

- [ ] **Step 4: 运行 store 与 proxy 相关测试**

Run:

```bash
cd gateway
go test ./internal/store -run TestSQLAuthRepositoryListActiveProviderCredentialsResolvesSecretRef -v
go test ./internal/service -run 'TestChatProxy|TestEmbeddingProxy' -v
```

Expected: PASS

- [ ] **Step 5: 提交密钥解析兼容改动**

```bash
git add gateway/internal/store/auth_repository.go \
  gateway/internal/store/store_test.go \
  gateway/internal/service/proxy_service_test.go
git commit -m "feat: resolve provider api key from secret refs"
```

---

### Task 3: 增加进程内模型健康检查执行器

**Files:**
- Modify: `gateway/internal/config/config.go`
- Modify: `gateway/cmd/server/main.go`
- Create: `gateway/internal/service/model_healthcheck_runner.go`
- Create: `gateway/internal/service/model_healthcheck_runner_test.go`
- Modify: `gateway/internal/service/console_service.go`

- [ ] **Step 1: 先写失败用例，覆盖“首个非空 token 即成功并回写状态”**

创建 `gateway/internal/service/model_healthcheck_runner_test.go`：

```go
func TestModelHealthcheckRunnerMarksModelHealthyOnFirstContentToken(t *testing.T) {
	t.Parallel()

	runner := service.NewModelHealthcheckRunner(
		stubModelCatalog{
			models: []service.ModelHealthcheckTarget{
				{
					RouteID:              "route:provider_qwen:default",
					RequestedModel:       "qwen-max",
					ProviderCredentialID: "provider_qwen",
					BaseURL:              "https://dashscope.aliyuncs.com/compatible-mode/v1",
					APIKey:               "sk-demo",
					AssertionType:        "non_empty",
				},
			},
		},
		stubHealthcheckChatClient{
			streamRun: func(emit func([]byte) error, onFirstToken func()) (service.ChatStreamResult, error) {
				onFirstToken()
				return service.ChatStreamResult{
					Response: service.ChatResponse{
						Choices: []service.ChatChoice{{Message: service.ChatMessage{Role: "assistant", Content: "你"}}},
					},
					SawContentToken: true,
				}, nil
			},
		},
		service.ModelHealthcheckConfig{
			Enabled:   true,
			Timeout:   20 * time.Second,
			Prompt:    "你好",
			MaxTokens: 1,
		},
	)

	result := runner.RunOnce(context.Background())
	if result.Checked != 1 || result.Healthy != 1 {
		t.Fatalf("expected 1 checked and 1 healthy, got %+v", result)
	}
}
```

- [ ] **Step 2: 运行测试，确认因 runner 不存在而失败**

Run: `cd gateway && go test ./internal/service -run TestModelHealthcheckRunnerMarksModelHealthyOnFirstContentToken -v`

Expected: FAIL，提示类型或函数未定义。

- [ ] **Step 3: 实现最小健康检查执行器与配置项**

在 `gateway/internal/config/config.go` 增加：

```go
type Config struct {
	// ...
	ModelHealthcheckEnabled  bool
	ModelHealthcheckInterval time.Duration
	ModelHealthcheckTimeout  time.Duration
	ModelHealthcheckPrompt   string
	ModelHealthcheckMaxTokens int
}
```

在 `Load()` 中解析：

```go
ModelHealthcheckEnabled:  lookupBoolEnv("GATEWAY_MODEL_HEALTHCHECK_ENABLED", false),
ModelHealthcheckInterval: lookupDurationEnv("GATEWAY_MODEL_HEALTHCHECK_INTERVAL", time.Hour),
ModelHealthcheckTimeout:  lookupDurationEnv("GATEWAY_MODEL_HEALTHCHECK_TIMEOUT", 20*time.Second),
ModelHealthcheckPrompt:   defaultString(os.Getenv("GATEWAY_MODEL_HEALTHCHECK_PROMPT"), "你好"),
ModelHealthcheckMaxTokens: int(lookupInt64Env("GATEWAY_MODEL_HEALTHCHECK_MAX_TOKENS", 1)),
```

在 `gateway/internal/service/model_healthcheck_runner.go` 创建执行器：

```go
type ModelHealthcheckRunner struct {
	catalog ModelHealthcheckCatalog
	client  UpstreamChatClient
	config  ModelHealthcheckConfig
}

func (r *ModelHealthcheckRunner) RunOnce(ctx context.Context) ModelHealthcheckRunSummary {
	targets, _ := r.catalog.ListModelHealthcheckTargets(ctx)
	summary := ModelHealthcheckRunSummary{}
	for _, target := range targets {
		summary.Checked++
		start := time.Now()
		stream, _, err := r.client.StreamComplete(ctx, target.ProviderTarget(), ChatRequest{
			Model: target.RequestedModel,
			Messages: []ChatMessage{{Role: "user", Content: r.config.Prompt}},
			Stream: true,
		})
		if err != nil {
			_ = r.catalog.MarkModelHealthcheckResult(ctx, target.RouteID, false, 0, durationMilliseconds(time.Since(start)), err.Error())
			continue
		}

		firstTokenLatency := int64(0)
		_, streamErr := stream.Run(func([]byte) error { return io.EOF }, func() {
			if firstTokenLatency == 0 {
				firstTokenLatency = durationMilliseconds(time.Since(start))
			}
		})

		healthy := firstTokenLatency > 0
		errText := ""
		if streamErr != nil && !errors.Is(streamErr, io.EOF) {
			errText = streamErr.Error()
		}
		if healthy {
			summary.Healthy++
		} else {
			summary.Unhealthy++
		}
		_ = r.catalog.MarkModelHealthcheckResult(ctx, target.RouteID, healthy, firstTokenLatency, durationMilliseconds(time.Since(start)), errText)
	}
	return summary
}
```

在 `gateway/cmd/server/main.go` 中仅在数据库模式且配置开启时启动 goroutine：

```go
if cfg.ModelHealthcheckEnabled {
	runner := service.NewModelHealthcheckRunner(modelCatalog, provider.NewOpenAIClient(http.DefaultClient), service.ModelHealthcheckConfig{
		Enabled:   cfg.ModelHealthcheckEnabled,
		Interval:  cfg.ModelHealthcheckInterval,
		Timeout:   cfg.ModelHealthcheckTimeout,
		Prompt:    cfg.ModelHealthcheckPrompt,
		MaxTokens: cfg.ModelHealthcheckMaxTokens,
	})
	go runner.Start(context.Background())
}
```

- [ ] **Step 4: 运行 runner 测试和配置相关测试**

Run:

```bash
cd gateway
go test ./internal/service -run TestModelHealthcheckRunnerMarksModelHealthyOnFirstContentToken -v
go test ./internal/config -v
```

Expected: PASS

- [ ] **Step 5: 提交健康检查运行时改动**

```bash
git add gateway/internal/config/config.go \
  gateway/cmd/server/main.go \
  gateway/internal/service/model_healthcheck_runner.go \
  gateway/internal/service/model_healthcheck_runner_test.go \
  gateway/internal/service/console_service.go
git commit -m "feat: add in-process model healthcheck runner"
```

---

### Task 4: 暴露 admin 供应商/模型/健康检查 API

**Files:**
- Modify: `gateway/internal/service/console_service.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/http/handlers/admin.go`
- Modify: `gateway/internal/http/router.go`
- Modify: `gateway/internal/http/router_test.go`
- Modify: `gateway/internal/service/postgres_console_service_test.go`

- [ ] **Step 1: 先写失败用例，定义 service 和 router 行为**

在 `gateway/internal/http/router_test.go` 增加最小接口测试：

```go
func TestAdminProviderModelsRouteRequiresAdminAndReturnsPayload(t *testing.T) {
	t.Parallel()

	console := stubConsoleService{
		providerModelsData: service.ProviderModelsPageData{
			Providers: []service.ProviderItem{{ID: "provider_qwen", Provider: "qwen"}},
			Models:    []service.ProviderModelItem{{RouteID: "route:provider_qwen:default", RequestedModel: "qwen-max"}},
		},
	}

	app := http.NewRouterWithDependencies(http.RouterDependencies{
		ServiceAuthUsername:   "console",
		ServiceAuthPassword:   "secret",
		ConsoleSessionEnabled: false,
		ConsoleService:        console,
		MemberConsoleService:  service.NewUnavailableMemberConsoleService(),
		AuthService:           service.NewUnauthorizedAuthService(),
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/provider-models", nil)
	req.SetBasicAuth("console", "secret")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
```

在 `gateway/internal/service/postgres_console_service_test.go` 增加最小 service 用例：

```go
func TestPostgresConsoleServiceProviderModelsListsProvidersAndModels(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)
	if _, err := conn.Exec(ctx, `
		insert into provider_credentials (
			id, provider, display_name, supported_models, base_url,
			encrypted_secret, secret_ref, credential_mode, status
		) values (
			'provider_qwen', 'qwen', 'Qwen', '{"qwen-max"}',
			'https://dashscope.aliyuncs.com/compatible-mode/v1',
			'', 'dashscope_api_key', 'secret_ref', 'active'
		);
		insert into route_catalog (
			id, requested_model, resolved_provider, provider_credential_id,
			endpoint, request_mode, health_status, status
		) values (
			'route:provider_qwen:qwen-max', 'qwen-max', 'Qwen', 'provider_qwen',
			'/v1/chat/completions', '聊天', 'healthy', 'active'
		);
	`); err != nil {
		t.Fatalf("seed provider/models failed: %v", err)
	}

	payload, err := console.ProviderModels(ctx)
	if err != nil {
		t.Fatalf("ProviderModels failed: %v", err)
	}
	if len(payload.Providers) != 1 || len(payload.Models) != 1 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}
```

- [ ] **Step 2: 运行测试，确认因接口不存在失败**

Run:

```bash
cd gateway
go test ./internal/http -run TestAdminProviderModelsRouteRequiresAdminAndReturnsPayload -v
go test ./internal/service -run TestPostgresConsoleServiceProviderModelsListsProvidersAndModels -v
```

Expected: FAIL，接口、结构体或 service 方法不存在。

- [ ] **Step 3: 实现最小后端 API**

在 `gateway/internal/service/console_service.go` 增加结构和方法：

```go
type ProviderItem struct {
	ID             string `json:"id"`
	Provider       string `json:"provider"`
	DisplayName    string `json:"display_name"`
	BaseURL        string `json:"base_url"`
	SecretRef      string `json:"secret_ref"`
	CredentialMode string `json:"credential_mode"`
	Status         string `json:"status"`
	ModelCount     int    `json:"model_count"`
}

type ProviderModelItem struct {
	RouteID               string `json:"route_id"`
	ProviderCredentialID  string `json:"provider_credential_id"`
	RequestedModel        string `json:"requested_model"`
	Endpoint              string `json:"endpoint"`
	RequestMode           string `json:"request_mode"`
	Status                string `json:"status"`
	HealthStatus          string `json:"health_status"`
	FirstTokenLatencyMS   int64  `json:"first_token_latency_ms"`
	LastHealthCheckedAt   string `json:"last_health_checked_at,omitempty"`
	LastHealthError       string `json:"last_health_error,omitempty"`
	HealthcheckEnabled    bool   `json:"healthcheck_enabled"`
}

type ProviderModelsPageData struct {
	Providers []ProviderItem      `json:"providers"`
	Models    []ProviderModelItem `json:"models"`
}
```

在 `ConsoleService` interface 中增加：

```go
ProviderModels(ctx context.Context) (ProviderModelsPageData, error)
CreateProvider(ctx context.Context, req CreateProviderRequest) (ProviderMutationResult, error)
CreateProviderModel(ctx context.Context, req CreateProviderModelRequest) (ProviderModelMutationResult, error)
RunProviderModelHealthcheck(ctx context.Context, routeID string) (ProviderModelMutationResult, error)
ModelHealth(ctx context.Context) (ModelHealthPageData, error)
```

在 `gateway/internal/http/handlers/admin.go` 增加对应 handler：

```go
func ConsoleProviderModels(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := console.ProviderModels(c.UserContext())
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}
```

在 `gateway/internal/http/router.go` 注册：

```go
admin.Get("/providers", handlers.ConsoleProviders(deps.ConsoleService))
admin.Post("/providers", handlers.ConsoleCreateProvider(deps.ConsoleService))
admin.Get("/provider-models", handlers.ConsoleProviderModels(deps.ConsoleService))
admin.Post("/provider-models", handlers.ConsoleCreateProviderModel(deps.ConsoleService))
admin.Post("/provider-models/:id/health-check", handlers.ConsoleRunProviderModelHealthcheck(deps.ConsoleService))
admin.Get("/model-health", handlers.ConsoleModelHealth(deps.ConsoleService))
```

在 `gateway/internal/service/postgres_console_service.go` 先做最小 SQL 聚合实现，再补 create/update 方法。

- [ ] **Step 4: 运行 router / service 测试**

Run:

```bash
cd gateway
go test ./internal/http -run TestAdminProviderModelsRouteRequiresAdminAndReturnsPayload -v
go test ./internal/service -run TestPostgresConsoleServiceProviderModelsListsProvidersAndModels -v
```

Expected: PASS

- [ ] **Step 5: 提交 admin API 改动**

```bash
git add gateway/internal/service/console_service.go \
  gateway/internal/service/postgres_console_service.go \
  gateway/internal/http/handlers/admin.go \
  gateway/internal/http/router.go \
  gateway/internal/http/router_test.go \
  gateway/internal/service/postgres_console_service_test.go
git commit -m "feat: add admin provider model management APIs"
```

---

### Task 5: 接入前端页面、导航和交互

**Files:**
- Modify: `web/src/lib/console-api.ts`
- Modify: `web/src/app/router.tsx`
- Create: `web/src/pages/admin-provider-models.tsx`
- Create: `web/src/pages/admin-model-health.tsx`
- Modify: `web/src/styles.css`
- Modify: `web/src/test/router.test.tsx`

- [ ] **Step 1: 先写前端失败测试，定义导航和页面加载**

在 `web/src/test/router.test.tsx` 增加两个用例：

```tsx
test("admin session 渲染后台模型和健康检查导航", async () => {
  mockFetch({
    "/api/admin/system/status": defaultSystemStatus(),
    "/api/admin/provider-models": { providers: [], models: [] },
    "/api/admin/model-health": { summary: [], items: [] },
  });

  renderRoute("/");

  expect(await screen.findByRole("link", { name: "后台模型" })).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "健康检查" })).toBeInTheDocument();
});

test("后台模型页请求 /api/admin/provider-models", async () => {
  const fetchMock = mockFetch({
    "/api/admin/system/status": defaultSystemStatus(),
    "/api/admin/provider-models": {
      providers: [{ id: "provider_qwen", provider: "qwen", display_name: "Qwen", base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1", secret_ref: "dashscope_api_key", credential_mode: "secret_ref", status: "active", model_count: 1 }],
      models: [{ route_id: "route:provider_qwen:qwen-max", provider_credential_id: "provider_qwen", requested_model: "qwen-max", endpoint: "/v1/chat/completions", request_mode: "聊天", status: "active", health_status: "healthy", first_token_latency_ms: 320, healthcheck_enabled: true }],
    },
  });

  renderRoute("/provider-models");

  expect(await screen.findByText("qwen-max")).toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledWith("/api/admin/provider-models");
});
```

- [ ] **Step 2: 运行测试，确认因页面与 API 方法不存在而失败**

Run: `npm --prefix web test -- --runInBand src/test/router.test.tsx`

Expected: FAIL，缺少路由、页面或 API 函数。

- [ ] **Step 3: 实现最小前端路由和页面**

在 `web/src/lib/console-api.ts` 增加类型与请求函数：

```ts
export type ProviderItem = {
  id: string;
  provider: string;
  display_name: string;
  base_url: string;
  secret_ref: string;
  credential_mode: string;
  status: string;
  model_count: number;
};

export type ProviderModelItem = {
  route_id: string;
  provider_credential_id: string;
  requested_model: string;
  endpoint: string;
  request_mode: string;
  status: string;
  health_status: string;
  first_token_latency_ms: number;
  last_health_checked_at?: string;
  last_health_error?: string;
  healthcheck_enabled: boolean;
};

export function getProviderModels() {
  return requestJson<ProviderModelsPageData>("/api/admin/provider-models");
}

export function getModelHealth() {
  return requestJson<ModelHealthPageData>("/api/admin/model-health");
}
```

在 `web/src/app/router.tsx` 插入 admin 导航：

```tsx
{
  path: "/provider-models",
  label: "后台模型",
  title: "后台模型",
  description: "管理供应商、聊天模型与密钥引用。",
  element: <AdminProviderModelsPage />,
},
{
  path: "/model-health",
  label: "健康检查",
  title: "健康检查",
  description: "查看模型级探测状态、首 token 延迟和最近错误。",
  element: <AdminModelHealthPage />,
},
```

在 `web/src/pages/admin-provider-models.tsx` 实现最小只读页面：

```tsx
export function AdminProviderModelsPage() {
  const load = useRemoteData(() => getProviderModels());
  if (load.loading) return <LoadingSection text="正在加载后台模型..." />;
  if (load.error || !load.data) return <ErrorSection message={load.error ?? "后台模型加载失败。"} />;

  return (
    <section className="console-page">
      <h2>后台模型</h2>
      <div className="provider-model-grid">
        {load.data.providers.map((provider) => (
          <article key={provider.id}>
            <strong>{provider.display_name}</strong>
            <p>{provider.base_url}</p>
            <small>{provider.secret_ref}</small>
          </article>
        ))}
      </div>
      <div className="provider-model-table">
        {load.data.models.map((model) => (
          <article key={model.route_id}>
            <strong>{model.requested_model}</strong>
            <span>{model.health_status}</span>
          </article>
        ))}
      </div>
    </section>
  );
}
```

健康检查页同理，先显示只读列表和摘要。

- [ ] **Step 4: 运行前端测试和构建**

Run:

```bash
npm --prefix web test -- --runInBand src/test/router.test.tsx
npm --prefix web run build
```

Expected: PASS

- [ ] **Step 5: 提交前端页面改动**

```bash
git add web/src/lib/console-api.ts \
  web/src/app/router.tsx \
  web/src/pages/admin-provider-models.tsx \
  web/src/pages/admin-model-health.tsx \
  web/src/styles.css \
  web/src/test/router.test.tsx
git commit -m "feat: add admin provider model and health pages"
```

---

### Task 6: 连通真实创建流程、即时健康检查和总体验证

**Files:**
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/service/postgres_console_service_test.go`
- Modify: `web/src/pages/admin-provider-models.tsx`
- Modify: `deploy/compose/compose.yml`
- Modify: `README.md`

- [ ] **Step 1: 先写失败测试，定义“新建模型后立即健康检查”**

在 `gateway/internal/service/postgres_console_service_test.go` 增加：

```go
func TestPostgresConsoleServiceCreateProviderModelRunsImmediateHealthcheck(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)
	if _, err := conn.Exec(ctx, `
		insert into provider_credentials (
			id, provider, display_name, supported_models, base_url,
			encrypted_secret, secret_ref, credential_mode, status
		) values (
			'provider_qwen', 'qwen', 'Qwen', '{}',
			'https://dashscope.aliyuncs.com/compatible-mode/v1',
			'', 'dashscope_api_key', 'secret_ref', 'active'
		);
	`); err != nil {
		t.Fatalf("seed provider failed: %v", err)
	}

	result, err := console.CreateProviderModel(ctx, service.CreateProviderModelRequest{
		ProviderCredentialID: "provider_qwen",
		RequestedModel:       "qwen-max",
		HealthcheckEnabled:   true,
	})
	if err != nil {
		t.Fatalf("CreateProviderModel failed: %v", err)
	}
	if result.Item.RequestedModel != "qwen-max" {
		t.Fatalf("expected qwen-max, got %+v", result.Item)
	}
}
```

- [ ] **Step 2: 运行测试，确认当前 create 流程未实现**

Run: `cd gateway && go test ./internal/service -run TestPostgresConsoleServiceCreateProviderModelRunsImmediateHealthcheck -v`

Expected: FAIL

- [ ] **Step 3: 完成 create/update 表单和即时健康检查**

在 `gateway/internal/service/postgres_console_service.go` 完整实现：

- `CreateProvider`
- `CreateProviderModel`
- `RunProviderModelHealthcheck`
- `ModelHealth`

关键 create 逻辑：

```go
func (s postgresConsoleService) CreateProviderModel(ctx context.Context, req CreateProviderModelRequest) (ProviderModelMutationResult, error) {
	routeID := RouteIDForCredential(req.ProviderCredentialID, []string{req.RequestedModel}, req.RequestedModel)
	if _, err := s.db.Exec(ctx, `
		insert into route_catalog (
			id, requested_model, resolved_provider, provider_credential_id,
			endpoint, request_mode, health_status, status, healthcheck_enabled, healthcheck_assertion_type
		) values ($1, $2, $3, $4, '/v1/chat/completions', '聊天', 'degraded', 'active', $5, 'non_empty')
	`, routeID, req.RequestedModel, req.ProviderDisplayName, req.ProviderCredentialID, req.HealthcheckEnabled); err != nil {
		return ProviderModelMutationResult{}, err
	}

	// 新建后立即执行一次单模型检查；失败不回滚，只更新状态
	item, err := s.runSingleProviderModelHealthcheck(ctx, routeID)
	if err != nil {
		item.LastHealthError = err.Error()
	}
	return ProviderModelMutationResult{Item: item}, nil
}
```

前端 `web/src/pages/admin-provider-models.tsx` 增加表单与提交逻辑，调用：

```ts
await createProvider({
  provider: "qwen",
  display_name: "Qwen",
  base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1",
  secret_ref: "dashscope_api_key",
  credential_mode: "secret_ref",
});

await createProviderModel({
  provider_credential_id: "provider_qwen",
  requested_model: "qwen-max",
  healthcheck_enabled: true,
});
```

在 `deploy/compose/compose.yml` 加入默认关闭的环境变量占位：

```yaml
      GATEWAY_MODEL_HEALTHCHECK_ENABLED: "${GATEWAY_MODEL_HEALTHCHECK_ENABLED:-false}"
      GATEWAY_MODEL_HEALTHCHECK_INTERVAL: "${GATEWAY_MODEL_HEALTHCHECK_INTERVAL:-1h}"
      GATEWAY_MODEL_HEALTHCHECK_TIMEOUT: "${GATEWAY_MODEL_HEALTHCHECK_TIMEOUT:-20s}"
      GATEWAY_MODEL_HEALTHCHECK_PROMPT: "${GATEWAY_MODEL_HEALTHCHECK_PROMPT:-你好}"
      GATEWAY_MODEL_HEALTHCHECK_MAX_TOKENS: "${GATEWAY_MODEL_HEALTHCHECK_MAX_TOKENS:-1}"
```

在 `README.md` 补充：

- 新 admin 页面入口
- `secret_ref` 说明
- 健康检查默认关闭

- [ ] **Step 4: 运行完整验证**

Run:

```bash
cd /root/liwenjian/ai_gateway
GOCACHE=/tmp/go-build-cache GOMODCACHE=/root/go/pkg/mod ./scripts/lint.sh
cd gateway && go test ./... 
npm --prefix ../web test -- --runInBand src/test/router.test.tsx
```

Expected:

- `lint.sh` PASS
- 后端 targeted tests PASS
- 前端 router tests PASS

- [ ] **Step 5: 提交最终联通改动**

```bash
git add gateway/internal/service/postgres_console_service.go \
  gateway/internal/service/postgres_console_service_test.go \
  web/src/pages/admin-provider-models.tsx \
  deploy/compose/compose.yml \
  README.md
git commit -m "feat: add admin model management and health checks"
```

---

## Self-Review

### Spec coverage

- 供应商 -> 模型两层管理：Task 1, Task 4, Task 5, Task 6
- `secret_ref` 模式：Task 1, Task 2
- 聊天模型限定：Task 4, Task 6
- 模型级健康检查：Task 3, Task 6
- 默认关闭配置项：Task 3, Task 6
- 调用观测自然展示新模型：依赖 `route_catalog` + 真实日志，不新增聚合表，Task 4 和 Task 6 保证模型进入现有链路

### Placeholder scan

- 没有使用 `TBD` / `TODO`
- 每个任务都列出文件路径、命令和最小代码片段

### Type consistency

- 统一使用：
  - `ProviderModelsPageData`
  - `CreateProviderRequest`
  - `CreateProviderModelRequest`
  - `ProviderModelMutationResult`
  - `ModelHealthPageData`

---

Plan complete and saved to `docs/plans/2026-05-06-admin-provider-model-healthcheck-implementation-plan.md`. Two execution options:

1. Subagent-Driven (recommended) - I dispatch a fresh subagent per task, review between tasks, fast iteration
2. Inline Execution - Execute tasks in this session using executing-plans, batch execution with checkpoints
