# AI Gateway 租户制 API Key 分发与治理平台设计

> 日期：2026-04-27
> 状态：Draft for review
> 目标：将当前项目从“AI 网关演示台”收敛为“租户制 API Key 分发、调用治理与审计平台”
> 参考：
> - `ai-gateway.md` 中的网关分层思路
> - `packycode-cost` 的 token 状态、用量展示与日志观测思路
> - 阿里 Higress 的统一入口、鉴权、路由与治理理念

## 1. 设计结论

本轮重设计的结论不是继续堆叠“模型调用页面”，而是把产品主线明确为：

`账号申请 -> admin 审批 -> 开通租户访问 -> 用户自助创建平台 API Key -> 调用统一平台接口 -> 统计租户级 token 消耗 -> 记录成功/失败 -> 提供审计与风控视图`

这个平台对外卖的是“统一 AI 接入能力”，不是“把上游模型厂商直接暴露给用户”。  
用户永远只看到平台 API Key、平台额度、平台调用日志；平台内部如何路由到 DashScope、OpenAI 或其他上游，是平台实现细节，不对租户用户暴露。

因此，本项目第一优先级应调整为：

1. 租户制账号审批与开通
2. 平台 API Key 分发与生命周期管理
3. 租户级 token / 请求量 / 错误率治理
4. 调用明细、失败记录、审计事件闭环
5. 平台侧隐藏真实上游模型凭据与内部路由细节

以下能力在本轮降级为次要或隐藏：

- 面向用户的 RAG 产品化后台
- 面向用户的 Playground 主入口
- 过强的“模型路由展示台”叙事
- 用户可直接感知的上游模型供应商和真实凭据

## 2. 已确认约束

本设计严格基于已确认决策：

- 账号审批模式：`先审批账号`
- 用户与租户关系：`多用户同租户`
- 配额主维度：`仅租户级`
- API Key 获取方式：`审批通过后用户自助创建`
- 控制台入口模式：`同一个控制台，按角色显示不同菜单`
- 平台必须屏蔽自身的真实上游 LLM，不暴露给用户

## 3. 产品重新定义

### 3.1 产品定位

产品应重新命名和叙述为：

- 中文：`AI 接入平台`
- 英文：`AI API Access Platform`

它解决的不是“某个开发者怎么试玩模型”，而是：

- 企业或团队如何申请接入平台
- 平台如何审批和开通访问
- 用户如何安全地领取平台 API Key
- 平台如何记录调用、统计 token、发现失败并进行风控

### 3.2 目标用户

只有两类核心角色：

1. `平台管理员 admin`
2. `租户用户 member`

第一版不再引入更多复杂角色，例如财务管理员、审计员、租户管理员、只读访客。  
这些角色可以后续扩展，但 MVP 不需要。

### 3.3 成功标准

如果这版设计到位，用户在演示时应当能清楚看到：

- 平台存在真实的账号申请与审批入口
- admin 可以审批账号、监控租户、查看异常
- 用户可以自助创建和轮换平台 API Key
- 所有调用都会沉淀租户级日志、token 消耗和失败记录
- 平台不会把 DashScope / Qwen 之类的真实凭据暴露给用户

## 4. 方案比较

### 4.1 方案 A：API Key 分发平台优先

核心：

- 账号审批
- 租户开通
- 用户自助创建平台 API Key
- 租户级 token 账本
- 调用与失败审计

优点：

- 最贴合当前目标
- 最能体现平台化和治理能力
- 最容易向招聘方证明“不是简单套壳聊天页”

缺点：

- 需要弱化现有 RAG/Playground 叙事
- 需要重做控制台信息架构

### 4.2 方案 B：模型网关优先

核心：

- 路由
- 回退
- 限流
- 统一协议代理

优点：

- 技术味更浓
- 适合展示网关与中间件能力

缺点：

- 无法突出分发、审批、租户治理
- 离你现在真正想要的作品方向偏掉

### 4.3 方案 C：控制面 + 数据面双中台

核心：

- 同时建设审批控制面和网关数据面

优点：

- 最完整
- 扩展性强

缺点：

- 第一版太重
- 容易把重点再次做散

### 4.4 推荐方案

本轮采用 `方案 A：API Key 分发平台优先`。

原因：

- 它最符合你现在反复强调的主线
- 它保留了网关能力，但不让网关能力喧宾夺主
- 它最能体现“用户拿到的是平台能力，不是厂商密钥”

## 5. 角色与权限模型

### 5.1 admin

admin 可以：

