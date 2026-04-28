# AI Gateway 注册密码与本地图形验证码设计

> 日期：2026-04-28
> 状态：Draft for review
> 目标：让外部用户在提交接入申请时即可设置登录密码，并且必须先通过本地图形验证码校验，admin 审批通过后即可直接登录控制台
> 适用范围：
> - 未登录申请用户
> - 平台管理员 `admin`
> - 审批通过后的租户用户 `member`

## 1. 设计结论

本轮采用：

`方案 A：申请表直存密码哈希 + 本地图形验证码 + 审批通过后直接登录`

主流程收敛为：

1. 未登录用户访问 `/apply`
2. 先获取本地图形验证码
3. 用户填写 `邮箱 / 姓名 / 公司 / 用途 / 密码 / 确认密码 / 验证码`
4. 用户先点击“验证验证码”
5. 验证成功后，注册按钮才可点击
6. 提交申请时后端再次基于验证码通过凭证校验
7. 申请写入 `account_applications`，同时写入 `password_hash`
8. admin 审批通过后，系统创建正式 `member` 用户并写入密码哈希
9. 用户随后可以直接使用 `邮箱 + 密码` 登录控制台

产品结论是：

- 注册不是直接开户，而是“先申请、后审批”
- 密码不是审批后再补，而是在申请时就设定
- 验证码不是装饰，而是提交申请前的必过门槛
- 平台只保存密码哈希，不保存密码明文

## 2. 已确认约束

以下约束已经明确，不再变更：

- 验证码类型：`本地图形验证码`
- 部署方式：`自部署，不依赖第三方验证码服务`
- 密码生效时机：`申请提交时就保存密码哈希`
- 审批通过后的登录方式：`直接使用申请时设置的密码登录`
- 邮箱占用规则：
  - 同邮箱不允许同时存在有效正式账号和新的申请
  - 同邮箱不允许重复提交 `pending` 申请
  - 同邮箱如果历史申请为 `rejected`，允许重新发起一条新申请
- 验证码策略：
  - 一次性使用
  - `5 分钟` 内有效
  - 验证码通过后，注册提交成功或失败都立即失效
- 项目级开发流程约束：`每完成一个开发任务，必须提交一次独立 git commit`

## 3. 问题定义

当前项目的申请与登录链路存在结构性缺口：

1. `/apply` 页面只能提交基础信息，不能设置登录密码
2. admin 审批通过后，新用户没有自己的密码，无法靠本次申请直接登录
3. 登录页里的普通用户账号仍然主要依赖运行时预置账号，不是真实的“申请开户”流程
4. 公开申请接口缺少有效的人机校验，容易被脚本刷申请
5. 当前 `ApproveApplication` 会创建用户和租户成员关系，但没有完整的“申请密码 -> 正式密码”迁移逻辑

如果继续沿用当前结构，只会得到一个“能提交申请、但申请后并不能直接使用”的半成品。

## 4. 方案比较

### 4.1 方案 A：申请表直存密码哈希 + 本地图形验证码

核心：

- 注册页直接填写密码
- 后端保存 `password_hash`
- 图形验证码先校验，校验通过后才可提交
- admin 审批通过后直接转成正式账号

优点：

- 链路最短
- 最符合已确认需求
- 用户心智最自然：申请时设置密码，审批通过后直接登录
- 能和现有 `bcrypt` 登录逻辑直接对齐

缺点：

- 需要扩展申请表结构
- 需要补验证码挑战与验证状态管理
- 需要更严格地处理申请阶段的密码哈希保留与清理

### 4.2 方案 B：申请时填写密码，但审批通过后强制再设置一次

优点：

- 更保守

缺点：

- 和“审批通过后直接登录”的目标冲突
- 多一次激活步骤，链路变长

### 4.3 方案 C：申请时不存密码，审批通过后再发激活流程

优点：

- 申请表更轻

缺点：

- 不符合这轮已经确认的方案 A
- 需要额外做激活 token、首次设置密码页面和到期逻辑

### 4.4 推荐方案

采用 `方案 A`。

这是唯一同时满足“注册时设密码”“先验证码再提交”“审批通过后直接登录”三项约束的方案。

## 5. 总体流程设计

### 5.1 申请用户流程

