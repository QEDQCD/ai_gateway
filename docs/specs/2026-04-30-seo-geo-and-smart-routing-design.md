# SEO/GEO 与智能模型路由设计

## 背景

当前 `ai_gateway` 已具备统一平台 API Key、租户治理、调用观测、审计与按模型计价能力，但还有两个明显缺口：

1. 对外公开页几乎没有可抓取的站点级元信息与 AI 可读摘要，`SEO/GEO` 价值没有被释放。
2. 网关当前仍主要依据请求里显式传入的 `model` 解析路由，缺少“简单任务走快模型、复杂编码走强模型”的策略能力。

同时，系统虽然已经有“路由健康”“失败记录”等观测页面，但尚未形成真正的模型降级 / 熔断执行链路。

## 本轮目标

本轮只做两件事，并控制范围：

1. 为公开页补齐最小可上线的 `SEO/GEO` 能力。
2. 为 `POST /v1/chat/completions` 增加首版规则驱动的智能模型路由能力。

## 本轮不做

以下能力只在设计和数据结构中预留，不作为本轮必交付运行逻辑：

1. 小模型预分类。
2. 基于失败阈值的自动熔断与自动降级执行。
3. 租户自定义路由策略。
4. 在 member 侧暴露真实内部强弱模型或真实上游 provider 细节。
5. 全站 SSR 或完整搜索落地页体系。

## 用户已确认的约束

### 1. 智能路由优先级

本轮主线是智能路由，`SEO/GEO` 只做最小可上线版。

### 2. 分类方式

本轮采用纯规则分类，不引入小模型分类器。

### 3. SEO/GEO 范围

只优化公开可访问页面：

- `/login`
- `/apply`
- 站点级静态抓取资产

控制台登录后页面不作为本轮公开抓取目标。

### 4. 熔断/降级

不是本轮必做项，只做设计预留。

## 方案对比

### 方案 A：最小功能版

- `SEO/GEO` 只加 `title/description/robots`
- 智能路由只做简单关键词切换
- 不加观测字段

优点：

- 改动最小
- 上线最快

缺点：

- 不可观测
- 很难证明平台具备真实路由能力

### 方案 B：能力优先版

- `SEO/GEO` 加基础元信息、FAQ、AI 摘要页
- 智能路由加规则分类、内部模型档位、审计记录
- 控制台可看到命中结果

优点：

- 平台能力完整
- 可解释、可观测

缺点：

- 改动面比方案 A 大

### 方案 C：主线能力版

- 采用方案 B
- 但熔断/降级只预留设计，不在本轮执行

优点：

- 能体现网关的真实路由价值
- 风险可控，不把容灾与路由一轮耦合

缺点：

- 需要补一点后端观测字段和控制台展示

## 结论

采用 **方案 C**。

原因：

1. 公开页 `SEO/GEO` 可以低风险补齐。
2. 智能路由是平台差异化能力，必须在本轮真正落到请求链路里。
3. 熔断/降级如果跟首版规则路由一起上，会显著放大调试与回滚成本。

## 总体架构

### 1. 公开页 SEO/GEO

为 `web` 公开页补齐三层信息：

1. 站点级：
   - `title`
   - `meta description`
   - `canonical`
   - Open Graph
   - Twitter Card
2. 页面级：
   - `/login` 的页面标题与摘要
   - `/apply` 的页面标题与摘要
3. 静态抓取资产：
   - `robots.txt`
   - `sitemap.xml`
   - `llms.txt` 或等价 AI 摘要文件

### 2. 智能路由

仅改造 `POST /v1/chat/completions`：

1. 解析请求中的 `messages`
2. 规则分类器判断任务类型
3. 输出内部目标模型档位
4. 用目标模型档位驱动 `RouteService.Resolve`
5. 记录分类结果、路由原因与实际模型
6. 转发到最终上游模型

### 3. 熔断/降级预留

本轮只预留：

1. 模型失败计数窗口概念
2. 模型健康阈值配置概念
3. 未来降级去向概念

不在本轮拦截真实请求。

## 公开页 SEO/GEO 设计

## 1. 目标页面

本轮只覆盖：

- `/login`
- `/apply`
- 根站点静态资产