- 查看账号申请列表
- 审批/拒绝账号申请
- 创建或开通租户
- 把审批通过的用户加入某个租户
- 查看所有租户总览
- 查看任意租户的调用、失败、token 消耗、审计
- 禁用用户
- 禁用租户
- 强制停用某个 API Key

admin 不应该：

- 在前台暴露真实上游密钥明文
- 手工录入用户的上游模型凭据给用户使用

### 5.2 member

member 可以：

- 登录控制台
- 查看自己所属租户信息
- 查看租户剩余额度、已用 token、成功率、失败数
- 自助创建、轮换、停用自己的平台 API Key
- 查看自己发起的调用明细
- 查看自己发起请求的失败记录

member 不可以：

- 查看其他租户的数据
- 查看平台真实 provider 凭据
- 查看真实上游 API Key
- 修改租户总额度
- 审批其他账号

### 5.3 权限边界原则

- 所有接口都必须基于 `role + tenant scope` 做服务端校验
- 前端隐藏不算权限控制
- admin 能跨租户看全局
- member 只能看自己租户，且调用明细默认只看自己发起的请求

## 6. 信息架构重构

### 6.1 控制台模式

保留一个控制台入口，但登录后根据角色展示不同菜单。

### 6.2 admin 侧导航

建议导航：

1. `总览`
2. `账号申请`
3. `租户管理`
4. `调用观测`
5. `失败分析`
6. `审计事件`
7. `系统设置`

说明：

- `路由`
  - 从主导航降级到系统设置内的二级页
- `调试场`
  - admin 可保留，但不作为主导航首要项
- `知识库`
  - 前端隐藏

### 6.3 member 侧导航

建议导航：

1. `我的总览`
2. `API 密钥`
3. `调用记录`
4. `失败记录`
5. `审计轨迹`

说明：

- member 不看“全局路由”和“系统健康”
- member 不看内部 RAG
- member 不看真实 provider 名称和内部路由策略

### 6.4 页面目标

页面职责必须单一：

- `总览`
  - 看健康和额度，不承担配置职责
- `账号申请`
  - 只处理审批
- `租户管理`
  - 只处理租户信息和状态
- `API 密钥`
  - 只处理 key 生命周期
- `调用记录`
  - 只处理成功/失败请求明细
- `失败记录`
  - 只处理失败原因、分类和重试建议
- `审计轨迹`
  - 只处理关键操作链

## 7. 核心业务流程

### 7.1 账号申请与审批

流程：

1. 用户提交账号申请
2. 系统生成 `account_application`
3. admin 在“账号申请”页查看待处理申请
4. admin 审批通过
5. 系统创建用户并绑定到目标租户
6. 记录审批审计事件
7. 用户获得控制台访问权

审批通过时必须记录：

- 谁审批的
- 审批时间
- 分配到了哪个租户
- 是否附带备注

### 7.2 用户自助创建 API Key

流程：

1. 已通过审批的用户进入 `API 密钥`
2. 创建平台 API Key
3. 后端只存 hash，不存可逆明文
4. 前端仅一次性返回原始明文用于复制
5. 后续列表中只显示脱敏值
6. 审计日志记录“谁在何时创建了 key”

### 7.3 请求调用链

流程：

1. 用户用平台 API Key 调用统一平台接口
2. Gateway 校验 key 是否有效、是否属于某租户
3. 根据租户状态与额度进行放行或拒绝
4. Gateway 内部路由到平台绑定的 provider
5. 返回标准化响应
6. 写入调用日志、token 消耗、成功/失败状态
7. 如失败，再写入失败事件和错误分类

### 7.4 额度消耗链

流程：

1. 请求完成后提取 `input_tokens / output_tokens / total_tokens`
2. 若上游无 usage，按平台估算规则补足
3. 累加到租户级账本
4. 更新租户概览和趋势汇总
5. 触发阈值告警与异常检测

## 8. 数据模型

### 8.1 新增或强化的核心实体

建议数据对象如下：

1. `account_applications`
2. `users`
3. `tenants`
4. `tenant_memberships`
5. `platform_api_keys`
6. `tenant_usage_ledger`
7. `llm_request_logs`
8. `llm_request_failures`
9. `audit_events`
10. `provider_credentials`

### 8.2 `account_applications`

字段建议：

- `id`
- `email`
- `name`
- `company_name`
- `use_case`
- `status`
- `reviewer_id`
- `review_comment`
- `reviewed_at`
- `created_at`

状态：

- `pending`
- `approved`
- `rejected`

### 8.3 `tenant_memberships`

作用：

- 解决“多用户同租户”的关系模型

字段建议：

