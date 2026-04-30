# 按模型计价的 Token 分类统计与费用展示设计

## 背景

当前平台已经具备租户级 API Key 分发、请求日志、调用观测、失败分类和基础额度控制能力，但 Token 统计仍然只有较粗的 `total_tokens` 口径，存在几个明显缺口：

1. 无法区分输入 Token、输出 Token 与缓存 Token。
2. 无法按不同 Token 类型应用不同单价。
3. 前端无法同时展示“Token 数量”和“对应费用”。
4. 当前请求、聚合、账本三层数据结构都没有缓存 Token 与费用快照字段。
5. 如果未来模型定价调整，历史请求费用会缺少可追溯的计价快照。

这会让平台在“租户自助查看消耗”“管理员做成本观测”“按模型比较使用成本”这三件事上都不够完整。

## 目标

本次设计只覆盖以下目标：

1. 支持按模型配置输入 / 输出 / 缓存三类 Token 的单价。
2. 在后端真实记录并聚合：
   - `input_tokens`
   - `output_tokens`
   - `cached_tokens`
   - `input_cost`
   - `output_cost`
   - `cached_cost`
   - `total_cost`
3. 在 admin 和 member 两套调用观测视图中，同时展示 Token 数量与费用。
4. 保持当前额度控制逻辑不变，仍按 `total_tokens` 扣减，不改审批与配额系统。

## 用户已确认的口径

### 1. 计价粒度

采用“按模型计价”。

例如：

- `qwen-flash.input = 2 ￥/M`
- `qwen-flash.output = 20 ￥/M`
- `qwen-flash.cached = 0.5 ￥/M`

### 2. 缓存 Token 缺失策略

若上游返回中没有 `cached_tokens`，本次按 `0` 处理，不做估算。

### 3. 限额口径

租户额度、审批 Token 上限、member 侧剩余额度继续按 `total_tokens` 口径执行，本次不改为按金额扣减。

### 4. 单价配置入口

本次单价先放在后端配置文件或环境变量中，不做后台管理页面。

## 方案对比

### 方案 A：在现有 usage 真值链路上扩字段并持久化费用快照

做法：

- 扩展 `llm_request_logs`
- 扩展 `llm_usage_agg_hourly`
- 扩展 `tenant_usage_ledger`
- 请求结束时完成计价并落库
- usage / audit 接口直接读取真实费用字段

优点：

- 历史费用可追溯
- 调用观测、审计、导出、后续账单都能复用同一口径
- 不会因为后续修改单价而篡改历史数据

缺点：

- 需要补 migration、usage recording、aggregator、console API 和前端页面

### 方案 B：新增独立账单表，usage 只保留 token

做法：

- usage 继续只存 token
- 另建 billing 明细与 billing 聚合表

优点：

- 后续扩月账单、账期结算更清晰

缺点：

- 当前阶段过重
- 会引入第二套聚合链路，不利于快速稳定上线

### 方案 C：页面展示时临时按配置计算费用

做法：

- 数据库不改
- 前端拿到 token 后自行乘以价格

优点：

- 开发最快

缺点：

- 后端没有统一真值
- 审计、接口、导出和前端结果可能不一致
- 单价变化会导致历史金额漂移

## 结论

采用 **方案 A**。

理由很明确：你要做的是“平台级 API Key 分发与租户成本观测”，不是一次性的前端展示，因此费用必须进入后端真值链路并以请求时快照持久化。

## 详细设计

## 1. 总体架构

本次改造保持现有链路不拆分，只在每一层补齐 Token 分类与费用字段：

1. 请求代理结束后，从上游 usage 中提取：
   - `prompt_tokens`
   - `completion_tokens`
   - `cached_tokens`，若无则为 `0`
2. 根据请求中的目标模型，读取后端定价配置。
3. 在 usage recording 阶段直接计算：
   - 输入费用
   - 输出费用
   - 缓存费用
   - 总费用
4. 将“Token 分类 + 费用快照”写入 `llm_request_logs`。
5. 聚合器将同一批真实字段汇总到 `llm_usage_agg_hourly` 与 `tenant_usage_ledger`。
6. `UsageOverview / UsageTrends / UsageRequests / Audit` 统一从这些真实字段返回展示值。
7. 前端只做展示与格式化，不在浏览器里自行计算账务。

## 2. 单价配置

### 2.1 配置模型

2026-04-30 的实际实现不是 YAML 配置文件，而是由 `gateway/internal/config/config.go` 解析环境变量。

实际变量名如下：

