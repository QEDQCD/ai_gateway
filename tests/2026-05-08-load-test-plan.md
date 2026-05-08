# AI Gateway 压测计划（第一版）

## 1. 目标

本轮压测目标不是打满极限，而是在**严格成本约束**下，验证当前网关对已健康模型的可用性、稳定性和基础并发承载能力，形成后续正式压测的安全基线。

重点验证：

1. `qwen-flash`
2. `mimo-v2.5-pro`
3. `deepseek-r1-distill-qwen-7b`（特殊模型，单独验证）

当前已知状态：

- `qwen-flash`：健康
- `mimo-v2.5-pro`：健康
- `deepseek-r1-distill-qwen-7b`：健康，但属于高 reasoning 开销模型
- `qwen2.5-1.5b-instruct`：当前降级，不纳入本轮压测

## 2. 压测范围

本轮仅覆盖以下能力：

- 通过网关统一入口调用模型
- 显式指定 `model` 参数
- 并发请求下的成功率、响应延迟、首字延迟趋势
- 网关在一分钟窗口内的稳定性
- 调用观测、模型健康墙、路由解析是否与实际调用一致
- 验证 `deepseek-r1-distill-qwen-7b` 在高 `max_tokens` 条件下是否能产出最终答案

本轮不覆盖：

- 长上下文输入
- 流式长输出
- embedding / rerank / knowledge search
- 多租户隔离极限测试
- 恶意输入、安全对抗、越权调用

## 3. 成本与停止条件

### 3.1 Token 预算

按你的限制执行：

- `qwen` 系列总消耗上限：**300000 tokens**
- `mimo` 系列总消耗上限：**200000 tokens**

归类方式：

- `qwen-flash`、`deepseek-r1-distill-qwen-7b` 计入 `qwen` 预算
- `mimo-v2.5-pro` 计入 `mimo` 预算

补充说明：

- `deepseek-r1-distill-qwen-7b` 会先消耗大量 `reasoning token`
- 当 `max_tokens` 太小（如 `128`、`256`）时，可能在思考阶段就 `finish_reason=length`
- 因此它虽然健康，但不适合作为第一轮“低成本短回复模型”与 `qwen-flash`、`mimo-v2.5-pro` 混压

### 3.2 硬停止条件

满足任一条件立即停止：

1. `qwen` 累计消耗达到 `300000`
2. `mimo` 累计消耗达到 `200000`
3. 连续 5 轮失败率超过 `50%`
4. 单模型连续 5 次出现 `5xx` / 路由失败 / 超时
5. 网关出现明显异常：容器重启、接口 500、观测页无数据、DB 写入异常
6. 压测窗口达到 **1 分钟**

## 4. 压测对象与入口

统一网关入口：

- `http://127.0.0.1:32658/v1/chat/completions`

标准调用格式：

```bash
curl -sS \
  -H "Authorization: Bearer $AGW_API_KEY" \
  -H "Content-Type: application/json" \
  -X POST http://127.0.0.1:32658/v1/chat/completions \
  -d '{
    "model":"qwen-flash",
    "messages":[{"role":"user","content":"你好"}]
  }'
```

同理替换为：

- `qwen-flash`
- `mimo-v2.5-pro`
- `deepseek-r1-distill-qwen-7b`

## 5. 压测方法

### 5.1 先做预检

正式压测前，每个模型先串行调用 3 次，确认：

- 能正常返回
- 返回模型名与请求模型一致
- 调用观测里能看到数据
- 错误信息已中文化
- 不出现 `route resolution failed`

其中 `deepseek-r1-distill-qwen-7b` 的预检单独要求：

- 至少验证一次 `max_tokens=512`
- 确认返回的是最终 `content`，而不是仅有 `reasoning_content`
- 若仅返回 reasoning 兜底文本，不进入第一轮并发压测

### 5.2 一分钟保守压测

采用**低风险、可观测优先**策略，不一开始就冲高并发。

建议分两部分执行，总时长控制在 1 分钟内：

#### 特殊模型单验：`deepseek-r1-distill-qwen-7b`

- 单独验证，不并入第一轮混合压测
- 并发：`1`
- `max_tokens`：建议 `512`
- 目标：确认其能否稳定产出最终答案，以及估算 reasoning 开销

通过后再决定是否进入下一轮专项压测

#### 第一轮混合压测：仅 `qwen-flash` + `mimo-v2.5-pro`

#### 阶段 A：单并发基线（10 秒）

- 并发：`1`
- 模型：`qwen-flash`、`mimo-v2.5-pro`
- 目标：建立单请求 RT / TTFT / 成功率基线

#### 阶段 B：小并发验证（20 秒）

- 并发：`2`
- 方式：每个模型 1 路并发
- 目标：验证两个主压模型同时被调度时是否稳定

