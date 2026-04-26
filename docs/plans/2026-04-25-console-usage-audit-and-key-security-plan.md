# 控制台调用观测、审计与密钥安全增强 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在不重做现有控制台导航的前提下，完成左下角状态块隐藏、API 密钥安全展示与多选权限、调用观测深色可视化增强，以及基于真实 `llm_request_logs / llm_request_events` 的审计页重构。

**Architecture:** 前端继续沿用现有 `react-router-dom` 页面结构，只在 `layout.tsx`、`api-keys.tsx`、`usage.tsx`、`audit.tsx` 和共享展示组件内做局部重构；后端仅调整 `ConsoleService.Audit()` 的数据模型和 PostgreSQL 查询来源，管理 API 路径保持不变。测试按 TDD 执行：先写失败的 Vitest / Go 集成测试，再补最小实现，通过后再做样式收口和提交。

**Tech Stack:** React 18, React Router 6, TypeScript, Vitest, Go 1.22, Fiber, PostgreSQL, Testcontainers.

---

## File Structure

- Modify: `web/src/app/layout.tsx`
  - 删除侧栏左下状态块 DOM 与交互状态，只保留顶部 badge 所需的系统状态请求。
- Modify: `web/src/pages/api-keys.tsx`
  - 以页面内存态保存一次性 `raw_key`，新增脱敏显示、复制按钮、多选权限下拉和前端校验。
- Modify: `web/src/pages/usage.tsx`
  - 将现有调用观测改造成深色工作台布局，保留现有 usage 接口与分页行为。
- Modify: `web/src/pages/audit.tsx`
  - 消费新的审计数据结构，渲染真实指标卡、事件流和扩展明细表。
- Modify: `web/src/components/console.tsx`
  - 允许 `DataTable` 接受 `ReactNode` 单元格，并提供轻量状态 pill / 强弱条组件。
- Modify: `web/src/lib/console-api.ts`
  - 同步前端类型，新增审计 `metrics/events` 字段，并收紧 API key scope 联合类型。
- Modify: `web/src/styles.css`
  - 清理左下状态块样式，增加 API key 多选、脱敏结果区、调用观测深色可视化和审计页样式。
- Modify: `gateway/internal/service/console_service.go`
  - 扩展 `AuditPageData` / `AuditItem` / `AuditEvent` / `AuditMetric` 类型，固定后端 JSON 契约。
- Modify: `gateway/internal/service/postgres_console_service.go`
  - 将 `Audit()` 主数据源切到 `llm_request_logs / llm_request_events`，为空时 fallback 到 `audit_logs`。
- Test: `web/src/test/router.test.tsx`
  - 覆盖左下状态隐藏、密钥脱敏复制、多选权限、调用观测新语义、审计页真实数据渲染。
- Test: `gateway/internal/service/postgres_console_service_test.go`
  - 覆盖审计主路径和 fallback 路径。

### Task 1: Hide Sidebar Status Block And Preserve Topbar Status

**Files:**
- Modify: `web/src/app/layout.tsx`
- Modify: `web/src/styles.css`
- Test: `web/src/test/router.test.tsx`

- [ ] **Step 1: 写一个失败的路由测试，锁定“左下状态块消失但顶部 badge 保留”的行为**

```tsx
test("侧栏隐藏左下状态块但顶部 badge 继续展示系统状态", async () => {
  mockFetch({
    "/api/admin/overview": {
      stats: [],
      route_health: [],
      top_models: [],
      recent_alerts: [],
      audit_snapshot: [],
    },
    "/api/admin/system/status": defaultSystemStatus(),
  });

  renderRoute("/");

  expect(await screen.findByRole("heading", { level: 1, name: "总览" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /控制台预览版/ })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /数据库模式/ })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /网关健康 健康/ })).not.toBeInTheDocument();
  expect(screen.getByText("健康")).toBeInTheDocument();
  expect(screen.getByText("配额保护 已启用")).toBeInTheDocument();
});
```

- [ ] **Step 2: 运行测试，确认它先失败**

Run: `npm test -- src/test/router.test.tsx -t "侧栏隐藏左下状态块但顶部 badge 继续展示系统状态"`

Expected: FAIL，报出仍然找到了侧栏状态按钮，例如 `Found an accessible element with the role "button"`。

- [ ] **Step 3: 删除左下状态块 JSX 和相关状态，保留顶部 badge 所需的数据请求**

