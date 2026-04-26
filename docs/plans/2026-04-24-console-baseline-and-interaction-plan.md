# Console Baseline And Interaction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 AI Gateway 控制台满足“唯一正式入口 + 所有可见交互真实联动”的基线要求，并隐藏暂不开放的 RAG 前端能力。

**Architecture:** 后端新增一组轻量控制台状态与 API Key 管理接口，继续挂在现有 `/admin` Basic Auth 体系下；前端围绕 `layout + router + api-keys page + console-api` 做最小改造，移除 `知识库` 导航，新增状态弹层、系统状态读取和 API Key 生命周期弹窗。测试遵循 TDD，优先补现有 `router.test.tsx` 与 Go router/service/store 测试，不做超出本轮范围的 UI 大重构。

**Tech Stack:** React + React Router + Vitest + Fiber + Go + PostgreSQL + 现有 `store/auth_repository` / `service/postgres_console_service`

---

## File Map

### Frontend

- Modify: `/root/liwenjian/ai_gateway/web/src/app/router.tsx`
  - 移除 `知识库` 导航项
- Modify: `/root/liwenjian/ai_gateway/web/src/app/layout.tsx`
  - 左侧状态入口与顶部状态 badge 改为真实交互
- Modify: `/root/liwenjian/ai_gateway/web/src/lib/console-api.ts`
  - 新增系统状态与 API Key 管理请求
- Modify: `/root/liwenjian/ai_gateway/web/src/pages/api-keys.tsx`
  - 行选择、弹窗、创建/轮换/停用闭环
- Modify: `/root/liwenjian/ai_gateway/web/src/styles.css`
  - 状态入口、弹层、API Key 操作区样式
- Modify: `/root/liwenjian/ai_gateway/web/src/test/router.test.tsx`
  - 导航隐藏、状态入口、API Key 生命周期、回归测试

### Backend

- Modify: `/root/liwenjian/ai_gateway/gateway/internal/service/console_service.go`
  - 增加系统状态 DTO 与 API Key 管理方法
- Modify: `/root/liwenjian/ai_gateway/gateway/internal/service/postgres_console_service.go`
  - 实现系统状态读取与 API Key 创建/轮换/停用
- Modify: `/root/liwenjian/ai_gateway/gateway/internal/http/handlers/admin.go`
  - 新增系统状态与 API Key 管理 handler
- Modify: `/root/liwenjian/ai_gateway/gateway/internal/http/router.go`
  - 注册新增 `/admin/system/status` 和 `/admin/api-keys/*` 路由
- Modify: `/root/liwenjian/ai_gateway/gateway/internal/store/auth_repository.go`
  - 补充平台 key 管理查询/写入接口
- Modify: `/root/liwenjian/ai_gateway/gateway/internal/http/router_test.go`
  - 路由、鉴权、query/body、错误码测试
- Modify: `/root/liwenjian/ai_gateway/gateway/internal/service/postgres_console_service_test.go`
  - 系统状态与 API Key 生命周期测试
- Modify: `/root/liwenjian/ai_gateway/gateway/internal/store/auth_repository_test.go`
  - 创建/轮换/停用持久化测试

### Optional migration

- Create if needed: `/root/liwenjian/ai_gateway/gateway/db/migrations/0007_extend_platform_api_keys_for_example-console-user.sql`
  - 仅当现有表结构无法支持轮换时间、停用状态或额外元数据时新增

---

### Task 1: Hide RAG Frontend Entry And Wire Console Status API

**Files:**
- Modify: `/root/liwenjian/ai_gateway/web/src/app/router.tsx`
- Modify: `/root/liwenjian/ai_gateway/web/src/app/layout.tsx`
- Modify: `/root/liwenjian/ai_gateway/web/src/lib/console-api.ts`
- Modify: `/root/liwenjian/ai_gateway/web/src/test/router.test.tsx`
- Modify: `/root/liwenjian/ai_gateway/web/src/styles.css`
- Modify: `/root/liwenjian/ai_gateway/gateway/internal/service/console_service.go`
- Modify: `/root/liwenjian/ai_gateway/gateway/internal/service/postgres_console_service.go`
- Modify: `/root/liwenjian/ai_gateway/gateway/internal/http/handlers/admin.go`
- Modify: `/root/liwenjian/ai_gateway/gateway/internal/http/router.go`
- Test: `/root/liwenjian/ai_gateway/gateway/internal/http/router_test.go`
- Test: `/root/liwenjian/ai_gateway/gateway/internal/service/postgres_console_service_test.go`

