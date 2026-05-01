# Qwen 与 Xiaomi MIMO 双 Provider 接入 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在保留 Qwen 的前提下新增 Xiaomi MIMO provider，将复杂 chat 请求路由到 `mimo-v2.5-pro`，并让 admin 路由观测页按 `Qwen` 与 `MIMO` 两组展示真实调用数据，同时确保真实 API Key 只通过 secret file 安全注入。

**Architecture:** 后端继续复用现有 OpenAI-compatible provider client，不新增独立 SDK；重点改造配置与 seed 结构，使数据库模式能够同时种入 Qwen 与 MIMO 两条 provider credential。观测层继续使用真实 `route_catalog`、`llm_request_logs` 与 usage 聚合数据，在 `/api/admin/routes` 返回 provider 分组结构，前端按两块列表渲染；member 页面不显式暴露 MIMO 品牌或模型名。

**Tech Stack:** Go, Fiber, PostgreSQL, Redis, Docker Compose, React, Vitest

---

## 文件结构

本轮会触达这些文件，并保持责任边界清晰：

- `gateway/internal/config/config.go`
  - 读取双 provider seed 配置与 MIMO 相关环境变量
- `gateway/internal/config/config_test.go`
  - 覆盖新配置默认值与 `_FILE` 注入
- `gateway/db/runtime.go`
  - 从单 provider seed 扩成双 provider seed
- `gateway/db/runtime_test.go`
  - 校验 Qwen + MIMO 两条 provider credential 都被正确落库
- `gateway/cmd/server/main.go`
  - 将双 provider seed 配置传给 `SeedDemoData`
- `gateway/cmd/server/main_test.go`
  - 校验数据库模式下 `chat` 能命中 MIMO，`embeddings` 仍走 Qwen
- `gateway/internal/service/console_service.go`
  - 扩展 routes 页面数据结构，支持 provider 分组
- `gateway/internal/service/postgres_console_service.go`
  - `/api/admin/routes` 查询与组装逻辑改成按 provider 分组
- `gateway/internal/service/postgres_console_service_test.go`
  - 覆盖 Qwen / MIMO 分组展示与无数据分组场景
- `web/src/lib/console-api.ts`
  - 解析新的 provider 分组 routes 结构
- `web/src/pages/routes.tsx`
  - admin 路由页改成 `Qwen 路由观测` / `MIMO 路由观测`
- `web/src/test/router.test.tsx`
  - 覆盖两组路由观测展示，且 member 侧不暴露 MIMO
- `deploy/compose/compose.yml`
  - 挂载 `mimo_api_key`，注入 MIMO 相关配置
- `deploy/compose/.env.example`
  - 只提供 MIMO 相关占位配置，不写真实 key
- `README.md`
  - 说明 secret file 结构、默认角色分工与验证命令

## Task 1: 双 Provider 配置与安全注入

**Files:**
- Modify: `gateway/internal/config/config.go`
- Modify: `gateway/internal/config/config_test.go`
- Modify: `deploy/compose/compose.yml`
- Modify: `deploy/compose/.env.example`
- Test: `gateway/internal/config/config_test.go`

- [ ] **Step 1: 先写配置层红灯测试**

在 [`gateway/internal/config/config_test.go`](/root/liwenjian/ai_gateway/gateway/internal/config/config_test.go) 新增测试，要求 `Load()` 在未显式设置时就能给出双 provider 默认值，并能从 `_FILE` 读取 MIMO key：

```go
func TestLoadDefaultsMIMOSeedProvider(t *testing.T) {
	t.Setenv("GATEWAY_MIMO_PROVIDER_API_KEY", "")
	t.Setenv("GATEWAY_MIMO_PROVIDER_BASE_URL", "")
	t.Setenv("GATEWAY_MIMO_PROVIDER_DISPLAY_NAME", "")
	t.Setenv("GATEWAY_CHAT_REASONING_MODEL", "")

	cfg := Load()

	if cfg.MIMOProviderBaseURL != "https://api.xiaomimimo.com/v1" {
		t.Fatalf("expected MIMO base URL %q, got %q", "https://api.xiaomimimo.com/v1", cfg.MIMOProviderBaseURL)
	}
	if cfg.MIMOProviderDisplayName != "Xiaomi MIMO" {
		t.Fatalf("expected MIMO display name %q, got %q", "Xiaomi MIMO", cfg.MIMOProviderDisplayName)
	}
	if cfg.ChatReasoningModel != "mimo-v2.5-pro" {
		t.Fatalf("expected reasoning model %q, got %q", "mimo-v2.5-pro", cfg.ChatReasoningModel)
	}
}

func TestLoadReadsMIMOAPIKeyFromFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "mimo_api_key")
	if err := os.WriteFile(keyPath, []byte("file-secret-key"), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	t.Setenv("GATEWAY_MIMO_PROVIDER_API_KEY_FILE", keyPath)

	cfg := Load()

	if cfg.MIMOProviderAPIKey != "file-secret-key" {
		t.Fatalf("expected MIMO provider key %q, got %q", "file-secret-key", cfg.MIMOProviderAPIKey)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/config -run 'TestLoadDefaultsMIMOSeedProvider|TestLoadReadsMIMOAPIKeyFromFile' -count=1`