```tsx
import { NavLink, Outlet, useMatches } from "react-router-dom";

import { getSystemStatus } from "../lib/console-api";
import { useRemoteData } from "../lib/use-remote-data";

export function AppLayout({ navigation }: { navigation: readonly ConsoleNavigationItem[] }) {
  const matches = useMatches();
  const { data: systemStatus, error: systemStatusError } = useRemoteData(getSystemStatus);
  const current =
    matches.reduce<ConsoleRouteMeta | undefined>(
      (matchedMeta, match) => (isConsoleRouteMeta(match.handle) ? match.handle : matchedMeta),
      undefined,
    ) ?? toRouteMeta(navigation[0]);
  const statusPlaceholder = systemStatusError ? "状态获取失败" : "状态加载中";
  const gatewayHealth = systemStatus?.gateway_health ?? statusPlaceholder;
  const quotaProtection = systemStatus?.quota_protection ?? statusPlaceholder;
  const isGatewayHealthy = gatewayHealth === "健康";

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="sidebar__brand">AI 网关控制台</div>
        <nav className="sidebar__nav">
          {navigation.map((item) => (
            <NavLink
              key={item.path}
              end={item.path === "/"}
              to={item.path}
              className={({ isActive }) =>
                isActive ? "sidebar__link sidebar__link--active" : "sidebar__link"
              }
            >
              {item.label}
            </NavLink>
          ))}
        </nav>
      </aside>
      <div className="shell-main">
        <header className="topbar">
          <div className="topbar__meta">
            <h1>{current.title}</h1>
            <p>{current.description}</p>
          </div>
          <div className="topbar__badges">
            <span className={getBadgeClassName(isGatewayHealthy)}>{gatewayHealth}</span>
            <span className="status-badge status-badge--neutral">配额保护 {quotaProtection}</span>
          </div>
        </header>
        <main className="page-content">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
```

```css
.sidebar {
  background: #152132;
  color: #eef3f8;
  padding: 24px 20px;
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.sidebar__nav {
  display: grid;
  gap: 8px;
}
```

- [ ] **Step 4: 重新运行路由测试，确认新行为通过**

Run: `npm test -- src/test/router.test.tsx -t "侧栏隐藏左下状态块但顶部 badge 继续展示系统状态"`

Expected: PASS，Vitest 输出 `1 passed`。

- [ ] **Step 5: 提交这个最小变更**

```bash
git add web/src/app/layout.tsx web/src/styles.css web/src/test/router.test.tsx
git commit -m "feat: hide sidebar status block"
```

### Task 2: Secure API Key Result Display And Multi-Select Scopes

**Files:**
- Modify: `web/src/pages/api-keys.tsx`
- Modify: `web/src/lib/console-api.ts`
- Modify: `web/src/styles.css`
- Test: `web/src/test/router.test.tsx`

- [ ] **Step 1: 先写失败测试，锁定“只显示脱敏值 + 可复制完整值 + 多选权限下拉”**

```tsx
test("API 密钥页新建后仅显示脱敏密钥并允许复制完整值", async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.toString();

    if (url === "/api/admin/system/status") {
      return new Response(JSON.stringify(defaultSystemStatus()), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }
    if (url === "/api/admin/api-keys" && !init?.method) {
      return new Response(
        JSON.stringify({
          items: [],
          credential_mode: "平台 API Key 与上游凭据分离",
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      );
    }
    if (url === "/api/admin/api-keys" && init?.method === "POST") {
      expect(JSON.parse(String(init.body))).toEqual({
        tenant_id: "tenant_alpha",
        name: "new-key",
        scopes: ["chat", "rag"],
      });

      return new Response(
        JSON.stringify({
          item: {
            id: "pak_new",
            name: "new-key",
            tenant: "tenant_alpha",
            status: "启用",
            scopes: ["chat", "rag"],
            last_used_at: "2026-04-24T12:00:00+08:00",
          },
          raw_key: "agw_abcd_full_secret_wxyz",
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      );
    }

    throw new Error(`Unexpected fetch url: ${url}`);
  });

  const clipboardWriteText = vi.fn().mockResolvedValue(undefined);
  vi.stubGlobal("fetch", fetchMock);
  Object.assign(navigator, {
    clipboard: {
      writeText: clipboardWriteText,
    },
  });

  renderRoute("/api-keys");

  fireEvent.click(await screen.findByRole("button", { name: "新建密钥" }));
  fireEvent.change(screen.getByLabelText("租户 ID"), { target: { value: "tenant_alpha" } });
  fireEvent.change(screen.getByLabelText("名称"), { target: { value: "new-key" } });
  fireEvent.click(screen.getByRole("button", { name: "权限范围" }));
  fireEvent.click(screen.getByRole("checkbox", { name: "rag" }));
  fireEvent.click(screen.getByRole("button", { name: "确认创建" }));

  expect(await screen.findByText("agw_abcd************wxyz")).toBeInTheDocument();
  expect(screen.queryByText("agw_abcd_full_secret_wxyz")).not.toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: "复制完整密钥" }));
  expect(clipboardWriteText).toHaveBeenCalledWith("agw_abcd_full_secret_wxyz");
});
```

```tsx
test("API 密钥页轮换时回填当前权限范围", async () => {
  mockFetch({
    "/api/admin/api-keys": {
      items: [
        {
          id: "pak_live_console",
          name: "生产网关",
          tenant: "tenant_alpha",
          status: "启用",
          scopes: ["chat", "rag"],
          last_used_at: "2026-04-24T11:58:00+08:00",
        },
      ],
      credential_mode: "平台 API Key 与上游凭据分离",
    },
  });

  renderRoute("/api-keys");

  fireEvent.click(await screen.findByRole("button", { name: "轮换密钥" }));
  fireEvent.click(screen.getByRole("button", { name: "新权限范围" }));

  expect(screen.getByRole("checkbox", { name: "chat" })).toBeChecked();
  expect(screen.getByRole("checkbox", { name: "rag" })).toBeChecked();
  expect(screen.getByRole("checkbox", { name: "embeddings" })).not.toBeChecked();
});
```

