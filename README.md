# AI Gateway

一个面向企业与团队的租户制 AI API 接入平台。它把账号申请、租户开通、平台 API Key 分发、调用治理、审计追踪和统一接口接入放在同一套系统里，对外只暴露平台能力，不暴露真实上游凭据。

## 产品定位

平台当前主线是：

`账号申请 -> admin 审批 -> 开通租户 -> member 自助创建平台 API Key -> 调用统一接口 -> 按租户统计 token / 请求 / 失败 -> 输出审计记录`

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

### 2. 准备 secrets 目录

```bash
mkdir -p "${HOME}/.ai_gateway_secrets"
```

至少需要准备以下文件：

- `dashscope_api_key`
- `gateway_seed_platform_api_key`
- `provider_master_key`

示例命令：

```bash
printf 'your-dashscope-api-key\n' > "${HOME}/.ai_gateway_secrets/dashscope_api_key"
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

## 部署后验证

### 查看容器状态

```bash
docker compose --env-file deploy/compose/.env.local -f deploy/compose/compose.yml ps
```

### 健康检查

```bash
curl http://127.0.0.1:32658/healthz
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
  -d '{"email":"admin@example.com","password":"<GATEWAY_CONSOLE_ADMIN_PASSWORD>"}' | jq -r '.token')

curl -u <GATEWAY_SERVICE_AUTH_USERNAME>:<GATEWAY_SERVICE_AUTH_PASSWORD> \
  -H "X-Console-Session: ${SESSION_TOKEN}" \
  http://127.0.0.1:32658/admin/system/status

curl -u <GATEWAY_SERVICE_AUTH_USERNAME>:<GATEWAY_SERVICE_AUTH_PASSWORD> \
  -H "X-Console-Session: ${SESSION_TOKEN}" \
  http://127.0.0.1:32658/admin/api-keys
```

### 前端登录

前端 `31873` 不再使用浏览器 Basic Auth，而是直接打开应用内登录页。登录账号密码同样来自 `deploy/compose/.env.local`：

- 管理员账号：固定使用 `admin@example.com`
- 普通用户账号：固定使用 `member-a@example.com`
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
