# Qwen 与 Xiaomi MIMO 双 Provider 接入设计

> 日期：2026-05-01
> 状态：Draft for review
> 目标：在保留 Qwen 的前提下，新增 Xiaomi MIMO 作为平台内部强模型档位，并让 admin 在路由观测中看到 `qwen + mimo` 的真实调用情况

## 背景

当前项目已经具备以下能力：

1. 平台 API Key 分发与租户治理
2. 基于规则的智能模型路由
3. usage / audit / latency wall 等真实调用观测
4. 以 Qwen 为默认上游的实际部署链路

但目前还有三个明显缺口：

1. 上游 provider 仍然基本等价于“只有 Qwen 一条主线路”
2. 强模型档位没有接入新的模型供应方，平台缺少跨 provider 的实际路由价值
3. admin 路由观测页没有按 provider 分开展示真实调用情况，当前只能看到 Qwen 这一组

这轮的目标不是重做网关架构，而是在现有 OpenAI-compatible provider 接入框架上，把 Qwen 与 Xiaomi MIMO 接成双 provider，并打通观测闭环。

## 本轮目标

本轮只做以下事情：

1. 保留 Qwen 作为现有快模型与 embeddings 主线路
2. 新增 Xiaomi MIMO provider credential
3. 将平台内部强模型档位切到 `mimo-v2.5-pro`
4. admin 路由观测页按 provider 分成两组：
   - `Qwen 路由观测`
   - `MIMO 路由观测`
5. 确保所有真实 key 只通过 secret file 或等价安全注入方式进入运行时，不进入代码、文档、`.env.example`、提交记录

## 本轮不做

以下能力不作为本轮强制交付项：

1. MIMO embeddings 接入
2. member 侧直接暴露 `mimo-v2.5-pro`
3. provider 动态后台配置页
4. 多 provider 自动熔断与自动回退
5. 基于健康度的实时 provider 权重调度

说明：

- 用户已经明确接受：如果 MIMO 当前没有稳定可验证的 embeddings 官方能力，本轮就继续由 Qwen 负责 embeddings。

## 已确认约束

这轮设计严格基于以下已确认决策：

1. 接入方案采用“保留 Qwen + 新增 Xiaomi MIMO”的双 provider 方案
2. `mimo-v2.5-pro` 作为平台内部强模型档位
3. Qwen 的新 key 替换当前运行中的旧 Qwen key
4. MIMO 需要参与平台能力，但 member 默认不可见
5. admin 可以在观测侧看到 MIMO
6. admin 路由观测页面必须分成两块列表：
   - Qwen 一组
   - MIMO 一组
7. 代码中不得明文出现任何真实 API Key
8. git 提交记录中不得出现任何真实 API Key
9. 开发流程约束保持不变：每完成一项独立任务，必须提交一次独立的 `git commit`

## 方案比较

### 方案 A：替换式单 Provider 强模型升级

- 保留 Qwen 作为唯一 provider
- 仅修改强模型档位配置
- 不引入 MIMO 的独立 provider 维度

优点：

- 改动最小
- 对现有数据库和观测影响最小

缺点：

- 不能体现跨 provider 路由能力
- admin 侧无法区分 Qwen 与 MIMO 的真实轨迹
- 不符合“新增小米 provider”的目标

### 方案 B：标准双 Provider 接入

- 保留 Qwen provider
- 新增 Xiaomi MIMO provider
- chat 强模型档位切到 MIMO
- embeddings 继续留在 Qwen
- admin 观测按 provider 分组

优点：

- 能真实体现平台的多 provider 路由能力
- 风险可控，不会把 embeddings 一起拖进不确定能力
- 最符合当前产品与演示目标

缺点：

- 需要扩 seed 数据、compose secrets、观测聚合与前端展示

### 方案 C：全能力双 Provider 对齐

- chat / embeddings 全部同时改造成双 provider
- 前台也立即暴露 provider 维度

优点：

- 功能最完整

缺点：

- 如果 MIMO embeddings 不稳定，会形成半实现状态
- 会过早暴露 provider 细节给 member

## 结论

采用 **方案 B：标准双 Provider 接入**。

原因：

1. 它最符合“保留 Qwen、加入 Xiaomi MIMO”的要求
2. 它能真正体现平台的 API 网关路由能力，而不是只改一个配置值
3. 它避免把未确认能力硬接进来，降低上线风险

## 总体架构

## 1. Provider 拆分

平台内部新增两类上游 provider credential：

1. `Qwen Provider`
   - provider 标识仍使用现有 Qwen / DashScope 线路
   - 承担：
     - 快模型 chat
     - embeddings
     - 现有默认调用链路
2. `Xiaomi MIMO Provider`
   - 新增 provider 标识
   - 使用 OpenAI-compatible chat 调用方式
   - 只承担：
     - 强模型 chat

## 2. 路由职责

### chat/completions

- 简单任务默认仍走 Qwen 快模型
- 复杂编码任务走 `mimo-v2.5-pro`
- 最终 usage / audit 里必须能看到：
  - `request_model`
  - `resolved_model`
  - `target_model_tier`
  - `routing_reason`
  - `provider_credential_id`

