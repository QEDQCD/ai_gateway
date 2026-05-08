# Enterprise Governance Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现一期“部门级治理”能力：审批增加租户自然月金额上限、admin 账单页展示租户账单与关联供应商、聊天/向量请求的基础输入校验与敏感信息脱敏。

**Architecture:** 复用现有租户额度、费用账本和调用观测体系，不新增独立账单主表。金额上限作为 `tenant_quota_policies` 的扩展字段，账单页优先读 `tenant_usage_ledger` 和 `llm_request_logs` 聚合结果，输入安全采用“请求前基础校验 + 落库前/展示前脱敏”的轻量闭环，不改写实际转发给上游的正文。

**Tech Stack:** Go, Fiber, PostgreSQL, React, React Router, Vitest, pgx, 现有 `ConsoleService` / `QuotaGuard` / `console-api`

---

### Task 1: 为租户策略增加月度金额上限字段

**Files:**
- Create: `gateway/db/migrations/0018_add_tenant_cost_limit.sql`
- Modify: `gateway/db/runtime.go`
- Modify: `gateway/db/runtime_test.go`
- Modify: `gateway/internal/store/models.go`
- Modify: `gateway/internal/service/console_service.go`

- [ ] **Step 1: 写迁移测试目标说明并确认字段落点**

本任务的目标是把租户自然月金额上限放到现有 `tenant_quota_policies`，避免新建第二套治理表。字段使用微元，命名为 `cost_limit_microyuan`。

- [ ] **Step 2: 新增数据库迁移**

在 `gateway/db/migrations/0018_add_tenant_cost_limit.sql` 写入：

```sql
alter table tenant_quota_policies
  add column cost_limit_microyuan bigint not null default 0 check (cost_limit_microyuan >= 0);
```

- [ ] **Step 3: 更新 seed 数据**

修改 `gateway/db/runtime.go` 中初始化 `tenant_quota_policies` 的语句，补充 `cost_limit_microyuan`，默认给演示租户一个非零值，例如：

```sql
insert into tenant_quota_policies (tenant_id, period_type, request_limit, token_limit, cost_limit_microyuan, effective_from) values
  ('tenant_alpha', 'monthly', 1800000, 24000000, 10000000000, now()),
  ('tenant_beta', 'monthly', 1200000, 16000000, 10000000000, now()),
  ('tenant_gamma', 'monthly', 900000, 12000000, 10000000000, now())
```

- [ ] **Step 4: 更新运行时数据库测试**

在 `gateway/db/runtime_test.go` 的 schema 断言里加入：

- `tenant_quota_policies.cost_limit_microyuan` 列存在
- 类型为 `bigint`
- 默认值为 `0`
- 负数插入失败

- [ ] **Step 5: 扩展服务层类型**

修改 `gateway/internal/service/console_service.go` 中 `ApproveApplicationRequest`，增加：

```go
CostLimitMicroyuan int64 `json:"cost_limit_microyuan"`
```

同时扩展需要返回租户配额摘要的结构，增加：

```go
CostLimit string `json:"cost_limit,omitempty"`
CostUsed string `json:"cost_used,omitempty"`
CostRemaining string `json:"cost_remaining,omitempty"`
```

如果当前 `TenantQuotaSummary` 仍只服务 token/request，可先在账单页单独返回成本概览，不强行塞进已有摘要。

- [ ] **Step 6: 运行迁移相关测试**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./db -run TestApplyMigrations -v`
Expected: PASS，且新字段断言通过

- [ ] **Step 7: Commit**

```bash
git add gateway/db/migrations/0018_add_tenant_cost_limit.sql gateway/db/runtime.go gateway/db/runtime_test.go gateway/internal/service/console_service.go gateway/internal/store/models.go
git commit -m "feat: add tenant monthly cost limit field"
```

### Task 2: 审批链路支持月度金额上限

**Files:**
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/service/postgres_console_service_test.go`
- Modify: `gateway/internal/http/router_test.go`
- Modify: `web/src/lib/console-api.ts`
- Modify: `web/src/pages/admin-applications.tsx`
- Modify: `web/src/test/router.test.tsx`

- [ ] **Step 1: 先写前后端失败测试**