- [ ] **Step 1: Write the failing frontend tests for hidden knowledge-base nav and clickable status entries**

```tsx
test("控制台导航隐藏知识库入口", async () => {
  mockFetch({
    "/api/admin/overview": {
      stats: [],
      route_health: [],
      top_models: [],
      recent_alerts: [],
      audit_snapshot: [],
    },
    "/api/admin/system/status": {
      console_stage: "控制台预览版",
      run_mode: "数据库模式",
      gateway_health: "健康",
      quota_protection: "已启用",
      console_entry: "31873",
      gateway_admin_api: "32658",
      internal_services: ["31427"],
      hidden_modules: ["RAG 控制台", "知识库"],
    },
  });

  renderRoute("/");

  expect(await screen.findByRole("heading", { level: 1, name: "总览" })).toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "知识库" })).not.toBeInTheDocument();
});

test("左侧状态入口点击后展示真实系统状态", async () => {
  mockFetch({
    "/api/admin/overview": {
      stats: [],
      route_health: [],
      top_models: [],
      recent_alerts: [],
      audit_snapshot: [],
    },
    "/api/admin/system/status": {
      console_stage: "控制台预览版",
      run_mode: "数据库模式",
      gateway_health: "健康",
      quota_protection: "已启用",
      console_entry: "31873",
      gateway_admin_api: "32658",
      internal_services: ["31427"],
      hidden_modules: ["RAG 控制台", "知识库"],
    },
  });

  renderRoute("/");

  fireEvent.click(await screen.findByRole("button", { name: "控制台预览版" }));

  expect(await screen.findByText("RAG 控制台")).toBeInTheDocument();
  expect(screen.getByText("31873")).toBeInTheDocument();
});
```

- [ ] **Step 2: Run frontend test to verify it fails**

Run: `cd /root/liwenjian/ai_gateway/web && npm test -- --runInBand src/test/router.test.tsx`
Expected: FAIL because `知识库` 仍存在导航，状态 badge 不是按钮，且 `/api/admin/system/status` 尚未实现

- [ ] **Step 3: Write the failing backend tests for system status endpoint**

```go
func TestAdminSystemStatusRouteReturnsConsoleStatus(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "example-console-user",
		ServiceAuthPassword: "change-me-console-password",
		ConsoleService: stubConsoleService{
			systemStatus: service.ConsoleSystemStatus{
				ConsoleStage:    "控制台预览版",
				RunMode:         "数据库模式",
				GatewayHealth:   "健康",
				QuotaProtection: "已启用",
				ConsoleEntry:    "31873",
				GatewayAdminAPI: "32658",
				InternalServices: []string{"31427"},
				HiddenModules:    []string{"RAG 控制台", "知识库"},
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/system/status", nil)
	req.SetBasicAuth("example-console-user", "change-me-console-password")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 4: Run backend test to verify it fails**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/http ./internal/service -count=1`
Expected: FAIL because `ConsoleSystemStatus` and `/admin/system/status` route do not exist

- [ ] **Step 5: Implement backend system status DTO and route**

```go
type ConsoleSystemStatus struct {
	ConsoleStage     string   `json:"console_stage"`
	RunMode          string   `json:"run_mode"`
	GatewayHealth    string   `json:"gateway_health"`
	QuotaProtection  string   `json:"quota_protection"`
	ConsoleEntry     string   `json:"console_entry"`
	GatewayAdminAPI  string   `json:"gateway_admin_api"`
	InternalServices []string `json:"internal_services"`
	HiddenModules    []string `json:"hidden_modules"`
}

type ConsoleService interface {
	Overview(ctx context.Context) (OverviewPageData, error)
	SystemStatus(ctx context.Context) (ConsoleSystemStatus, error)
	APIKeys(ctx context.Context) (APIKeysPageData, error)
}

func ConsoleSystemStatusHandler(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := console.SystemStatus(c.UserContext())
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}
```

- [ ] **Step 6: Implement frontend system status fetch and layout behavior**