- `id`
- `tenant_id`
- `user_id`
- `role`
- `status`
- `created_at`

第一版 `role` 可只保留：

- `member`

平台级 `admin` 可不放在租户 membership 内，而是平台全局角色。

### 8.4 `platform_api_keys`

字段建议：

- `id`
- `tenant_id`
- `creator_user_id`
- `name`
- `key_prefix`
- `key_hash`
- `status`
- `last_used_at`
- `created_at`
- `rotated_at`
- `disabled_at`

说明：

- 不保存完整明文
- `key_prefix` 用于前台脱敏显示
- key 归属到租户，同时保留创建者

### 8.5 `tenant_usage_ledger`

作用：

- 作为租户级 token 账本，不按用户拆总额度

字段建议：

- `id`
- `tenant_id`
- `bucket_start`
- `input_tokens`
- `output_tokens`
- `total_tokens`
- `request_count`
- `success_count`
- `failure_count`
- `estimated_count`
- `updated_at`

### 8.6 `llm_request_logs`

它仍然是观测主表，但需要更贴合平台主线。

字段建议：

- `id`
- `tenant_id`
- `user_id`
- `platform_api_key_id`
- `request_path`
- `request_model`
- `response_model`
- `usage_source`
- `usage_status`
- `status_code`
- `latency_ms`
- `input_tokens`
- `output_tokens`
- `total_tokens`
- `error_code`
- `error_message_digest`
- `provider_route_label`
- `request_started_at`
- `request_completed_at`
- `created_at`

注意：

- `provider_route_label` 只给 admin 看
- member 默认只看平台级逻辑名称，例如“标准对话模型”

### 8.7 `llm_request_failures`

建议把失败记录从调用明细里抽出专门视图或专表，便于排查。

字段建议：

- `id`
- `request_log_id`
- `tenant_id`
- `user_id`
- `platform_api_key_id`
- `failure_stage`
- `error_category`
- `status_code`
- `retryable`
- `user_message`
- `internal_message_digest`
- `created_at`

失败阶段：

- `auth`
- `quota`
- `validation`
- `upstream`
- `timeout`
- `internal`

### 8.8 `audit_events`

既记录平台管理动作，也记录高风险用户动作。

字段建议：

- `id`
- `actor_type`
- `actor_user_id`
- `tenant_id`
- `event_type`
- `target_type`
- `target_id`
- `detail`
- `ip_digest`
- `created_at`

应覆盖的事件：

- 账号审批通过/拒绝
- 用户加入租户
- API Key 创建/轮换/停用
- 租户禁用/恢复
- 额度阈值告警
- 高失败率告警

## 9. API 设计

### 9.1 admin 侧接口

建议新增或重构：

- `GET /admin/applications`
- `POST /admin/applications/:id/approve`
- `POST /admin/applications/:id/reject`
- `GET /admin/tenants`
- `GET /admin/tenants/:id`
- `POST /admin/tenants/:id/suspend`
- `POST /admin/tenants/:id/restore`
- `GET /admin/usage/overview`
- `GET /admin/usage/requests`
- `GET /admin/failures`
- `GET /admin/audit-events`

### 9.2 member 侧接口

建议新增或重构：

- `GET /me/profile`
- `GET /me/tenant`
- `GET /me/api-keys`
- `POST /me/api-keys`
- `POST /me/api-keys/:id/rotate`
- `POST /me/api-keys/:id/deactivate`
- `GET /me/usage/overview`
- `GET /me/usage/requests`
- `GET /me/failures`
- `GET /me/audit-events`

### 9.3 平台调用接口

对外仍保持统一入口，例如：

- `POST /v1/chat/completions`
- `POST /v1/embeddings`

要求：

- 认证只接受平台 API Key
- 不接受用户直接带上游厂商 key
- 用户看见的是平台模型语义，不是平台内部 provider 凭据

## 10. 前端页面设计

### 10.1 admin 总览

应重点展示：

- 待审批账号数
- 活跃租户数
- 今日请求量
- 今日 token 消耗
- 今日失败率
- 最近高风险租户

### 10.2 账号申请页

列表字段建议：

- 申请人
- 邮箱
- 公司/团队
- 用途说明
- 申请时间
- 当前状态

详情抽屉：

- 申请信息
- 审批按钮
- 拒绝按钮
- 审批备注
- 指定租户

### 10.3 租户管理页

展示：

- 租户名称
- 用户数
- 今日 token
- 7 日请求量
- 失败率
- 当前状态

动作：

- 查看成员
- 查看最近 API Key
- 查看异常
- 禁用/恢复

