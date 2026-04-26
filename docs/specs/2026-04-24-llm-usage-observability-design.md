# AI Gateway 调用观测与 Token 计量设计文档

- 日期：2026-04-24
- 项目目录：`$PROJECT_ROOT`
- 设计主题：面向平台管理员与租户的 LLM 调用观测、Token 计量与失败分析
- 参考项目：`relay-pulse`

## 1. 背景

当前 `ai_gateway` 已经具备基础控制台能力，但观测面仍停留在轻量级汇总：

- 已有页面集中在总览、路由、Playground、审计
- 已有数据表以 `audit_logs`、`playground_runs`、`route_catalog` 为主
- 已有指标偏向请求量、成功率、配额使用率、活跃 key、最近审计

当前缺口在于：

- 无法准确查看每次调用的 `input_tokens`、`output_tokens`、`total_tokens`
- 无法按租户、平台 API Key、Provider、Model、Route、Endpoint 做聚合
- 无法清晰区分成功、失败、超时、限流、上游鉴权失败等状态
- 无法给租户展示自己的调用明细与趋势
- 无法为后续账单、限额、告警、BYOK 成本归因提供稳定数据基础

本设计的目标不是继续堆叠 `audit_logs`，而是新增一套独立的“调用明细层 + 聚合层 + 控制台展示层”。

## 2. 设计目标

本次设计需要同时满足以下目标：

- 让平台管理员查看全量调用、Token 使用、成功率、失败分类、慢请求和热点模型
- 让租户只查看自己的调用数据与使用趋势
- 以“上游 `usage` 优先，本地估算兜底”的方式建立可解释的 Token 计量口径
- 借鉴 `relay-pulse` 的历史样本、状态分层、趋势聚合和热力图思路
- 保持与现有 Go 网关、PostgreSQL、RabbitMQ、React 控制台兼容
- 为后续账单、配额、告警、BYOK 成本分析预留字段和接口

## 3. 非目标

本次设计明确不包含：

- 正式计费与财务结算
- 多维 BI 报表系统
- 独立 ClickHouse / Prometheus / Loki 分析栈
- 原始 Prompt 和完整响应全文的长期保存
- 对历史旧调用做复杂回填

## 4. 用户与权限边界

### 4.1 角色定义

- 平台管理员：查看全量租户、全量平台 API Key、全量 Provider / Model / Route 数据
- 租户用户：只能查看自己租户下的数据

### 4.2 权限原则

- 所有观测查询都必须带有作用域过滤
- 平台管理员可指定 `tenant_id` 做筛选，也可查看全局
- 租户用户不能跨租户查看任何统计、明细、事件流或错误样本
- 页面可以复用同一套组件，但服务端必须强制执行作用域限制，不能依赖前端隐藏

## 5. 总体方案

方案采用三层结构：

1. 调用明细层
   - 保存每次 LLM 请求的最终事实记录
   - 负责支撑明细查询、问题排查、失败原因分析

2. 调用事件层
   - 保存一次请求生命周期中的关键节点
   - 负责支撑最近事件流、阶段性失败定位、状态变化展示

3. 聚合统计层
   - 按小时聚合核心指标
   - 负责支撑总览卡片、趋势图、热力图、模型和 API Key 排行

主请求链路负责生成可信明细；异步聚合链路负责刷新统计结果。这样可以保证接口延迟不因为复杂聚合而明显变差。

## 6. 数据模型设计

### 6.1 `llm_request_logs`

该表是调用观测的主表，一次网关转发调用对应一条记录。

建议字段：

- `id`
- `request_id`
- `tenant_id`
- `project_id`
- `gateway_api_key_id`
- `gateway_api_key_name`
- `route_id`
- `endpoint`
- `provider`
- `model`
- `upstream_base_url`
- `http_method`
- `request_at`
- `finish_at`
- `latency_ms`
- `status`
- `http_status_code`
- `error_code`
- `error_category`
- `error_message_digest`
- `input_tokens`
- `output_tokens`
- `total_tokens`
- `usage_source`
- `cost_amount`
- `cost_currency`
- `byok_provider_credential_id`
- `is_byok`
- `client_ip_hash`
- `user_agent_digest`
- `trace_id`
- `created_at`

字段约束原则：