Expected: FAIL，提示 `Config` 结构缺少 MIMO 字段或 `Load()` 未给默认值。

- [ ] **Step 3: 最小实现配置字段**

在 [`gateway/internal/config/config.go`](/root/liwenjian/ai_gateway/gateway/internal/config/config.go) 为 `Config` 增加以下字段，并在 `Load()` 中读取：

```go
type Config struct {
	// existing fields...
	MIMOProviderBaseURL     string
	MIMOProviderAPIKey      string
	MIMOProviderDisplayName string
}

func Load() Config {
	// existing logic...
	return Config{
		// existing fields...
		MIMOProviderBaseURL:     defaultString(lookupEnv("GATEWAY_MIMO_PROVIDER_BASE_URL"), "https://api.xiaomimimo.com/v1"),
		MIMOProviderAPIKey:      lookupEnv("GATEWAY_MIMO_PROVIDER_API_KEY"),
		MIMOProviderDisplayName: defaultString(lookupEnv("GATEWAY_MIMO_PROVIDER_DISPLAY_NAME"), "Xiaomi MIMO"),
		ChatReasoningModel:      defaultString(os.Getenv("GATEWAY_CHAT_REASONING_MODEL"), "mimo-v2.5-pro"),
	}
}
```

- [ ] **Step 4: 补 Compose 与 `.env.example` 占位配置**

在 [`deploy/compose/compose.yml`](/root/liwenjian/ai_gateway/deploy/compose/compose.yml) 增加 MIMO 相关环境变量与 secret mount：

```yaml
environment:
  GATEWAY_MIMO_PROVIDER_BASE_URL: "${GATEWAY_MIMO_PROVIDER_BASE_URL:-https://api.xiaomimimo.com/v1}"
  GATEWAY_MIMO_PROVIDER_DISPLAY_NAME: "${GATEWAY_MIMO_PROVIDER_DISPLAY_NAME:-Xiaomi MIMO}"
  GATEWAY_MIMO_PROVIDER_API_KEY_FILE: "/run/secrets/mimo_api_key"
volumes:
  - ${AI_GATEWAY_SECRET_DIR:-../../.ai_gateway_secrets}/mimo_api_key:/run/secrets/mimo_api_key:ro
```

在 [`deploy/compose/.env.example`](/root/liwenjian/ai_gateway/deploy/compose/.env.example) 只增加非敏感占位：

```dotenv
GATEWAY_MIMO_PROVIDER_BASE_URL=https://api.xiaomimimo.com/v1
GATEWAY_MIMO_PROVIDER_DISPLAY_NAME=Xiaomi MIMO
GATEWAY_CHAT_REASONING_MODEL=mimo-v2.5-pro
```

- [ ] **Step 5: 运行测试验证通过**

