# AI Gateway

一个面向企业与团队的租户制 AI API 接入平台。它把账号申请、租户开通、平台 API Key 分发、调用治理、审计追踪和统一接口接入放在同一套系统里，对外只暴露平台能力，不暴露真实上游凭据。

## 产品定位

平台当前主线是：

`账号申请 -> admin 审批 -> 开通租户 -> member 自助创建平台 API Key -> 调用统一接口 -> 按租户统计 token / 请求 / 失败 -> 输出审计记录`

当前默认模型分层是：

- 快模型档位：Qwen（`qwen-flash`）
- 强模型档位：MIMO（`mimo-v2.5-pro`）
- 向量能力：仍由 Qwen（`text-embedding-v4`）承担

平台对外提供的是统一入口：

- admin 审批账号申请并开通租户
- member 审批通过后自助创建、轮换、停用平台 API Key
- 平台按租户记录请求量、Token、失败率与审计事件
- 平台隐藏真实上游 provider 凭据，仅暴露统一平台接口

## 服务组成

仓库当前包含以下服务与基础设施：

- `gateway`
  - Go 编写的核心网关
  - 负责统一鉴权、租户隔离、平台 API Key 生命周期、调用记录与审计接口
- `web`
  - React 编写的中文控制台
  - 负责 admin/member 视角下的总览、账号申请、租户管理、API 密钥、调用观测与审计页面
- `internal-search`
  - Python 编写的内部检索支撑服务
  - 仅在容器网络内部供网关调用，不对外暴露独立入口
- `model-service`
  - 模型服务占位层

Compose 编排还会启动：

- PostgreSQL：平台业务数据、调用记录、审计数据
- Redis：缓存与限流依赖
- RabbitMQ：异步消息

## 目录结构

```text
ai_gateway/
├── deploy/         # Compose 与部署配置
├── docs/           # 设计文档
├── gateway/        # Go 网关
├── model-service/  # 模型服务占位层
├── rag-service/    # 内部检索支撑服务实现
├── scripts/        # 测试、lint、验证脚本
└── web/            # React 控制台
```

## 快速开始

### 1. 准备环境变量

```bash
cd ai_gateway
cp deploy/compose/.env.example deploy/compose/.env.local
```

按需编辑 `deploy/compose/.env.local`，填入本地账号、密码与端口配置。

安全要求：

- 不要直接使用 `.env.example` 中的 `change-me-*` 示例值启动服务。
- 数据库模式下，`gateway` 会拒绝空的控制台服务认证、空的控制台会话密钥，以及未替换的示例控制台密码。
- Compose 默认只把宿主机端口绑定到 `127.0.0.1`；如需公网访问，请通过受控的反向代理、VPN 或 frp 隧道暴露必要入口，不要直接暴露数据库、Redis 或 RabbitMQ。

### 模型计价配置

Token 计价由 `gateway` 进程读取环境变量，单位是“`微元 / 百万 Token`”：

- `2_000_000` 表示 `2.00 元 / 1M tokens`
- `20_000_000` 表示 `20.00 元 / 1M tokens`
- `500_000` 表示 `0.50 元 / 1M tokens`

当前代码中的实际变量名与默认值如下：

- `GATEWAY_MODEL_TOKEN_PRICING_DEFAULT_INPUT_MICROYUAN_PER_MILLION=2000000`
- `GATEWAY_MODEL_TOKEN_PRICING_DEFAULT_OUTPUT_MICROYUAN_PER_MILLION=20000000`
- `GATEWAY_MODEL_TOKEN_PRICING_DEFAULT_CACHED_MICROYUAN_PER_MILLION=500000`
- `GATEWAY_MODEL_TOKEN_PRICING_QWEN_FLASH_INPUT_MICROYUAN_PER_MILLION`
- `GATEWAY_MODEL_TOKEN_PRICING_QWEN_FLASH_OUTPUT_MICROYUAN_PER_MILLION`
- `GATEWAY_MODEL_TOKEN_PRICING_QWEN_FLASH_CACHED_MICROYUAN_PER_MILLION`

