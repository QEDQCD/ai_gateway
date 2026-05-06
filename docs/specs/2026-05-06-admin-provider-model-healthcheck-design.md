# Admin 供应商模型管理与健康检查设计

## 背景

当前 `ai_gateway` 已有两层与模型调用相关的数据结构：

- `provider_credentials`：表示供应商凭据与上游接入信息
- `route_catalog`：表示可被平台调用的模型路由

现状存在两个问题：

1. admin 侧无法在界面中直接新增供应商和后台模型，只能依赖初始化配置或手工写库。
2. 路由健康状态主要依赖已有静态字段和真实调用沉淀，缺少持续性的主动探测，无法及时识别“HTTP 可达但模型无真实输出”的假活状态。

本次目标是在不引入独立 worker 的前提下，扩展 admin 控制台能力：

- admin 可在页面中创建供应商并在供应商下新增聊天模型。
- 健康检查由 `gateway` 进程内置定时器执行，默认关闭。
- 健康检查成功后，模型状态能在 admin 页面与调用观测链路中体现。

## 目标

- 增加 admin 侧“供应商 -> 模型”两层管理能力。
- 新增模型时支持录入：
  - 供应商 `provider`
  - 展示名称 `display_name`
  - `base_url`
  - `requested_model`
  - `secret_ref`
- `api_key` 不进入页面或数据库明文，不通过 admin 页面直接存储。
- 健康检查仅覆盖聊天模型，不考虑 embedding。
- 健康检查使用真实流式请求，以“收到首个非空内容 token”为成功判定。
- 健康检查配置为服务端环境变量，默认关闭。
- 新模型在真实调用发生后，能自然出现在调用观测、审计和路由相关页面中。

## 非目标

- 不实现 embedding 模型的后台管理与健康检查。
- 不新增独立健康检查 worker 或单独部署进程。
- 不在第一版实现页面级全局健康检查配置编辑。
- 不在第一版实现复杂断言 DSL；仅保留后续扩展空间。
- 不在页面展示真实 API Key。

## 用户决策

本设计基于以下已确认约束：

- 模型管理采用两层结构：
  - 供应商层
  - 模型层
- 现有 `qwen-flash` 与 `mimo-v2.5-pro` 分别归属到：
  - `qwen`
  - `mimo`
- 健康检查按模型独立生效，不按供应商聚合。
- `api_key` 不入页面数据库，只保存 `secret_ref` 引用。
- 健康检查在 `gateway` 进程内部用定时器执行。
- 健康检查仅覆盖聊天模型。
- 成功断言第一版默认为：
  - 收到任意非空文本即成功
  - 结构上保留模型级扩展能力

## 方案选择

### 方案 1：最小侵入方案

直接复用现有 `provider_credentials.supported_models` 与 `route_catalog`，仅补 admin 表单和健康检查逻辑。

优点：

- 改动小
- 不引入新层次

问题：

- 供应商与模型边界继续混杂
- 未来模型级配置和健康检查会越来越难维护

### 方案 2：结构化扩展方案

保留 `provider_credentials` 表示供应商，保留 `route_catalog` 表示模型路由，但明确两者职责边界，并为供应商增加 `secret_ref` 模式。

优点：

- 与当前库表结构兼容
- 能自然承接“供应商 -> 模型”关系
- 适合后续继续增加 `qwen-max`、`qwen-plus` 等模型

成本：

- 需要补 DB、service、router、UI 和后台定时任务

### 方案 3：拆分新表方案

新建独立模型配置表和健康检查表，逐步弱化 `route_catalog`。

优点：

- 结构最干净

问题：

- 对现有实现侵入最大
- 第一版收益不够高

### 结论

采用 **方案 2**。

## 数据模型设计

### 供应商层：`provider_credentials`

`provider_credentials` 继续承担供应商级职责：

- `id`
- `provider`
- `display_name`
- `status`
- `base_url`
- `supported_models`
- 供应商级密钥来源

为了支持 `secret_ref` 模式，建议补充字段：

- `secret_ref text not null default ''`
  - 表示服务器侧 secret 引用名
  - 例如：
    - `dashscope_api_key`
    - `mimo_api_key`
    - `provider_qwen_max_key`
- `credential_mode text not null default 'encrypted'`
  - 可选值：
    - `encrypted`
    - `secret_ref`

兼容策略：

- 旧供应商继续保留 `encrypted_secret`
- 新建供应商优先使用 `credential_mode='secret_ref'`
- 读取时按顺序解析：
  - `secret_ref`
  - `encrypted_secret`

### 模型层：`route_catalog`

`route_catalog` 继续承担模型配置职责，每一行代表一个可调用聊天模型。

当前保留字段：

- `id`
- `requested_model`
- `provider_credential_id`
- `resolved_provider`
- `endpoint`
- `request_mode`
- `latency_ms`
- `health_status`

