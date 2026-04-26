# AI Gateway

一个面向多模型接入场景的 AI 网关项目。它把统一鉴权、API Key 生命周期、模型路由、调用观测、审计追踪和 RAG 服务整合到同一套系统里，目标不是只做一个“AI 页面”，而是做一个可以真实部署上线的平台型网关。

## 项目做了什么

这个仓库当前包含 3 个核心服务和一套可直接启动的部署方案：

- `gateway`
  - 使用 Go 编写的核心网关
  - 提供统一入口、模型路由、平台 API Key 管理、管理接口、调用记录与审计能力
- `rag-service`
  - 使用 Python 编写的 RAG 后端服务
  - 负责文档切片、检索、问答与内部知识库能力
- `web`
  - 使用 React 编写的中文控制台
  - 提供总览、API 密钥、路由、调试场、调用观测、审计等页面

配套基础设施通过 Docker Compose 编排：

- PostgreSQL：业务数据、调用记录、知识库数据
- Redis：缓存与限流依赖
- RabbitMQ：异步消息
- model-service：模型服务占位层

## 核心能力

- 统一平台 API Key 鉴权，而不是把上游模型厂商凭据直接暴露给用户
- 统一模型名到上游供应商模型的映射与路由
- 控制台支持 API Key 新建、轮换、停用、删除
- 调用观测页面支持总调用、成功率、Token、延时健康墙、失败分类、调用明细
- 审计页面支持最近事件流和调用追踪
- RAG 后端服务已独立拆分，便于后续继续扩展文档检索与问答链路
- 所有真实密钥通过外部文件或环境变量注入，不要求把敏感值写进仓库

## 架构概览

```text
                +----------------------+
                |        Web 控制台     |
                |  React + Nginx + 鉴权 |
                +----------+-----------+
                           |
                           v
+--------------------------+--------------------------+
|                         Gateway                      |
| Go / REST API / API Key / 路由 / 审计 / 调用观测     |
+------------+------------------+---------------------+
             |                  |                     |
             v                  v                     v
      +-------------+    +-------------+      +---------------+
      | PostgreSQL  |    |   Redis     |      |   RabbitMQ    |
      +-------------+    +-------------+      +---------------+
             |
             v
      +-------------+            +---------------+
      | RAG Service |<---------->| Model Service |
      +-------------+            +---------------+
```

## 目录结构

```text
ai_gateway/
├── deploy/         # Compose 与部署相关配置
├── docs/           # 可提交的 specs / plans
├── gateway/        # Go 网关服务
├── model-service/  # 模型服务占位层
├── rag-service/    # Python RAG 服务
├── scripts/        # 测试与 lint 脚本
└── web/            # React 控制台
```

## 快速开始

### 1. 准备环境变量文件

```bash
cd ai_gateway
cp deploy/compose/.env.example deploy/compose/.env.local
```

然后编辑 `deploy/compose/.env.local`，填入你自己的示例账号和密码。

### 2. 准备本地 secrets 目录

```bash
mkdir -p "${HOME}/.ai_gateway_secrets"
```

至少需要准备以下文件：

- `dashscope_api_key`
  - 上游模型供应商 API Key
- `gateway_seed_platform_api_key`
  - 网关初始化用的平台 API Key
- `provider_master_key`
  - 供应商密钥加密主密钥
  - 要求：32 字节原始字符串，或 base64 编码后的 32 字节内容
- `redis.users.acl`
  - Redis ACL 配置
- `web_console.htpasswd`
  - Web 控制台基础鉴权文件

示例命令：

```bash
printf 'your-dashscope-api-key\n' > "${HOME}/.ai_gateway_secrets/dashscope_api_key"
printf 'your-seed-platform-api-key\n' > "${HOME}/.ai_gateway_secrets/gateway_seed_platform_api_key"
openssl rand -base64 32 > "${HOME}/.ai_gateway_secrets/provider_master_key"
cp deploy/redis/users.acl.example "${HOME}/.ai_gateway_secrets/redis.users.acl"
printf "example-console-user:$(openssl passwd -apr1 'change-me-console-password')\n" > "${HOME}/.ai_gateway_secrets/web_console.htpasswd"
```

如果你修改了 `.env.local` 里的 Redis 用户名或密码，需要同步修改 `redis.users.acl`。

### 3. 启动服务

```bash
docker compose --env-file deploy/compose/.env.local -f deploy/compose/compose.yml up -d --build
```

### 4. 访问入口

默认 Compose 暴露以下端口：

- 控制台前端：`http://127.0.0.1:31873`
- 网关管理接口：`http://127.0.0.1:32658`
- RAG 服务：`http://127.0.0.1:31427`
- PostgreSQL：`127.0.0.1:32143`
- Redis：`127.0.0.1:31879`
- RabbitMQ AMQP：`127.0.0.1:32361`
- RabbitMQ 管理台：`http://127.0.0.1:32704`

## 部署后如何验证

### 查看容器状态

```bash
docker compose --env-file deploy/compose/.env.local -f deploy/compose/compose.yml ps
```

### 健康检查

```bash
curl http://127.0.0.1:32658/healthz
curl http://127.0.0.1:31427/healthz
```

### 管理接口检查

管理接口使用 Basic Auth，用户名和密码来自 `deploy/compose/.env.local`：

```bash
curl -u <GATEWAY_SERVICE_AUTH_USERNAME>:<GATEWAY_SERVICE_AUTH_PASSWORD> \
  http://127.0.0.1:32658/admin/system/status

curl -u <GATEWAY_SERVICE_AUTH_USERNAME>:<GATEWAY_SERVICE_AUTH_PASSWORD> \
  http://127.0.0.1:32658/admin/api-keys
```

## 本地开发命令

```bash
make test
make lint
make dev-up
make dev-down
```

对应脚本：

- `make test`
  - 运行 Go、RAG、Web 测试
- `make lint`
  - 运行 Go 格式检查与 Web 构建检查
- `make dev-up`
  - 构建前端并使用 Compose 启动整套服务
- `make dev-down`
  - 停止 Compose 服务

## 文档导航

- 设计规格：`docs/specs/`
- 实施计划：`docs/plans/`

这些目录是当前仓库对外保留的可提交文档版本。  
`docs/superpowers/` 属于内部工作目录，已经被 `.gitignore` 排除。

## 安全说明

- 仓库中不应提交真实 API Key、真实控制台账号密码、真实数据库密码、真实 Redis ACL 和真实 htpasswd 文件
- `deploy/compose/.env.example` 只提供占位符，不提供真实值
- `web/.htpasswd`、`deploy/redis/users.acl`、`docs/superpowers/`、`.ai_gateway_secrets/` 默认不进入 Git
- 供应商密钥通过文件挂载注入，网关内部支持加密存储

## 适合展示的能力点

如果你把这个项目用于简历、面试或公开仓库展示，它比较适合体现这些能力：

- Go 后端服务开发
- RESTful API 设计与平台化能力
- API 网关与统一鉴权设计
- 多服务协作与前后端联动
- Docker Compose 部署与上线闭环
- LLM 网关、调用观测与 RAG 后端拆分
