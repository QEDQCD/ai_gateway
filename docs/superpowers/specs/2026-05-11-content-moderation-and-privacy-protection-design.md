# 2026-05-11 内容审核与隐私保护设计

## 1. 目标

为 AI Gateway 的 `POST /v1/chat/completions` 增加一条请求前安全治理链路，满足以下目标：

1. 在请求真正转发到业务模型前，使用 `qwen-mt-flash` 对用户提交内容做安全审核。
2. 对明显网络攻击类请求进行拦截，不再继续调用业务模型。
3. 对用户消息中的敏感信息进行脱敏，至少覆盖手机号，并将脱敏后的文本发送给实际业务模型。
4. 当审核模型不可用、超时或报错时，请求不被拦截，但仍需执行本地正则脱敏，避免隐私明文继续向上游扩散。
5. 日志、审计和调用观测中只保存脱敏后的文本摘要，不保存用户原始敏感内容。

本次范围仅覆盖 `chat/completions`。不扩展到 `embeddings`、RAG 或其他文本入口。

---

## 2. 范围与非目标

### 2.1 本次范围

- 网关入口：`POST /v1/chat/completions`
- 审核模型：`qwen-mt-flash`
- 审核动作：识别并拦截明显网络攻击类输入
- 隐私保护：识别并替换手机号等敏感信息，最低保证手机号被替换为 `***`
- 转发行为：真正发给业务模型的是脱敏后的消息内容
- 落库与展示：日志、审计、调用观测展示均使用脱敏后的摘要

### 2.2 非目标

本期不做以下内容：

- 不覆盖 `embeddings`
- 不实现员工号、终端用户 ID 等更细粒度身份字段
- 不做租户级/用户级内容审核策略配置
- 不做审核模型多节点路由与负载均衡
- 不实现复杂 PII 全量实体抽取平台，仅先落地手机号必保底 + 可扩展框架
- 不做图片、文件、语音等多模态审核
- 不做独立审核控制台页面

---

## 3. 总体方案

采用“**本地规则预处理 + 大模型审核判定 + 本地脱敏兜底**”方案。

### 3.1 请求处理顺序

`chat/completions` 请求进入后，链路变为：

1. 平台 API Key 鉴权与基础请求解析
2. 提取用户消息内容，构造审核输入
3. 调用 `qwen-mt-flash` 执行审核
4. 若审核结论为攻击类请求，则直接拦截并返回中文错误
5. 若审核通过，则对用户消息执行脱敏替换
6. 若审核模型失败，则跳过拦截，只做本地正则脱敏
7. 将脱敏后的请求继续送入原有智能路由/上游模型调用链
8. 响应、日志、观测继续沿用现有脱敏链路

### 3.2 设计原则

- **拦截与脱敏分离**：攻击拦截主要由 `qwen-mt-flash` 决策；隐私保护即使模型失败也必须成立。
- **上游只见脱敏文本**：真正转发给业务模型的正文不再保留原始手机号。
- **结构化判定**：审核模型输出必须是严格 JSON，网关不信任自由文本说明。
- **失败可降级**：审核模型失败时不扩大故障面，不影响主链路可用性。
- **最小侵入**：尽量在现有 `chat/completions` 请求进入到路由决策前插入，不重写整个代理架构。

---

## 4. 组件设计

### 4.1 新增 `ContentGuardService`

新增一个独立服务，职责只有两类：

1. 基于 `qwen-mt-flash` 对请求做安全审核
2. 生成“最终可转发”的脱敏消息集合

建议位置：

- `gateway/internal/service/content_guard_service.go`
- `gateway/internal/service/content_guard_service_test.go`

建议接口：

```go
type ContentGuardService interface {
    GuardChatMessages(ctx context.Context, req GuardChatRequest) (GuardChatResult, error)
}
```

其中：

- `GuardChatRequest` 包含原始 `messages`、租户/模型上下文、请求 ID 等信息
- `GuardChatResult` 包含：
  - `Decision`：`allow` / `block`
  - `Reason`：中文原因
  - `AttackType`：攻击类型标识
  - `SanitizedMessages`：脱敏后的消息
  - `AuditSource`：`llm_guard` / `fallback_regex`
  - `ModelAvailable`：审核模型是否正常返回

### 4.2 审核模型客户端

不新造一套 HTTP 客户端，优先复用现有 OpenAI 兼容客户端能力，单独封装一个“审核模型调用器”。

新增薄封装，例如：

- `gateway/internal/service/moderation_client.go`

职责：

- 向 `qwen-mt-flash` 发送固定 prompt
- 要求返回固定 JSON
- 做超时控制、JSON 解析、字段校验
- 不把原始自由文本判定结果直接暴露给业务层

### 4.3 本地脱敏器复用与扩展

复用已有：

- `gateway/internal/security/redaction.go`

但需从“仅展示/响应脱敏”扩展为“**请求转发前脱敏**”。

