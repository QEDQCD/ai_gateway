# 调试场流式交互与首 Token 观测设计

## 背景

当前平台已经支持 `/v1/chat/completions` 的 `stream=true`，但控制台“调试场”仍然只有阻塞式体验：

1. 管理员无法在控制台中实时查看流式返回内容。
2. 无法手动停止正在进行的长响应请求。
3. 平台没有持久化“首 Token 延迟”这一对调试和性能分析都很关键的指标。
4. 用户主动停止流式请求后，现有审计与调用观测页面无法区分“请求失败”和“已返回部分结果后由用户停止”。

这使得平台虽然具备网关级流式代理能力，但控制台调试体验和观测体系仍停留在阻塞式阶段。

## 目标

本次设计只覆盖两个目标：

1. 为控制台调试场增加“默认非流式，勾选后流式，并支持开始 / 停止”的真实交互。
2. 为平台增加“首 Token 延迟”和“客户端中断事件”的真实记录与展示能力，并进入调用观测 / 审计视图。

## 用户确认的行为口径

### 1. 调试场交互

采用：

- 默认非流式
- 勾选后进入流式模式
- 支持“开始流式”与“停止”

### 2. 客户端主动中断的统计语义

采用：

- 只要上游已经开始返回内容，请求主状态仍记为“成功”
- 额外在事件流中记录“客户端中断”

### 3. 首 Token 延迟的展示范围

采用：

- 调试场显示本次请求的首 Token 延迟
- 调用观测和审计页也显示历史记录

## 方案对比

### 方案 A：在现有控制台 HTTP 体系上新增 SSE 调试场接口与观测字段

做法：

- 保留现有阻塞式调试场接口
- 新增流式调试场接口
- 前端用 `fetch + ReadableStream + AbortController` 实现开始/停止
- 数据库增加 `first_token_latency_ms`
- 在 `llm_request_events` 中新增流式生命周期事件

优点：

- 最大化复用现有控制台服务、网关鉴权、usage 记录和审计页面。
- 不需要新建 WebSocket 会话层。
- 与已经完成的 `/v1/chat/completions` 流式代理天然一致。

缺点：

- 需要同时改动前端、console service、usage 记录和展示层。

### 方案 B：单独为调试场建立 WebSocket 会话服务

做法：

- 调试场不复用现有 HTTP 请求模型，改走专用双向通道

优点：

- 交互更灵活，后续容易加入参数热更新

缺点：

- 新协议、新状态机、新鉴权面，超出当前需求
- 与现有审计和 usage 体系容易出现双轨

### 方案 C：只在前端做流式展示，不做观测落库

做法：

- 调试场拿到流式响应后只做页面显示
- 不记录首 Token 延迟和客户端中断

优点：

- 上线最快

缺点：

- 不满足“调用观测 / 审计也要展示历史记录”的明确要求

## 结论

采用 **方案 A**。

这是当前唯一同时满足“真实流式调试体验”“停止能力”“首 Token 指标持久化”“审计与调用观测统一口径”的方案。

## 详细设计

## 1. 数据模型

### 1.1 `llm_request_logs`

新增字段：

- `first_token_latency_ms integer not null default 0 check (first_token_latency_ms >= 0)`

语义：

- 非流式请求：默认 `0`
- 流式请求但未收到任何 token：`0`
- 流式请求收到首 token：记录从请求开始到首个上游内容 chunk 被平台确认的耗时

原因：

- 这是请求级指标，适合放在 request log 主表中，便于调用明细、筛选与后续聚合。

### 1.2 `llm_request_events`

继续复用现有表，不新增结构字段，只新增事件类型语义：

- `stream_started`
- `first_token_emitted`
- `client_aborted`
- `stream_completed`

说明：

- 当前 `event_type` 没有数据库枚举约束，因此不需要专门 migration 扩展 check constraint。

## 2. 后端服务边界

### 2.1 调试场接口

保留：

- `POST /admin/playground/chat`

新增：

- `POST /admin/playground/chat/stream`

请求体：

```json
{
  "model": "qwen-flash",
  "prompt": "请总结最近一次发布。",
  "stream": true
}
```

说明：

- 虽然接口本身已是流式专用，但请求体仍保留 `stream` 字段，方便前后端状态对齐，也便于未来合并接口时保持兼容。

### 2.2 SSE 响应协议

调试场流式接口返回 `text/event-stream`，包含四类事件：

1. `event: meta`
   - 请求开始时立即发出
   - 包含 `request_id`、`model`、`endpoint`
2. `event: token`
   - 每次收到上游可转发 chunk 时发出
   - `data` 内容为上游原始 `data: ...` JSON 片段
3. `event: stats`
   - 在首 token 到达时发出一次
   - 请求结束时再发出一次
   - 包含 `first_token_latency_ms`、`status`、`latency_ms`