原因：

1. 这两个页面天然可公开访问。
2. 控制台内页是登录后应用，对外抓取价值低。
3. 当前前端是 SPA，本轮不应假装实现了完整营销站。

## 2. 页面语义增强

### `/login`

目标表达：

- 这是 AI Gateway 控制台登录入口
- 平台提供统一 API Key 分发、租户治理、调用审计、Token 观测
- 平台屏蔽真实上游模型细节

页面结构要求：

- 一个明确的 `h1`
- 一段 1 到 2 句的平台摘要
- 一组简短能力标签

### `/apply`

目标表达：

- 这是 AI Gateway 接入申请入口
- 用户可以提交企业或团队接入申请
- 审批通过后可获得平台级接入能力

页面结构要求：

- 一个明确的 `h1`
- 一段说明审批流程的摘要
- 一段说明适用对象或接入价值的说明

## 3. 元信息要求

### 站点级

至少包含：

- `title`
- `meta[name="description"]`
- `meta[name="robots"]`
- `link[rel="canonical"]`
- `meta[property="og:title"]`
- `meta[property="og:description"]`
- `meta[property="og:type"]`

### 页面级

公开页应根据当前路由更新：

- 页面标题
- 页面描述
- canonical

如果没有引入完整 head 管理库，也可以使用轻量级自定义 hook，在路由页面挂载时直接更新 `document.title` 与对应 meta 节点。

## 4. GEO 设计

本轮 `GEO` 目标不是“排名技巧”，而是让搜索引擎与大模型系统更容易正确理解站点。

因此公开页应增加可被直接抽取的结构化文本：

1. 平台是什么
2. 解决什么问题
3. 适合谁
4. 提供哪些能力

建议额外增加一组简短 FAQ 文本块，例如：

- 什么是 AI Gateway
- 平台 API Key 与上游模型 API Key 有什么区别
- 如何申请接入
- 平台是否隐藏真实上游模型凭据

FAQ 可以只做页面结构化文本，也可以同步输出 JSON-LD。

## 5. 静态抓取资产

### `robots.txt`

要求：

- 允许抓取公开页面
- 不主动鼓励抓取登录后控制台路径

### `sitemap.xml`

只列出：

- `/login`
- `/apply`

### `llms.txt`

提供一份面向 AI 抓取系统的简短文本摘要，内容包括：

- 平台定位
- 核心能力
- 公开入口
- 不公开暴露上游模型凭据

## 智能路由设计

## 1. 路由目标

本轮不是让用户显式选择内部强弱模型，而是让平台自动选择执行档位。

对外接口保持不变：

- `POST /v1/chat/completions`

对内新增两个内部语义档位：

- `gateway-chat-fast`
- `gateway-chat-reasoning`

建议默认映射：

- `gateway-chat-fast -> qwen-flash`
- `gateway-chat-reasoning -> 强能力聊天模型`

具体强模型名不在前台暴露，由服务端配置决定。

## 2. 插入位置

当前请求链路是：

1. `RequirePlatformAPIKey`
2. 从请求中解析 `model`
3. `AuthService.Resolve`
4. `RouteService.Resolve`
5. `ChatProxyService`

本轮改为：

1. 解析 `model + messages`
2. 规则分类器输出 `task_class + target_model_tier`
3. `AuthService.Resolve` 使用 `target_model_tier` 而非原始用户模型
4. `RouteService.Resolve` 继续负责根据目标模型档位选择真实 provider route
5. `ChatProxyService` 透传到上游

这样职责边界清晰：

- 分类器只判断任务类型
- 路由服务只做模型到 provider 的映射
- 代理服务只负责调用上游

## 3. 任务分类

### 分类结果

首版只定义三类：

- `simple_qa`
- `coding_complex`
- `generic`

实际路由上：

- `coding_complex -> gateway-chat-reasoning`
- `simple_qa -> gateway-chat-fast`
- `generic -> gateway-chat-fast`

### 复杂编码任务高置信规则

满足任意一组高置信规则即可判为 `coding_complex`：

1. 命中编码或工程关键词：
   - `写代码`
   - `实现`
   - `重构`
   - `修复 bug`
   - `debug`
   - `报错`
   - `异常`
   - `单元测试`
   - `SQL 优化`
   - `架构设计`