- [ ] **Step 2: 运行测试，确认当前实现会失败**

Run: `npm test -- src/test/router.test.tsx -t "API 密钥页新建后仅显示脱敏密钥并允许复制完整值|API 密钥页轮换时回填当前权限范围"`

Expected: FAIL，错误表现应包含以下至少一项：
- 页面直接渲染了 `agw_abcd_full_secret_wxyz`
- 找不到名称为 `权限范围` 或 `新权限范围` 的按钮
- POST body 仍然使用旧的自由文本输入逻辑

- [ ] **Step 3: 收紧 scope 类型，并在前端用页面内存态保存完整密钥，只渲染脱敏值**

```ts
export const apiKeyScopes = ["chat", "rag", "embeddings"] as const;

export type APIKeyScope = (typeof apiKeyScopes)[number];

export type APIKeyItem = {
  id: string;
  name: string;
  tenant: string;
  status: string;
  scopes: APIKeyScope[];
  last_used_at: string;
};

export type APIKeyMutationResult = {
  item: APIKeyItem;
  raw_key?: string;
};

export function createAPIKey(payload: { tenant_id: string; name: string; scopes: APIKeyScope[] }) {
  return requestJson<APIKeyMutationResult>("/api/admin/api-keys", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function rotateAPIKey(id: string, payload: { name?: string; scopes?: APIKeyScope[] }) {
  return requestJson<APIKeyMutationResult>(`/api/admin/api-keys/${id}/rotate`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}
```

```tsx
const AVAILABLE_SCOPES: APIKeyScope[] = ["chat", "rag", "embeddings"];

type SecretSnapshot = {
  name: string;
  status: string;
  rawKey: string;
  maskedKey: string;
};

function maskAPIKey(rawKey: string) {
  if (rawKey.length <= 12) {
    return rawKey;
  }
  return `${rawKey.slice(0, 8)}************${rawKey.slice(-4)}`;
}

export function APIKeysPage() {
  const { data, loading, error } = useRemoteData(getAPIKeys);
  const [selectedScopes, setSelectedScopes] = useState<APIKeyScope[]>(["chat"]);
  const [scopeMenuOpen, setScopeMenuOpen] = useState(false);
  const [secretSnapshot, setSecretSnapshot] = useState<SecretSnapshot | null>(null);

  function toggleScope(scope: APIKeyScope) {
    setSelectedScopes((current) =>
      current.includes(scope) ? current.filter((item) => item !== scope) : [...current, scope],
    );
  }

  async function copySecret() {
    if (!secretSnapshot) {
      return;
    }
    await navigator.clipboard.writeText(secretSnapshot.rawKey);
    setActionNotice("已复制完整密钥");
  }

  async function handleConfirm() {
    if ((actionMode === "create" || actionMode === "rotate") && selectedScopes.length === 0) {
      setActionError("至少选择 1 个权限范围。");
      return;
    }

    if (actionMode === "create") {
      const result = await createAPIKey({
        tenant_id: tenantID.trim(),
        name: name.trim(),
        scopes: selectedScopes,
      });
      setSecretSnapshot(
        result.raw_key
          ? {
              name: result.item.name,
              status: result.item.status,
              rawKey: result.raw_key,
              maskedKey: maskAPIKey(result.raw_key),
            }
          : null,
      );
    }
  }

  return (
    <div className="page-grid">
      <section className="section-card">
        <label className="field-shell">
          权限范围
          <div className="scope-picker">
            <button
              type="button"
              className="button-shell button-shell--full"
              onClick={() => setScopeMenuOpen((current) => !current)}
            >
              {selectedScopes.length > 0 ? selectedScopes.join(", ") : "请选择权限范围"}
            </button>
            {scopeMenuOpen ? (
              <div className="scope-picker__menu">
                {AVAILABLE_SCOPES.map((scope) => (
                  <label key={scope} className="scope-picker__option">
                    <input
                      type="checkbox"
                      checked={selectedScopes.includes(scope)}
                      onChange={() => toggleScope(scope)}
                    />
                    <span>{scope}</span>
                  </label>
                ))}
              </div>
            ) : null}
          </div>
        </label>
      </section>

      {secretSnapshot ? (
        <section className="section-card section-card--success">
          <h3>安全结果</h3>
          <p>名称：{secretSnapshot.name}</p>
          <p>状态：{secretSnapshot.status}</p>
          <p className="secret-text">
            密钥：<code>{secretSnapshot.maskedKey}</code>
          </p>
          <div className="page-actions">
            <button type="button" className="button-shell button-shell--primary" onClick={copySecret}>
              复制完整密钥
            </button>
          </div>
          <p>仅本次会话可复制完整密钥，刷新页面后失效。</p>
        </section>
      ) : null}
    </div>
  );
}
```

```css
.button-shell--full {
  width: 100%;
  justify-content: space-between;
}

.scope-picker {
  position: relative;
  display: grid;
  gap: 8px;
}

.scope-picker__menu {
  position: absolute;
  top: calc(100% + 8px);
  left: 0;
  right: 0;
  z-index: 2;
  border: 1px solid #d6dee7;
  border-radius: 12px;
  background: #ffffff;
  box-shadow: 0 16px 32px rgba(24, 40, 61, 0.12);
  padding: 12px;
  display: grid;
  gap: 8px;
}

.scope-picker__option {
  display: flex;
  align-items: center;
  gap: 10px;
}

.secret-text code {
  letter-spacing: 0.08em;
}
```