建议新增字段：

- `status text not null default 'active'`
  - 可选值：
    - `active`
    - `disabled`
- `healthcheck_enabled boolean not null default false`
- `healthcheck_assertion_type text not null default 'non_empty'`
  - 第一版仅使用：
    - `non_empty`
- `last_health_checked_at timestamptz`
- `last_health_error text not null default ''`
- `first_token_latency_ms integer not null default 0`

字段语义：

- `health_status`
  - `healthy`
  - `warning`
  - `degraded`
- `latency_ms`
  - 最近一次健康检查或路由探测得到的总耗时
- `first_token_latency_ms`
  - 最近一次流式健康检查拿到首个非空 token 的耗时

### 现有模型落位

初始数据需要满足：

- `qwen` 供应商下存在 `qwen-flash`
- `mimo` 供应商下存在 `mimo-v2.5-pro`

后续新增 `qwen-max`、`qwen-plus` 等模型时，均挂在对应供应商下。

## 服务端架构

### 1. 供应商配置读取

扩展 `routeService` 和相关 repository：

- provider 解析不再只依赖 `encrypted_secret`
- 当 `credential_mode='secret_ref'` 时，从服务器 secret 目录或配置映射中解析真实密钥
- `ProviderTarget` 仍然向调用层暴露统一结构：
  - `CredentialID`
  - `Provider`
  - `BaseURL`
  - `APIKey`

这样调用代理、流式请求、调用观测无需感知密钥来源差异。

### 2. admin 模型管理能力

扩展 `ConsoleService` 与相关 HTTP handler，新增两类能力：

- 供应商管理
- 模型管理

供应商管理职责：

- 列表查询
- 新建供应商
- 编辑供应商基础信息
- 启用/停用供应商

模型管理职责：

- 列表查询
- 按供应商查看模型
- 新建聊天模型
- 编辑模型配置
- 启用/停用模型
- 手动触发单模型健康检查

### 3. 健康检查执行器

在 `gateway` 进程内部新增后台健康检查执行器：

- 启动条件：
  - `GATEWAY_MODEL_HEALTHCHECK_ENABLED=true`
- 执行方式：
  - `time.Ticker`
- 周期：
  - 默认 `1h`
  - 可配置

执行流程：

1. 查询所有：
   - `route_catalog.status='active'`
   - `route_catalog.healthcheck_enabled=true`
   - `route_catalog.request_mode='聊天'`
2. 逐个构造聊天请求
3. 使用流式方式请求上游
4. 收到首个非空 `delta.content` 立即判定成功
5. 客户端主动中断流
6. 回写：
   - `health_status`
   - `latency_ms`
   - `first_token_latency_ms`
   - `last_health_checked_at`
   - `last_health_error`

### 4. 健康检查请求策略

第一版请求参数：

- endpoint：`/v1/chat/completions`
- `stream=true`
- `messages=[{"role":"user","content":"你好"}]`
- `max_tokens=1` 或极小值

成功条件：

- 收到任意非空文本内容

失败条件：

- 请求错误
- 上游超时
- 鉴权失败
- 流结束前没有拿到任何非空内容

### 5. Token 成本控制

健康检查改用流式并在首个非空 token 后立刻终止，通常会比等待完整响应更省输出 token。

但不能假设输出 token 恒定为 1，因为：

- 上游可能已在服务端继续生成少量内容
- 一个 chunk 可能包含多个 token

因此真正的控成本策略是：

- `stream=true`
- `max_tokens` 尽量小
- 首个非空 token 即中断

## 接口设计

建议新增 admin API：

- `GET /admin/providers`
- `POST /admin/providers`
- `POST /admin/providers/:id/update`
- `POST /admin/providers/:id/disable`
- `GET /admin/provider-models`
- `POST /admin/provider-models`
- `POST /admin/provider-models/:id/update`
- `POST /admin/provider-models/:id/disable`
- `POST /admin/provider-models/:id/health-check`
- `GET /admin/model-health`

接口职责：

- `/admin/providers`
  - 供应商维度列表与管理
- `/admin/provider-models`
  - 模型维度列表与管理
- `/admin/model-health`
  - 单独给健康检查页面提供聚合视图

创建模型时的行为：

1. 先保存供应商/模型记录
2. 立即执行一次单模型健康检查
3. 返回：
   - 模型配置已保存
   - 当前健康状态
   - 错误摘要（如果失败）

模型保存失败才算创建失败。  
健康检查失败不回滚模型，只把状态标记为不健康。

## 前端设计

### 左侧导航

admin 左侧导航新增两个页面：

- `后台模型`
- `健康检查`

不放进 member 导航。

### 后台模型页

页面结构分左右两栏或上下两区：

- 供应商列表
- 供应商下模型列表