- `request_id` 全局唯一，用于跨日志、事件、链路追踪
- `status` 表示最终态
- `error_category` 表示标准化失败分类
- `error_message_digest` 只保存脱敏摘要，不保存敏感原文
- `usage_source` 取值为 `upstream` 或 `estimated`
- `cost_amount` 首版允许为空，但字段必须存在
- `gateway_api_key_name` 使用冗余快照，避免后续名称修改影响历史报表

建议索引：

- `idx_llm_request_logs_request_at`
- `idx_llm_request_logs_tenant_request_at`
- `idx_llm_request_logs_key_request_at`
- `idx_llm_request_logs_provider_model_request_at`
- `idx_llm_request_logs_status_request_at`
- `uidx_llm_request_logs_request_id`

当数据量增长到月级千万记录时，可按 `request_at` 做月分区；首版先不强制引入分区。

### 6.2 `llm_request_events`

该表保存请求生命周期中的关键事件，主要用于最近事件流和链路排查。

建议字段：

- `id`
- `request_id`
- `tenant_id`
- `event_type`
- `event_time`
- `stage`
- `status`
- `message`
- `payload_json`
- `created_at`

典型事件：

- `request_received`
- `auth_checked`
- `route_selected`
- `upstream_started`
- `usage_parsed`
- `request_finished`
- `request_failed`

设计原则：

- `payload_json` 只放轻量上下文，例如 `provider`、`model`、`route_id`、`usage_source`
- 不存储完整请求体、原始 API Key、完整 Prompt 或完整响应

### 6.3 `llm_usage_agg_hourly`

该表用于控制台高频查询。

聚合维度：

- `bucket_time`
- `tenant_id`
- `gateway_api_key_id`
- `route_id`
- `provider`
- `model`

聚合指标：

- `request_count`
- `success_count`
- `failed_count`
- `timeout_count`
- `rate_limited_count`
- `auth_failed_count`
- `input_tokens`
- `output_tokens`
- `total_tokens`
- `estimated_usage_count`
- `estimated_cost`
- `avg_latency_ms`
- `p50_latency_ms`
- `p95_latency_ms`

设计原则：

- 小时聚合表是控制台默认数据源
- 7 天以内趋势直接查小时表
- 更长周期可在查询层按天二次汇总，不额外引入日表作为首版必须项

## 7. 状态与失败分类

### 7.1 最终状态

`llm_request_logs.status` 统一取以下值：

- `success`
- `failed`
- `timeout`
- `rate_limited`
- `auth_failed`
- `upstream_error`

### 7.2 标准失败分类

`error_category` 统一取以下值：

- `auth_failed`
- `rate_limited`
- `bad_request`
- `upstream_auth_failed`
- `upstream_rate_limited`
- `upstream_server_error`
- `upstream_timeout`
- `network_error`
- `internal_error`

### 7.3 页面中文映射

前端统一展示为：

- 鉴权失败
- 限流
- 请求参数错误
- 上游鉴权失败
- 上游限流
- 上游服务异常
- 上游超时
- 网络异常
- 网关内部错误

分类标准必须在服务端固定，前端只负责映射，不自行推断。

## 8. Token 计量规则

本项目采用如下口径：

- 优先使用上游模型响应中的 `usage`
- 上游未返回 `usage` 时，使用本地 tokenizer 估算
- 估算结果必须明确标记来源，不能与上游精确计量混淆

### 8.1 字段计算规则

- `input_tokens`
  - 先取上游 `prompt_tokens` 或兼容字段
  - 没有时按请求消息和输入文本估算
- `output_tokens`
  - 先取上游 `completion_tokens` 或兼容字段
  - 没有时按响应文本估算
- `total_tokens`
  - 优先取上游 `total_tokens`
  - 无上游值时使用 `input_tokens + output_tokens`

### 8.2 计量来源

`usage_source`：

- `upstream`
- `estimated`

控制台需要展示：

- 总 Token
- 精确计量占比
- 估算计量占比

这样既满足运营视角，也能避免后续账单争议。

## 9. 请求采集链路

### 9.1 同步主链路

请求进入网关后的推荐流程：

