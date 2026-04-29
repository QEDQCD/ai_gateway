# Playground Streaming And First Token Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为控制台调试场增加可停止的流式响应模式，并把首 Token 延迟与客户端中断事件接入调用观测和审计页面。

**Architecture:** 复用现有聊天流式代理和控制台 HTTP 体系，新增一个流式调试场接口；在 usage 记录主表中增加 `first_token_latency_ms`，在事件表中补充流式生命周期事件；前端使用 `fetch + ReadableStream + AbortController` 实现开始 / 停止交互。

**Tech Stack:** Go, Fiber, PostgreSQL migrations, React, TypeScript, ReadableStream, AbortController

---

### Task 1: 扩展数据库与观测模型

**Files:**
- Create: `gateway/db/migrations/0010_add_first_token_latency_to_usage_logs.sql`
- Modify: `gateway/internal/service/usage_recording.go`
- Modify: `gateway/internal/service/console_service.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Test: `gateway/internal/service/usage_recording_test.go`
- Test: `gateway/internal/service/postgres_console_service_test.go`

- [ ] **Step 1: 先写失败测试，验证 request log 尚未携带首 Token 延迟**
- [ ] **Step 2: 增加 migration，为 `llm_request_logs` 添加 `first_token_latency_ms`**
- [ ] **Step 3: 扩展 `UsageRecord` 与控制台 DTO，携带 `first_token_latency_ms`**
- [ ] **Step 4: 在 usage 落库和查询 SQL 中读写新字段**
- [ ] **Step 5: 运行 `go test ./internal/service ./db -run 'TestUsage|TestPostgresConsoleService'`**
- [ ] **Step 6: Commit**

### Task 2: 扩展聊天流式链路，采集首 Token 与客户端中断

**Files:**
- Modify: `gateway/internal/provider/openai_client.go`
- Modify: `gateway/internal/provider/openai_client_stream_test.go`
- Modify: `gateway/internal/service/proxy_service.go`
- Modify: `gateway/internal/service/proxy_service_test.go`

- [ ] **Step 1: 先写 provider/service 失败测试，验证无法返回首 Token 指标和中断信号**
- [ ] **Step 2: 为流式聊天返回类型增加首 Token 回调和中断标记**
- [ ] **Step 3: 在 provider 流式消费时识别首个非空内容 token**
- [ ] **Step 4: 在 service 收尾时记录 `first_token_latency_ms`，并为中断场景写 `client_aborted` 事件**
- [ ] **Step 5: 运行 `go test ./internal/provider ./internal/service -run 'TestOpenAIClient|TestChatProxy'`**
- [ ] **Step 6: Commit**

### Task 3: 为控制台调试场增加流式接口

**Files:**
- Modify: `gateway/internal/service/console_service.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/http/handlers/admin.go`
- Modify: `gateway/internal/http/router.go`
- Modify: `gateway/internal/http/router_test.go`

- [ ] **Step 1: 先写 handler / router 失败测试，验证 `/admin/playground/chat/stream` 不存在**
- [ ] **Step 2: 增加 `StreamPlayground(...)` 服务接口和流式 session 类型**
- [ ] **Step 3: 在 postgres console service 中实现流式调试场，并写入调试场审计记录**
- [ ] **Step 4: 暴露 `POST /admin/playground/chat/stream`，返回 `text/event-stream`**
- [ ] **Step 5: 运行 `go test ./internal/http ./cmd/server -run 'TestAdmin|TestPlayground'`**
- [ ] **Step 6: Commit**

### Task 4: 为调用观测与审计补充首 Token 与流式事件展示

**Files:**
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/service/postgres_console_service_test.go`
- Modify: `web/src/lib/console-api.ts`
- Modify: `web/src/pages/usage.tsx`
- Modify: `web/src/pages/audit.tsx`

- [ ] **Step 1: 先写后端测试，验证 usage requests / audit 结果中还没有首 Token 字段**
- [ ] **Step 2: 在 usage requests / audit 查询结果中增加 `first_token_latency`**
- [ ] **Step 3: 为新增事件类型增加中文映射和事件描述**
- [ ] **Step 4: 调整前端表格与事件流，显示首 Token 与客户端中断文案**
- [ ] **Step 5: 运行 `go test ./internal/service -run 'TestPostgresConsoleServiceUsageRequests|TestPostgresConsoleServiceAudit'`**
- [ ] **Step 6: Commit**

### Task 5: 实现调试场前端流式交互与开始/停止

**Files:**
- Modify: `web/src/lib/console-api.ts`
- Modify: `web/src/pages/playground.tsx`
- Modify: `web/src/components/console/*`（如需要，尽量保持最小改动）

- [ ] **Step 1: 先写前端失败测试或最小行为测试，验证调试场还没有流式开关和停止按钮**
- [ ] **Step 2: 为调试场请求模型新增 `stream` 字段与流式请求工具函数**
- [ ] **Step 3: 用 `AbortController` 实现开始 / 停止**
- [ ] **Step 4: 在结果区实时追加内容，并显示首 Token 延迟、总延迟、最终状态**
- [ ] **Step 5: 运行前端测试或构建校验命令**
- [ ] **Step 6: Commit**

### Task 6: 全量回归与文档收尾

**Files:**
- Modify: `docs/specs/2026-04-29-playground-streaming-and-first-token-observability-design.md`
- Modify: `docs/plans/2026-04-29-playground-streaming-and-first-token-observability-plan.md`

- [ ] **Step 1: 运行后端全量测试 `go test ./...`**
- [ ] **Step 2: 运行前端校验命令，至少覆盖类型检查 / 构建**
- [ ] **Step 3: 自检文档与实现是否一致，补掉歧义**
- [ ] **Step 4: Commit**