- [ ] **Step 4: 轮换操作回填当前 scopes，并确保任何时候都不直接把 `raw_key` 渲染到 DOM**

```tsx
function openCreate() {
  resetFeedback();
  setActionMode("create");
  setTenantID(selectedItem?.tenant ?? "");
  setName("");
  setSelectedScopes(["chat"]);
  setScopeMenuOpen(false);
}

function openRotate() {
  if (!selectedItem) {
    return;
  }

  resetFeedback();
  setActionMode("rotate");
  setName(selectedItem.name);
  setSelectedScopes(selectedItem.scopes);
  setScopeMenuOpen(false);
}

if (actionMode === "rotate" && selectedItem) {
  const result = await rotateAPIKey(selectedItem.id, {
    name: name.trim(),
    scopes: selectedScopes,
  });
  setSecretSnapshot(
    result.raw_key
      ? {
          name: result.item.name,
          status: result.item.status,
          rawKey: result.raw_key,
          maskedKey: maskAPIKey(result.raw_key),
        }
      : null,
  );
}
```

- [ ] **Step 5: 跑完整个路由测试文件，确认没有引入回归**

Run: `npm test -- src/test/router.test.tsx`

Expected: PASS，Vitest 输出 `All tests passed` 或 `passed`，且 API 密钥相关测试全部通过。

- [ ] **Step 6: 提交这组安全与交互变更**

```bash
git add web/src/pages/api-keys.tsx web/src/lib/console-api.ts web/src/styles.css web/src/test/router.test.tsx
git commit -m "feat: secure api key console display"
```

### Task 3: Redesign Usage Page With Dark Visual Analytics

**Files:**
- Modify: `web/src/components/console.tsx`
- Modify: `web/src/pages/usage.tsx`
- Modify: `web/src/styles.css`
- Test: `web/src/test/router.test.tsx`

- [ ] **Step 1: 先写失败测试，锁定新的“深色工作台 + 事件流 + pill”语义**

```tsx
test("调用观测页使用可视化组件展示状态、事件流与来源 pill", async () => {
  mockFetch({
    "/api/admin/usage/overview": {
      total_requests: 128,
      success_rate: "98.40%",
      total_tokens: "24,560",
      average_latency: "182 ms",
      estimated_share: "12.00%",
    },
    "/api/admin/usage/trends": {
      requests: [
        { label: "04-24 18:00", value: "64" },
        { label: "04-24 19:00", value: "64" },
      ],
      tokens: [
        { label: "04-24 18:00", value: "12,100" },
        { label: "04-24 19:00", value: "12,460" },
      ],
      success: [
        { label: "04-24 18:00", value: "97.00%" },
        { label: "04-24 19:00", value: "99.80%" },
      ],
    },
    "/api/admin/usage/failures": {
      breakdown: [
        { label: "限流", value: "3 次" },
        { label: "上游服务异常", value: "1 次" },
      ],
      recent_events: ["04-24 19:08 · 限流 · 请求失败（429）"],
    },
    "/api/admin/usage/requests?limit=20&offset=0": {
      items: [
        {
          request_id: "llmreq_demo_002",
          tenant: "tenant_demo",
          endpoint: "/v1/embeddings",
          model: "text-embedding-3-small",
          status: "限流",
          total_tokens: "16",
          latency: "95 ms",
          usage_source: "估算",
        },
      ],
      total: 1,
      limit: 20,
      offset: 0,
    },
  });

  renderRoute("/usage");

  expect(await screen.findByText("实时运行视图")).toBeInTheDocument();
  expect(screen.getByText("异常事件流")).toBeInTheDocument();
  expect(screen.getByLabelText("状态 限流")).toBeInTheDocument();
  expect(screen.getByLabelText("来源 估算")).toBeInTheDocument();
});
```

- [ ] **Step 2: 运行测试，确认当前页面缺少这些新语义**

Run: `npm test -- src/test/router.test.tsx -t "调用观测页使用可视化组件展示状态、事件流与来源 pill"`

Expected: FAIL，提示找不到 `实时运行视图`、`异常事件流` 或对应 `aria-label`。

- [ ] **Step 3: 先让共享表格支持 `ReactNode`，并补轻量视觉组件**

```tsx
import type { ReactNode } from "react";

export function DataTable({
  columns,
  rows,
  emptyMessage = "暂无数据",
}: {
  columns: string[];
  rows: ReactNode[][];
  emptyMessage?: string;
}) {
  return (
    <table className="data-table">
      <thead>
        <tr>
          {columns.map((column) => (
            <th key={column}>{column}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.length > 0 ? (
          rows.map((row, rowIndex) => (
            <tr key={`${rowIndex}-${rowIndex}`}>
              {row.map((cell, cellIndex) => (
                <td key={`${rowIndex}-${cellIndex}`}>{cell}</td>
              ))}
            </tr>
          ))
        ) : (
          <tr>
            <td colSpan={columns.length} className="table-empty-cell">
              {emptyMessage}
            </td>
          </tr>
        )}
      </tbody>
    </table>
  );
}

export function StatusPill({
  label,
  tone,
}: {
  label: string;
  tone: "success" | "warning" | "danger" | "neutral";
}) {
  return (
    <span className={`status-pill status-pill--${tone}`} aria-label={`状态 ${label}`}>
      {label}
    </span>
  );
}

export function SourcePill({ label }: { label: string }) {
  return (
    <span className="source-pill" aria-label={`来源 ${label}`}>
      {label}
    </span>
  );
}
```