1. 请求进入统一代理入口
2. 鉴权成功后生成 `request_id` 和 `trace_id`
3. 在内存中维护本次请求上下文
4. 写入 `request_received` 事件
5. 完成路由选择并写入 `route_selected` 事件
6. 请求上游模型
7. 解析响应、状态码、延迟、`usage`
8. 必要时执行本地 Token 估算
9. 生成 `llm_request_logs` 最终记录
10. 写入 `request_finished` 或 `request_failed` 事件
11. 发布 usage 聚合事件到 RabbitMQ

### 9.2 异步聚合链路

异步消费者从队列中消费 usage 事件后：

1. 读取标准化请求结果
2. 以 `bucket_time + tenant_id + key_id + provider + model + route_id` 为维度聚合
3. 更新 `llm_usage_agg_hourly`
4. 在必要时触发后续告警或阈值检查

### 9.3 降级策略

为了避免队列异常影响主调用链路：

- 主请求必须以 `llm_request_logs` 落库成功为优先
- RabbitMQ 不可用时：
  - 记录错误日志
  - 标记 usage 事件投递失败
  - 允许通过离线补偿任务从明细表重刷小时聚合

这保证“统计可以延迟，但调用事实不能丢”。

## 10. 控制台页面设计

本设计不建议再把调用统计散落在 `dashboard`、`routes`、`api-keys` 多个页面里，而是新增一个集中式页面：`调用观测`。

### 10.1 一级导航

新增导航：

- `/usage`

保留现有页面职责：

- `dashboard`：保留平台级总览和少量核心摘要
- `api-keys`：保留密钥管理
- `routes`：保留路由策略与路由健康
- `audit`：保留管理操作审计，不混入调用观测

### 10.2 `调用观测` 页面结构

页面采用单页多模块结构，重点借鉴 `relay-pulse` 的摘要 + 趋势 + 热力图 + 最近事件流组合。

模块包括：

1. 顶部摘要卡片
   - 调用总数
   - 成功率
   - 总 Token
   - 平均延迟
   - 估算 Token 占比

2. 时间趋势区
   - 调用次数趋势
   - Token 趋势
   - 成功 / 失败趋势

3. 失败分析区
   - 失败分类占比
   - 最近失败样本
   - 上游失败与网关失败对比

4. 热力图区
   - 以小时粒度展示调用活跃度或失败密度
   - 支持按租户、Key、Provider、Model 切换维度

5. 维度排行区
   - Top API Keys
   - Top Models
   - Top Providers
   - Top Routes

6. 最近事件流
   - 展示最近请求完成、失败、超时、限流等事件

7. 调用明细表
   - 支持筛选和分页
   - 支持查看单条请求的状态、Token、延迟、错误分类、使用来源

### 10.3 管理员与租户展示差异

管理员页面：

- 默认可查看全量数据
- 支持按租户过滤
- 可查看全局 Top 榜单

租户页面：

- 只显示当前租户数据
- 不显示其他租户相关筛选项
- 可查看本租户 API Key / Model / Route 的排行和明细

页面结构保持一致，仅筛选器和数据范围不同。

## 11. 查询接口设计

建议新增以下后端接口，统一返回中文字段对应的结构化数据。

### 11.1 总览接口

`GET /console/usage/overview`

用途：

- 顶部摘要卡片
- 当前筛选条件下的总调用量、成功率、总 Token、平均延迟、估算占比

支持筛选：

- `from`
- `to`
- `tenant_id`
- `gateway_api_key_id`
- `provider`
- `model`
- `route_id`

### 11.2 趋势接口

`GET /console/usage/trends`

用途：

- 调用趋势图
- Token 趋势图
- 成功 / 失败趋势图

支持参数：

- `metric=requests|tokens|success_rate|latency`
- `granularity=hour|day`
- 其余筛选参数同上

### 11.3 失败分析接口

`GET /console/usage/failures`

用途：

- 返回失败分类分布
- 返回最近失败样本
- 返回网关侧与上游侧失败对比

### 11.4 维度拆解接口

`GET /console/usage/breakdown`

用途：

- 按 `api_key`、`provider`、`model`、`route` 维度做排行和占比

关键参数：

- `dimension=api_key|provider|model|route`
- `metric=requests|tokens|failures|latency`

### 11.5 热力图接口

`GET /console/usage/heatmap`

用途：

- 返回小时级矩阵数据
- 用于展示调用活跃度或失败密度

### 11.6 明细接口

`GET /console/usage/requests`

用途：

- 分页查询调用明细

支持筛选：