本期至少保留以下规则：

- 中国大陆手机号匹配后替换为 `***`

本期统一替换策略：

- 敏感片段整体替换为 `***`

也就是说：

- `13812345678` → `***`

而不是继续采用原来展示层的 `138XXXX5678` 样式。原因是本次目标是“向上游模型发送脱敏后的正文”，用 `***` 更稳，不会保留可逆线索。

为避免影响已有“展示层局部掩码”逻辑，建议新增一个更明确的接口，例如：

- `RedactTextForDisplay(text string) string`：保留原先 `138XXXX5678`
- `SanitizeTextForUpstream(text string) string`：统一替换为 `***`

这样可以避免把当前已有页面/响应展示语义一起改坏。

---

## 5. 审核模型协议

### 5.1 审核输入

发送给 `qwen-mt-flash` 的 prompt 需要约束成单一任务：

- 判断用户文本是否包含明显网络攻击意图
- 若不是攻击，则识别需要脱敏的敏感片段
- 仅输出 JSON，不输出解释性散文

待审核文本来源：

- 仅抽取 `messages` 中 `role=user` 的内容
- `system` / `assistant` 消息不参与用户攻击判定

### 5.2 审核输出 JSON

建议固定为：

```json
{
  "decision": "allow",
  "reason": "未检测到明显网络攻击内容",
  "attack_type": "",
  "redactions": [
    {
      "type": "phone",
      "text": "13812345678",
      "replacement": "***"
    }
  ]
}
```

字段约束：

- `decision`：只能是 `allow` 或 `block`
- `reason`：中文短句
- `attack_type`：可为空；若 `block`，则必须是枚举之一
- `redactions`：可为空数组

### 5.3 攻击类型枚举

本次只覆盖明显网络攻击类：

- `sql_injection`
- `xss`
- `command_injection`
- `ssrf`
- `path_traversal`
- `deserialization_exploit`
- `privilege_bypass`
- `malicious_script_generation`
- `other_attack`

### 5.4 输出校验

网关侧必须严格校验：

- JSON 可解析
- `decision` 合法
- `block` 时 `reason` 非空
- `redactions` 每项 `text` 与 `replacement` 合法

若不合法，按“审核失败”处理，而不是盲信模型输出。

---

## 6. 业务流程细节

### 6.1 审核通过

当 `qwen-mt-flash` 返回 `allow`：

1. 用模型返回的 `redactions` 先替换用户消息中的敏感片段
2. 替换完成后，再运行本地正则脱敏兜底
3. 得到 `SanitizedMessages`
4. 把 `SanitizedMessages` 送入原有代理链路

这样做的原因：

- 模型识别可能比正则更灵活
- 但本地规则对手机号是确定性的兜底

### 6.2 审核拦截

当 `qwen-mt-flash` 返回 `block`：

1. 不再调用实际业务模型
2. 返回中文错误，例如：
   - `请求被安全策略拦截：检测到疑似 SQL 注入内容。`
3. 写审计/事件流，标记为安全拦截
4. 调用观测中记为失败或单独状态，原因使用中文可理解文案

### 6.3 审核模型失败

当 `qwen-mt-flash` 超时、网络错误、响应格式非法或不可用：

1. 不拦截请求
2. 直接对用户消息运行本地正则脱敏
3. 用本地脱敏后的消息继续调用业务模型
4. 记录一条审计/告警事件，说明“审核服务降级为本地规则模式”

这与用户明确选择的策略一致：**B：失败时放行，但只做本地正则脱敏**。

---

## 7. 接入点设计

### 7.1 代理主链路接入位置

从当前代码结构看，最合适的接入点在：

- `gateway/internal/service/proxy_service.go`

原因：

- 这里已经负责处理聊天请求、上游调用和现有响应脱敏
- 在这里插入“请求前审核 + 请求前脱敏”最自然
- 可以确保流式与非流式都共享同一入口治理逻辑

建议改造方式：

1. 在处理 `ChatCompletionRequest` 时，先调用 `ContentGuardService.GuardChatMessages`
2. 将返回的 `SanitizedMessages` 写回请求对象
3. 再继续现有 route resolve / provider call 逻辑

### 7.2 不修改的部分

本次不要求改动：

- 控制台页面展示结构
- 路由策略页面
- 审批逻辑
- provider model 管理页

只需要让现有调用观测/审计在落库时自然带出“已脱敏正文”和“审核拦截原因”。

---

## 8. 数据与审计

### 8.1 日志存储

现有 `llm_request_logs` 中涉及 prompt/response 摘要的字段，只允许写入脱敏后的文本。

这意味着：

- 即使请求原文含手机号，库里最终也只能看到 `***`
- 若请求被拦截，也只能记录脱敏后的摘要和中文原因

### 8.2 新增审计语义

建议在现有事件/错误语义基础上增加两类事件：

- `security_guard_blocked`
- `security_guard_fallback`

含义：