#### 阶段 C：轻度冲击（30 秒）

- 并发：`4`
- 分配建议：
  - `qwen-flash`：2
  - `mimo-v2.5-pro`：2
- 目标：验证一分钟窗口内的轻量承压能力

> 说明：本轮不建议直接上 10+ 并发。当前目标是“确认稳定可压”，不是“极限打爆”。

## 6. 请求参数建议

为减少 token 消耗，统一使用短请求：

```json
{
  "model": "<模型名>",
  "messages": [
    {"role": "user", "content": "你好,一句话回答"}
  ]
}
```

控制原则：

- 输入尽量短
- 输出尽量短
- 如网关已支持，可增加 `max_tokens: 16` 或更低
- 若要测流式，优先用极短输出，避免无意义烧 token

特殊例外：

- `deepseek-r1-distill-qwen-7b` 不适用低 `max_tokens` 策略
- 对该模型建议单独使用 `max_tokens: 512`
- 否则很容易只消耗 reasoning token，拿不到最终回答

## 7. 观测指标

压测过程中重点记录：

1. 请求总数
2. 成功数 / 失败数
3. 成功率
4. P50 / P90 / P95 响应时间
5. TTFT（如平台已有统计）
6. 每模型调用次数
7. 每模型 token 消耗
8. 路由解析是否命中正确模型
9. 健康墙状态是否变化
10. 容器 CPU / 内存是否明显抖动

## 8. 风险点

### 8.1 `mimo-v2.5-pro`

- 该模型此前健康检查已适配 reasoning 流式行为
- 压测时需确认正式调用路径与健康检查路径在流式处理上表现一致

### 8.2 `deepseek-r1-distill-qwen-7b`

- 之前出现过 `route resolution failed`
- 当前路由与空 `content` 兼容问题已修复
- 但该模型在 DashScope 兼容接口下会优先输出大量 `reasoning_content`
- 当 `max_tokens` 太小时，可能在思考阶段就被截断
- 因此必须单独验证，不应直接并入第一轮混合压测

### 8.3 `qwen2.5-1.5b-instruct`

- 当前处于 `degraded`
- 上游返回 `403 access_denied`
- 本轮压测不纳入，避免混淆平台问题与上游权限问题

## 9. 通过标准

满足以下条件，可认为本轮压测通过：

1. `qwen-flash`、`mimo-v2.5-pro` 在混合压测中都至少成功调用一次以上
2. `deepseek-r1-distill-qwen-7b` 至少完成一次单独验证，并确认其是否适合下一轮专项压测
3. 一分钟内无网关崩溃、无容器重启
4. 总体成功率不低于 `95%`
5. 无持续性 `5xx`
6. 调用观测与模型健康墙数据能正确反映本轮请求
7. 模型返回与请求模型一致，不发生串模
8. 未突破 token 预算

## 10. 下一步执行建议

下一步不要直接“正式开压”，而是按下面顺序推进：

### 第一步：补一个压测脚本

建议在 `tests/` 下新增脚本，能力包括：

- 支持传入模型名列表
- 支持并发数、持续时间、请求体模板
- 统计成功率、耗时、错误分布
- 累计各模型 token 消耗
- 达到预算后自动停止

推荐脚本名：

- `tests/run_gateway_load_test.sh`

### 第二步：做 3 模型预检

先串行调用：

- `qwen-flash`
- `mimo-v2.5-pro`
- `deepseek-r1-distill-qwen-7b`

每个模型 3 次。

其中：

- `qwen-flash`、`mimo-v2.5-pro` 使用低 `max_tokens` 预检
- `deepseek-r1-distill-qwen-7b` 至少有 1 次使用 `max_tokens=512`

### 第三步：执行 1 分钟轻压测

先单独验证 `deepseek-r1-distill-qwen-7b`，再按“阶段 A / B / C”执行混合压测，边压边看：

- 调用观测
- 模型健康墙
- gateway 日志
- docker 容器状态

### 第四步：产出结果文档

压测后输出一份结果文档，至少包含：

- 各模型请求数
- 成功率
- RT / TTFT
- token 消耗
- 失败原因分类
- 是否建议进入下一轮更高并发压测

## 11. 结论

本轮建议采用**保守、可回退、强观测**的方式推进。当前最合理的下一步是：

1. 先补 `tests/run_gateway_load_test.sh`
2. 再做三模型预检
3. 单独验证 `deepseek-r1-distill-qwen-7b`
4. 然后执行 `qwen-flash` + `mimo-v2.5-pro` 的 1 分钟轻压测

这样可以在不明显超预算的前提下，快速确认当前 AI Gateway 是否具备“可稳定承载小规模并发调用”的基础能力。
