# 固定问答语义缓存（小模型判定 + 白名单缓存）设计

## 1. 设计结论

本期在 `ai_gateway` 中新增一套 **固定问答语义缓存** 能力，目标是：

- 对高重复、低风险、平台内置的固定问答请求，优先命中缓存
- 命中后直接返回预置答案，不再调用后端主 LLM
- 未命中或判定异常时，保持现有主链路不变，直接放行到原有模型路由

本期采用方案：

- **前置判定模型**：`qwen-mt-flash`
- **缓存范围**：仅固定问答类
- **答案来源**：平台内置白名单 FAQ
- **隔离维度**：`platform_api_key_id`
- **请求范围**：仅 `POST /v1/chat/completions` 的**非流式**请求
- **命中后返回**：直接返回标准答案，不做二次润色
- **判定失败策略**：直接放行到原主模型链路
- **计费策略**：缓存命中仍按正常 LLM 调用口径计费

本期不实现：

- embedding / 向量数据库语义检索
- 跨 API Key 共享缓存
- 流式命中返回
- admin 页面动态维护 FAQ
- 基于真实历史问答自动学习缓存

## 2. 为什么选择这个方案

### 2.1 备选方案

#### 方案 A：纯规则白名单缓存

仅通过问题标准化 + 固定规则映射命中 FAQ。

优点：

- 实现最轻
- 成本最低
- 几乎没有误命中链路

缺点：

- 只能覆盖非常有限的问法变体
- 无法满足“小模型做语义判定”的目标

#### 方案 B：小模型判定 + 平台内置白名单缓存

先调用 `qwen-mt-flash` 对用户问题做固定问答意图识别；若识别到某个内置 FAQ key，则直接返回标准答案。

优点：

- 能识别一定的问法变体
- 不需要引入 embedding、向量库和相似度阈值治理
- 与现有聊天主链路兼容，改造可控

缺点：

- 每次进入判定链路都会额外调用一次小模型
- 比纯规则方案增加少量延迟与 token 成本

#### 方案 C：向量语义缓存

把问题与答案做 embedding 存到向量库或 Redis 向量索引中，请求时做相似度匹配，命中后直接返回。

优点：

- 更接近通用语义缓存
- 可覆盖更多自然语言变体

缺点：

- 需要补 embedding 模型、索引、阈值、误命中治理、回写策略
- 本期复杂度明显过高

### 2.2 最终选择

采用 **方案 B**。

原因：

1. 用户已明确要求用小模型做前置判断。
2. 当前仓库虽然已有 Redis 依赖，但没有现成的向量检索闭环，本期直接上语义向量缓存风险过高。
3. 固定问答场景天然适合“判定 -> 白名单答案”的轻量闭环。
4. 即使判定失败，也可以直接回退主链路，不影响现有可用性。

## 3. 当前系统基础与约束

### 3.1 已有基础

当前仓库已经具备以下可复用能力：

1. **统一聊天入口**
   - 现有主入口为 `/v1/chat/completions`
   - 已有完整的请求转发、认证、路由和调用观测链路

2. **请求日志与费用统计**
   - 已有 `llm_request_logs`
   - 已有 `llm_request_events`
   - 已有小时级聚合与租户账本能力

3. **平台 API Key 维度归因**
   - 调用链路中已稳定存在 `platform_api_key_id`
   - 可作为缓存隔离维度

4. **Redis 依赖已存在**
   - `gateway/go.mod` 已引入 `github.com/redis/go-redis/v9`
   - 具备接入轻量缓存层的基础

### 3.2 本期约束

1. 缓存仅覆盖**固定问答类**。
2. 命中结果必须来自**平台内置答案**，不能由判定模型直接生成。
3. 缓存隔离做到 **API Key 级别**。
4. 流式请求不进入缓存链路，避免影响现有流式实现。
5. 命中后仍按正常模型调用口径计费，保证业务账单口径一致。

## 4. 目标与非目标

## 4.1 目标

本期目标如下：

- 为非流式聊天请求增加固定问答缓存命中能力
- 通过 `qwen-mt-flash` 识别用户是否在问平台内置 FAQ
- 命中时直接返回标准答案，减少主模型消耗
- 在调用观测中区分“缓存命中”与“真实上游调用”
- 保持主链路稳定：判定失败或未命中时无感回退

## 4.2 非目标