Run: `go test ./internal/config -run 'TestLoadDefaultsMIMOSeedProvider|TestLoadReadsMIMOAPIKeyFromFile' -count=1`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add gateway/internal/config/config.go gateway/internal/config/config_test.go deploy/compose/compose.yml deploy/compose/.env.example
git commit -m "feat: add mimo provider config and secret wiring"
```

## Task 2: 数据库模式 seed 扩成双 Provider

**Files:**
- Modify: `gateway/db/runtime.go`
- Modify: `gateway/db/runtime_test.go`
- Modify: `gateway/cmd/server/main.go`
- Test: `gateway/db/runtime_test.go`

- [ ] **Step 1: 先写双 provider seed 红灯测试**

在 [`gateway/db/runtime_test.go`](/root/liwenjian/ai_gateway/gateway/db/runtime_test.go) 增加测试，要求 `SeedDemoData` 能同时落两条 provider credential：

```go
func TestSeedDemoDataSeedsQwenAndMIMOProviders(t *testing.T) {
	ctx := context.Background()
	conn := openTestDatabase(t)
	codec := mustTestCodec(t)

	err := SeedDemoData(ctx, conn, SeedConfig{
		PlatformAPIKey:          "platform-live-key",
		QwenProviderBaseURL:     "https://dashscope.aliyuncs.com/compatible-mode/v1",
		QwenProviderAPIKey:      "qwen-provider-secret",
		QwenProvider:            "dashscope",
		QwenProviderDisplayName: "Qwen Primary",
		MIMOProviderBaseURL:     "https://api.xiaomimimo.com/v1",
		MIMOProviderAPIKey:      "mimo-provider-secret",
		MIMOProvider:            "mimo",
		MIMOProviderDisplayName: "Xiaomi MIMO",
		SecretCodec:             codec,
		PlatformKeyCodec:        codec,
	})
	if err != nil {
		t.Fatalf("SeedDemoData failed: %v", err)
	}

	rows, err := conn.Query(ctx, `select id, provider, display_name, supported_models from provider_credentials where provider in ('dashscope', 'mimo') order by provider asc`)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	defer rows.Close()
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./db -run TestSeedDemoDataSeedsQwenAndMIMOProviders -count=1`

Expected: FAIL，提示 `SeedConfig` 缺字段或只落一条 provider。

- [ ] **Step 3: 扩 `SeedConfig` 与 seed 逻辑**

把 [`gateway/db/runtime.go`](/root/liwenjian/ai_gateway/gateway/db/runtime.go) 的单 provider seed 改成双 provider 结构：

```go
type SeedConfig struct {
	PlatformAPIKey           string
	QwenProviderBaseURL      string
	QwenProviderAPIKey       string
	QwenProvider             string
	QwenProviderDisplayName  string
	MIMOProviderBaseURL      string
	MIMOProviderAPIKey       string
	MIMOProvider             string
	MIMOProviderDisplayName  string
	SecretCodec              *secret.Codec
	PlatformKeyCodec         *secret.Codec
	AdminPassword            string
	MemberPassword           string
}
```

provider insert 逻辑改成一次写入 Qwen、MIMO、RAG 三条：

```sql
insert into provider_credentials (id, provider, display_name, encrypted_secret, status, supported_models, base_url) values
  ('provider_qwen_primary', 'dashscope', 'Qwen Primary', $1, 'active', '{"qwen-flash","text-embedding-v4"}', 'https://dashscope.aliyuncs.com/compatible-mode/v1'),
  ('provider_mimo_primary', 'mimo', 'Xiaomi MIMO', $2, 'active', '{"mimo-v2.5-pro"}', 'https://api.xiaomimimo.com/v1'),
  ('provider_rag_service', 'rag', '知识库检索服务', $3, 'active', '{"rag-query"}', 'http://rag-service:8000')
on conflict (id) do update set
  provider = excluded.provider,
  display_name = excluded.display_name,
  encrypted_secret = excluded.encrypted_secret,
  status = excluded.status,
  supported_models = excluded.supported_models,
  base_url = excluded.base_url;
```

- [ ] **Step 4: 在 `newDatabaseBackedServerApp` 传入双 provider seed**

更新 [`gateway/cmd/server/main.go`](/root/liwenjian/ai_gateway/gateway/cmd/server/main.go) 的 `SeedDemoData` 调用：

```go
if err := gatewaydb.SeedDemoData(ctx, pool, gatewaydb.SeedConfig{
	PlatformAPIKey:          cfg.SeedPlatformAPIKey,
	QwenProviderBaseURL:     cfg.SeedProviderBaseURL,
	QwenProviderAPIKey:      cfg.SeedProviderAPIKey,
	QwenProvider:            cfg.SeedProvider,
	QwenProviderDisplayName: cfg.SeedProviderDisplayName,
	MIMOProviderBaseURL:     cfg.MIMOProviderBaseURL,
	MIMOProviderAPIKey:      cfg.MIMOProviderAPIKey,
	MIMOProvider:            "mimo",
	MIMOProviderDisplayName: cfg.MIMOProviderDisplayName,
	SecretCodec:             providerSecretCodec,
	PlatformKeyCodec:        platformAPIKeySecretCodec,
	AdminPassword:           cfg.SeedAdminPassword,
	MemberPassword:          cfg.SeedMemberPassword,
}); err != nil {
	panic(err)
}
```

- [ ] **Step 5: 运行测试验证通过**

Run: `go test ./db -run TestSeedDemoDataSeedsQwenAndMIMOProviders -count=1`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add gateway/db/runtime.go gateway/db/runtime_test.go gateway/cmd/server/main.go
git commit -m "feat: seed qwen and mimo providers in database mode"
```

## Task 3: 路由与代理行为切到 MIMO reasoning，并保持 embeddings 走 Qwen

**Files:**
- Modify: `gateway/cmd/server/main_test.go`
- Modify: `gateway/tests/integration/proxy_test.go`
- Test: `gateway/cmd/server/main_test.go`
- Test: `gateway/tests/integration/proxy_test.go`

- [ ] **Step 1: 先写红灯测试**

在 [`gateway/tests/integration/proxy_test.go`](/root/liwenjian/ai_gateway/gateway/tests/integration/proxy_test.go) 增加两条测试：

```go
func TestSmartRoutingUsesMIMOForComplexChat(t *testing.T) {
	// provider server 记录上游 model
	// 请求内容包含 debug + code fence
	// 断言上游收到 model == "mimo-v2.5-pro"
}

func TestEmbeddingsStillUseQwenProvider(t *testing.T) {
	// embeddings 请求
	// 断言 provider credential id == "provider_qwen_primary"
}
```

数据库模式测试也要补 reasoning 默认值：

```go
app := newServerApp(config.Config{
	// existing fields...
	ChatFastModel:      "qwen-flash",
	ChatReasoningModel: "mimo-v2.5-pro",
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./cmd/server ./tests/integration -run 'TestSmartRoutingUsesMIMOForComplexChat|TestEmbeddingsStillUseQwenProvider' -count=1`

Expected: FAIL，复杂 chat 仍走旧 reasoning 模型或 embeddings 路由未区分 Qwen / MIMO。

- [ ] **Step 3: 最小实现与测试 helper 调整**

更新测试 helper 的 provider credential 列表，显式放两条 provider：

```go
credentials := []store.ProviderCredentialRecord{
	{
		ID:              "provider_qwen_primary",
		Provider:        "dashscope",
		DisplayName:     "Qwen Primary",
		BaseURL:         providerBaseURL,
		APIKey:          "provider-secret-key",
		SupportedModels: []string{"qwen-flash", "text-embedding-v4"},
	},
	{
		ID:              "provider_mimo_primary",
		Provider:        "mimo",
		DisplayName:     "Xiaomi MIMO",
		BaseURL:         providerBaseURL,
		APIKey:          "provider-secret-key",
		SupportedModels: []string{"mimo-v2.5-pro"},
	},
}
```

并让 smart routing helper 使用：

```go
service.NewRuleBasedSmartRouter(service.SmartRoutingConfig{
	FastModelTier:      "qwen-flash",
	ReasoningModelTier: "mimo-v2.5-pro",
})
```

- [ ] **Step 4: 运行测试验证通过**

Run: `go test ./cmd/server ./tests/integration -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gateway/cmd/server/main_test.go gateway/tests/integration/proxy_test.go
git commit -m "feat: route complex chat to mimo and keep embeddings on qwen"
```

## Task 4: `/api/admin/routes` 后端改成按 Provider 分组

**Files:**
- Modify: `gateway/internal/service/console_service.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/service/postgres_console_service_test.go`
- Test: `gateway/internal/service/postgres_console_service_test.go`

- [ ] **Step 1: 先写后端红灯测试**

在 [`gateway/internal/service/postgres_console_service_test.go`](/root/liwenjian/ai_gateway/gateway/internal/service/postgres_console_service_test.go) 增加测试，要求 routes 接口返回 `qwen_groups` 与 `mimo_groups`：

```go
func TestRoutesGroupsQwenAndMIMOSeparately(t *testing.T) {
	// 准备 route_catalog + provider_credentials 测试数据
	// Qwen: qwen-flash
	// MIMO: mimo-v2.5-pro

	payload, err := service.Routes(ctx)
	if err != nil {
		t.Fatalf("Routes failed: %v", err)
	}

	if len(payload.QwenGroups) != 1 {
		t.Fatalf("expected 1 qwen group, got %d", len(payload.QwenGroups))
	}
	if len(payload.MIMOGroups) != 1 {
		t.Fatalf("expected 1 mimo group, got %d", len(payload.MIMOGroups))
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/service -run TestRoutesGroupsQwenAndMIMOSeparately -count=1`

Expected: FAIL，`RoutesPageData` 还没有分组字段。

- [ ] **Step 3: 扩展 routes 数据结构**

在 [`gateway/internal/service/console_service.go`](/root/liwenjian/ai_gateway/gateway/internal/service/console_service.go) 增加：

```go
type RouteGroup struct {
	Title string      `json:"title"`
	Items []RouteItem `json:"items"`
}

type RoutesPageData struct {
	Stats         []RouteMetric `json:"stats"`
	QwenGroups    []RouteGroup  `json:"qwen_groups"`
	MIMOGroups    []RouteGroup  `json:"mimo_groups"`
	PolicySummary []string      `json:"policy_summary"`
}
```

- [ ] **Step 4: 改 `postgresConsoleService.Routes` 组装逻辑**

在 [`gateway/internal/service/postgres_console_service.go`](/root/liwenjian/ai_gateway/gateway/internal/service/postgres_console_service.go) 按 provider 归组：

```go
qwenItems := make([]RouteItem, 0)
mimoItems := make([]RouteItem, 0)

for rows.Next() {
	var provider string
	var item RouteItem
	// scan requested_model, resolved_provider, provider_credential_id, latency_ms, health_status
	if strings.Contains(strings.ToLower(provider), "mimo") {
		mimoItems = append(mimoItems, item)
		continue
	}
	qwenItems = append(qwenItems, item)
}

return RoutesPageData{
	Stats: stats,
	QwenGroups: []RouteGroup{{Title: "Qwen 路由观测", Items: qwenItems}},
	MIMOGroups: []RouteGroup{{Title: "MIMO 路由观测", Items: mimoItems}},
	PolicySummary: policySummary,
}, nil
```

- [ ] **Step 5: 运行测试验证通过**

Run: `go test ./internal/service -run TestRoutesGroupsQwenAndMIMOSeparately -count=1`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add gateway/internal/service/console_service.go gateway/internal/service/postgres_console_service.go gateway/internal/service/postgres_console_service_test.go
git commit -m "feat: group admin routes by qwen and mimo"
```

## Task 5: admin 路由观测页前端拆成 Qwen / MIMO 两块

**Files:**
- Modify: `web/src/lib/console-api.ts`
- Modify: `web/src/pages/routes.tsx`
- Modify: `web/src/test/router.test.tsx`
- Test: `web/src/test/router.test.tsx`

- [ ] **Step 1: 先写前端红灯测试**

在 [`web/src/test/router.test.tsx`](/root/liwenjian/ai_gateway/web/src/test/router.test.tsx) 改 `/api/admin/routes` mock，并断言两组：

```tsx
test("路由页按 Qwen 与 MIMO 两组展示真实调用数据", async () => {
  mockFetch({
    "/api/admin/routes": {
      stats: [{ label: "启用供应商", value: "5" }],
      qwen_groups: [{ title: "Qwen 路由观测", items: [{ requested_model: "qwen-flash", route_label: "default-route", credential: "Qwen Primary", latency: "218 ms", status: "健康" }] }],
      mimo_groups: [{ title: "MIMO 路由观测", items: [{ requested_model: "mimo-v2.5-pro", route_label: "reasoning-route", credential: "Xiaomi MIMO", latency: "322 ms", status: "健康" }] }],
      policy_summary: ["模型优先解析已启用。"],
    },
  });

  renderRoute("/routes");

  expect(await screen.findByText("Qwen 路由观测")).toBeInTheDocument();
  expect(screen.getByText("MIMO 路由观测")).toBeInTheDocument();
  expect(screen.getByText("qwen-flash")).toBeInTheDocument();
  expect(screen.getByText("mimo-v2.5-pro")).toBeInTheDocument();
});
```

- [ ] **Step 2: 运行测试确认失败**

Run: `npm --prefix web test -- --runInBand src/test/router.test.tsx -t '路由页按 Qwen 与 MIMO 两组展示真实调用数据'`

Expected: FAIL，前端类型与页面仍只支持单个 `items` 表。

- [ ] **Step 3: 扩展前端类型与页面**

在 [`web/src/lib/console-api.ts`](/root/liwenjian/ai_gateway/web/src/lib/console-api.ts) 增加：

```ts
export type RouteGroup = {
  title: string;
  items: RouteItem[];
};

export type RoutesPageData = {
  stats: RouteMetric[];
  qwen_groups: RouteGroup[];
  mimo_groups: RouteGroup[];
  policy_summary: string[];
};
```

在 [`web/src/pages/routes.tsx`](/root/liwenjian/ai_gateway/web/src/pages/routes.tsx) 改成两块 section：

```tsx
{data.qwen_groups.map((group) => (
  <section key={group.title} className="section-card">
    <h2>{group.title}</h2>
    <DataTable ... rows={group.items.map(...)} />
  </section>
))}
{data.mimo_groups.map((group) => (
  <section key={group.title} className="section-card">
    <h2>{group.title}</h2>
    <DataTable ... rows={group.items.map(...)} />
  </section>
))}
```

并在空数据时显示：

```tsx
<p>暂无真实调用数据</p>
```

- [ ] **Step 4: 运行测试验证通过**

Run: `npm --prefix web test -- --runInBand src/test/router.test.tsx`

Expected: PASS

- [ ] **Step 5: 运行构建**

Run: `npm --prefix web run build`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/console-api.ts web/src/pages/routes.tsx web/src/test/router.test.tsx
git commit -m "feat: split admin routes page into qwen and mimo groups"
```

## Task 6: 文档、安全扫描与部署验证

**Files:**
- Modify: `README.md`
- Modify: `docs/specs/2026-05-01-mimo-and-qwen-multi-provider-routing-design.md`

- [ ] **Step 1: 更新 README**

在 [`README.md`](/root/liwenjian/ai_gateway/README.md) 增加：

```md
## 双 Provider 说明

- Qwen 负责快模型 chat 与 embeddings
- Xiaomi MIMO 负责强模型 chat
- admin 路由观测页按 Qwen / MIMO 两组展示

## Secret 文件

- `${AI_GATEWAY_SECRET_DIR}/dashscope_api_key`
- `${AI_GATEWAY_SECRET_DIR}/mimo_api_key`
- `${AI_GATEWAY_SECRET_DIR}/provider_master_key`
```

并加入验证命令：

```bash
curl -sS http://127.0.0.1:32658/v1/chat/completions \
  -H "Authorization: Bearer <platform-key>" \
  -H "Content-Type: application/json" \
  --data '{"model":"qwen-flash","messages":[{"role":"user","content":"请帮我 debug 这段 panic 代码 ```go\npanic(\"x\")\n```"}]}'