供应商列表展示：

- `provider`
- `display_name`
- `base_url`
- `secret_ref`
- `status`
- 模型数量

模型列表展示：

- `requested_model`
- 所属供应商
- `endpoint`
- `status`
- `health_status`
- `first_token_latency_ms`
- `last_health_checked_at`
- `last_health_error`

交互能力：

- 新建供应商
- 编辑供应商
- 新建模型
- 编辑模型
- 启用/停用
- 手动触发健康检查

### 健康检查页

单独提供一个健康检查页面，不混入调用观测。

页面展示：

- 全局开关当前状态（只读）
- 当前健康检查周期（只读）
- 所有启用健康检查的模型
- 最近一次检查时间
- 最近结果
- 首 token 延迟
- 最近总耗时
- 最近错误摘要

可以附带简单摘要卡片：

- 总模型数
- 已启用检查数
- 健康数
- 不健康数

### 调用观测联动

调用观测页不需要新增一套新的真实请求聚合逻辑。

只要：

- 新模型存在于 `route_catalog`
- 新模型被真实调用后落入 `llm_request_logs`

它就会自然出现在：

- 调用观测
- 审计
- 路由相关视图

需要补充的是：

- 路由页和相关 admin 页面要能展示“已配置但暂无真实调用”的模型
- 健康检查状态要能独立展示，不能完全依赖真实业务流量

## 配置项设计

建议新增 `gateway` 环境变量：

- `GATEWAY_MODEL_HEALTHCHECK_ENABLED=false`
- `GATEWAY_MODEL_HEALTHCHECK_INTERVAL=1h`
- `GATEWAY_MODEL_HEALTHCHECK_TIMEOUT=20s`
- `GATEWAY_MODEL_HEALTHCHECK_PROMPT=你好`
- `GATEWAY_MODEL_HEALTHCHECK_MAX_TOKENS=1`

说明：

- 默认关闭
- 页面第一版不提供这些全局值的编辑能力
- 配置变更通过部署环境完成

## 错误处理

### 创建供应商失败

失败场景：

- `provider` 重复
- `display_name` 为空
- `base_url` 非法
- `secret_ref` 为空

处理方式：

- 前端表单校验
- 后端返回明确错误信息

### 创建模型失败

失败场景：

- `requested_model` 重复
- 供应商不存在
- endpoint 不允许
- request mode 非聊天

处理方式：

- 第一版只允许固定 endpoint：
  - `/v1/chat/completions`
- 第一版只允许固定模式：
  - `聊天`

### 健康检查失败

失败不会删除模型，也不会阻断模型保存。

失败只会影响：

- `health_status`
- 健康检查页状态
- 路由相关状态展示

### 兼容老数据

旧数据兼容要求：

- 旧供应商的 `encrypted_secret` 仍可读取
- 旧模型继续沿用 `route_catalog`
- 老模型未启用健康检查时，不参与定时探测

## 测试策略

后端测试应覆盖：

- 新供应商创建/更新/停用
- 新模型创建/更新/停用
- `secret_ref` 模式解析
- 旧 `encrypted_secret` 模式兼容
- 健康检查成功：
  - 首个非空 token 即成功
- 健康检查失败：
  - 超时
  - 鉴权失败
  - 空输出
- 创建模型后自动健康检查
- 调用观测中能识别新模型

前端测试应覆盖：

- admin 导航新增两个入口
- 后台模型页加载和表单提交
- 健康检查页加载与状态展示
- 创建模型成功但健康检查失败时的界面反馈

## 风险与取舍

### 风险 1：密钥引用解析复杂度上升

引入 `secret_ref` 后，调用时需要额外解析服务器侧密钥来源。

取舍：

- 这是本需求的必要复杂度
- 但仍值得做，因为它避免了在页面和数据库中再次扩散真实密钥

### 风险 2：定时任务与主进程耦合

健康检查内置在 `gateway` 进程中，意味着它和主服务生命周期绑定。

取舍：

- 第一版改造最小
- 以后若探测规模增大，再拆 worker

### 风险 3：健康状态与真实业务调用状态不一致

主动探测的健康结果与真实用户请求质量可能存在偏差。

取舍：

- 健康检查页明确展示“主动探测结果”
- 调用观测继续展示真实请求数据
- 两者分开，不混淆统计口径

## 结论

第一版采用：

- `provider_credentials` 表示供应商
- `route_catalog` 表示模型
- `secret_ref` 作为新供应商密钥来源
- `gateway` 内置定时健康检查器
- 仅覆盖聊天模型
- 流式请求首个非空 token 即判成功
- 配置项默认关闭

这样可以在不推翻现有调用链路的前提下，让 admin 真正具备后台模型管理能力，并让模型健康状态从“静态配置”升级为“可主动探测、可持续展示”的运行态信息。