后端测试新增两个场景：

1. `ApproveApplication` 未传 `cost_limit_microyuan` 时按默认值落库或按服务层默认值处理
2. 传入显式金额上限时，`tenant_quota_policies.cost_limit_microyuan` 正确持久化

前端测试新增两个场景：

1. 审批弹窗显示“月度金额上限（￥）”
2. 提交审批 payload 时包含金额字段

- [ ] **Step 2: 扩展 HTTP 与前端 payload 类型**

修改 `web/src/lib/console-api.ts`：

```ts
export type ApproveApplicationPayload = {
  actor_id: string;
  comment: string;
  tenant_id: string;
  token_limit: number;
  cost_limit_microyuan: number;
  allowed_models: string[];
};
```

保持传输单位为微元，避免后端处理浮点。前端负责把“元”转换成微元。

- [ ] **Step 3: 扩展审批弹窗状态与表单**

在 `web/src/pages/admin-applications.tsx`：

- 新增本地状态 `costLimitYuan`
- 默认值设为 `10000`
- `handleApprove()` 中把元转成微元：

```ts
const approvalCostLimitYuan = Number(costLimitYuan.trim());
const approvalCostLimitMicroyuan = Math.round(approvalCostLimitYuan * 1_000_000);
```

并在 payload 中提交：

```ts
cost_limit_microyuan: approvalCostLimitMicroyuan,
```

- [ ] **Step 4: 在审批弹窗新增输入控件**

在 `web/src/pages/admin-applications.tsx` 的审批表单里增加：

```tsx
<label className="field-shell">
  月度金额上限（￥）
  <input
    type="number"
    min={1}
    value={costLimitYuan}
    disabled={submitting}
    onChange={(event) => setCostLimitYuan(event.target.value)}
  />
</label>
```

校验要求：

- 必须是正数
- 小数允许但最终四舍五入到微元

- [ ] **Step 5: 扩展后端审批入参校验与写库 SQL**

在 `gateway/internal/service/postgres_console_service.go` 的 `ApproveApplication()`：

- 读取 `req.CostLimitMicroyuan`
- 若 `<= 0`，直接返回 `400`
- 在 `insert into tenant_quota_policies` 语句中增加 `cost_limit_microyuan`
- 在 `on conflict` 更新中同步更新 `cost_limit_microyuan`

- [ ] **Step 6: 更新路由层测试**

修改 `gateway/internal/http/router_test.go` 的审批路由测试，请求 body 增加：

```json
"cost_limit_microyuan":10000000000
```

并断言 captured request 中该字段正确透传。

- [ ] **Step 7: 跑定向测试**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/service ./internal/http -run 'ApproveApplication|AdminApplications' -v`
Expected: PASS

Run: `cd /root/liwenjian/ai_gateway/web && npm test -- src/test/router.test.tsx -t "审批|账号申请"`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add gateway/internal/service/postgres_console_service.go gateway/internal/service/postgres_console_service_test.go gateway/internal/http/router_test.go web/src/lib/console-api.ts web/src/pages/admin-applications.tsx web/src/test/router.test.tsx
git commit -m "feat: add monthly tenant cost limit to approval flow"
```

### Task 3: 在租户额度校验器中加入金额超限拦截

**Files:**
- Modify: `gateway/internal/service/tenant_quota_guard.go`
- Modify: `gateway/internal/service/tenant_quota_guard_test.go`
- Modify: `gateway/internal/service/auth_service_test.go`

- [ ] **Step 1: 写失败测试**

在 `gateway/internal/service/tenant_quota_guard_test.go` 增加场景：

- 同一自然月内 `tenant_usage_ledger.total_cost_microyuan >= tenant_quota_policies.cost_limit_microyuan`
- 调用 `CheckTenantQuota()` 返回 `ErrQuotaExceeded`

- [ ] **Step 2: 扩展额度查询 SQL**

修改 `DatabaseQuotaGuard.CheckTenantQuota()` 查询：

- 读取 `cost_limit_microyuan`
- 在同一自然月内聚合 `tenant_usage_ledger.total_cost_microyuan`

可采用：