本期明确不做：

- 泛化语义缓存
- embedding 模型与向量库
- admin 页面维护 FAQ
- FAQ 按租户定制
- 对流式请求做缓存命中伪流式返回
- 将缓存命中视为“免费请求”

## 5. 主链路设计

### 5.1 入口范围

仅拦截：

- `POST /v1/chat/completions`
- 且 `stream != true`

以下请求直接走原有主链路：

- `stream=true`
- 非聊天接口
- 结构不合法的请求

### 5.2 处理流程

处理链路如下：

1. 网关收到非流式聊天请求
2. 完成现有认证、API Key 识别、租户归因等前置步骤
3. 从请求中提取用于判定的用户问题
4. 做基础标准化
5. 调用 `qwen-mt-flash` 做固定问答意图识别
6. 若返回“命中某个 FAQ key，且置信度达标”：
   - 按 `platform_api_key_id + faq_key` 查询缓存答案
   - 命中则直接返回标准答案
   - 未命中则读取内置 FAQ 标准答案并写入缓存后返回
7. 若判定失败、超时、不确定或未达阈值：
   - 直接放行到原主模型调用链路
8. 结果进入调用日志、事件流和聚合链路

### 5.3 用户问题提取规则

为了降低实现复杂度并减少误判，本期只取：

- `messages` 中**最后一条 `role=user`** 的文本内容

本期不处理：

- 多模态输入
- 图片、文件引用
- 工具调用上下文
- 多轮会话整体语义压缩

### 5.4 标准化规则

在送入判定模型前，对文本做基础标准化：

- 去首尾空白
- 合并连续空格
- 全角/半角常见标点归一
- 去掉明显无意义结尾标点重复，如 `？？？`、`！！！`
- 保留中文原文，不做激进改写

标准化目标不是直接命中缓存，而是让小模型更容易稳定识别固定问答意图。

## 6. FAQ 白名单设计

### 6.1 一期 FAQ 范围

一期先内置少量高重复、低风险、平台性质的问答，例如：

- `faq.greeting.hello`
  - 问题意图：你好 / hi / hello
- `faq.identity.who_are_you`
  - 问题意图：你是谁
- `faq.capability.what_can_you_do`
  - 问题意图：你可以做什么
- `faq.platform.what_is_this`
  - 问题意图：这个平台是做什么的

### 6.2 FAQ 结构

建议在服务端维护内置 FAQ 注册表，字段至少包括：

- `faq_key`
- `title`
- `answer`
- `enabled`
- `version`
- `tags`

例如：

- `faq_key`: `faq.identity.who_are_you`
- `answer`: 平台标准回复
- `version`: `v1`

### 6.3 标准答案来源

本期答案来源固定为代码内置配置，不从数据库动态读取。

原因：

- 平台固定问答变化频率低
- 先把链路做稳定
- 避免本期把 admin 配置界面也一并做掉

## 7. 小模型判定设计

### 7.1 判定模型

固定使用：

- `qwen-mt-flash`

该模型只承担：

- 判断用户问题是否属于固定问答
- 如果属于，返回命中的 `faq_key`
- 给出置信度或判定状态

它**不负责生成最终答案**。

### 7.2 判定输出结构

建议判定输出为严格 JSON，例如：

- `matched`: `true/false`
- `faq_key`: 命中的 FAQ key，未命中为空
- `confidence`: `0~1`
- `reason`: 简短解释

例如：

```json
{
  "matched": true,
  "faq_key": "faq.identity.who_are_you",
  "confidence": 0.97,
  "reason": "用户在询问系统身份"
}
```

### 7.3 命中阈值

由于本期目标是“省钱优先、避免误命中”，建议阈值偏保守：

- `matched = true`
- `faq_key` 必须在平台注册表中存在且启用
- `confidence >= 0.90`

任一条件不满足，都视为未命中并放行主链路。

### 7.4 判定失败与超时

以下情况都直接放行主链路：

- 判定接口超时
- 判定模型 HTTP 错误
- 判定结果解析失败
- JSON 字段不完整
- `faq_key` 不存在
- 置信度不达阈值

这样可保证缓存能力是“加分项”，不会成为主链路阻塞点。

## 8. 缓存与存储设计

### 8.1 为什么仍然需要缓存层

虽然 FAQ 答案本身是代码内置的，但仍建议保留轻量缓存层，原因有三点：