- `GATEWAY_MODEL_TOKEN_PRICING_DEFAULT_INPUT_MICROYUAN_PER_MILLION`
- `GATEWAY_MODEL_TOKEN_PRICING_DEFAULT_OUTPUT_MICROYUAN_PER_MILLION`
- `GATEWAY_MODEL_TOKEN_PRICING_DEFAULT_CACHED_MICROYUAN_PER_MILLION`
- `GATEWAY_MODEL_TOKEN_PRICING_QWEN_FLASH_INPUT_MICROYUAN_PER_MILLION`
- `GATEWAY_MODEL_TOKEN_PRICING_QWEN_FLASH_OUTPUT_MICROYUAN_PER_MILLION`
- `GATEWAY_MODEL_TOKEN_PRICING_QWEN_FLASH_CACHED_MICROYUAN_PER_MILLION`

默认值与单位：

- 单位统一为“微元 / 百万 Token”
- `default.input = 2_000_000`，即 `2.00 元 / M`
- `default.output = 20_000_000`，即 `20.00 元 / M`
- `default.cached = 500_000`，即 `0.50 元 / M`
- `qwen-flash` 三项在未显式设置时，逐项回退到 `default`

因此当前可视为内置了两组定价键：

- `default`
- `qwen-flash`

### 2.2 查找顺序

按以下顺序解析请求单价：

1. 精确匹配 `model`
2. 若未命中，回退到 `default`

补充实现细节：

- `service.ModelPricingResolver` 要求存在 `default`
- 但当前 `config.Load()` 会始终构造 `default`，并在环境变量缺失时使用内置默认值
- 因此在当前实现里，“未配置任何 token pricing 环境变量”不会导致启动失败，而是回退到代码默认值

原因：

- 请求级模型查找仍然遵循“精确命中优先，未命中回退 default”
- 通过代码内置默认值保证本地部署与未显式配置场景也能得到稳定金额

### 2.3 运行时口径

费用按请求完成时命中的单价快照计算并写入数据库。后续即使管理员修改配置，历史请求金额也不得发生变化。

## 3. 数据模型

## 3.1 `llm_request_logs`

新增字段：

- `cached_tokens integer not null default 0 check (cached_tokens >= 0)`
- `input_price_microyuan_per_million bigint not null default 0 check (input_price_microyuan_per_million >= 0)`
- `output_price_microyuan_per_million bigint not null default 0 check (output_price_microyuan_per_million >= 0)`
- `cached_price_microyuan_per_million bigint not null default 0 check (cached_price_microyuan_per_million >= 0)`
- `input_cost_microyuan bigint not null default 0 check (input_cost_microyuan >= 0)`
- `output_cost_microyuan bigint not null default 0 check (output_cost_microyuan >= 0)`
- `cached_cost_microyuan bigint not null default 0 check (cached_cost_microyuan >= 0)`
- `total_cost_microyuan bigint not null default 0 check (total_cost_microyuan >= 0)`

说明：

- 价格和费用都使用整数微元存储，避免浮点误差。
- 请求日志保留单价快照，便于事后核账。

字段映射：

- `input_tokens = prompt_tokens`
- `output_tokens = completion_tokens`
- `cached_tokens = upstream.cached_tokens || 0`
- `total_tokens` 保持现有含义不变

## 3.2 `llm_usage_agg_hourly`

新增字段：

- `cached_tokens integer not null default 0 check (cached_tokens >= 0)`
- `input_cost_microyuan bigint not null default 0 check (input_cost_microyuan >= 0)`
- `output_cost_microyuan bigint not null default 0 check (output_cost_microyuan >= 0)`
- `cached_cost_microyuan bigint not null default 0 check (cached_cost_microyuan >= 0)`
- `total_cost_microyuan bigint not null default 0 check (total_cost_microyuan >= 0)`

说明：

- 聚合层只保留累计值，不重复保存单价快照。

## 3.3 `tenant_usage_ledger`

新增字段：

- `cached_tokens integer not null default 0 check (cached_tokens >= 0)`
- `input_cost_microyuan bigint not null default 0 check (input_cost_microyuan >= 0)`
- `output_cost_microyuan bigint not null default 0 check (output_cost_microyuan >= 0)`
- `cached_cost_microyuan bigint not null default 0 check (cached_cost_microyuan >= 0)`
- `total_cost_microyuan bigint not null default 0 check (total_cost_microyuan >= 0)`

说明：

- 账本继续服务于租户配额与周期统计。
- 本次不让账本承担金额限额控制，只承担金额统计展示。