其中 `qwen-flash` 的三项在未显式设置时会回退到 `default` 对应值。

示例：

```dotenv
GATEWAY_MODEL_TOKEN_PRICING_DEFAULT_INPUT_MICROYUAN_PER_MILLION=2000000
GATEWAY_MODEL_TOKEN_PRICING_DEFAULT_OUTPUT_MICROYUAN_PER_MILLION=20000000
GATEWAY_MODEL_TOKEN_PRICING_DEFAULT_CACHED_MICROYUAN_PER_MILLION=500000

# 可选；留空时 qwen-flash 跟随 default
GATEWAY_MODEL_TOKEN_PRICING_QWEN_FLASH_INPUT_MICROYUAN_PER_MILLION=
GATEWAY_MODEL_TOKEN_PRICING_QWEN_FLASH_OUTPUT_MICROYUAN_PER_MILLION=
GATEWAY_MODEL_TOKEN_PRICING_QWEN_FLASH_CACHED_MICROYUAN_PER_MILLION=
```

如果你不是直接运行 `gateway` 二进制，而是走容器编排，请确认这些变量已经被实际注入到 `gateway` 容器环境；仅写入宿主机 `.env.local` 但未透传到容器时，网关进程不会看到这些值。

### 智能路由配置

`POST /v1/chat/completions` 已支持首版规则智能路由。当前由 `gateway` 进程读取以下配置：

- `GATEWAY_CHAT_FAST_MODEL`
- `GATEWAY_CHAT_REASONING_MODEL`
- `GATEWAY_SMART_ROUTING_CODING_KEYWORDS`
- `GATEWAY_SMART_ROUTING_LONG_PROMPT_THRESHOLD`

示例：

```dotenv
GATEWAY_CHAT_FAST_MODEL=qwen-flash
GATEWAY_CHAT_REASONING_MODEL=qwen-flash
GATEWAY_SMART_ROUTING_CODING_KEYWORDS=写代码,实现,重构,debug,报错,异常,单元测试,架构设计
GATEWAY_SMART_ROUTING_LONG_PROMPT_THRESHOLD=240
```

说明：

- 默认部署值把 `fast` 和 `reasoning` 都指向 `qwen-flash`，优先保证当前 DashScope key 可直接上线
- 如果你的账号已开通更强模型，例如 `qwen-plus`，只需要把 `GATEWAY_CHAT_REASONING_MODEL` 改成对应模型即可，无需改代码

当前策略只对 `chat/completions` 生效：

- 普通问答默认走快模型档位
- 命中编码关键词、代码块、报错/堆栈等信号时切到强模型档位
- 调用观测与审计会记录 `task_class`、`target_model_tier`、`routing_reason`、`resolved_model`

当前默认部署值：

- `GATEWAY_CHAT_FAST_MODEL=qwen-flash`
- `GATEWAY_CHAT_REASONING_MODEL=mimo-v2.5-pro`
- `GATEWAY_SEED_PROVIDER=dashscope`
- `GATEWAY_MIMO_PROVIDER_BASE_URL=https://api.xiaomimimo.com/v1`

### 2. 准备 secrets 目录

```bash
mkdir -p "${HOME}/.ai_gateway_secrets"
```

至少需要准备以下文件：

- `dashscope_api_key`
- `mimo_api_key`
- `gateway_seed_platform_api_key`
- `provider_master_key`

示例命令：

```bash
printf 'your-dashscope-api-key\n' > "${HOME}/.ai_gateway_secrets/dashscope_api_key"
printf 'your-mimo-api-key\n' > "${HOME}/.ai_gateway_secrets/mimo_api_key"
printf 'your-seed-platform-api-key\n' > "${HOME}/.ai_gateway_secrets/gateway_seed_platform_api_key"
openssl rand -base64 32 > "${HOME}/.ai_gateway_secrets/provider_master_key"
```

### 3. 用 Compose 启动

