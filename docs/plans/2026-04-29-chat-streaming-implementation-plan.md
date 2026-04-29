# Chat Streaming Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `/v1/chat/completions` 增加兼容 OpenAI 的流式响应能力，并保持 usage / 失败记录链路完整。

**Architecture:** 在现有阻塞式聊天代理旁新增一条流式聊天链路。HTTP handler 负责输出 SSE，service 负责鉴权与 usage 收尾，provider 负责调用并解析上游 SSE，再汇总为最终 `ChatResponse` 供记录层复用。

**Tech Stack:** Go, Fiber, fasthttp stream writer, OpenAI-compatible SSE, Go testing

---

### Task 1: 扩展聊天请求模型与服务接口

**Files:**
- Modify: `gateway/internal/service/proxy_service.go`
- Test: `gateway/internal/service/proxy_service_test.go`

- [ ] Step 1: 先为 `stream=true` 增加失败测试，确认当前接口不支持
- [ ] Step 2: 在 `ChatRequest` 中增加 `Stream` 字段
- [ ] Step 3: 为 `ChatProxyService` / `UpstreamChatClient` 增加流式方法定义和返回类型
- [ ] Step 4: 运行 `go test ./internal/service -run TestChatProxy`
- [ ] Step 5: Commit

### Task 2: 实现 provider 流式 SSE 读取与响应汇总

**Files:**
- Modify: `gateway/internal/provider/openai_client.go`
- Create: `gateway/internal/provider/openai_client_stream_test.go`

- [ ] Step 1: 先写 provider 流式测试，覆盖 chunk 透传、`[DONE]`、usage 汇总
- [ ] Step 2: 实现 `StreamComplete(...)`
- [ ] Step 3: 解析 `data:` 行并汇总最终 `ChatResponse`
- [ ] Step 4: 运行 `go test ./internal/provider -run TestOpenAIClient`
- [ ] Step 5: Commit

### Task 3: 实现 service 层流式编排和 usage 收尾

**Files:**
- Modify: `gateway/internal/service/proxy_service.go`
- Modify: `gateway/internal/service/proxy_service_test.go`

- [ ] Step 1: 写 service 流式测试，覆盖成功记录和失败记录
- [ ] Step 2: 实现 `chatProxyService.Stream(...)`
- [ ] Step 3: 复用 `NewChatUsageRecord(...)` 记录最终 usage
- [ ] Step 4: 运行 `go test ./internal/service -run TestChatProxy`
- [ ] Step 5: Commit

### Task 4: 实现 HTTP handler 的 SSE 输出

**Files:**
- Modify: `gateway/internal/http/handlers/chat.go`
- Modify: `gateway/tests/integration/proxy_test.go`

- [ ] Step 1: 先写集成测试，验证 `stream=true` 返回 `text/event-stream`
- [ ] Step 2: 在 handler 中根据 `req.Stream` 切换 JSON / SSE 分支
- [ ] Step 3: 使用 stream writer 按 chunk 输出并 flush
- [ ] Step 4: 运行 `go test ./tests/integration -run TestChatCompletionProxy`
- [ ] Step 5: Commit

### Task 5: 回归验证与文档收尾

**Files:**
- Modify: `docs/specs/2026-04-29-chat-streaming-design.md`
- Modify: `docs/plans/2026-04-29-chat-streaming-implementation-plan.md`

- [ ] Step 1: 运行聊天相关单元测试与集成测试
- [ ] Step 2: 如有接口行为偏差，先补测试再修正
- [ ] Step 3: 自检文档与实现是否一致
- [ ] Step 4: Commit