## 4. 计价公式

统一采用：

```text
billable_input_tokens = max(input_tokens - cached_tokens, 0)
billable_cached_tokens = min(max(cached_tokens, 0), max(input_tokens, 0))

input_cost_microyuan = billable_input_tokens * input_price_microyuan_per_million / 1_000_000
output_cost_microyuan = max(output_tokens, 0) * output_price_microyuan_per_million / 1_000_000
cached_cost_microyuan = billable_cached_tokens * cached_price_microyuan_per_million / 1_000_000
total_cost_microyuan = input_cost_microyuan + output_cost_microyuan + cached_cost_microyuan
```

说明：

- 当前实现把 `cached_tokens` 视为输入 Token 的子集，避免对缓存命中的输入部分重复按普通输入计费
- 若 `cached_tokens > input_tokens`，计费时会被钳制到 `input_tokens`
- 若任一 Token 值为负数，计费前会先归一化为 `0`

### 4.1 四舍五入

使用整数除法前先做半舍入：

```text
(tokens * price + 500_000) / 1_000_000
```

这样 0.5 元 / M 这种价格在小请求下不会系统性偏低。

### 4.2 显示格式

后端统一提供格式化字符串：

- 单价：`2.00 ￥/M`
- 金额：`0.12 ￥`
- Token：继续使用当前页面的人性化显示，如 `12,480`

## 5. 后端服务改造

## 5.1 usage recording

当前记录请求 usage 的服务需要补齐：

1. 从响应 usage 中读取 `prompt_tokens`、`completion_tokens`
2. 读取 `cached_tokens`，若不存在则置 `0`
3. 根据模型查价
4. 计算三类费用与总费用
5. 将所有字段一次写入 `llm_request_logs`

如果上游只返回 `total_tokens` 而未返回输入/输出明细：

- 继续沿用当前已有兼容逻辑生成 `prompt_tokens / completion_tokens`
- `cached_tokens` 仍为 `0`

## 5.2 usage aggregator

聚合器在处理请求记录时，需要同步累加：

- `cached_tokens`
- `input_cost_microyuan`
- `output_cost_microyuan`
- `cached_cost_microyuan`
- `total_cost_microyuan`

## 5.3 console service 返回结构

### `UsageOverviewData`

新增字段：

- `input_tokens`
- `output_tokens`
- `cached_tokens`
- `input_cost`
- `output_cost`
- `cached_cost`
- `total_cost`
- `pricing_models`

其中 `pricing_models` 用于展示当前系统可见的模型单价，例如：

```json
[
  {
    "model": "qwen-flash",
    "input_price": "2.00 ￥/M",
    "output_price": "20.00 ￥/M",
    "cached_price": "0.50 ￥/M"
  }
]
```

说明：

- overview 里既返回聚合费用，也返回当前计价规则摘要。
- 当前实现中的 `pricing_models` 不是直接读取静态配置，而是基于查询窗口内请求日志中出现过的 `(model, input_price, output_price, cached_price)` 去重后返回。

### `UsageTrendData`

保留现有：

- `requests`
- `tokens`
- `success`

新增：

- `costs`

`costs` 表示每个时间桶内的 `total_cost` 趋势，单位为后端已格式化的金额序列。

### `UsageRequestsPageData`

每条请求新增字段：

- `input_tokens`
- `output_tokens`
- `cached_tokens`
- `input_cost`
- `output_cost`
- `cached_cost`
- `total_cost`
- `input_price`
- `output_price`
- `cached_price`

说明：

- 请求明细页必须能看出“这次请求为什么花了这些钱”。

### `AuditItem`

当前已实现新增字段：

- `total_cost`

审计页重点展示结果：

- 谁在什么时间调用了哪个模型
- 产生了多少费用
- 请求最终成功还是失败

补充说明：

- 三类 Token 与单价快照当前在 `UsageOverview / UsageRequests` 中展示
- `AuditItem` 当前没有返回 `input_tokens / output_tokens / cached_tokens`

## 6. HTTP API 合同

本次不新增独立 API，直接扩展现有接口返回体：

- `GET /api/admin/usage/overview`
- `GET /api/admin/usage/trends`
- `GET /api/admin/usage/requests`
- `GET /api/admin/audit`
- `GET /api/me/usage/overview`
- `GET /api/me/usage/requests`
- `GET /api/me/failures` 不改

原则：

- 不修改路径
- 不改变现有字段语义
- 通过附加字段实现向后兼容

## 7. 前端展示设计

