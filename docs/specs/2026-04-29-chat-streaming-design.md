# 聊天流式响应改造设计

## 背景

当前网关的 `/v1/chat/completions` 只支持阻塞式响应。请求进入 [gateway/internal/http/handlers/chat.go](/root/liwenjian/ai_gateway/gateway/internal/http/handlers/chat.go) 后，会调用 `ChatProxyService.Complete(...)` 获取完整 `ChatResponse`，最后一次性 `c.JSON(...)` 返回。上游 provider 侧 [gateway/internal/provider/openai_client.go](/root/liwenjian/ai_gateway/gateway/internal/provider/openai_client.go) 也是完整 JSON decode 模式。

这会带来三个问题：

1. 用户无法在长回复场景下实时收到 token。
2. 平台无法对接 OpenAI/DashScope 兼容的 `stream=true` 调用方式。
3. 后续做实时观测、取消响应、首 token 延迟统计时没有协议基础。

## 目标

为 `/v1/chat/completions` 增加与 OpenAI 兼容的流式响应能力，同时保留现有阻塞式响应路径不变。

本次改造的完成标准：

1. 请求体支持 `stream: true`。
2. 当 `stream=true` 时，网关将上游 SSE 响应转发给客户端，并以 `data: ...\n\n` / `data: [DONE]\n\n` 形式输出。
3. 平台在流式请求结束后仍然记录 usage、状态码、延迟、失败信息。
4. 当上游流式响应未携带可信 usage 时，平台回落到现有估算逻辑，不破坏 token 观测。
5. 非流式请求行为和现有测试保持兼容。

## 方案对比

### 方案 A：真实流式代理，解析 SSE 并保留 usage 记录

做法：

- 请求模型增加 `stream` 字段。
- handler 检测 `stream=true` 后走流式分支。
- provider 层新增流式调用接口，请求上游 `stream=true`，逐行读取 SSE。
- service 层在流结束后汇总最终 usage record 并写库。

优点：

- 协议真实，兼容上游 LLM 厂商。
- 用户能获得真实的 token-by-token 返回。
- 观测链路仍然完整，后续扩展首 token 延迟和中断统计也有基础。

缺点：

- 改动面比阻塞式转发更大。
- 需要处理 SSE 分帧、结束标记、客户端断开和异常收尾。

### 方案 B：先拿完整响应，再伪装成分块输出

做法：

- 仍然调用现有阻塞式上游接口。
- 网关把最终完整文本拆成多个 chunk 输出给客户端。

优点：

- 实现最简单。

缺点：

- 不是真流式，没有首 token 延迟收益。
- 用户会误以为平台支持流式，实际只是“延迟后再切片”。
- 观测数据和协议语义都不准确。

### 方案 C：字节级透传上游流，不解析内容

做法：

- provider 返回原始流。
- handler 直接把字节流转给客户端，不解析 chunk。

优点：

- 改动较小。

缺点：

- 平台拿不到稳定的 usage / model / 完成状态。
- 出错和中断时难以形成高质量审计与失败记录。

## 结论

采用 **方案 A**。

这是当前唯一同时满足“真流式体验”和“平台侧审计/usage 不退化”的方案。

## 详细设计

### 1. 请求与接口边界

`service.ChatRequest` 增加：

- `Stream bool \`json:"stream,omitempty"\``

服务接口拆成两条能力：

- `Complete(...) (ChatResponse, error)` 保留现有阻塞式路径。
- `Stream(...) (ChatStreamResult, error)` 新增流式路径。

其中 `ChatStreamResult` 不直接暴露底层 HTTP response，而是提供：

- 状态码
- 响应头
- 流式写出函数
- 最终 `ChatResponse` 汇总结果
- 流式过程中的上游错误

这样 handler 只负责 HTTP 输出，service 负责鉴权上下文校验、usage 记录和上游调用编排。

### 2. HTTP Handler

聊天 handler 增加分支：

- `stream=false` 或未传：维持当前 `c.JSON(...)`
- `stream=true`：设置 `Content-Type: text/event-stream`、`Cache-Control: no-cache`，使用 Fiber/Fasthttp 的 stream writer 输出

输出策略：

1. 从 service 获取流式结果。
2. 逐个 chunk 写入客户端并 flush。
3. 正常结束时输出 `[DONE]`。
4. 如果流尚未开始就失败，返回标准 HTTP 错误。
5. 如果流进行中途失败，不再切换 HTTP 状态码，只结束流，并由 service 写入失败 usage。

### 3. Provider 适配

`OpenAIClient` 新增流式聊天调用。

请求：

- `POST /chat/completions`
- 仍使用平台保存的 provider API key
- body 内保留 `stream: true`

读取：

- 逐行解析 `data: ...`
- 忽略空行和非 `data:` 行
- 遇到 `data: [DONE]` 结束
- 对 JSON chunk 做最小解析，用于收集：
  - `model`
  - `choices[].delta.role/content`
  - 可能存在的 `usage`

转发：

- 按原格式把 `data: ...\n\n` 写给下游客户端

汇总：

- 在 provider 层构造最终 `ChatResponse`
- 将各个 `delta.content` 追加为最终 assistant message
- 若上游 chunk 含 usage，则写入 `resp.Usage`

### 4. Usage / 审计

流式请求完成后，仍以 `NewChatUsageRecord(...)` 落库。

处理规则：

- 上游最终携带可信 usage：`UsageSourceUpstream`
- 未携带 usage：按现有 `estimateChatUsage(...)` 估算，`UsageSourceEstimated`
- 上游中途失败：按 HTTP 状态码映射 `UsageStatus`
- 客户端主动断开：记录为响应阶段失败，状态码沿用已知上游状态或 `499/200` 语义中的安全降级值；本次先按“请求成功建立但响应未完整写出”归到 `failed/response`

说明：

现有表结构已能记录 request log / failure / lifecycle event，本次不新增表结构，只复用现有记录模型。

### 5. 测试策略

至少补齐以下测试：

1. provider 流式解析测试
2. chat handler 在 `stream=true` 时返回 SSE 的集成测试
3. 流式完成后 usage event 仍被发布
4. 非流式路径回归测试

## 风险与边界

本次不做：

1. 前端控制台的流式 playground 展示优化
2. 首 token 延迟单独落库
3. 中途取消请求的专门状态枚举
4. embeddings / RAG 的流式化

这些能力可以在聊天流式链路稳定后继续追加。