- [ ] **Step 4: 用现有 usage 接口数据重组调用观测页面，渲染深色工作台、强弱条和事件流**

```tsx
function toneForStatus(status: string): "success" | "warning" | "danger" | "neutral" {
  if (status === "成功") {
    return "success";
  }
  if (status === "限流") {
    return "warning";
  }
  if (status.includes("失败") || status.includes("异常")) {
    return "danger";
  }
  return "neutral";
}

function toCount(value: string) {
  const match = value.match(/\d+/);
  return match ? Number(match[0]) : 0;
}

export function UsagePage() {
  const [offset, setOffset] = useState(0);
  const loadRequests = useCallback(
    () =>
      getUsageRequests({
        limit: PAGE_SIZE,
        offset,
      }),
    [offset],
  );
  const overview = useRemoteData(getUsageOverview);
  const trends = useRemoteData(getUsageTrends);
  const failures = useRemoteData(getUsageFailures);
  const requests = useRemoteData(loadRequests);

  if (!overview.data || !trends.data || !failures.data || !requests.data) {
    return <LoadingSection text="正在加载调用观测数据..." />;
  }

  const failureMax = Math.max(1, ...failures.data.breakdown.map((item) => toCount(item.value)));

  return (
    <div className="page-grid page-grid--usage">
      <section className="section-card usage-hero">
        <div className="section-card__header">
          <h2>实时运行视图</h2>
          <p>请求量、Token、成功率和估算占比全部来自 usage 接口。</p>
        </div>
        <div className="stats-grid stats-grid--five">
          <StatCard label="总调用数" value={String(overview.data.total_requests)} />
          <StatCard label="成功率" value={overview.data.success_rate} />
          <StatCard label="总 Token" value={overview.data.total_tokens} />
          <StatCard label="平均延迟" value={overview.data.average_latency} />
          <StatCard label="估算占比" value={overview.data.estimated_share} />
        </div>
      </section>

      <section className="section-card">
        <h2>趋势概览</h2>
        <div className="three-column-grid">
          <MetricSeriesSection title="调用次数趋势" points={trends.data.requests} />
          <MetricSeriesSection title="Token 趋势" points={trends.data.tokens} />
          <MetricSeriesSection title="成功率趋势" points={trends.data.success} />
        </div>
      </section>

      <div className="two-column-grid">
        <section className="section-card">
          <h2>失败分类强弱条</h2>
          <ul className="meter-list">
            {failures.data.breakdown.map((item) => (
              <li key={item.label} className="meter-list__item">
                <div className="meter-list__meta">
                  <span>{item.label}</span>
                  <strong>{item.value}</strong>
                </div>
                <div className="meter-list__track">
                  <span
                    className="meter-list__fill"
                    style={{ width: `${(toCount(item.value) / failureMax) * 100}%` }}
                  />
                </div>
              </li>
            ))}
          </ul>
        </section>

        <section className="section-card">
          <h2>异常事件流</h2>
          <ul className="event-timeline">
            {failures.data.recent_events.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        </section>
      </div>

      <section className="section-card">
        <h2>调用明细</h2>
        <DataTable
          columns={["请求 ID", "租户", "端点", "模型", "状态", "总 Token", "延迟", "计量来源"]}
          rows={requests.data.items.map((item) => [
            item.request_id,
            item.tenant,
            item.endpoint,
            item.model,
            <StatusPill key={`${item.request_id}-status`} label={item.status} tone={toneForStatus(item.status)} />,
            item.total_tokens,
            item.latency,
            <SourcePill key={`${item.request_id}-source`} label={item.usage_source} />,
          ])}
        />
      </section>
    </div>
  );
}
```

```css
.page-grid--usage .section-card {
  background: linear-gradient(180deg, #111b2b 0%, #0c1422 100%);
  border-color: #24364d;
  color: #edf3fb;
  box-shadow: 0 18px 36px rgba(9, 17, 31, 0.28);
}

.page-grid--usage .stat-card__label {
  color: #8ea2ba;
}

.page-grid--usage .stat-card__value {
  color: #ffffff;
}

.status-pill,
.source-pill {
  display: inline-flex;
  align-items: center;
  padding: 6px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 700;
}

.status-pill--success {
  background: rgba(29, 185, 84, 0.18);
  color: #8cf0b5;
}

.status-pill--warning {
  background: rgba(255, 184, 0, 0.16);
  color: #ffd970;
}

.status-pill--danger {
  background: rgba(255, 96, 96, 0.18);
  color: #ffb0b0;
}

.status-pill--neutral,
.source-pill {
  background: rgba(255, 255, 255, 0.08);
  color: #d7e2ee;
}

.meter-list,
.event-timeline {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 12px;
}

.meter-list__track {
  height: 8px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.08);
  overflow: hidden;
}

.meter-list__fill {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, #3db6ff 0%, #7c5cff 100%);
}
```