```tsx
export type ConsoleSystemStatus = {
  console_stage: string;
  run_mode: string;
  gateway_health: string;
  quota_protection: string;
  console_entry: string;
  gateway_admin_api: string;
  internal_services: string[];
  hidden_modules: string[];
};

export function getSystemStatus() {
  return requestJson<ConsoleSystemStatus>("/api/admin/system/status");
}

export const navigation = [
  { path: "/", label: "总览", title: "总览", description: "查看网关健康、路由态势与核心平台指标。", element: <DashboardPage /> },
  { path: "/api-keys", label: "API 密钥", title: "API 密钥", description: "管理平台密钥、权限范围与租户访问状态。", element: <APIKeysPage /> },
  { path: "/routes", label: "路由", title: "路由", description: "检查模型映射、供应商解析与回退策略。", element: <RoutesPage /> },
  { path: "/playground", label: "调试场", title: "调试场", description: "在正式使用前验证模型请求与路由结果。", element: <PlaygroundPage /> },
  { path: "/usage", label: "调用观测", title: "调用观测", description: "查看 Token、成功率、失败分类与调用明细。", element: <UsagePage /> },
  { path: "/audit", label: "审计", title: "审计", description: "追踪请求历史、供应商解析与运维事件。", element: <AuditPage /> },
] satisfies readonly ConsoleRouteDefinition[];
```

- [ ] **Step 7: Run focused tests to verify they pass**

Run: `cd /root/liwenjian/ai_gateway/web && npm test -- --runInBand src/test/router.test.tsx`
Expected: PASS with hidden `知识库` nav and working status entry

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/http ./internal/service -count=1`
Expected: PASS with `/admin/system/status` route registered

- [ ] **Step 8: Commit**

```bash
git add web/src/app/router.tsx web/src/app/layout.tsx web/src/lib/console-api.ts web/src/styles.css web/src/test/router.test.tsx gateway/internal/service/console_service.go gateway/internal/service/postgres_console_service.go gateway/internal/http/handlers/admin.go gateway/internal/http/router.go gateway/internal/http/router_test.go gateway/internal/service/postgres_console_service_test.go
git commit -m "feat: add console status entry behavior"
```

### Task 2: Add API Key Management Persistence And Admin Endpoints

**Files:**
- Modify: `/root/liwenjian/ai_gateway/gateway/internal/store/auth_repository.go`
- Modify: `/root/liwenjian/ai_gateway/gateway/internal/service/console_service.go`
- Modify: `/root/liwenjian/ai_gateway/gateway/internal/service/postgres_console_service.go`
- Modify: `/root/liwenjian/ai_gateway/gateway/internal/http/handlers/admin.go`
- Modify: `/root/liwenjian/ai_gateway/gateway/internal/http/router.go`
- Test: `/root/liwenjian/ai_gateway/gateway/internal/store/auth_repository_test.go`
- Test: `/root/liwenjian/ai_gateway/gateway/internal/service/postgres_console_service_test.go`
- Test: `/root/liwenjian/ai_gateway/gateway/internal/http/router_test.go`
- Create if required: `/root/liwenjian/ai_gateway/gateway/db/migrations/0007_extend_platform_api_keys_for_example-console-user.sql`

- [ ] **Step 1: Write failing repository and service tests for create/rotate/deactivate**

```go
func TestSQLAuthRepositoryCreatePlatformAPIKey(t *testing.T) {
	t.Parallel()

	repo, ctx := newSQLAuthRepositoryForTest(t)
	record, rawKey, err := repo.CreatePlatformAPIKey(ctx, CreatePlatformAPIKeyParams{
		Name:     "prod-gateway-2",
		TenantID: "tenant_alpha",
		Scopes:   []string{"chat", "embeddings"},
	})
	if err != nil {
		t.Fatalf("CreatePlatformAPIKey failed: %v", err)
	}
	if rawKey == "" {
		t.Fatal("expected raw key")
	}
	if record.Status != domain.StatusActive {
		t.Fatalf("expected active status, got %q", record.Status)
	}
}

func TestPostgresConsoleServiceRotatePlatformAPIKeyReturnsRawKeyOnce(t *testing.T) {
	t.Parallel()

	console, _ := newUsageConsoleService(t, context.Background())
	result, err := console.RotateAPIKey(context.Background(), "pak_live_console")
	if err != nil {
		t.Fatalf("RotateAPIKey failed: %v", err)
	}
	if strings.TrimSpace(result.RawKey) == "" {
		t.Fatal("expected rotated raw key")
	}
}
```

- [ ] **Step 2: Run backend tests to verify they fail**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/store ./internal/service ./internal/http -count=1`
Expected: FAIL because repository/service methods and DTOs do not exist

- [ ] **Step 3: Implement repository methods and migration if needed**