- `security_guard_blocked`：审核模型判定为攻击类并已拦截
- `security_guard_fallback`：审核模型失败，请求改用本地规则脱敏后放行

### 8.3 调用观测展示语义

控制台不一定要本期新增 UI，但后端要产出可展示字段，以便现有异常事件流能看懂：

- `安全拦截：检测到疑似 SQL 注入内容`
- `审核降级：审核服务暂时不可用，已按本地规则脱敏后继续转发`

---

## 9. 错误处理

### 9.1 对用户返回的错误

拦截类错误必须使用中文、可理解，但不泄露过多防护细节。

建议格式：

- `请求被安全策略拦截：检测到疑似 SQL 注入内容。`
- `请求被安全策略拦截：检测到疑似跨站脚本攻击内容。`

不建议返回：

- 详细规则命中位置
- 内部 prompt
- 审核模型原始返回

### 9.2 内部容错

若审核阶段发生以下错误：

- 审核模型超时
- 审核模型 5xx
- JSON 解析失败
- 结构校验失败

统一视为：

- `guard_degraded`

处理结果：

- 不拦截
- 本地脱敏
- 继续请求
- 记审计事件

---

## 10. 配置设计

新增配置建议：

- `GATEWAY_CONTENT_GUARD_ENABLED=true|false`
- `GATEWAY_CONTENT_GUARD_MODEL=qwen-mt-flash`
- `GATEWAY_CONTENT_GUARD_TIMEOUT_MS=3000`

说明：

- 默认建议开启
- 审核模型名单独可配，后续便于替换
- 超时要短，避免给主链路增加不可接受的阻塞

若当前项目已有 provider credential / route catalog 能表达 `qwen-mt-flash`，优先走现有 provider 体系，不单独硬编码 secret。

---

## 11. 测试策略

按照 TDD 进行，覆盖以下场景。

### 11.1 后端单元/集成测试

1. **攻击类请求被拦截**
   - 输入明显 SQL 注入样例
   - 断言不调用业务模型
   - 返回中文拦截错误

2. **正常请求放行**
   - 审核模型返回 `allow`
   - 请求继续调用真实上游 mock

3. **手机号被替换为 `***` 后再发给上游**
   - 输入 `我手机号是13812345678`
   - 断言上游接收到的是 `我手机号是***`

4. **审核模型失败时仍走本地脱敏**
   - 审核模型 mock 超时/报错
   - 断言请求未被拦截
   - 断言上游收到的是本地脱敏后的内容

5. **模型 redactions + 本地兜底叠加生效**
   - 模型识别一部分，本地规则再补手机号

6. **流式与非流式共用同一请求前脱敏逻辑**
   - `stream=false` 与 `stream=true` 都验证上游入参正文

7. **非法审核 JSON 自动降级**
   - 审核模型返回非 JSON 或字段非法
   - 断言按 fallback 处理

### 11.2 不变性验证

- 现有响应侧脱敏测试继续保持通过
- 现有调用观测详情里仍只展示脱敏后的输入输出摘要

---

## 12. 代码落点建议

### 12.1 新增文件

- `gateway/internal/service/content_guard_service.go`
- `gateway/internal/service/content_guard_service_test.go`
- `gateway/internal/service/moderation_client.go`

### 12.2 修改文件

- `gateway/internal/service/proxy_service.go`
- `gateway/internal/service/proxy_service_test.go`
- `gateway/internal/security/redaction.go`
- `gateway/internal/security/redaction_test.go`
- 如有需要：`gateway/internal/config/config.go`
- 如有需要：`gateway/cmd/server/main.go`

---

## 13. 风险与权衡

### 13.1 延迟增加

每个 `chat/completions` 请求都额外多一次 `qwen-mt-flash` 调用，会增加首包延迟。

应对方式：

- 严格超时，例如 3 秒
- 审核失败时快速降级，不阻塞主调用

### 13.2 模型误判

大模型审核可能误判正常请求为攻击内容。

本期接受这个风险，但控制范围：

- 只拦截“明显网络攻击类”
- 不把 prompt injection、安全研究讨论等更宽泛内容纳入本期拦截

### 13.3 脱敏过度影响回答质量

把手机号替换成 `***` 再发给业务模型，可能影响某些需要原文手机号的业务回答。

这是本期的明确 trade-off：

- 目标优先级是**隐私保护高于原文保真**

---

## 14. 结论

本设计采用“**审核模型判攻击 + 本地规则兜底脱敏 + 上游只见脱敏文本**”的方式，在不大改现有网关架构的前提下，为 `chat/completions` 增加一条可上线的安全治理链路。

其核心价值是：

- 攻击类内容在进入业务模型前被拦截
- 用户手机号等敏感信息不会再以明文形式继续发送到上游模型
- 审核模型不可用时，平台仍保持可用，并至少完成本地隐私保护
- 后续可以自然扩展到身份证、邮箱、银行卡、住址等更多脱敏类型