- [ ] **Step 5: 运行 usage 相关测试，确认新语义、分页和错误处理都通过**

Run: `npm test -- src/test/router.test.tsx -t "调用观测页"`

Expected: PASS，原有分页测试继续通过，新增可视化语义测试也通过。

- [ ] **Step 6: 提交调用观测视觉增强**

```bash
git add web/src/components/console.tsx web/src/pages/usage.tsx web/src/styles.css web/src/test/router.test.tsx
git commit -m "feat: redesign usage console"
```

### Task 4: Rebuild Audit Page On Real Usage Logs

**Files:**
- Modify: `gateway/internal/service/console_service.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `web/src/lib/console-api.ts`
- Modify: `web/src/pages/audit.tsx`
- Modify: `web/src/styles.css`
- Test: `gateway/internal/service/postgres_console_service_test.go`
- Test: `web/src/test/router.test.tsx`

- [ ] **Step 1: 先写 Go 失败测试，锁定“主读 usage logs/events，空时 fallback 到 audit_logs”**

```go
func TestPostgresConsoleServiceAuditUsesUsageLogsAndEvents(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	payload, err := console.Audit(ctx)
	if err != nil {
		t.Fatalf("Audit failed: %v", err)
	}

	if len(payload.Metrics) != 4 {
		t.Fatalf("expected 4 metrics, got %d", len(payload.Metrics))
	}
	if len(payload.Events) == 0 {
		t.Fatal("expected audit events from llm_request_events")
	}
	if len(payload.Items) == 0 {
		t.Fatal("expected audit items from llm_request_logs")
	}
	if payload.Items[0].RequestModel == "" {
		t.Fatal("expected request_model to be populated")
	}
	if payload.Items[0].UsageSource == "" {
		t.Fatal("expected usage_source to be populated")
	}
}
```

```go
func TestPostgresConsoleServiceAuditFallsBackToAuditLogsWhenUsageDataMissing(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `delete from llm_request_events; delete from llm_request_logs;`); err != nil {
		t.Fatalf("delete usage tables failed: %v", err)
	}

	payload, err := console.Audit(ctx)
	if err != nil {
		t.Fatalf("Audit failed: %v", err)
	}

	if len(payload.Items) == 0 {
		t.Fatal("expected fallback audit items")
	}
	if payload.Items[0].RequestModel != "-" {
		t.Fatalf("expected fallback request_model '-', got %q", payload.Items[0].RequestModel)
	}
}
```

- [ ] **Step 2: 运行 Go 测试，确认类型和行为都还没实现**

Run: `go test ./internal/service -run "TestPostgresConsoleServiceAudit(UsesUsageLogsAndEvents|FallsBackToAuditLogsWhenUsageDataMissing)" -count=1`

Expected: FAIL，典型报错包括：
- `payload.Metrics undefined`
- `payload.Events undefined`
- `payload.Items[0].RequestModel undefined`

- [ ] **Step 3: 扩展审计数据类型，先把后端 JSON 契约固定下来**

```go
type AuditMetric struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type AuditEvent struct {
	Time   string `json:"time"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type AuditItem struct {
	Time          string `json:"time"`
	Tenant        string `json:"tenant"`
	Endpoint      string `json:"endpoint"`
	RequestModel  string `json:"request_model"`
	UpstreamModel string `json:"upstream_model"`
	Status        string `json:"status"`
	Provider      string `json:"provider"`
	Latency       string `json:"latency"`
	UsageSource   string `json:"usage_source"`
}

type AuditPageData struct {
	Metrics   []AuditMetric  `json:"metrics"`
	Events    []AuditEvent   `json:"events"`
	Items     []AuditItem    `json:"items"`
	Summaries []AuditSummary `json:"summaries"`
}
```

- [ ] **Step 4: 改写 `Audit()`，优先聚合 `llm_request_logs / llm_request_events`，为空时 fallback**

```go
func (s postgresConsoleService) Audit(ctx context.Context) (AuditPageData, error) {
	var totalRequests int64
	var failedRequests int64
	var rateLimitedRequests int64
	var upstreamErrors int64
	if err := s.db.QueryRow(ctx, `
		select
			count(*),
			count(*) filter (where usage_status <> 'success'),
			count(*) filter (where usage_status = 'rate_limited' or status_code = 429),
			count(*) filter (where usage_status in ('upstream_error', 'auth_failed', 'timeout'))
		from llm_request_logs
		where request_started_at >= now() - interval '24 hours';
	`).Scan(&totalRequests, &failedRequests, &rateLimitedRequests, &upstreamErrors); err != nil {
		return AuditPageData{}, err
	}

	if totalRequests == 0 {
		return s.auditFromFallbackLogs(ctx)
	}

	itemRows, err := s.db.Query(ctx, `
		select
			to_char(l.request_started_at at time zone 'Asia/Shanghai', 'MM-DD HH24:MI'),
			l.tenant_id,
			l.request_path,
			l.request_model,
			coalesce(nullif(l.upstream_model, ''), l.request_model),
			l.usage_status,
			pc.display_name,
			l.latency_ms,
			l.usage_source
		from llm_request_logs l
		left join provider_credentials pc on pc.id = l.provider_credential_id
		order by l.request_started_at desc, l.id desc
		limit 12;
	`)
	if err != nil {
		return AuditPageData{}, err
	}
	defer itemRows.Close()

	items := make([]AuditItem, 0, 12)
	for itemRows.Next() {
		var item AuditItem
		var status string
		var latencyMS int64
		var usageSource string
		if err := itemRows.Scan(
			&item.Time,
			&item.Tenant,
			&item.Endpoint,
			&item.RequestModel,
			&item.UpstreamModel,
			&status,
			&item.Provider,
			&latencyMS,
			&usageSource,
		); err != nil {
			return AuditPageData{}, err
		}
		item.Status = translateUsageStatus(status)
		item.Latency = fmt.Sprintf("%d ms", latencyMS)
		item.UsageSource = translateUsageSource(usageSource)
		items = append(items, item)
	}
	if err := itemRows.Err(); err != nil {
		return AuditPageData{}, err
	}

	eventRows, err := s.db.Query(ctx, `
		select
			to_char(e.created_at at time zone 'Asia/Shanghai', 'MM-DD HH24:MI'),
			e.event_type,
			e.usage_status,
			e.detail
		from llm_request_events e
		order by e.created_at desc
		limit 8;
	`)
	if err != nil {
		return AuditPageData{}, err
	}
	defer eventRows.Close()

	events := make([]AuditEvent, 0, 8)
	for eventRows.Next() {
		var event AuditEvent
		var status string
		if err := eventRows.Scan(&event.Time, &event.Type, &status, &event.Detail); err != nil {
			return AuditPageData{}, err
		}
		event.Status = translateUsageStatus(status)
		events = append(events, event)
	}
	if err := eventRows.Err(); err != nil {
		return AuditPageData{}, err
	}

	return AuditPageData{
		Metrics: []AuditMetric{
			{Label: "最近 24 小时请求", Value: formatLargeNumber(int(totalRequests))},
			{Label: "失败请求", Value: formatLargeNumber(int(failedRequests))},
			{Label: "限流次数", Value: formatLargeNumber(int(rateLimitedRequests))},
			{Label: "上游错误", Value: formatLargeNumber(int(upstreamErrors))},
		},
		Events: events,
		Items:  items,
		Summaries: []AuditSummary{
			{Title: "真实摘要", Content: fmt.Sprintf("最近 24 小时共 %d 次请求，其中 %d 次失败。", totalRequests, failedRequests)},
			{Title: "数据来源", Content: "本页优先展示 llm_request_logs 与 llm_request_events 的真实聚合结果。"},
		},
	}, nil
}
```

```go
func (s postgresConsoleService) auditFromFallbackLogs(ctx context.Context) (AuditPageData, error) {
	rows, err := s.db.Query(ctx, `
		select
			to_char(created_at at time zone 'Asia/Shanghai', 'MM-DD HH24:MI'),
			tenant_id,
			endpoint,
			status_code,
			provider_display_name,
			latency_ms
		from audit_logs
		order by created_at desc
		limit 10;
	`)
	if err != nil {
		return AuditPageData{}, err
	}
	defer rows.Close()

	items := make([]AuditItem, 0, 10)
	for rows.Next() {
		var item AuditItem
		var statusCode int
		var latencyMS int64
		if err := rows.Scan(&item.Time, &item.Tenant, &item.Endpoint, &statusCode, &item.Provider, &latencyMS); err != nil {
			return AuditPageData{}, err
		}
		item.RequestModel = "-"
		item.UpstreamModel = "-"
		item.Status = fmt.Sprintf("%d", statusCode)
		item.Latency = fmt.Sprintf("%d ms", latencyMS)
		item.UsageSource = "fallback"
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return AuditPageData{}, err
	}

	return AuditPageData{
		Metrics: []AuditMetric{
			{Label: "最近 24 小时请求", Value: formatLargeNumber(len(items))},
			{Label: "失败请求", Value: "0"},
			{Label: "限流次数", Value: "0"},
			{Label: "上游错误", Value: "0"},
		},
		Events: nil,
		Items:  items,
		Summaries: []AuditSummary{
			{Title: "真实摘要", Content: "usage 日志为空，当前展示 fallback 审计日志。"},
		},
	}, nil
}
```

- [ ] **Step 5: 运行 Go 测试，确认后端主路径与 fallback 路径通过**

Run: `go test ./internal/service -run "TestPostgresConsoleServiceAudit(UsesUsageLogsAndEvents|FallsBackToAuditLogsWhenUsageDataMissing)" -count=1`

Expected: PASS，Go test 输出 `ok`。

- [ ] **Step 6: 再写一个失败的前端测试，锁定审计页新结构**

```tsx
test("审计页优先展示 usage 日志聚合结果", async () => {
  const fetchMock = mockFetch({
    "/api/admin/audit": {
      metrics: [
        { label: "最近 24 小时请求", value: "128" },
        { label: "失败请求", value: "4" },
        { label: "限流次数", value: "2" },
        { label: "上游错误", value: "1" },
      ],
      events: [
        {
          time: "04-24 19:08",
          type: "request_failed",
          status: "限流",
          detail: "请求失败（429）",
        },
      ],
      items: [
        {
          time: "04-24 19:08",
          tenant: "tenant_demo",
          endpoint: "/v1/chat/completions",
          request_model: "qwen-flash",
          upstream_model: "qwen-flash",
          status: "成功",
          provider: "DashScope 主路由",
          latency: "82 ms",
          usage_source: "上游返回",
        },
      ],
      summaries: [
        { title: "真实摘要", content: "最近 24 小时共 128 次请求，其中 4 次失败。" },
      ],
    },
  });

  renderRoute("/audit");

  expect(await screen.findByText("最近 24 小时请求")).toBeInTheDocument();
  expect(screen.getByText("真实摘要")).toBeInTheDocument();
  expect(screen.getByText("qwen-flash")).toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledWith("/api/admin/audit");
});
```

- [ ] **Step 7: 运行前端测试，确认当前页面还不能消费新结构**

Run: `npm test -- src/test/router.test.tsx -t "审计页优先展示 usage 日志聚合结果"`

Expected: FAIL，典型报错是 `Cannot read properties of undefined` 或者找不到 `最近 24 小时请求`。

- [ ] **Step 8: 同步前端类型并重写审计页，展示真实 metrics/events/items**

```ts
export type AuditMetric = {
  label: string;
  value: string;
};

export type AuditEvent = {
  time: string;
  type: string;
  status: string;
  detail: string;
};

export type AuditItem = {
  time: string;
  tenant: string;
  endpoint: string;
  request_model: string;
  upstream_model: string;
  status: string;
  provider: string;
  latency: string;
  usage_source: string;
};

export type AuditPageData = {
  metrics: AuditMetric[];
  events: AuditEvent[];
  items: AuditItem[];
  summaries: AuditSummary[];
};
```

```tsx
export function AuditPage() {
  const { data, loading, error } = useRemoteData(getAudit);

  if (loading) {
    return <LoadingSection text="正在加载审计日志..." />;
  }

  if (error || !data) {
    return <ErrorSection message={error ?? "审计数据加载失败。"} />;
  }

  return (
    <div className="page-grid">
      <div className="stats-grid stats-grid--four">
        {data.metrics.map((metric) => (
          <StatCard key={metric.label} label={metric.label} value={metric.value} />
        ))}
      </div>

      <div className="two-column-grid">
        <section className="section-card">
          <h2>最近事件流</h2>
          <ul className="event-timeline">
            {data.events.map((event) => (
              <li key={`${event.time}-${event.type}`}>
                <strong>{event.time}</strong>
                <span>{event.status}</span>
                <p>{event.detail}</p>
              </li>
            ))}
          </ul>
        </section>

        <div className="page-grid">
          {data.summaries.map((summary) => (
            <section key={summary.title} className="section-card">
              <h3>{summary.title}</h3>
              <p>{summary.content}</p>
            </section>
          ))}
        </div>
      </div>

      <section className="section-card">
        <h2>审计明细</h2>
        <DataTable
          columns={["时间", "租户", "端点", "请求模型", "上游模型", "状态", "供应商", "延迟", "计量来源"]}
          rows={data.items.map((item) => [
            item.time,
            item.tenant,
            item.endpoint,
            item.request_model,
            item.upstream_model,
            item.status,
            item.provider,
            item.latency,
            item.usage_source,
          ])}
        />
      </section>
    </div>
  );
}
```

```css
.stats-grid--four {
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.event-timeline li {
  border: 1px solid #e3eaf1;
  border-radius: 12px;
  padding: 12px;
  background: #f8fbfe;
}
```

- [ ] **Step 9: 跑后端测试 + 全量路由测试，确认审计页前后端联调完成**

Run: `go test ./internal/service -count=1 && npm test -- src/test/router.test.tsx`

Expected: 两段都 PASS；`go test` 输出 `ok`，Vitest 输出 `passed`。

- [ ] **Step 10: 提交审计重构**

```bash
git add gateway/internal/service/console_service.go gateway/internal/service/postgres_console_service.go gateway/internal/service/postgres_console_service_test.go web/src/lib/console-api.ts web/src/pages/audit.tsx web/src/styles.css web/src/test/router.test.tsx
git commit -m "feat: rebuild audit page from usage logs"
```

## Self-Review Checklist

- Spec coverage:
  - 左下角状态块隐藏且顶部 badge 保留：Task 1
  - API 密钥只显示脱敏值、复制完整值、多选 scopes：Task 2
  - 调用观测改为更强可视化且不新增图表库：Task 3
  - 审计页优先使用 `llm_request_logs / llm_request_events`，`audit_logs` 仅 fallback：Task 4
- Placeholder scan:
  - 已手动检查本计划，没有未展开的占位语句、延后实现说明或跨任务引用缩写。
- Type consistency:
  - API key scopes 在 `console-api.ts` 和 `api-keys.tsx` 中统一为 `APIKeyScope`
  - 审计结构在 Go 与 TypeScript 中统一使用 `metrics/events/items/summaries`