### embeddings

- 本轮继续只走 Qwen
- 即使平台内部增加 MIMO provider，也不让 embeddings 自动落到 MIMO

### 其他内部能力

- RAG 与内部检索不受本轮影响
- 现有 route resolution 继续复用，不单独引入新协议层

## 3. OpenAI-compatible 接入原则

当前代码库已经有 `OpenAIClient`，并且 DashScope 也是在此基础上做兼容接入。

因此这轮不新增新的独立 SDK 客户端，而是复用同一条 OpenAI-compatible provider 通路：

1. provider 只需要提供：
   - `base_url`
   - `api_key`
   - `supported_models`
2. `chat/completions` 继续通过统一 `OpenAIClient` 发起
3. 如果后续小米接口在响应格式上与标准 OpenAI chat 明显偏离，再单独拆 provider client

这意味着：

- 代码层主要是 provider credential 与配置扩展
- 不是“为小米单独接一整套适配层”

当前已确认的接入信息为：

1. Qwen 继续使用现有 DashScope OpenAI-compatible 路径
2. Xiaomi MIMO chat 使用用户已提供的 OpenAI-compatible 接口与模型：
   - `base_url = https://api.xiaomimimo.com/v1`
   - `model = mimo-v2.5-pro`
3. MIMO embeddings 由于本轮未确认稳定官方能力，不纳入本轮实现

## 配置与密钥设计

## 1. 密钥注入原则

所有真实 API Key 必须满足：

1. 不进入源码
2. 不进入测试快照
3. 不进入 `.env.example`
4. 不进入 README 示例
5. 不进入 git diff 与 commit message

## 2. 推荐的 secret file 结构

继续沿用当前项目的 `${AI_GATEWAY_SECRET_DIR}` 机制。

建议新增或更新以下 secret 文件：

1. `dashscope_api_key`
   - 存放新的 Qwen key
2. `mimo_api_key`
   - 存放 Xiaomi MIMO key
3. `provider_master_key`
   - 继续用于 provider credential 入库加密

## 3. 运行时读取方式

运行时通过 `*_FILE` 或等价文件挂载方式读取：

1. Qwen seed provider key 从 `dashscope_api_key` 读取
2. MIMO seed provider key 从 `mimo_api_key` 读取
3. 两条 provider credential 入库前均使用 `provider_master_key` 加密

## 4. 数据库存储原则

`provider_credentials` 表内不存明文 key，只存：

- `encrypted_secret`
- provider 元信息
- `supported_models`
- `base_url`

查询路由时再由服务端在内存中解密，不对控制台前端返回明文。

## Provider 与模型设计

## 1. Qwen Provider

职责：

1. 默认快模型线路
2. embeddings 线路
3. 当前部署下的默认可用模型组

建议支持模型至少包含：

1. `qwen-flash`
2. 当前项目使用中的 embeddings 模型

## 2. Xiaomi MIMO Provider

职责：

1. 复杂 chat 任务的强模型线路

建议首版支持模型只包含：

1. `mimo-v2.5-pro`

说明：

- 不把 MIMO embeddings 写成已支持能力
- 不在本轮扩更多 MIMO 模型

## 3. 模型档位映射

本轮内部档位映射建议变成：

1. `fast tier -> qwen-flash`
2. `reasoning tier -> mimo-v2.5-pro`

如果后续发现某个环境下 MIMO key 对目标模型无权限，允许通过环境变量把 `reasoning tier` 暂时回切到 Qwen，但代码结构仍保持双 provider。

## Seed 与部署设计

## 1. Seed 数据

当前 seed 逻辑需要从“单 provider seed”扩展到“多 provider seed”。

至少要支持：

1. seed Qwen provider credential
2. seed MIMO provider credential
3. route catalog 中能生成两条 provider 对应的初始 route

注意：

- 如果继续只保留一个 `SeedProvider` 配置结构，后续会非常别扭
- 本轮应把 seed provider 能力扩成“provider 列表”或最少“双 provider seed”

## 2. Compose 设计

`deploy/compose/compose.yml` 需要扩展：

1. 挂载 `mimo_api_key`
2. 保留 `dashscope_api_key`
3. 增加 MIMO 对应的运行时配置项
4. 不在 compose 文件中直接写真实 key

`.env.example` 只保留：

- 模型名
- provider base URL
- secret dir 引用

不提供真实 key。

## 控制台设计影响

## 1. Admin 路由观测页

admin 路由观测页必须从“单列表视图”改成“按 provider 分组视图”。

固定分成两块：

### Qwen 路由观测

展示 Qwen 相关真实调用：

1. 模型名
2. 请求数
3. 成功率
4. 平均延迟
5. 最近失败情况
6. 最近健康状态

### MIMO 路由观测

展示 MIMO 相关真实调用：

1. 模型名
2. 请求数
3. 成功率
4. 平均延迟
5. 最近失败情况
6. 最近健康状态

要求：

1. 两块列表独立渲染，不做混表
2. 如果某一组当前没有真实数据，显示“暂无真实调用数据”
3. 不使用 mock 冒充 provider 真实调用