4. `event: done`
   - 流式请求正常结束时发出

中断场景：

- 若前端主动 abort，服务端不保证客户端能收到 `done`，但必须在后端事件流中落 `client_aborted`

### 2.3 Console Service

现有接口：

- `RunPlayground(ctx, req PlaygroundRunRequest) (PlaygroundRunResponse, error)`

新增接口：

- `StreamPlayground(ctx, req PlaygroundRunRequest) (PlaygroundStreamSession, error)`

新增类型：

- `PlaygroundStreamSession`
  - `ContentType string`
  - `StatusCode int`
  - `Run(func([]byte) error) (PlaygroundRunResponse, error)`

这样保持与现有聊天流式代理一致的抽象：

- handler 负责 HTTP 写出
- console service 负责模型解析、调试场审计、首 token 指标采集和结果收尾

## 3. 首 Token 采集

### 3.1 采集时机

首 Token 的定义：

- 第一个包含非空 `delta.content` 的上游聊天 chunk 到达平台的时间点

不计入：

- 仅包含 role 的 chunk
- 空 chunk
- usage chunk
- `[DONE]`

### 3.2 采集逻辑

在 provider 流式消费阶段：

1. 请求开始时记录 `startedAt`
2. 当首次发现非空 `delta.content`：
   - 计算 `first_token_latency_ms`
   - 通过回调上报给 service
3. service 在本次请求结束时将该值写入 `llm_request_logs`
4. 同时插入 `first_token_emitted` 事件

### 3.3 非首 token 场景

若整个流只有错误、空 chunk 或直接中断：

- `first_token_latency_ms = 0`
- 不写 `first_token_emitted`

## 4. 客户端中断

### 4.1 前端行为

前端使用 `AbortController`：

- 点击“开始流式”时创建 controller
- 点击“停止”时调用 `abort()`

### 4.2 服务端语义

当客户端断开连接且平台已经收到至少一个内容 token：

- 主请求仍按成功处理
- 记录 `client_aborted` 事件
- `response_excerpt` 与实时返回内容保持一致，只保存已收到部分

当客户端在首 token 前就断开：

- 本次调试场请求可按失败或取消处理，但为了统一，本设计仅保证“已收到内容后中断”的 B 口径；首 token 前断开不作为本次重点能力

### 4.3 审计展示文案

示例：

- `用户在调试场发起 qwen-flash 流式请求，首 Token 于 182 ms 返回`
- `用户主动停止流式请求，已返回部分结果`

## 5. 调试场前端设计

## 5.1 表单交互

新增字段：

- `流式响应` 勾选框

按钮行为：

- 非流式未运行：`提交请求`
- 流式未运行：`开始流式`
- 流式运行中：`停止`

### 5.2 结果区

结果区拆成两部分：

1. `实时输出`
   - 流式模式下实时追加文本
   - 停止后保留已返回内容
2. `执行指标`
   - 平台路由结果
   - 执行端点
   - 首 Token 延迟
   - 总延迟
   - 状态

### 5.3 状态文案

- 未开始：`等待执行`
- 流式进行中：`正在接收模型返回...`
- 用户停止：`已由用户手动停止`
- 正常完成：`已完成`

## 6. 调用观测与审计展示

### 6.1 调用明细

`UsageRequestItem` 新增：

- `first_token_latency string`

调用明细表新增一列：

- `首 Token`

显示规则：

- 大于 0：例如 `182 ms`
- 等于 0：显示 `--`

### 6.2 审计明细

`AuditItem` 新增：

- `first_token_latency string`

审计明细表新增一列：

- `首 Token`

### 6.3 最近事件流

继续使用 `llm_request_events`，但补充新的中文语义映射：

- `stream_started` -> `已发起流式请求`
- `first_token_emitted` -> `首 Token 已返回`
- `client_aborted` -> `用户主动停止流式请求`
- `stream_completed` -> `流式响应已完成`

## 7. 回退与兼容

1. 不影响现有非流式调试场接口。
2. 不影响 `/v1/chat/completions` 公共网关接口。
3. 旧数据没有 `first_token_latency_ms` 时，页面统一显示 `--`。
4. 若浏览器不支持流式读取，前端回退到现有阻塞式调用入口。

## 8. 测试策略

至少覆盖：

1. console service 流式运行成功时记录 `first_token_latency_ms`
2. console service 在用户中断时写入 `client_aborted` 事件
3. 调试场流式 handler 返回 `text/event-stream`
4. 前端勾选流式后进入开始/停止交互
5. 调试场收到 token 时实时渲染文本并展示首 Token 延迟
6. 调用观测和审计页能显示新增字段

## 9. 非目标

本次不做：

1. member 侧开放调试场
2. WebSocket 调试协议
3. 将首 Token 延迟纳入总览卡片或聚合报表
4. 对 RAG / embeddings 增加流式调试