```bash
docker compose --env-file deploy/compose/.env.local -f deploy/compose/compose.yml up -d --build
```

如果你调整过 `.env.local` 里的 PostgreSQL、RabbitMQ 或 Redis 账号密码，首次重启前建议清理旧数据卷后再拉起，否则旧卷会继续保留历史鉴权信息：

```bash
docker compose --env-file deploy/compose/.env.local -f deploy/compose/compose.yml down -v
docker compose --env-file deploy/compose/.env.local -f deploy/compose/compose.yml up -d --build
```

### 4. 访问入口

默认端口如下：

- 控制台前端：`http://127.0.0.1:31873`
- 网关管理接口：`http://127.0.0.1:32658`
- PostgreSQL：`127.0.0.1:32143`
- Redis：`127.0.0.1:31879`
- RabbitMQ AMQP：`127.0.0.1:32361`
- RabbitMQ 管理台：`http://127.0.0.1:32704`

## 公开页 SEO / GEO

公开站点当前已提供以下可抓取资产：

- `GET /robots.txt`
- `GET /sitemap.xml`
- `GET /llms.txt`

公开页面 `GET /login` 与 `GET /apply` 已补齐页面标题、描述、canonical 与 AI 可读摘要，控制台登录后页面仍不作为本轮抓取目标。

## 部署后验证

### 查看容器状态

```bash
docker compose --env-file deploy/compose/.env.local -f deploy/compose/compose.yml ps
```

### 健康检查

```bash
curl http://127.0.0.1:32658/healthz
```

### 公开页与抓取资产检查

```bash
curl -I http://127.0.0.1:31873/login
curl http://127.0.0.1:31873/robots.txt
curl http://127.0.0.1:31873/sitemap.xml
curl http://127.0.0.1:31873/llms.txt
```

内部检索支撑服务不映射宿主机端口，如需排查可使用：

```bash
docker compose --env-file deploy/compose/.env.local -f deploy/compose/compose.yml ps
docker compose --env-file deploy/compose/.env.local -f deploy/compose/compose.yml logs --tail=50 internal-search
```

### 管理接口检查

管理接口现在同时要求：

- 网关 Basic Auth：`GATEWAY_SERVICE_AUTH_USERNAME` / `GATEWAY_SERVICE_AUTH_PASSWORD`
- 控制台会话：先用控制台账号密码调用登录接口拿到 `token`

示例：

```bash
SESSION_TOKEN=$(curl -s -X POST http://127.0.0.1:32658/console/session/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"<ADMIN_CONSOLE_EMAIL>","password":"<GATEWAY_CONSOLE_ADMIN_PASSWORD>"}' | jq -r '.token')

curl -u <GATEWAY_SERVICE_AUTH_USERNAME>:<GATEWAY_SERVICE_AUTH_PASSWORD> \
  -H "X-Console-Session: ${SESSION_TOKEN}" \
  http://127.0.0.1:32658/admin/system/status

curl -u <GATEWAY_SERVICE_AUTH_USERNAME>:<GATEWAY_SERVICE_AUTH_PASSWORD> \
  -H "X-Console-Session: ${SESSION_TOKEN}" \
  http://127.0.0.1:32658/admin/api-keys
```

### 智能路由验证

准备一个平台 API Key 后，可以直接用复杂编码请求验证是否命中强模型档位：

```bash
curl -sS http://127.0.0.1:32658/v1/chat/completions \
  -H "Authorization: Bearer <platform-key>" \
  -H "Content-Type: application/json" \
  --data '{"model":"qwen-flash","messages":[{"role":"user","content":"请帮我 debug 这段 panic 代码 ```go\npanic(\"x\")\n```"}]}'