## 2. Admin 调用观测 / 审计

admin 调用观测与审计页继续允许看到：

1. `resolved_model`
2. `provider display name`
3. 路由原因
4. provider 级失败事件

这样 admin 可以判断：

1. 复杂任务是否真的被切到了 MIMO
2. 某个 provider 最近是否不稳定

## 3. Member 展示边界

member 仍然保持平台抽象，不新增 Xiaomi MIMO 显式曝光。

允许 member 看到的仍然是：

1. 平台策略
2. 任务类型
3. 与 provider 无关的最小必要结果信息

不允许 member 看到：

1. `Xiaomi MIMO` provider 品牌
2. provider credential
3. provider 级路由切换细节
4. `mimo-v2.5-pro` 模型名

因此，如果当前 member 页面存在“实际模型”字段，本轮需要收敛为：

1. 对 member 隐藏该字段，或
2. 将其改写成平台级表达，例如：
   - `平台快速策略`
   - `平台高能力策略`

不能把 `mimo-v2.5-pro` 直接展示给 member。

## 数据与聚合设计

现有 usage / audit / latency wall 数据源继续复用，不新建一套旁路统计。

这轮的关键是聚合口径必须显式按 provider 区分：

1. Qwen 聚合
2. MIMO 聚合

至少要保证以下查询维度可用：

1. `provider_credential_id`
2. `provider display name`
3. `request_model`
4. `resolved_model`
5. `status_code`
6. `latency`

如果当前某些 admin 页面仍然把 provider 聚合成一条默认线路，需要改成 provider-aware 聚合。

## 安全设计

## 1. 代码与文档边界

必须确保：

1. 不在源码中写入真实 key
2. 不在测试里写入真实 key
3. 不在 README 中出现真实 key
4. 不在 spec / plan 文档中出现真实 key

## 2. 运行日志边界

provider 请求失败时：

1. 可以记录 provider 名、模型名、状态码、错误摘要
2. 不可以记录 `Authorization` 头
3. 不可以把 key 片段拼进错误信息

如果上游错误响应会回显敏感字段，应继续走现有错误摘要收敛策略，必要时补脱敏。

## 3. git 提交边界

必须遵守：

1. 提交前扫描工作区，确认无真实 key
2. commit message 不出现 key
3. 如需生成本地 secret 文件，只在工作机 `${AI_GATEWAY_SECRET_DIR}` 下操作，不纳入 git

## 测试策略

## 1. 后端单元测试

至少覆盖：

1. Qwen 与 MIMO provider credential 均可被加载
2. 路由到 `mimo-v2.5-pro` 时能正确解析 provider route
3. embeddings 仍然解析到 Qwen

## 2. 后端集成测试

至少覆盖：

1. 复杂 chat 请求最终上游模型为 `mimo-v2.5-pro`
2. 简单 chat 请求仍走 Qwen
3. embeddings 请求仍走 Qwen
4. usage / audit 中能看到 MIMO 调用记录

## 3. 前端测试

至少覆盖：

1. admin 路由观测页出现 `Qwen 路由观测`
2. admin 路由观测页出现 `MIMO 路由观测`
3. 两块数据独立展示，不是单一混表
4. member 页面不出现 Xiaomi MIMO 字样

## 4. 安全验证

至少执行：

1. 仓库搜索 `sk-` 等常见 key 模式，确认没有真实泄露
2. 搜索新增 secret 文件名，确认只出现在挂载与配置引用中
3. 检查 git diff，确认不包含真实 key

## 验收标准

本轮完成后，至少满足：

1. 平台同时存在 Qwen 与 Xiaomi MIMO 两条 provider credential
2. 复杂 chat 请求可以真实路由到 `mimo-v2.5-pro`
3. embeddings 继续稳定走 Qwen
4. admin 路由观测页分成 `Qwen` 与 `MIMO` 两个展示列表
5. usage / audit 能看到两类 provider 的真实调用轨迹
6. member 不看到 Xiaomi MIMO provider 细节
7. 代码、文档、提交记录中不出现真实 API Key

## 风险与缓解

### 1. MIMO 模型权限风险

风险：

- 当前 key 可能对 `mimo-v2.5-pro` 无权限

缓解：

- 首先通过真实调用验证 chat 权限
- 保留环境变量回切机制
- 不把 embeddings 一起绑定到 MIMO

### 2. 单 provider seed 结构阻碍扩展

风险：

- 现有 seed 结构更偏单 provider，扩双 provider 时容易出现逻辑补丁

缓解：

- 本轮直接把 seed 逻辑提升到 provider 列表级，不继续堆单 provider 特判

### 3. 观测页仍只展示默认线路

风险：

- 后端 SQL 或前端页面仍按旧口径聚合，导致 MIMO 数据存在但看不见

缓解：

- 把 provider 分组作为这轮明确验收项
- 前后端都补回归测试

## 后续扩展

下一阶段可以继续扩展：

1. MIMO embeddings 接入
2. provider 级健康阈值与熔断
3. admin 可配置的 provider 路由策略
4. 更细的 provider 成本对比与模型健康评分