2. 包含明显代码块或代码片段：
   - `` ``` ``
   - `func `
   - `class `
   - `package `
   - `import `
3. 包含明显报错堆栈或异常模式：
   - `Traceback`
   - `Exception`
   - `panic:`
   - `SyntaxError`
   - `stack trace`
4. 文本较长，且同时出现工程语义与代码语义

### 简单问答

以下情况优先视为 `simple_qa`：

- 常规知识问答
- 简短翻译
- 普通解释
- 短摘要
- 通用产品咨询

### 输入范围

分类器不只看最后一条 message，而是聚合当前 messages 全量文本。

原因：

1. 多轮对话里，编码上下文可能在前几轮。
2. 只看最后一句容易误判“继续”“帮我改下”这类上下文依赖请求。

## 4. 用户显式 model 的处理

首版采用：

- **平台策略优先**

这意味着：

1. 用户即使传入了某个真实模型名，平台仍可根据规则改写到内部目标档位。
2. member 默认不感知内部实际强弱模型切换。

如果未来需要更细粒度控制，可增加：

- 租户级“关闭智能路由，按显式模型直通”的开关

但本轮不做。

## 5. 路由可解释性

规则分类器必须输出至少以下信息：

- `task_class`
- `matched_rules`
- `target_model_tier`

示例：

```json
{
  "task_class": "coding_complex",
  "matched_rules": ["keyword:debug", "pattern:code_block", "pattern:stack_trace"],
  "target_model_tier": "gateway-chat-reasoning"
}
```

## 数据模型与观测

## 1. 请求日志

建议在 `llm_request_logs` 扩展以下字段：

- `task_class`
- `routing_reason`
- `target_model_tier`
- `resolved_model`

字段含义：

- `task_class`：分类结果，如 `simple_qa`、`coding_complex`
- `routing_reason`：命中规则摘要，优先存短字符串或 JSON 文本
- `target_model_tier`：内部目标档位，如 `gateway-chat-fast`
- `resolved_model`：最终打到上游时的真实模型名

如果本轮为了控制改动面，可以先只在 `llm_request_logs` 落这些字段，不急于同步到所有聚合表。

## 2. 审计与调用观测

admin 侧建议新增以下展示字段：

- 任务分类
- 路由档位
- 路由原因
- 实际模型

member 侧建议默认收敛为：

- 平台已自动选择执行策略
- 可展示“任务类型”与“平台策略”
- 不强调真实上游模型供应商与内部路由标签

## 3. 聚合统计

如果本轮要做轻聚合，建议至少支持：

- `simple_qa` vs `coding_complex` 请求数
- 强模型请求占比
- 最近命中的路由原因样例

这些可以先通过查询最近请求日志得到，不要求本轮新建复杂聚合表。

## 配置设计

## 1. 模型档位映射

后端新增静态配置：

- 快模型真实目标
- 强模型真实目标

示例语义：

- `GATEWAY_CHAT_FAST_MODEL`
- `GATEWAY_CHAT_REASONING_MODEL`

## 2. 规则阈值

首版建议配置化但不做后台管理：

- 复杂任务关键词列表
- 长度阈值
- 是否启用代码块检测
- 是否启用堆栈检测

## 3. 熔断预留配置

本轮只预留概念，不执行：

- `model_failure_threshold`
- `model_failure_window`
- `fallback_model_tier`

## 控制台设计影响

## 1. Admin

建议在 `调用观测` 页面增加：

- 智能路由命中概况卡片
- 明细表中的任务分类 / 路由档位 / 路由原因列

建议在 `审计` 页面增加更有意义的中文描述，例如：

- `检测到复杂编码请求，已路由至高能力模型`
- `检测到普通问答请求，已路由至经济模型`

## 2. Member

member 页只保留平台级表达：

- `普通问答`
- `复杂任务`
- `平台自动选择策略`

不暴露：

- 真实 provider 名称
- 内部 provider credential
- 平台内部强模型标识细节

## 测试策略

## 1. 单元测试

必须覆盖：

1. 简单问答命中快模型
2. 编码关键词命中强模型
3. 代码块命中强模型
4. 报错堆栈命中强模型
5. 空输入与边界输入回退默认快模型

## 2. 网关集成测试

必须覆盖：

1. 发起 `chat/completions` 请求
2. 断言最终上游请求使用的是预期真实模型
3. 断言 usage / audit 能看到分类结果与路由原因

## 3. 前端测试

必须覆盖：

1. `/login`、`/apply` 的标题和关键信息存在
2. `robots.txt`、`sitemap.xml`、`llms.txt` 存在
3. admin 路由观测页面能展示新增智能路由字段

## 上线顺序

建议按两步推进：

### 第一步

1. 公开页 `SEO/GEO`
2. 规则分类器
3. 内部模型档位映射
4. 路由观测与审计字段

### 第二步

1. 模型失败窗口统计
2. 真实熔断 / 降级执行
3. 更细的模型健康可视化
4. 可选的小模型边界判定

## 风险与缓解

### 1. 规则误判

风险：

- 普通请求误进强模型
- 编码请求漏判到快模型

缓解：

- 首版只对高置信编码信号切强模型
- 记录命中率与规则原因，便于回放调参

### 2. 强模型成本抬升

风险：

- 复杂编码任务占比高时，整体成本上升

缓解：

- 控制台展示强模型请求占比
- 后续按租户或路由策略继续细分

### 3. 用户显式模型与平台策略冲突

风险：

- 用户以为自己指定了某模型，但平台实际改写了

缓解：

- 首版在文档中明确平台策略优先
- member 页只表达“平台自动选择执行策略”

### 4. SPA 的抓取限制

风险：

- 仅靠前端 SPA，抓取效果有限

缓解：

- 本轮只对公开页与静态资产做最小增强
- 不承诺完整 SSR

## 验收标准

本轮完成后，至少满足：

1. `/login` 与 `/apply` 拥有明确的页面标题、描述与可抓取静态资产。
2. `POST /v1/chat/completions` 可以根据规则自动把复杂编码任务路由到强模型档位。
3. admin 可以在调用观测或审计里看到分类结果与路由原因。
4. member 仍然只看到平台级统一接口能力，不看到内部 provider 细节。
5. 熔断/降级能力被明确记录为下一阶段扩展，而不是半实现状态。

## 实施收口说明

截至本轮收口，设计已对应落地到以下结果：

1. 公开页已补齐：
   - `/login`
   - `/apply`
   - `/robots.txt`
   - `/sitemap.xml`
   - `/llms.txt`
2. `chat/completions` 已在请求链路前半段完成规则分类，再按内部模型档位解析真实上游模型。
3. usage / audit 持久化并回传以下字段：
   - `task_class`
   - `routing_reason`
   - `target_model_tier`
   - `resolved_model`
4. 控制台展示已分层：
   - admin `调用观测` 展示任务分类、目标档位、路由原因、实际模型
   - admin `审计` 展示请求模型、实际模型、上游模型与路由字段
   - member `调用观测` 只展示任务类型、平台策略、实际模型，不暴露 provider 凭据
5. 当前已完成的最小验证口径：
   - 前端路由测试通过
   - Web 构建通过
   - README 已补充公开资产检查与智能路由验证命令

## 验证建议

建议按以下顺序验证整轮能力：

1. 公开页：
   - `curl -I http://127.0.0.1:31873/login`
   - `curl http://127.0.0.1:31873/robots.txt`
   - `curl http://127.0.0.1:31873/sitemap.xml`
   - `curl http://127.0.0.1:31873/llms.txt`
2. 网关健康：
   - `curl http://127.0.0.1:32658/healthz`
3. 智能路由：
   - 用复杂编码 prompt 调用 `POST /v1/chat/completions`
   - 再检查 `admin/usage/requests` 与 `admin/audit` 是否出现 `coding_complex`、`gateway-chat-reasoning` 和最终 `resolved_model`

## 后续扩展

下一阶段可以在本设计基础上继续扩展：

1. 小模型边界样本分类
2. 模型失败阈值熔断
3. 备用模型自动切换
4. 租户级路由策略开关
5. 更完整的公开站点与内容页体系