```sql
select
  p.request_limit,
  p.token_limit,
  p.cost_limit_microyuan,
  coalesce(u.requests_used, 0),
  coalesce(u.tokens_used, 0),
  coalesce(l.total_cost_microyuan, 0)
from tenant_quota_policies p
left join tenant_quota_usage_periods u ...
left join (
  select tenant_id, coalesce(sum(total_cost_microyuan), 0) as total_cost_microyuan
  from tenant_usage_ledger
  where tenant_id = $1
    and bucket_start >= $2
    and bucket_start < $3
  group by tenant_id
) l on l.tenant_id = p.tenant_id
```

- [ ] **Step 3: 增加金额判断**

在 `CheckTenantQuota()` 中加入：

```go
if requestLimit > 0 && requestsUsed >= requestLimit {
  return ErrQuotaExceeded
}
if tokenLimit > 0 && tokensUsed >= tokenLimit {
  return ErrQuotaExceeded
}
if costLimitMicroyuan > 0 && totalCostMicroyuan >= costLimitMicroyuan {
  return ErrQuotaExceeded
}
```

不要把 `0` 当成超限，保留“0=未启用”的兼容语义。

- [ ] **Step 4: 扩展认证链路测试**

在 `gateway/internal/service/auth_service_test.go` 增加一个 fake quota guard 场景，确认 quota exceeded 仍然在认证阶段被正确拦截，不需要改错误码语义。

- [ ] **Step 5: 跑定向测试**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/service -run 'TestDatabaseQuotaGuardRejectsExhaustedTenant|CostLimit|AuthService' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add gateway/internal/service/tenant_quota_guard.go gateway/internal/service/tenant_quota_guard_test.go gateway/internal/service/auth_service_test.go
git commit -m "feat: enforce tenant monthly cost limit"
```

### Task 4: 增加 admin 租户账单聚合接口

**Files:**
- Modify: `gateway/internal/service/console_service.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/service/postgres_console_service_test.go`
- Modify: `gateway/internal/http/handlers/admin.go`
- Modify: `gateway/internal/http/router.go`
- Modify: `gateway/internal/http/router_test.go`

- [ ] **Step 1: 定义账单数据结构**

在 `gateway/internal/service/console_service.go` 增加：

- `TenantBillingSummary`
- `TenantBillingProviderItem`
- `TenantBillingModelItem`
- `TenantBillingAPIKeyItem`
- `TenantBillingPageData`
- `TenantBillingQuery`

字段要足够支撑页面展示：

- summary: `tenant_id`, `tenant_name`, `month`, `total_requests`, `total_tokens`, `total_cost`, `active_api_keys`
- providers: `provider`, `request_count`, `total_tokens`, `total_cost`, `share`
- models: `model`, `provider`, `request_count`, `total_cost`
- api_keys: `platform_api_key_id`, `name`, `request_count`, `total_cost`

- [ ] **Step 2: 给 ConsoleService 增加方法**

在接口中增加：

```go
TenantBilling(ctx context.Context, query TenantBillingQuery) (TenantBillingPageData, error)
```

- [ ] **Step 3: 在 postgresConsoleService 实现聚合查询**

新增实现：

- 校验 `tenant_id` 和 `month`
- 按自然月时间窗口查询 `tenant_usage_ledger` 获取 summary
- 查询 `llm_request_logs` 聚出 provider/model/api_key 三组分项
- provider 通过 `provider_credential_id -> provider_credentials` 映射出显示名

- [ ] **Step 4: 新增 admin HTTP 路由**

在 `gateway/internal/http/router.go` 增加：

```go
admin.Get("/billing/tenant", handlers.ConsoleTenantBilling(deps.ConsoleService))
```

在 `gateway/internal/http/handlers/admin.go` 增加解析器：

- 读取 `tenant_id`
- 读取 `month`，格式如 `2026-05`
- 返回 JSON

- [ ] **Step 5: 补后端路由与服务测试**

测试至少覆盖：

1. 缺失 `tenant_id` 返回 400
2. 非法 `month` 返回 400
3. 正常请求返回 summary + providers + models + api_keys
4. provider 分项能正确按真实调用供应商聚合

- [ ] **Step 6: 跑定向测试**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/service ./internal/http -run 'TenantBilling|Billing' -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add gateway/internal/service/console_service.go gateway/internal/service/postgres_console_service.go gateway/internal/service/postgres_console_service_test.go gateway/internal/http/handlers/admin.go gateway/internal/http/router.go gateway/internal/http/router_test.go
git commit -m "feat: add admin tenant billing endpoint"
```

