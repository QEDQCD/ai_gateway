# Console Usage, Audit, And Key Security Design

> 日期：2026-04-25
> 状态：Draft for review
> 范围：控制台侧栏状态隐藏、API 密钥明文安全展示、多选权限下拉、调用观测可视化增强、审计页真实数据化

## 1. 目标

本轮修复目标有 4 个：

1. 隐藏控制台左下角状态块，保留顶部状态 badge。
2. API 密钥的新建/轮换结果任何时候都不显示完整明文；页面只显示脱敏值，并提供复制完整值的按钮。
3. API 密钥表单中的权限范围改为多选下拉，支持 `chat`、`rag`、`embeddings` 三种权限。
4. 调用观测页面参考 `relay-pulse` 的深色控制台视觉风格提升设计感与可视化；审计页面尽可能优先使用 `llm_request_logs / llm_request_events` 的真实数据。

本轮不做的事情：

- 不修改网关端口、登录方式或控制台正式入口语义。
- 不引入新的图表依赖库。
- 不重做整套控制台导航结构。
- 不恢复 RAG 前端模块。

## 2. 设计决策

### 2.1 左下角状态块处理

- 隐藏 [layout.tsx](/root/liwenjian/ai_gateway/web/src/app/layout.tsx) 中侧栏底部的 `控制台阶段 / 启动模式 / 网关健康` 整块。
- 顶部状态 badge 继续保留：
  - `网关健康`
  - `配额保护`
- 系统状态接口 `GET /api/admin/system/status` 仍保留，不删除。
  - 原因：顶部状态仍依赖该接口。
  - 后续如果要恢复独立系统状态页或抽屉，可继续复用。

### 2.2 密钥明文安全展示

- 后端在创建/轮换时仍返回 `raw_key`，因为前端复制完整值需要它。
- 前端不在任何可见区域展示完整 `raw_key`。
- 前端只展示脱敏值，例如：
  - `agw_12f8************7de3`
- 复制按钮点击后：
  - 复制完整原始密钥到剪贴板。
  - 页面提示“已复制完整密钥”。
- 原始密钥只保存在当前页面内存态中：
  - 不写入 `localStorage`
  - 不写入 URL
  - 不写入列表数据
  - 页面刷新后不再可见也不可复制

### 2.3 权限范围多选下拉

- 权限范围从自由文本输入改为“多选下拉”。
- 可选项固定为：
  - `chat`
  - `rag`
  - `embeddings`
- 创建密钥默认选中：
  - `chat`
- 轮换密钥默认回填当前密钥已有权限。
- UI 采用“下拉触发器 + 勾选列表 + 已选标签/计数”的轻量实现，不引入第三方组件库。

### 2.4 调用观测视觉方向

- 页面参考 `relay-pulse` 截图的视觉语言，但不机械照抄。
- 保留当前信息架构的核心数据来源：
  - `usage/overview`
  - `usage/trends`
  - `usage/failures`
  - `usage/requests`
- 改造方向：
  - 改为深色主视觉工作台
  - 顶部大指标卡片强化状态色
  - 趋势区改为更强的可视化模块
  - 失败分类改为“横向强弱条 + 事件流”
  - 明细列表增加状态 pill、来源 pill、视觉层级

不新增图表库，使用：

- CSS 渐变
- 卡片布局
- 轻量 SVG / 纯 CSS 趋势条
- 状态色与健康条

### 2.5 审计页真实数据优先级

- 审计页后端数据优先来自：
  - `llm_request_logs`
  - `llm_request_events`
- `audit_logs` 退化为补充或 fallback，而不是主来源。
- 页面摘要也改为真实聚合，不再主要依赖演示文案。

建议优先级：

1. 使用 `llm_request_logs` 作为主审计明细表。
2. 使用 `llm_request_events` 作为事件补充与错误上下文。
3. 如果 `llm_request_logs` 为空，再退回 `audit_logs`，保证页面不空白。

## 3. 页面与交互设计

### 3.1 控制台布局

- 删除侧栏底部状态区域 DOM。
- 顶栏状态 badge 保持两枚：
  - `健康/告警`
  - `配额保护 已启用/未启用`

### 3.2 API 密钥页

页面结构调整为四块：

1. 顶部操作区
   - `新建密钥`
   - `轮换密钥`
   - `停用密钥`
2. 密钥列表
   - 行选中态更明显
   - 状态使用 badge
3. 操作表单区
   - 创建表单
   - 轮换表单
   - 停用确认
4. 安全结果区
   - 显示脱敏后的密钥
   - 显示复制按钮
   - 显示“仅本次会话可复制”的说明

密钥结果区示例：

- 名称：`prod-gateway`
- 状态：`启用`
- 密钥：`agw_abcd************wxyz`
- 按钮：`复制完整密钥`

### 3.3 权限选择控件

权限多选下拉交互：

- 点击“权限范围”触发器展开。
- 展开后显示 3 个可勾选项。
- 已选结果显示为：
  - 标签列表，或
  - “已选 2 项：chat, rag”

表单行为：

- 创建时至少选择 1 项。
- 轮换时允许沿用原值或重新选择。
- 若用户取消所有选项，前端直接校验阻止提交。

### 3.4 调用观测页

新布局分为五区：

1. Hero 指标卡
   - 总调用数
   - 成功率
   - 总 Token
   - 平均延迟
   - 估算占比
2. 趋势总览
   - 请求量趋势
   - Token 趋势
   - 成功率趋势
3. 健康与异常区
   - 失败分类强弱条
   - 最近失败事件流
4. 调用明细表
   - 状态 pill
   - 来源 pill
   - 更强表头与行 hover
5. 分页区
   - 保留上一页/下一页
   - 增强当前页信息层级

视觉方向：