## 7.1 admin 调用观测页

顶部卡片由原来的粗粒度汇总改为：

- 总请求数
- 总 Token
- 输入 Token / 输入费用
- 输出 Token / 输出费用
- 缓存 Token / 缓存费用
- 总费用
- 平均延迟
- 成功率

补充一个“当前模型定价”卡片，展示当前配置中的模型与单价。

### 请求明细表

原“总 Token”列改为以下列：

- 输入 Token
- 输出 Token
- 缓存 Token
- 总 Token
- 总费用

行展开或次级文案中展示：

- 输入单价
- 输出单价
- 缓存单价

## 7.2 member 调用观测页

保留与 admin 相同的 Token / 费用口径，但只显示当前租户数据。

页面需要明确区分两类信息：

1. `额度`
   - 继续按 `total_tokens` 展示
2. `费用`
   - 作为观测信息展示，不参与本次限额判断

避免用户误以为“额度已经改成按人民币扣减”。

## 7.3 审计页

审计页的“调用明细”需要把费用语义补齐：

- 原先只显示状态与来源不够
- 现在要让管理员能直观看到某次调用对应的成本

建议在每条记录中展示：

- 模型
- 请求状态
- 输入 / 输出 / 缓存 Token
- 总费用
- 中文事件说明

## 8. 失败与边界处理

### 8.1 缓存 Token 缺失

- 统一记 `0`
- 页面不显示“估算”
- 页面直接显示 `0` 与 `0.00 ￥`

### 8.2 模型未命中显式定价

- 回退到 `default`
- 当前 `default` 由配置加载阶段始终构造；若未配置环境变量，则继续使用代码默认值

### 8.3 失败请求如何计费

沿用上游真实返回：

- 如果失败请求仍有 usage，则照常记录 Token 和费用
- 如果失败请求没有 usage，则所有 Token 和费用记 `0`

这样能真实反映“部分失败但已发生消耗”的场景。

### 8.4 流式请求

流式请求与阻塞式请求使用同一套 usage 记录逻辑：

- 若上游在最终 usage 中返回输入 / 输出 Token，则正常计价
- `cached_tokens` 若无则为 `0`

## 9. 测试策略

采用严格 TDD，至少覆盖：

### 9.1 migration 测试

验证新字段存在：

- `llm_request_logs.cached_tokens`
- `llm_request_logs.*price*`
- `llm_request_logs.*cost*`
- `llm_usage_agg_hourly.cached_tokens`
- `llm_usage_agg_hourly.*cost*`
- `tenant_usage_ledger.cached_tokens`
- `tenant_usage_ledger.*cost*`

### 9.2 usage recording 测试

验证：

- 命中精确模型价格时金额正确
- 未命中时回退到 `default`
- `cached_tokens` 缺失时为 `0`
- 四舍五入口径正确

### 9.3 usage aggregator 测试

验证：

- 小时聚合正确累加三类 Token 与四类费用
- tenant ledger 同步正确

### 9.4 console service 测试

验证：

- `UsageOverview` 返回新增字段
- `UsageTrends` 返回 `costs`
- `UsageRequests` 返回每次请求的 Token 分类与费用
- `Audit` 返回成本字段

### 9.5 前端测试

验证：

- admin 调用观测页展示三类 Token 与费用
- member 调用观测页展示三类 Token 与费用
- 请求明细表列渲染正确
- 额度区仍然是 `total_tokens` 口径

## 10. 验收标准

完成后应满足：

1. 任意一次真实请求都能在明细中看到输入、输出、缓存 Token 和总费用。
2. overview 能同时展示聚合 Token 与聚合费用。
3. trends 能看到费用趋势。
4. audit 能看到与请求相关的成本信息。
5. member 配额页仍按 `total_tokens` 展示，不误导为按金额限额。
6. 对于没有 `cached_tokens` 的上游，页面稳定显示 `0`，不出现空值或 `NaN`。
7. 修改价格配置后，新请求按新价格计算，历史请求金额不变。

## 11. 非目标

本次明确不做：

1. 后台可视化“模型定价”管理页面
2. 按金额做审批、限额或告警
3. 月账单、发票、导出对账单
4. 按租户差异化价格
5. 缓存 Token 估算逻辑

## 12. 实施拆分建议

后续 implementation plan 建议按以下顺序拆分：

1. 数据库 migration 与存储模型扩展
2. usage recording 与 aggregator 计价链路
3. console service / HTTP API 合同扩展
4. admin / member / audit 前端联动
5. 部署验证与真实请求回归