### Task 5: 新增 admin 账单页面与导航

**Files:**
- Create: `web/src/pages/admin-billing.tsx`
- Modify: `web/src/app/router.tsx`
- Modify: `web/src/lib/console-api.ts`
- Modify: `web/src/test/router.test.tsx`
- Modify: `web/src/styles.css`

- [ ] **Step 1: 先写前端失败测试**

在 `web/src/test/router.test.tsx` 增加场景：

1. admin 左侧导航出现“账单”
2. 访问 `/billing` 时请求 `/api/admin/billing/tenant?...`
3. 页面展示 summary/provider/model/api key 表格

- [ ] **Step 2: 扩展 console-api 类型与请求函数**

在 `web/src/lib/console-api.ts` 增加：

- `TenantBillingPageData`
- `getTenantBilling(tenantID: string, month: string)`

- [ ] **Step 3: 创建账单页面**

在 `web/src/pages/admin-billing.tsx`：

- 提供月份输入
- 提供租户输入或下拉
- 加载账单数据
- 展示概览卡片
- 展示三张表：供应商 / 模型 / API Key

尽量复用现有：

- `StatCard`
- `DataTable`
- `useRemoteData`

- [ ] **Step 4: 注册路由与导航**

在 `web/src/app/router.tsx` 的 adminNavigation 里增加：

```ts
{
  path: "/billing",
  label: "账单",
  title: "账单",
  description: "按租户查看自然月费用、供应商和模型分项。",
  element: <AdminBillingPage />,
}
```

- [ ] **Step 5: 补最小样式**

如果页面需要筛选表单或概览卡微调，只在 `web/src/styles.css` 做最小补充，不重构全局布局。

- [ ] **Step 6: 跑前端测试**

Run: `cd /root/liwenjian/ai_gateway/web && npm test -- src/test/router.test.tsx -t "账单|租户管理"`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add web/src/pages/admin-billing.tsx web/src/app/router.tsx web/src/lib/console-api.ts web/src/test/router.test.tsx web/src/styles.css
git commit -m "feat: add admin tenant billing page"
```

### Task 6: 为聊天与向量入口增加基础输入校验

**Files:**
- Modify: `gateway/internal/service/proxy_service.go`
- Modify: `gateway/internal/http/handlers/chat.go`
- Modify: `gateway/internal/http/handlers/embeddings.go`
- Modify: `gateway/internal/service/proxy_service_test.go`
- Modify: `gateway/internal/http/router_test.go`

- [ ] **Step 1: 写失败测试**

覆盖至少这些场景：

1. `model` 为空
2. `messages` 为空
3. 某条 `message.content` 为空
4. `max_tokens` 非法
5. embeddings `input` 为空

预期：返回 `400 bad request`，且不请求上游。

- [ ] **Step 2: 在 service 层补请求校验函数**

在 `gateway/internal/service/proxy_service.go` 增加最小校验函数：

```go
func validateChatRequest(req ChatRequest) error
func validateEmbeddingsRequest(req EmbeddingsRequest) error
```

校验只做结构和长度，不做语义审核。

- [ ] **Step 3: 在 handler 或 proxy 入口调用校验**

推荐在 handler 里先校验，再调用 proxy。这样：

- 非法请求更早失败
- 不进入上游
- 也不需要伪造 provider target

- [ ] **Step 4: 统一错误响应**

无须新增新的错误类型，直接返回 400，消息例如：

- `invalid request body`
- `model is required`
- `messages is required`

尽量保持控制面和调用面错误风格一致。

- [ ] **Step 5: 跑定向测试**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/service ./internal/http -run 'Chat|Embedding|invalid request' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add gateway/internal/service/proxy_service.go gateway/internal/http/handlers/chat.go gateway/internal/http/handlers/embeddings.go gateway/internal/service/proxy_service_test.go gateway/internal/http/router_test.go
git commit -m "feat: validate gateway request payloads"
```

