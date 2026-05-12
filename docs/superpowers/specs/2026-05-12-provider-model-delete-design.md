# 2026-05-12 后台模型删除设计

## 背景

当前 admin 侧“后台模型”页面支持：
- 查看 provider 列表
- 查看模型挂载列表
- 新建 provider
- 新建模型挂载
- 手动健康检查

但还缺少“删除模型挂载”的能力。用户要求：
- 删除入口放在“后台模型”列表页
- 删除对象仅为某个模型挂载配置，不删除 provider
- 历史调用观测数据继续保留显示
- 不需要 mockup，优先最小改造、直接落地

本次仅覆盖聊天模型挂载（`route_catalog.endpoint = '/v1/chat/completions'`）。

## 目标

为 admin 提供一个可用、可控、最小改造的“删除模型”能力：
- 前端：在“后台模型 -> 模型挂载”列表中，为每一行增加删除操作
- 后端：新增删除接口，按 `route_catalog.id` 删除模型挂载
- 数据：保留历史调用记录、异常事件、聚合用量等观测数据
- 安全：仅 admin 可操作，删除前需要明确确认

## 不在本次范围

- 不删除 `provider_credentials`
- 不批量删除模型
- 不增加“回收站”或“恢复删除”功能
- 不重做模型健康墙的交互
- 不处理 embeddings / internal-search 路由删除

## 现状约束

### 1. 模型挂载与 provider 的关系

当前：
- `provider_credentials` 表示上游供应商凭证
- `route_catalog` 表示模型挂载关系
- “后台模型”列表页展示的是 `route_catalog` 中 `endpoint = '/v1/chat/completions'` 的记录

因此，“删除模型”在语义上等价于：
- 删除某一条 `route_catalog` 记录
- 不影响它引用的 `provider_credentials`

### 2. 历史观测保留要求

用户明确要求：
- 删除模型挂载后，历史调用观测仍要继续保留显示

当前数据表中：
- `llm_request_logs.route_id` 是纯文本字段，没有外键到 `route_catalog`
- `llm_usage_agg_hourly.route_id` 是纯文本字段，没有外键到 `route_catalog`
- `llm_request_events` 依赖 `llm_request_logs`

这意味着：
- 删除 `route_catalog` 后，调用日志和聚合数据仍可保留
- 观测页面基于日志表的数据不会被自动清掉

### 3. 健康检查历史当前不满足要求

`model_healthcheck_history.route_id` 当前定义为：
- `references route_catalog(id) on delete cascade`

这会导致：
- 一旦删除 `route_catalog`，对应健康检查历史会被级联删除

这与“历史数据继续保留显示”的要求不一致。

## 可选方案对比

### 方案 A：真删除模型挂载，并修正健康检查历史外键（推荐）

行为：
- 真正删除 `route_catalog` 中对应记录
- 通过 migration 去掉 `model_healthcheck_history.route_id -> route_catalog(id) on delete cascade` 的删除联动
- 保留健康检查历史中的 `route_id` 作为快照文本

优点：
- 符合“删除模型挂载配置”的真实语义
- 不会在主表中留下大量软删除脏数据
- 观测和历史保留逻辑清晰

缺点：
- 需要一条数据库迁移
- 需要补充删除前后的测试

### 方案 B：软删除 route_catalog

行为：
- 不真删，只把 `route_catalog.status` 改为 `deleted` / `disabled`
- 页面默认不展示

优点：
- 几乎不动历史数据约束
- 风险较低

缺点：
- 语义不是真删除
- 表里会持续堆积无效挂载
- 后续所有查询都要额外处理隐藏逻辑

### 方案 C：仅提供“停用模型”

行为：
- 不删除，只停用

优点：
- 最稳

缺点：
- 不满足当前明确需求

## 最终方案

采用 **方案 A**。

### 设计原则

1. 删除对象只限模型挂载，不扩散到 provider。
2. 删除行为只影响未来路由，不影响历史观测。
3. 历史数据保留优先于数据库引用整洁。
4. 前端改动最小，复用现有“后台模型”页面。

## 数据设计

### 1. `route_catalog`

保持现有结构不变。

删除模型时：
- 直接按 `id` 删除对应行

### 2. `model_healthcheck_history`

新增 migration，取消 `route_id` 对 `route_catalog(id)` 的级联删除依赖。

目标状态：
- `route_id` 仍保留为文本字段
- 不再因 `route_catalog` 删除而丢失历史记录

实现方式建议：
- 删除旧外键约束
- 保留 `route_id text not null`
- 不新增新的级联逻辑

这样删除模型后：
- 健康检查历史仍可保留
- 后续如需按历史 `route_id` 查询，仍有快照值可用

### 3. 历史日志与聚合表

无需结构改动：
- `llm_request_logs`
- `llm_request_events`
- `llm_usage_agg_hourly`

原因：
- 这些表当前已经是“快照引用”模式，不依赖 `route_catalog` 外键

## 后端接口设计

新增接口：
- `DELETE /api/admin/provider-models/:id`

其中：
- `:id` 为 `route_catalog.id`

### 权限

- 仅 admin 可调用
- 复用现有 admin 路由鉴权中间件

### 成功返回

建议返回：

```json
{
  "deleted_id": "route:provider_xxx:model_xxx"
}
```

### 错误返回

统一中文语义：
- `404`：模型不存在
- `400`：模型删除失败，请确认当前模型状态
- `500`：删除失败，请稍后重试