- 时间范围
- 状态
- 失败分类
- Provider
- Model
- API Key
- Route
- `usage_source`

### 11.7 单请求详情接口

`GET /console/usage/requests/:request_id`

用途：

- 查询单次请求的完整观测信息与事件流

返回包含：

- 请求主记录
- 生命周期事件
- 已脱敏错误上下文

## 12. 与现有模块的边界

### 12.1 `audit_logs`

继续用于记录：

- 后台配置变更
- 密钥创建/禁用/轮换
- 路由策略修改
- 租户和权限管理操作

不再承载调用量、Token、失败分析等统计职责。

### 12.2 `playground_runs`

继续用于记录 Playground 的交互上下文，但真实模型调用统计最终也要落入 `llm_request_logs`，不能形成双轨统计口径。

### 12.3 路由目录与 Provider 配置

`route_catalog` 和现有路由配置仍是控制平面数据源；调用观测只读取其标识和快照，不反向承担路由配置职责。

## 13. 安全与隐私要求

观测系统必须满足以下要求：

- 不在任何统计表或接口中暴露上游供应商 API Key
- 不存储完整平台 API Key 明文
- `gateway_api_key_name` 可以显示，密钥值只显示脱敏前缀和后缀
- 不长期保存完整 Prompt 和完整响应内容
- 错误信息只保存脱敏摘要，不保存可能包含敏感业务数据的原文
- 租户只能访问自己范围内的数据
- BYOK 模式下只记录凭据引用 ID，不记录凭据内容

## 14. 借鉴 `relay-pulse` 的设计点

本设计参考 `relay-pulse`，但不直接复制其探活逻辑，而是借鉴以下思想：

- 历史样本优先：每次调用都形成可追踪事实记录
- 状态分层清晰：成功、慢、失败、限流、超时要能一眼看懂
- 聚合优先展示趋势：不是只看一堆明细表
- 热力图表达活跃时段与异常集中时段
- 最近事件流用于快速排障
- 颜色与严重度表达稳定，不靠临时文案拼接

## 15. 实施与迁移策略

建议按以下顺序落地：

1. 新增 PostgreSQL 迁移，创建三张新表和索引
2. 在代理主链路中补齐 `request_id`、标准状态分类和 `usage` 解析
3. 落地 `llm_request_logs` 同步写入
4. 接入 RabbitMQ usage 聚合消费者
5. 补齐控制台查询接口
6. 新增 `调用观测` 页面并联调
7. 为现有 `dashboard` 增加少量观测摘要入口
8. 增加离线重刷任务，用于按时间段重建小时聚合

这条路径风险最低，因为每一步都可以独立验证。

## 16. 测试策略

### 16.1 单元测试

- `usage` 解析器测试
- 本地 Token 估算器测试
- 失败分类映射测试
- 小时聚合器测试
- 查询参数校验测试

### 16.2 集成测试

- 成功调用时落库正确
- 上游无 `usage` 时自动估算并标记 `estimated`
- 上游返回限流、超时、鉴权失败时分类正确
- RabbitMQ 不可用时主请求仍能完成且明细不丢
- 管理员与租户的作用域过滤正确

### 16.3 前端测试

- `调用观测` 页面路由测试
- 时间筛选与图表切换测试
- 空数据、错误态、加载态测试
- 明细表分页与筛选测试

## 17. 验收标准

当以下条件全部满足时，可认为本设计落地成功：

- 每次真实 LLM 调用都能在明细表中查到
- 页面可查看总 Token、成功率、失败分类、延迟趋势
- 平台管理员可查看全量，租户只能查看本租户数据
- 上游返回 `usage` 与本地估算可区分展示
- 可按 API Key、Provider、Model、Route 维度聚合
- 失败分类可直接支持排障和后续告警
- 队列异常不会导致调用事实丢失

## 18. 推荐结论

本设计推荐采用“明细事实表 + 生命周期事件表 + 小时聚合表 + 集中式调用观测页面”的方案。

这是对当前 `ai_gateway` 最合适的演进路径，原因是：

- 比在 `audit_logs` 上继续打补丁更清晰
- 比直接上独立分析栈更轻、更适合当前项目阶段
- 能直接体现 API 网关、平台治理、Token 计量、租户隔离和可观测性能力
- 能平滑承接后续账单、配额、告警、BYOK 成本归因与 SLA 看板