### Task 7: 为日志与展示增加敏感信息脱敏

**Files:**
- Create: `gateway/internal/security/redaction.go`
- Create: `gateway/internal/security/redaction_test.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/service/usage_recording.go`
- Modify: `gateway/internal/service/postgres_console_service_test.go`
- Modify: `web/src/pages/audit.tsx`
- Modify: `web/src/pages/playground.tsx`
- Modify: `web/src/test/router.test.tsx`

- [ ] **Step 1: 先写纯函数测试**

在 `gateway/internal/security/redaction_test.go` 覆盖：

- 手机号 `13812345678 -> 138XXX5678`
- 身份证号 `110101199001011234 -> 110101XXXXXX1234`
- 邮箱 `alice@example.com -> a***e@example.com`（按你最终规则固定）

- [ ] **Step 2: 实现脱敏纯函数**

在 `gateway/internal/security/redaction.go` 增加：

```go
func RedactText(input string) string
```

实现基于正则顺序替换，保证：

- 幂等
- 空字符串安全
- 不抛异常

- [ ] **Step 3: 在后端持久化前接入脱敏**

在 `gateway/internal/service/usage_recording.go` 和 `gateway/internal/service/postgres_console_service.go` 中，对以下写库内容接入 `RedactText`：

- 错误消息
- playground response excerpt
- 审计展示中可能包含的自由文本

注意：

- 只改写存储和展示内容
- 不改写真正发给上游的用户 prompt

- [ ] **Step 4: 在前端展示侧补二次保护**

如 `AuditPage`、`PlaygroundPage` 存在直接展示自由文本摘要的地方，增加一个最小展示格式化函数，对历史脏数据做保护。

- [ ] **Step 5: 补集成测试**

后端测试断言：

- 写入日志或 playground 后，数据库中的内容已脱敏

前端测试断言：

- 返回未脱敏手机号时，页面展示的是脱敏结果

- [ ] **Step 6: 跑定向测试**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/security ./internal/service -run 'Redact|Playground|Audit' -v`
Expected: PASS

Run: `cd /root/liwenjian/ai_gateway/web && npm test -- src/test/router.test.tsx -t "审计|调试场"`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add gateway/internal/security/redaction.go gateway/internal/security/redaction_test.go gateway/internal/service/postgres_console_service.go gateway/internal/service/usage_recording.go gateway/internal/service/postgres_console_service_test.go web/src/pages/audit.tsx web/src/pages/playground.tsx web/src/test/router.test.tsx
git commit -m "feat: redact sensitive data in logs and console views"
```

### Task 8: 全链路验证与部署

**Files:**
- Verify only: `deploy/compose/compose.yml`
- Verify only: `web/dist` (build output)

- [ ] **Step 1: 跑一期相关后端测试集合**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/service ./internal/http -run 'ApproveApplication|QuotaGuard|TenantBilling|Chat|Embedding|Redact' -v`
Expected: PASS

- [ ] **Step 2: 跑一期相关前端测试集合**

Run: `cd /root/liwenjian/ai_gateway/web && npm test -- src/test/router.test.tsx -t "账号申请|账单|租户管理|审计|调试场"`
Expected: PASS

- [ ] **Step 3: 构建前端**

Run: `cd /root/liwenjian/ai_gateway/web && npm run build`
Expected: build 成功

- [ ] **Step 4: 重新部署 web 与 gateway**

Run: `cd /root/liwenjian/ai_gateway && DOCKER_CONFIG=/tmp/docker-config docker compose --env-file deploy/compose/.env.local -f deploy/compose/compose.yml up -d --build web gateway`
Expected: 容器正常启动

- [ ] **Step 5: 做健康检查**

Run: `curl -fsS http://127.0.0.1:31873/ >/dev/null && echo WEB_OK && curl -fsS http://127.0.0.1:32658/healthz >/dev/null && echo GATEWAY_OK`
Expected: 输出 `WEB_OK` 和 `GATEWAY_OK`

- [ ] **Step 6: Commit**

```bash
git add docs/plans/2026-05-07-enterprise-governance-phase1-implementation-plan.md
git commit -m "docs: add phase1 enterprise governance implementation plan"
```