1. 打开 `/apply`
2. 页面加载时请求一个新的图形验证码挑战
3. 用户填写申请信息和密码
4. 用户输入验证码并点击“验证验证码”
5. 后端校验成功后返回一次性 `captcha_pass_token`
6. 前端收到 `captcha_pass_token` 后，将“提交申请”按钮置为可点击
7. 用户点击“提交申请”
8. 后端消费 `captcha_pass_token`，创建申请记录
9. 返回 `pending` 状态

### 5.2 admin 审批通过流程

1. admin 在“账号申请”页选中 `pending` 申请
2. admin 指定 `tenant_id` 并点击“审批通过”
3. 后端创建或激活正式用户
4. 后端创建 `tenant_memberships`
5. 将申请中的 `password_hash` 写入正式用户
6. 清空申请记录中的 `password_hash`
7. 写入 `application_approved` 审计事件
8. 返回 `approved`

### 5.3 admin 拒绝流程

1. admin 选中 `pending` 申请
2. 点击“拒绝审批”
3. 后端将申请状态改为 `rejected`
4. 清空申请记录中的 `password_hash`
5. 写入 `application_rejected` 审计事件
6. 返回 `rejected`

### 5.4 后续登录流程

1. 用户进入登录页
2. 输入审批通过时对应的邮箱和自己注册时设置的密码
3. 系统走现有控制台会话登录逻辑
4. 登录成功后进入 `member` 视图

## 6. 状态机设计

### 6.1 验证码挑战状态

`issued -> verified -> consumed`

以及两种终态：

- `expired`
- `failed`

说明：

- `issued`
  - 已生成，等待输入
- `verified`
  - 用户已通过验证码校验，拿到一次性通过凭证
- `consumed`
  - 注册接口已消费该通过凭证
- `expired`
  - 超过 `5 分钟`
- `failed`
  - 超过最大尝试次数或显式作废

### 6.2 申请状态

`pending -> approved`

`pending -> rejected`

说明：

- 只有 `pending` 可被审批
- `approved` 与 `rejected` 都是终态
- `rejected` 后允许同邮箱创建新的申请

## 7. 数据模型设计

### 7.1 `account_applications` 扩展

新增字段：

- `password_hash text null`
- `email_normalized text not null`

字段含义：

- `password_hash`
  - 申请阶段暂存的登录密码哈希
  - 仅在 `pending` 阶段保留
  - 审批通过或拒绝后必须清空
- `email_normalized`
  - 统一使用小写、去空白后的邮箱
  - 用于去重和状态判断

约束：

- 对 `pending` 申请建立部分唯一索引：
  - `unique(email_normalized) where status = 'pending'`

设计意图：

- 允许保留多条历史 `rejected` 记录
- 防止同邮箱重复提交待审申请

### 7.2 `users` 保持现有密码模型

沿用现有：

- `password_hash`

密码算法继续与现有登录逻辑保持一致：

- 使用当前仓库已存在的 `bcrypt`

### 7.3 新增 `captcha_challenges`

新增验证码挑战表：

- `id text primary key`
- `answer_hash text not null`
- `status text not null`
- `verify_attempts integer not null default 0`
- `max_attempts integer not null default 5`
- `pass_token_hash text null`
- `issued_ip text not null default ''`
- `issued_user_agent text not null default ''`
- `verified_at timestamptz null`
- `consumed_at timestamptz null`
- `expires_at timestamptz not null`
- `created_at timestamptz not null default now()`
- `updated_at timestamptz not null default now()`

说明：

- 不存验证码图片二进制，只存答案哈希
- 图片在生成时直接返回给前端
- `pass_token_hash` 用于保存验证码通过后的一次性凭证哈希

### 7.4 数据保留原则

- `captcha_challenges`
  - 只保留短期数据，可通过定时清理任务删除过期记录
- `account_applications.password_hash`
  - 只在 `pending` 期间存在
  - 一旦 `approved` 或 `rejected`，立即清空
- 正式用户密码哈希只保留在 `users.password_hash`

## 8. 接口设计

### 8.1 获取验证码

`GET /console/captcha`

响应：

```json
{
  "captcha_id": "cap_123",
  "image_data": "data:image/png;base64,...",
  "expires_at": "2026-04-28T12:00:00Z"
}
```

说明：

- `image_data` 直接用于前端 `<img src="...">`
- 每次刷新验证码都生成新挑战