1. 可以沉淀 API Key 维度的命中记录
2. 可以为后续二期“动态 FAQ / 半结构化缓存”预留接口
3. 可以让“白名单答案返回”与“缓存命中观测”统一语义

### 8.2 缓存键设计

缓存维度按 API Key 隔离。

建议缓存 key 结构：

- `faq_cache:{platform_api_key_id}:{faq_key}:v{version}`

例如：

- `faq_cache:pak_live_console:faq.identity.who_are_you:v1`

### 8.3 缓存值结构

缓存值建议至少包括：

- `faq_key`
- `answer`
- `version`
- `source = builtin`
- `created_at`
- `updated_at`
- `hit_count`

### 8.4 Redis 与数据库职责划分

本期建议：

- **Redis**：在线快速读取 FAQ 返回体与命中计数
- **PostgreSQL**：记录请求日志、事件、费用和观测字段

本期不建议单独新增 FAQ 大表存答案正文；答案正文先以内置代码配置为主。

如果后续需要持久化 FAQ 配置，再补：

- `faq_catalog`
- `faq_cache_entries`

## 9. 返回与计费设计

### 9.1 返回格式

缓存命中时，直接返回与当前 OpenAI 兼容接口一致的非流式 JSON：

- `model`
- `choices[0].message.content`
- `usage`

返回表现上尽量与正常主模型返回保持一致，避免调用方需要写分支。

### 9.2 usage 口径

由于用户已明确要求“缓存命中也按正常 LLM 调用计费”，本期建议：

- `prompt_tokens`：按请求内容正常估算或复用现有统计逻辑
- `completion_tokens`：按标准答案内容估算
- `total_tokens`：二者求和
- `cost`：按**请求模型**的正常计费规则计算

注意：

- **不按 `qwen-mt-flash` 判定成本对外记账**
- 对业务侧账单而言，缓存命中看起来像一次正常请求

### 9.3 内部真实成本

内部可额外记录：

- `cache_hit=true`
- `classifier_model=qwen-mt-flash`
- `classifier_cost_estimate`

用于后续平台内部评估：

- 缓存命中是否真的省钱
- 判定链路本身是否值得保留

## 10. 观测与后台展示设计

### 10.1 调用日志

建议在 `llm_request_logs` 增加以下字段：

- `cache_hit boolean not null default false`
- `cache_type text not null default ''`
- `cache_key text not null default ''`
- `cache_faq_key text not null default ''`
- `classifier_model text not null default ''`
- `classifier_latency_ms integer not null default 0`
- `classifier_status text not null default ''`

字段语义：

- `cache_hit`
  - 是否命中缓存
- `cache_type`
  - 本期固定为 `faq_semantic`
- `cache_key`
  - 实际缓存键
- `cache_faq_key`
  - 命中的 FAQ key
- `classifier_model`
  - 本期固定为 `qwen-mt-flash`
- `classifier_latency_ms`
  - 判定耗时
- `classifier_status`
  - 例如：`hit` / `miss` / `timeout` / `invalid_result`

### 10.2 事件流

建议在 `llm_request_events` 中补充缓存事件，例如：

- `classifier_started`
- `classifier_hit`
- `classifier_miss`
- `cache_served`
- `fallback_upstream`

这样异常事件流和请求详情里可以明确看出：

- 是否先走了判定
- 为什么没有命中
- 是缓存返回还是上游返回

### 10.3 调用观测页展示

后台建议补充：

- 请求明细新增“是否缓存命中”列
- 请求详情新增“缓存类型 / FAQ key / 判定状态 / 判定耗时”
- 聚合页新增：
  - 缓存命中次数
  - 缓存命中率
  - 估算节省的主模型调用次数

## 11. 后端模块拆分建议

### 11.1 新增能力模块

建议拆成以下职责单元：

1. `FAQRegistry`
   - 平台内置 FAQ 注册表
   - 负责 FAQ key、答案、版本与启停状态

2. `FAQClassifierService`
   - 调用 `qwen-mt-flash`
   - 对输入问题做 FAQ 意图识别

3. `FAQCacheService`
   - 管理 Redis 读写
   - 负责按 API Key 维度命中 FAQ 返回内容

4. `CachedChatResponder`
   - 将 FAQ 命中结果包装成兼容 chat completion 的标准响应