```

- [ ] **Step 2: 全量测试与安全扫描**

Run:

```bash
./scripts/test.sh
./scripts/lint.sh
rg -n "sk-[a-zA-Z0-9]" . --glob '!docs/specs/*.md' --glob '!docs/plans/*.md'
docker compose --env-file deploy/compose/.env.example -f deploy/compose/compose.yml config >/tmp/compose-mimo.out
```

Expected:

- 测试通过
- lint 通过
- `rg` 不应命中任何真实 key
- compose 输出包含 `mimo_api_key` mount 与 MIMO 环境变量引用

- [ ] **Step 3: 本地拉起与真实验证**

Run:

```bash
docker compose --env-file deploy/compose/.env.local -f deploy/compose/compose.yml up -d --build
curl http://127.0.0.1:32658/healthz
curl -I http://127.0.0.1:31873/routes
```

然后使用 admin session 检查：

```bash
curl -u <service-user>:<service-pass> \
  -H "X-Console-Session: ${SESSION_TOKEN}" \
  http://127.0.0.1:32658/admin/routes
```

Expected:

- `admin/routes` 返回 `qwen_groups`
- `admin/routes` 返回 `mimo_groups`

- [ ] **Step 4: Commit**

```bash
git add README.md docs/specs/2026-05-01-mimo-and-qwen-multi-provider-routing-design.md
git commit -m "docs: document mimo and qwen multi provider rollout"
```

## 自检结论

- spec coverage:
  - 双 provider 安全注入：Task 1
  - 双 provider seed：Task 2
  - chat 走 MIMO / embeddings 走 Qwen：Task 3
  - admin routes 按 provider 分组：Task 4
  - admin 前端两块列表：Task 5
  - 文档、安全扫描、部署验证：Task 6
- placeholder scan:
  - 无 `TODO`、`TBD`、`implement later`
- type consistency:
  - 后端与前端统一使用 `qwen_groups`、`mimo_groups`
  - chat reasoning 模型统一使用 `mimo-v2.5-pro`
  - embeddings 继续声明为 Qwen 线路