```

期望结果：

- 请求可正常返回
- 网关会按规则把请求切到强模型档位
- `GET /admin/usage/requests` 与 `GET /admin/audit` 中能看到：
  - `task_class=coding_complex`
  - `target_model_tier=gateway-chat-reasoning`
  - 非空 `routing_reason`
  - 最终 `resolved_model`

### 费用展示与验证

完成一次真实模型调用后，可直接检查网关返回的费用相关字段。

admin 侧：

```bash
curl -s -u <GATEWAY_SERVICE_AUTH_USERNAME>:<GATEWAY_SERVICE_AUTH_PASSWORD> \
  -H "X-Console-Session: ${SESSION_TOKEN}" \
  http://127.0.0.1:32658/admin/usage/overview | jq '{input_tokens,output_tokens,cached_tokens,input_cost,output_cost,cached_cost,total_cost,pricing_models}'

curl -s -u <GATEWAY_SERVICE_AUTH_USERNAME>:<GATEWAY_SERVICE_AUTH_PASSWORD> \
  -H "X-Console-Session: ${SESSION_TOKEN}" \
  http://127.0.0.1:32658/admin/usage/trends | jq '{costs}'

curl -s -u <GATEWAY_SERVICE_AUTH_USERNAME>:<GATEWAY_SERVICE_AUTH_PASSWORD> \
  -H "X-Console-Session: ${SESSION_TOKEN}" \
  http://127.0.0.1:32658/admin/usage/requests | jq '{items: [.items[] | {request_id,model,input_tokens,output_tokens,cached_tokens,input_cost,output_cost,cached_cost,total_cost,input_price,output_price,cached_price}]}'

curl -s -u <GATEWAY_SERVICE_AUTH_USERNAME>:<GATEWAY_SERVICE_AUTH_PASSWORD> \
  -H "X-Console-Session: ${SESSION_TOKEN}" \
  http://127.0.0.1:32658/admin/audit | jq '{items: [.items[] | {time,request_model,upstream_model,total_cost,status}]}'
```

member 侧使用同一组 Basic Auth，并改为 member 登录拿到 `X-Console-Session`，检查：

- `GET /me/usage/overview`
- `GET /me/usage/requests`

当前已实现的展示口径是：

- `overview` 返回三类 Token、三类费用、总费用，以及 `pricing_models`
- `trends` 返回 `costs`
- `requests` 返回每次请求的 Token 分类、费用和单价快照
- `audit` 当前只返回 `total_cost`，不返回三类 Token 明细

### 前端登录

前端 `31873` 不再使用浏览器 Basic Auth，而是直接打开应用内登录页。登录邮箱和密码以实际部署配置为准，不要把真实账号或密码写入文档：

- 管理员邮箱：`<ADMIN_CONSOLE_EMAIL>`
- 普通用户邮箱：`<MEMBER_CONSOLE_EMAIL>`
- 对应密码：`GATEWAY_CONSOLE_ADMIN_PASSWORD`、`GATEWAY_CONSOLE_MEMBER_PASSWORD`

### 公开申请注册

未登录用户现在可以直接访问 `/apply` 发起接入申请，流程为：

1. 获取本地图形验证码
2. 输入邮箱、姓名、公司、用途、密码、确认密码
3. 先验证验证码
4. 验证通过后提交申请，状态进入 `pending`
5. admin 审批通过后，若填写的是新 `tenant_id`，系统会自动创建租户并绑定该用户
6. 用户可直接使用注册时设置的密码登录控制台

## 质量命令

```bash
./scripts/test.sh
./scripts/lint.sh
docker compose --env-file deploy/compose/.env.example -f deploy/compose/compose.yml config
```

其中：

- `./scripts/test.sh` 运行 gateway、内部检索支撑服务、web 的测试，并执行产品边界扫描与 Compose 配置校验
- `./scripts/lint.sh` 运行 Go 格式检查、Go vet、Python 编译检查、Web 构建检查，以及同一组边界/部署验证

## 安全说明

- 仓库中不应提交真实 API Key、真实控制台账号密码、真实数据库密码、真实 Redis ACL 文件
- `deploy/compose/.env.example` 只提供占位符，不提供真实值
- 平台只向租户用户暴露平台 API Key，不向前台返回真实上游凭据
- 真实上游密钥通过文件挂载或环境变量注入，由平台内部统一管理