5. `ChatProxyOrchestrator` 扩展
   - 在现有聊天代理链路中插入：
     - 判定
     - 缓存命中
     - fallback 主链路

### 11.2 不建议的做法

不建议把全部逻辑直接堆进现有 chat handler，原因：

- 判定、缓存、包装响应、观测字段会快速变得混乱
- 后续做二期扩展时很难继续维护

## 12. 失败处理与安全边界

### 12.1 安全边界

本期固定问答缓存只适用于：

- 平台通用介绍性问题
- 低风险、低歧义问题

不进入缓存白名单的内容包括但不限于：

- 涉及租户业务数据
- 涉及费用、额度、账户权限的具体问题
- 涉及多轮上下文推理的问题
- 涉及外部工具、知识库、RAG 的问题

### 12.2 失败处理原则

原则是：

- **宁可不命中，也不要错命中**

具体表现为：

- 判定结果不确定就放行
- 置信度偏低就放行
- Redis 异常就放行
- FAQ 配置异常就放行

### 12.3 降级策略

本期缓存链路必须天然支持降级：

- 关闭开关后，完全回到当前主链路
- 判定模型异常时，不影响主模型调用
- Redis 不可用时，不影响主模型调用

## 13. 配置设计

建议增加配置项：

- `FAQ_SEMANTIC_CACHE_ENABLED`
  - 默认 `false`
- `FAQ_SEMANTIC_CACHE_MODEL`
  - 默认 `qwen-mt-flash`
- `FAQ_SEMANTIC_CACHE_TIMEOUT_MS`
  - 默认 `1500`
- `FAQ_SEMANTIC_CACHE_CONFIDENCE_THRESHOLD`
  - 默认 `0.90`
- `FAQ_SEMANTIC_CACHE_REDIS_TTL_SECONDS`
  - 默认 `86400`

说明：

- 开关默认关闭，避免影响当前生产行为
- TTL 虽然对白名单 FAQ 不敏感，但可用于控制 hit_count 生命周期和后续版本升级

## 14. 数据库变更建议

一期建议最小改造，仅补日志与观测字段，不新增重表。

建议迁移：

- `llm_request_logs`
  - 增加缓存相关字段
- `llm_request_events`
  - 如当前结构允许，不一定需要改表，只需新增事件类型枚举约定

本期不新增：

- FAQ 主配置表
- FAQ 向量表
- FAQ 命中历史独立大表

## 15. 测试设计

### 15.1 单元测试

至少覆盖：

1. 标准化逻辑
2. FAQ 注册表查找
3. 小模型判定结果解析
4. 置信度阈值判断
5. Redis 命中与未命中逻辑
6. 缓存命中响应包装
7. fallback 主链路逻辑

### 15.2 集成测试

至少覆盖：

1. 非流式固定问答命中缓存
2. 非流式固定问答未命中走上游
3. 判定超时走上游
4. Redis 异常走上游
5. `stream=true` 直接走原链路
6. 同一 `platform_api_key_id` 命中缓存
7. 不同 `platform_api_key_id` 不共享缓存

### 15.3 观测验证

需要验证：

1. 命中请求 `cache_hit=true`
2. 命中请求仍产生正常费用与 token 统计
3. 未命中请求 `cache_hit=false`
4. 异常事件流可看见 `classifier_miss` / `fallback_upstream`

## 16. 分期建议

### 16.1 一期

本期只做：

- `qwen-mt-flash` 判定
- 平台内置 FAQ 白名单
- API Key 级缓存隔离
- 非流式命中返回
- 观测字段补齐

### 16.2 二期

可扩展：

- admin 页面维护 FAQ
- 按租户维护 FAQ
- 更细粒度统计缓存节省金额
- 根据历史命中自动优化 FAQ 白名单

### 16.3 三期 / TODO

后续再评估：

- embedding + 向量语义缓存
- 相似度检索
- FAQ 半自动学习与回写
- 流式缓存返回
- 用户级缓存策略

## 17. 实施建议

建议按以下顺序落地：

1. 先补 FAQ 注册表与判定服务接口
2. 再把判定链路接入现有非流式 chat proxy
3. 再接 Redis 缓存命中逻辑
4. 最后补观测字段、明细展示和测试

这样可以保证每一步都可回退、可验证，不会一次性把主链路改得过重。