### 8.2 校验验证码

`POST /console/captcha/verify`

请求：

```json
{
  "captcha_id": "cap_123",
  "captcha_code": "A7KQ"
}
```

响应：

```json
{
  "captcha_pass_token": "cp_opaque_token",
  "expires_at": "2026-04-28T12:00:00Z"
}
```

规则：

- 校验成功后：
  - 挑战状态从 `issued` 变成 `verified`
  - 生成一次性 `captcha_pass_token`
- 校验失败后：
  - `verify_attempts + 1`
  - 达到 `max_attempts` 后标记为 `failed`

### 8.3 提交申请

`POST /console/applications`

请求：

```json
{
  "email": "user@example.com",
  "name": "申请用户",
  "company_name": "Example Co",
  "use_case": "企业内部接入",
  "password": "Example1234",
  "captcha_pass_token": "cp_opaque_token"
}
```

响应：

```json
{
  "item": {
    "id": "app_xxx",
    "email": "user@example.com",
    "name": "申请用户",
    "company_name": "Example Co",
    "use_case": "企业内部接入",
    "status": "pending",
    "created_at": "2026-04-28T12:00:00+08:00"
  }
}
```

服务端校验顺序：

1. `email` 非空且格式合法
2. `name` 非空
3. `password` 满足强度要求
4. `captcha_pass_token` 存在且有效
5. 邮箱未占用正式账号
6. 邮箱没有 `pending` 申请
7. 若同邮箱历史申请均为 `rejected`，允许继续

关键行为：

- 注册接口消费 `captcha_pass_token` 后，无论申请创建成功还是失败，都必须让该 token 失效
- 如果因为邮箱冲突导致失败，用户必须重新获取并验证新的验证码

### 8.4 审批通过接口

继续使用现有：

`POST /admin/applications/:id/approve`

请求不需要新增字段，但服务端行为要变更：

- 从申请记录读取 `password_hash`
- 写入正式 `users.password_hash`
- 审批完成后清空申请记录中的 `password_hash`

### 8.5 拒绝审批接口

继续使用现有：

`POST /admin/applications/:id/reject`

服务端新增行为：

- 拒绝后清空申请记录中的 `password_hash`

## 9. 校验与错误语义

### 9.1 密码规则

第一版采用以下规则：

- 长度：`8-72` 个字符
- 至少包含：
  - `1` 个字母
  - `1` 个数字

拒绝：

- 全空白密码
- 超过 `72` 字节的密码

### 9.2 邮箱冲突规则

注册时按以下优先级判断：

1. 如果 `users` 中已存在该邮箱的正式账号：
   - 返回冲突
   - 提示“该邮箱已开通，请直接登录”
2. 如果 `account_applications` 中已存在 `pending`：
   - 返回冲突
   - 提示“该邮箱已有待审批申请”
3. 如果只有历史 `rejected`：
   - 允许新申请

### 9.3 验证码错误

- `captcha_id` 不存在：
  - 返回 `400`
- 验证码错误：
  - 返回 `400`
- 验证码过期：
  - 返回 `400`
- 验证码已验证但凭证已消费：
  - 返回 `400`

### 9.4 审批错误

- `password_hash` 为空的 `pending` 申请不得通过审批
- 如果审批时检测到目标邮箱已经存在正式账号：
  - 返回 `409`
  - 不允许静默覆盖已有正式用户密码

## 10. 前端交互设计

### 10.1 `/apply` 页面新增字段

新增：

- `密码`
- `确认密码`
- `验证码输入框`
- `验证码图片`
- `刷新验证码` 按钮
- `验证验证码` 按钮

### 10.2 按钮行为

- “提交申请”默认禁用
- 只有同时满足以下条件时才启用：
  - 必填字段已填写
  - 密码和确认密码一致
  - 验证码校验成功

### 10.3 验证码体验

- 首次进入页面自动拉取验证码
- 点击“刷新验证码”立即生成新验证码
- 验证失败时保留当前表单内容，只刷新验证码区域
- 验证成功后显示“验证码已通过”
- 如果验证码过期或提交失败导致凭证失效：
  - 前端提示重新验证验证码
  - “提交申请”重新禁用

### 10.4 成功态

申请提交成功后页面显示：