### 10.4 用户总览页

展示：

- 我所在租户
- 租户总额度使用率
- 我最近 24 小时调用数
- 我最近失败数
- 我最近创建的 key

重点不是展示 provider，而是展示：

- 平台可用性
- 配额状态
- 我的调用健康

### 10.5 API 密钥页

保留现有能力，但文案与逻辑需要切换到“用户自助管理”。

必须支持：

- 新建
- 轮换
- 停用
- 一次性复制
- 脱敏展示

明确不支持：

- 展示明文历史
- 展示上游厂商凭据

### 10.6 调用记录页

字段建议：

- 时间
- API Key
- 平台模型
- 状态
- 延迟
- token
- 来源

member 侧不默认显示：

- 真实 provider 名称
- 内部路由细节

### 10.7 失败记录页

字段建议：

- 时间
- 失败阶段
- 错误分类
- HTTP 状态
- 是否可重试
- 用户可理解的失败说明

页面目标：

- 让用户看懂为什么失败
- 让 admin 看出哪里在异常

### 10.8 审计轨迹页

member 看到的是与自己相关的关键操作：

- 创建 key
- 轮换 key
- 停用 key
- 高失败率提示

admin 看到的是全局治理事件：

- 审批通过/拒绝
- 租户冻结/恢复
- 异常高失败率
- 配额告警

## 11. 日志、失败与 token 设计

### 11.1 token 计量原则

- 优先使用上游真实 usage
- 缺失时使用平台估算
- 所有 token 统一沉淀到租户级账本
- UI 必须标出哪些是估算值

### 11.2 失败分类原则

失败必须可归因，不允许只有“调用失败”。

建议分类：

- `auth_failed`
- `quota_exceeded`
- `invalid_request`
- `rate_limited`
- `upstream_timeout`
- `upstream_error`
- `internal_error`

### 11.3 失败展示原则

- 对用户展示可理解中文说明
- 对 admin 展示更细的诊断分类
- 不向普通用户暴露内部敏感报错全文

### 11.4 审计原则

- 审计不是调用日志的重复
- 审计记录的是“谁做了什么关键动作”
- 调用日志记录的是“某次请求发生了什么”

## 12. 安全边界

### 12.1 凭据隔离

平台必须明确区分两类凭据：

1. `平台 API Key`
2. `平台内部 provider 凭据`

用户只接触第一类，绝不接触第二类。

### 12.2 平台 API Key 安全要求

- 数据库存 hash，不存明文
- 创建/轮换时只返回一次
- 前端只存在内存态
- 刷新页面后不可再次获取

### 12.3 上游凭据安全要求

- 只允许后端通过 secrets 文件或安全存储读取
- 不出现在前端接口响应
- 不出现在日志明文
- 不出现在审计明细

### 12.4 多租户隔离要求

- 所有 member 查询都强制带 `tenant_id` 作用域
- 跨租户访问一律拒绝
- 后端不依赖前端传来的租户归属作为唯一事实来源

## 13. 对现有项目的影响

### 13.1 需要弱化或隐藏的现有模块

- `知识库`
- 面向用户的 `RAG`
- 过于强调 provider / route 的展示逻辑

### 13.2 需要保留但重新定位的能力

- Gateway 转发与统一鉴权
- 调用观测
- API Key 生命周期
- 审计日志

### 13.3 需要新增的产品壳层

- 账号申请页
- 审批页
- 租户管理页
- 用户侧“我的总览”
- 失败记录页

## 14. 非目标

本轮明确不做：

- 用户 BYOK
- 用户自定义 provider
- 用户自己选真实上游模型供应商
- 完整 RAG 产品前台
- 复杂计费与支付
- 多级审批流
- 多维组织架构

## 15. 验收标准

如果一轮实现完成，至少应满足：

1. 存在 `账号申请 -> 审批 -> 加入租户` 的真实流程
2. 用户审批通过后可自助创建平台 API Key
3. 用户只能看到平台 API Key，看不到真实上游凭据
4. 每次调用都能记录租户、用户、API Key、token、状态、失败原因
5. admin 可以查看全局租户总览、失败情况和审计事件
6. member 只能看自己租户和自己相关调用
7. RAG/知识库不再作为本轮核心前台能力出现

## 16. 后续实施建议

实施顺序建议：

1. 先做身份与租户模型
2. 再做 API Key 自助管理
3. 再做租户级 token 与失败账本
4. 最后重构控制台信息架构

这样可以保证每一层完成后都能被验证，不会再次做成“前台看起来很多，但主链路不成立”的状态。