```go
type CreatePlatformAPIKeyParams struct {
	Name     string
	TenantID string
	Scopes   []string
}

type RotatePlatformAPIKeyResult struct {
	ID     string
	RawKey string
}

type PlatformAPIKeyAdminRepository interface {
	ListPlatformAPIKeys(ctx context.Context) ([]PlatformAPIKeyRecord, error)
	CreatePlatformAPIKey(ctx context.Context, params CreatePlatformAPIKeyParams) (PlatformAPIKeyRecord, string, error)
	RotatePlatformAPIKey(ctx context.Context, id string) (PlatformAPIKeyRecord, string, error)
	DeactivatePlatformAPIKey(ctx context.Context, id string) (PlatformAPIKeyRecord, error)
}
```

- [ ] **Step 4: Implement admin handlers and routes**

```go
admin.Get("/api-keys", handlers.ConsoleAPIKeys(deps.ConsoleService))
admin.Post("/api-keys", handlers.ConsoleCreateAPIKey(deps.ConsoleService))
admin.Post("/api-keys/:id/rotate", handlers.ConsoleRotateAPIKey(deps.ConsoleService))
admin.Post("/api-keys/:id/deactivate", handlers.ConsoleDeactivateAPIKey(deps.ConsoleService))
```

- [ ] **Step 5: Run backend tests to verify they pass**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/store ./internal/service ./internal/http -count=1`
Expected: PASS with create/rotate/deactivate endpoints returning correct JSON and error codes

- [ ] **Step 6: Commit**

```bash
git add gateway/internal/store/auth_repository.go gateway/internal/store/auth_repository_test.go gateway/internal/service/console_service.go gateway/internal/service/postgres_console_service.go gateway/internal/service/postgres_console_service_test.go gateway/internal/http/handlers/admin.go gateway/internal/http/router.go gateway/internal/http/router_test.go gateway/db/migrations/0007_extend_platform_api_keys_for_example-console-user.sql
git commit -m "feat: add admin api key lifecycle endpoints"
```

### Task 3: Build API Key Management UI With Real Feedback

**Files:**
- Modify: `/root/liwenjian/ai_gateway/web/src/lib/console-api.ts`
- Modify: `/root/liwenjian/ai_gateway/web/src/pages/api-keys.tsx`
- Modify: `/root/liwenjian/ai_gateway/web/src/components/console.tsx`
- Modify: `/root/liwenjian/ai_gateway/web/src/styles.css`
- Test: `/root/liwenjian/ai_gateway/web/src/test/router.test.tsx`

- [ ] **Step 1: Write failing UI tests for create/rotate/deactivate**

```tsx
test("API 密钥页支持新建密钥并展示一次性明文", async () => {
  const fetchMock = mockFetch({
    "/api/admin/api-keys": {
      items: [],
      credential_mode: "平台 API Key 与上游凭据分离",
    },
  });

  fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.toString();
    if (url === "/api/admin/api-keys" && init?.method === "POST") {
      return new Response(JSON.stringify({
        item: {
          id: "pak_new",
          name: "new-key",
          tenant: "tenant_alpha",
          status: "启用",
          scopes: ["chat"],
          last_used_at: "刚刚",
        },
        raw_key: "ak_live_new_secret",
      }), { status: 200, headers: { "Content-Type": "application/json" } });
    }
    throw new Error(`Unexpected fetch url: ${url}`);
  });

  renderRoute("/api-keys");
  fireEvent.click(await screen.findByRole("button", { name: "新建密钥" }));
  fireEvent.click(screen.getByRole("button", { name: "确认创建" }));

  expect(await screen.findByText("ak_live_new_secret")).toBeInTheDocument();
});
```

- [ ] **Step 2: Run frontend test to verify it fails**

Run: `cd /root/liwenjian/ai_gateway/web && npm test -- --runInBand src/test/router.test.tsx`
Expected: FAIL because page has no modal, row selection, or mutation requests

- [ ] **Step 3: Implement minimal API helpers and page state machine**

```tsx
export type APIKeyMutationResult = {
  item: APIKeyItem;
  raw_key?: string;
};