- 深色背景主面板
- 亮色图表线或趋势块
- 红/黄/绿状态对比
- 卡片边框、阴影、局部渐变

### 3.5 审计页

审计页改为真实调用审计视图：

1. 顶部真实摘要卡
   - 最近窗口总请求数
   - 失败请求数
   - 限流次数
   - 上游错误次数
2. 最近事件流
   - 直接来自 `llm_request_events`
3. 审计明细表
   - 时间
   - 租户
   - 端点
   - 请求模型
   - 上游模型
   - 状态
   - 提供商
   - 延迟
   - 计量来源
4. 真实摘要说明
   - 由真实聚合生成，而不是固定文案

## 4. 后端设计

### 4.1 API Key 明文返回与前端安全边界

后端 mutation 响应结构不改语义：

- `POST /admin/api-keys`
- `POST /admin/api-keys/:id/rotate`

返回：

```json
{
  "item": { "...": "..." },
  "raw_key": "agw_real_secret_value"
}
```

约束说明：

- 后端只在这两个 mutation 响应里返回一次完整值。
- `GET /admin/api-keys` 永远不返回明文。
- 后端不新增“再次获取原始密钥”接口。

### 4.2 审计数据查询重构

`ConsoleService.Audit()` 改为：

- 主查询从 `llm_request_logs` 读取最近审计明细
- 关联 `provider_credentials` / `route_catalog` / 最近事件
- 摘要统计基于 `llm_request_logs` 真值生成

建议查询字段：

- `request_started_at`
- `tenant_id`
- `request_path`
- `request_model`
- `upstream_model`
- `usage_status`
- `status_code`
- `provider_credential_id`
- `latency_ms`
- `usage_source`

最近事件来自 `llm_request_events`：

- `event_type`
- `detail`
- `created_at`

fallback 规则：

- 若 `llm_request_logs` 为空：
  - 使用现有 `audit_logs` 返回基本明细
  - 摘要降级但仍可展示

### 4.3 调用观测接口

本轮尽量复用现有 `usage/*` 接口，不新增复杂查询协议。

如前端视觉增强需要少量补充字段，只允许做轻量扩展，例如：

- 在请求明细中增加更适合展示的状态语义
- 在失败分类中保持排序稳定

不做的事情：

- 不新增 chart backend
- 不引入 websocket
- 不重做 usage 数据模型

## 5. 前端组件边界

### 5.1 需要修改的主要文件

- [layout.tsx](/root/liwenjian/ai_gateway/web/src/app/layout.tsx)
  - 删除左下角状态块渲染
- [api-keys.tsx](/root/liwenjian/ai_gateway/web/src/pages/api-keys.tsx)
  - 脱敏展示、复制按钮、多选下拉
- [usage.tsx](/root/liwenjian/ai_gateway/web/src/pages/usage.tsx)
  - 视觉重构
- [audit.tsx](/root/liwenjian/ai_gateway/web/src/pages/audit.tsx)
  - 更贴近真实审计数据的展示
- [console-api.ts](/root/liwenjian/ai_gateway/web/src/lib/console-api.ts)
  - 增加脱敏/复制所需类型兼容
- [styles.css](/root/liwenjian/ai_gateway/web/src/styles.css)
  - 深色可视化风格和多选下拉样式

### 5.2 组件策略

- 不引入新的 UI 框架。
- 尽量在现有 console 组件基础上扩展。
- 若调用观测页需要新的视觉卡片组件，可新增轻量组件，但只限本轮范围。

## 6. 安全与可维护性

### 6.1 密钥展示安全规则

- 明文只存在于当前前端内存态。
- 页面只显示脱敏值。
- 复制动作必须显式由用户触发。
- 页面刷新、路由离开或重新加载后，不再保留可复制值。

### 6.2 审计数据真实性

- 优先使用真实调用日志和事件日志。
- 文案摘要必须由真实统计生成。
- 避免继续依赖固定演示文案。

### 6.3 视觉增强约束

- 以 CSS 和现有 DOM 为主。
- 不引入重依赖图库。
- 保证移动端至少可降级阅读，不出现主要内容溢出不可见。

## 7. 测试策略

### 7.1 前端

至少补充/更新以下测试：

- 左下角状态块不再渲染
- 顶部状态 badge 仍正常显示
- 新建/轮换结果显示脱敏值而非完整明文
- 复制按钮存在，并调用剪贴板复制完整明文
- 权限范围多选下拉可选 `chat/rag/embeddings`
- 调用观测页新的可视化模块在数据加载后正常渲染
- 审计页展示来自新后端字段的真实数据

### 7.2 后端

至少补充/更新以下测试：

- `Audit()` 优先读取 `llm_request_logs / llm_request_events`
- 当 `llm_request_logs` 为空时，`Audit()` fallback 到 `audit_logs`
- API Key mutation 仍返回 `raw_key`
- `GET /admin/api-keys` 不返回明文

## 8. 验收标准

满足以下条件视为完成：

1. 左下角状态块完全隐藏，顶部状态仍存在。
2. 页面任何可见区域都不出现完整密钥明文。
3. 复制按钮能复制完整密钥。
4. 权限范围为多选下拉，并支持 `chat`、`rag`、`embeddings`。
5. 调用观测页视觉层次明显增强，并具备更强可视化。
6. 审计页优先展示真实 `llm_request_logs / llm_request_events` 数据。
7. 相关前后端测试通过。

## 9. Spec Self-Review

已检查：

- 无 TBD / TODO / 占位段落
- 范围聚焦于本轮 4 项修复
- 无与现有控制台正式入口语义冲突的内容
- 已明确“明文不显示但可复制完整值”的边界
- 已明确“审计真实数据优先级”为 `llm_request_logs / llm_request_events`