## 后端服务逻辑

在 `postgresConsoleService` 新增删除模型方法，例如：
- `DeleteProviderModel(ctx context.Context, id string) error`

处理步骤：
1. 校验 `id` 非空
2. 查询该 `route_catalog` 是否存在，并限定 `endpoint = '/v1/chat/completions'`
3. 执行删除
4. 检查受影响行数
5. 返回中文错误或成功结果

### 删除时不做的事情

- 不删除 `provider_credentials`
- 不清理 `llm_request_logs`
- 不清理 `llm_request_events`
- 不清理 `llm_usage_agg_hourly`
- 不主动清理 `model_healthcheck_history`

### 关于 provider.supported_models

当前创建模型时会把 `requested_model` append 到 `provider_credentials.supported_models`。

删除模型后有两个选择：

#### 选择 1：同步移除 `supported_models`
优点：
- provider 展示更准确

缺点：
- 如果多个历史逻辑依赖该数组做“能力声明”，删除会改变 provider 的历史声明含义
- 需要处理数组更新与边界情况

#### 选择 2：本期不回写 `supported_models`（推荐）
优点：
- 改动最小
- 风险最低
- “后台模型”真正的可调用列表本来就是读 `route_catalog`

缺点：
- provider 卡片里的 `supported_models` 可能短时间包含一个已删除模型名

本期采用 **选择 2**。

后续如果要提升一致性，可单独做“provider 支持模型数组重建/修正”能力。

## 前端交互设计

页面：
- `后台模型` 页
- 区块：`模型挂载`

### 列表变更

当前列：
- 请求模型
- Provider
- 凭证 ID
- 线路
- 模式
- 健康状态
- 延迟

新增一列：
- 操作

操作列内容：
- `删除` 按钮

### 删除确认

点击删除后弹出浏览器原生确认或现有轻量确认弹层，文案建议：

```text
确认删除模型「{requested_model}」吗？
这只会删除模型挂载配置，不会删除 Provider，也不会清理历史调用观测数据。
```

### 成功反馈

删除成功后：
- 刷新 `/api/admin/provider-models`
- 从列表中移除该模型
- 页面给出简短成功提示（如现有页面没有 toast，可先不加）

### 失败反馈

删除失败后：
- 直接展示中文错误消息

## 观测与历史展示约定

### 1. 调用观测

删除模型后，以下内容仍保留：
- 调用明细
- 异常事件流
- 失败分类统计
- 租户/账单相关历史统计

这些能力读取的是日志和聚合表，不依赖 `route_catalog` 主记录存在。

### 2. 模型健康墙

本期约定：
- 删除后的模型不再作为“当前有效模型”出现在模型健康墙主列表中
- 历史健康检查记录仍保留在 `model_healthcheck_history` 中

理由：
- 健康墙本质上是“当前在管模型”的运行态面板
- 删除后不再属于当前挂载模型，不应继续占据主视图
- 但底层历史记录保留，为以后需要扩展“历史模型健康查询”留口子

## 测试设计

### 后端单测

至少补以下用例：

1. 删除存在的聊天模型成功
2. 删除不存在模型返回 404
3. 非聊天端点路由不可通过该接口删除
4. 删除后 `ProviderModels()` 返回列表中不再包含该模型
5. 删除后 `llm_request_logs` 历史记录仍存在
6. 删除后 `llm_usage_agg_hourly` 历史聚合仍存在
7. 删除后 `model_healthcheck_history` 历史记录仍存在

### HTTP / handler 测试

至少补：
- admin 可调用 `DELETE /api/admin/provider-models/:id`
- 非 admin 被拒绝
- 返回中文错误语义正确

### 前端测试

至少补：
- 后台模型页渲染“删除”按钮
- 点击删除会发起 `DELETE /api/admin/provider-models/:id`
- 删除成功后刷新列表
- 删除失败时展示错误信息

## 实施步骤

1. 新增数据库 migration，解除 `model_healthcheck_history.route_id` 的级联删除依赖
2. 在后端 service 增加 `DeleteProviderModel`
3. 增加 admin handler 和路由注册
4. 在前端 `console-api` 增加删除方法
5. 在“后台模型”页模型挂载表格增加“操作/删除”
6. 补前后端测试
7. 重新部署 gateway 与 web

## 风险与取舍

### 风险 1：健康检查历史与当前路由脱钩

影响：
- 删除后 `model_healthcheck_history.route_id` 成为“孤立快照 id”

接受原因：
- 这是保留历史数据的必要代价
- 该表本质上也是历史事实表，允许引用快照值是合理的

### 风险 2：provider.supported_models 可能暂时不精确

影响：
- provider 列表里看到的支持模型数组，可能仍包含刚删除的模型名

接受原因：
- 实际可调用关系由 `route_catalog` 决定
- 本期目标是先安全落地删除能力，不顺手扩展 provider 元数据维护

### 风险 3：前端误删

缓解：
- 删除前必须确认
- 按钮只在 admin 页面提供
- 错误提示中文可理解

## 验收标准

满足以下条件即视为完成：

1. admin 在“后台模型”页能看到每个模型的“删除”按钮
2. 点击后确认删除，可成功删除某个聊天模型挂载
3. 删除后该模型不再出现在“后台模型”列表
4. 对应 provider 仍保留
5. 历史调用观测数据仍可查看
6. 历史健康检查记录不会因删除而被数据库级联清掉
7. 相关测试通过