export function createAPIKey(payload: { name: string; tenant: string; scopes: string[] }) {
  return requestJson<APIKeyMutationResult>("/api/admin/api-keys", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function rotateAPIKey(id: string) {
  return requestJson<APIKeyMutationResult>(`/api/admin/api-keys/${id}/rotate`, {
    method: "POST",
  });
}

export function deactivateAPIKey(id: string) {
  return requestJson<APIKeyMutationResult>(`/api/admin/api-keys/${id}/deactivate`, {
    method: "POST",
  });
}
```

- [ ] **Step 4: Implement page interactions and refresh flow**

```tsx
const [selectedKeyId, setSelectedKeyId] = useState<string | null>(null);
const [mutationMessage, setMutationMessage] = useState<string | null>(null);
const [revealedRawKey, setRevealedRawKey] = useState<string | null>(null);

async function handleDeactivate() {
  if (!selectedKeyId) return;
  await deactivateAPIKey(selectedKeyId);
  await reload();
  setMutationMessage("密钥已停用");
}
```

- [ ] **Step 5: Run frontend tests and build**

Run: `cd /root/liwenjian/ai_gateway/web && npm test -- --runInBand`
Expected: PASS with API Key lifecycle tests green

Run: `cd /root/liwenjian/ai_gateway/web && npm run build`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/console-api.ts web/src/pages/api-keys.tsx web/src/components/console.tsx web/src/styles.css web/src/test/router.test.tsx
git commit -m "feat: add api key lifecycle console ui"
```

### Task 4: Full Regression Sweep And Deployment Validation

**Files:**
- Modify if needed: `/root/liwenjian/ai_gateway/scripts/test.sh`
- Modify if needed: `/root/liwenjian/ai_gateway/README.md`
- Test: existing compose deployment and manual verification

- [ ] **Step 1: Add regression expectations to tests first if any visible gap remains**

```tsx
test("控制台入口语义明确且不再暴露知识库入口", async () => {
  renderRoute("/");
  expect(screen.queryByRole("link", { name: "知识库" })).not.toBeInTheDocument();
});
```

```go
func TestAdminSystemStatusRouteRequiresBasicAuth(t *testing.T) {
	t.Parallel()
	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "example-console-user",
		ServiceAuthPassword: "change-me-console-password",
		ConsoleService: stubConsoleService{},
	})
	req := httptest.NewRequest(http.MethodGet, "/admin/system/status", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run the full automated suite**

Run: `cd /root/liwenjian/ai_gateway && ./scripts/test.sh`
Expected: all Go tests, pytest, and web tests PASS

- [ ] **Step 3: Rebuild and redeploy compose services**

Run: `cd /root/liwenjian/ai_gateway && docker compose -f deploy/compose/compose.yml up -d --build gateway web`
Expected: `gateway` and `web` containers stay `Up`

- [ ] **Step 4: Perform manual verification**

Run:

```bash
curl -sS -u example-console-user:change-me-console-password http://127.0.0.1:32658/admin/system/status
curl -sS -u example-console-user:change-me-console-password http://127.0.0.1:32658/admin/api-keys
curl -sS -u example-console-user:change-me-console-password http://127.0.0.1:32658/admin/usage/overview
curl -sS -u example-console-user:change-me-console-password http://127.0.0.1:31873/api/admin/system/status
```

Expected:

- `system/status` returns `31873 / 32658 / 31427` semantic split
- `api-keys` list loads normally
- `usage/overview` still works
- `31873` proxy still reaches admin API

- [ ] **Step 5: Commit**

```bash
git add scripts/test.sh README.md web/src/test/router.test.tsx gateway/internal/http/router_test.go
git commit -m "test: add regression coverage for console baseline"
```

---

## Self-Review

### Spec coverage

- 唯一正式入口 `31873`：Task 1 system status + layout/nav
- `32658` 与 `31427` 语义澄清：Task 1 system status
- 隐藏 RAG / 知识库前端入口：Task 1 router/nav
- 左侧状态区可点击：Task 1 layout + tests
- API Key 创建/轮换/停用：Task 2 backend + Task 3 frontend
- 所有可见动作真实联动：Task 1 + Task 3 + Task 4 regression

### Placeholder scan

- 无 `TBD` / `TODO`
- 所有任务都给出明确文件路径、命令和预期结果

### Type consistency

- `ConsoleSystemStatus`、`APIKeyMutationResult`、`CreatePlatformAPIKeyParams` 在任务中命名一致
- `/api/admin/system/status`、`/api/admin/api-keys/:id/rotate`、`/api/admin/api-keys/:id/deactivate` 路径保持一致

---

Plan complete and saved to `docs/superpowers/plans/2026-04-24-console-baseline-and-interaction-plan.md`. Two execution options:

1. Subagent-Driven (recommended) - I dispatch a fresh subagent per task, review between tasks, fast iteration
2. Inline Execution - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