- 申请人
- 邮箱
- 状态 `pending`
- 文案提示：
  - “申请已提交，等待管理员审批。审批通过后可直接使用本次设置的密码登录。”

## 11. admin 控制台改动

### 11.1 申请列表

无需新增新的审批动作，仍保留：

- `审批通过`
- `拒绝审批`

### 11.2 审批摘要增强

建议增加只读提示：

- “该申请已设置登录密码”

注意：

- admin 不可查看用户密码明文
- admin 不可查看用户密码哈希

### 11.3 审批通过后的结果卡

审批通过后可以继续显示：

- 申请人
- 状态
- 租户

不新增任何密码展示能力。

## 12. 安全设计

### 12.1 密码安全

- 明文密码只存在于用户浏览器提交时的请求体中
- 服务端收到后立即转成 `bcrypt hash`
- 禁止在日志、审计事件、错误信息中记录密码明文
- 禁止在 `account_applications` 终态记录中长期保留密码哈希

### 12.2 验证码安全

- 验证码答案只存哈希
- 验证码通过凭证只存哈希
- 同一挑战最多尝试 `5` 次
- 过期自动失效
- 提交申请时必须消费通过凭证

### 12.3 抗滥用

第一版至少增加以下限流点：

- `GET /console/captcha`
  - 按 IP 做基础频率限制
- `POST /console/captcha/verify`
  - 按 IP 和 `captcha_id` 做限制
- `POST /console/applications`
  - 按 IP 和邮箱做基础频率限制

第一版不引入第三方风控服务。

## 13. 数据迁移设计

### 13.1 数据库迁移

需要新增迁移：

1. 为 `account_applications` 增加：
   - `password_hash`
   - `email_normalized`
2. 回填历史数据的 `email_normalized`
3. 为 `pending` 申请增加部分唯一索引
4. 新建 `captcha_challenges`

### 13.2 历史申请处理

历史申请记录如果没有 `password_hash`：

- 可以继续展示
- 但不能直接用于新的“审批通过即登录”链路

因此审批逻辑应明确区分：

- 新申请：
  - 必须有 `password_hash`
- 老申请：
  - 若无 `password_hash`，审批通过时直接报冲突，并提示“该申请来自旧流程，请要求用户重新申请”

## 14. 测试设计

### 14.1 后端测试

至少覆盖：

- 创建验证码成功
- 验证码校验成功
- 验证码错误
- 验证码过期
- 验证码多次错误后失效
- 验证码通过凭证只能消费一次
- 提交申请时写入 `password_hash`
- 同邮箱 `pending` 冲突
- 同邮箱正式账号冲突
- `rejected` 后可重新申请
- 审批通过时迁移 `password_hash` 到 `users`
- 审批通过后清空申请中的 `password_hash`
- 拒绝后清空申请中的 `password_hash`

### 14.2 前端测试

至少覆盖：

- `/apply` 页面显示密码和验证码控件
- 密码不一致时不能提交
- 验证码未通过时提交按钮禁用
- 验证码验证成功后提交按钮启用
- 注册成功后显示 `pending`
- 验证码失效后重新禁用提交按钮

### 14.3 集成测试

至少覆盖一条完整链路：

1. 获取验证码
2. 校验验证码
3. 提交申请
4. admin 审批通过
5. 用户使用注册密码成功登录

## 15. 非目标

本轮不包含：

- 邮箱验证码
- 短信验证码
- 忘记密码
- 重置密码邮件
- 首次激活链接
- 第三方风控服务接入
- 多因素认证

这些能力可以后续追加，但不属于本轮范围。

## 16. 实施建议

建议后续实施顺序：

1. 数据库迁移与约束补齐
2. 本地图形验证码服务与接口
3. 申请接口扩展为支持密码与验证码通过凭证
4. admin 审批逻辑迁移密码哈希并清理申请残留
5. 注册页前端改造
6. 登录与申请联调
7. 全链路回归测试

## 17. 设计结果

本设计完成后，平台的外部开户路径会从：

`提交资料 -> admin 审批 -> 还不能直接登录`

变成：

`图形验证码通过 -> 提交申请并设置密码 -> admin 审批 -> 直接登录 -> 创建平台 API Key`

这条链路更完整，也更符合“租户制 API Key 分发与治理平台”的产品形态。
