# AI Gateway MVP 实施计划

> **供代理式执行者使用：** 必选子技能：使用 `superpowers:subagent-driven-development`（推荐）或 `superpowers:executing-plans` 按任务逐项实现本计划。步骤使用复选框语法（`- [ ]`）进行跟踪。

**目标：** 构建一个可首次部署的 SaaS AI 网关版本，包含租户级平台 API Key、供应商凭据路由、React 管理控制台，以及作为示范 AI 业务的 Python RAG 服务。

**架构：** 仓库拆分为三个可运行应用：Go 网关负责平台控制面与请求处理面逻辑，Python FastAPI RAG 服务负责文档入库与检索问答，React + TypeScript 控制台负责租户侧操作。共享基础设施使用 PostgreSQL + pgvector、Redis、RabbitMQ、MinIO 和 Docker Compose；Kubernetes 清单作为可前向演进的部署层加入，但不作为首个发布目标。

**技术栈：** Go 1.22、Fiber、pgx、sqlc、testcontainers-go、Python 3.11、FastAPI、SQLAlchemy、pytest、React、TypeScript、Vite、React Router、TanStack Query、Playwright、PostgreSQL 16 + pgvector、Redis、RabbitMQ、MinIO、Prometheus、Grafana、Docker Compose。

---

## 文件结构

### 需要创建的仓库目录布局

- `gateway/`
  - `cmd/server/main.go` - 网关进程入口
  - `internal/config/config.go` - 基于环境变量的配置加载
  - `internal/http/router.go` - Fiber 路由注册
  - `internal/http/middleware/` - 认证、请求 ID、配额、日志
  - `internal/domain/` - 租户、API Key、供应商凭据、路由策略等类型
  - `internal/store/` - PostgreSQL 数据访问层
  - `internal/service/` - API Key 认证、路由解析、用量统计
  - `internal/provider/` - OpenAI 兼容接口与 DashScope 适配器
  - `internal/queue/` - RabbitMQ 异步用量/日志任务发布器
  - `internal/telemetry/` - Prometheus 指标与结构化日志
  - `db/migrations/` - SQL Schema 迁移文件
  - `db/query/` - sqlc 查询文件
  - `tests/integration/` - 网关集成测试

- `rag-service/`
  - `app/main.py` - FastAPI 入口
  - `app/core/config.py` - 配置
  - `app/api/routes.py` - 路由挂载
  - `app/models/` - SQLAlchemy 模型
  - `app/services/` - 文档导入、切片、检索、答案生成
  - `app/tasks/` - RabbitMQ 消费者与后台任务
  - `tests/` - 单元测试与 API 测试

- `web/`
  - `src/main.tsx` - React 启动入口
  - `src/app/router.tsx` - 路由树
  - `src/pages/` - dashboard、api keys、routes、playground、knowledge base、audit 页面
  - `src/features/` - 按功能拆分的 api/hooks/components
  - `src/lib/api.ts` - HTTP 客户端
  - `src/test/` - Vitest 配置
  - `e2e/` - Playwright 测试

- `deploy/`
  - `compose/compose.yml` - 本地与单机部署栈
  - `compose/.env.example` - 部署环境变量模板
  - `kubernetes/` - 后续 Deployment/Service/Ingress/Secret 清单
  - `grafana/` - dashboard JSON
  - `prometheus/prometheus.yml` - 抓取配置

- `scripts/`
  - `bootstrap.sh` - 初始开发环境准备
  - `test.sh` - 运行仓库测试套件
  - `lint.sh` - 运行仓库静态检查
  - `seed-demo.sh` - 初始化演示租户、演示 API Key、演示知识库

- `docs/`
  - `superpowers/specs/2026-04-22-ai-gateway-design.md` - 已确认的设计文档
  - `superpowers/plans/2026-04-22-ai-gateway-mvp.md` - 当前实施计划
  - `runbooks/local-dev.md` - 本地启动指南
  - `runbooks/deploy-compose.md` - 单机部署指南
  - `runbooks/troubleshooting.md` - 运维排障手册

## 任务 1：初始化 Monorepo 骨架

**文件：**
- 创建：`README.md`
- 创建：`.gitignore`
- 创建：`Makefile`
- 创建：`scripts/bootstrap.sh`
- 创建：`scripts/test.sh`
- 创建：`scripts/lint.sh`
- 创建：`gateway/go.mod`
- 创建：`rag-service/pyproject.toml`
- 创建：`web/package.json`
- 创建：`deploy/compose/.env.example`
- 测试：`README.md`

- [ ] **步骤 1：先写失败的仓库冒烟约定**

在顶层 `README.md` 中加入一个章节，定义预期的仓库命令和目录。使用下面这份骨架：

```md
# AI Gateway

## Services

- `gateway`: Go API gateway
- `rag-service`: Python RAG service
- `web`: React console

## Commands

- `make test`
- `make lint`
- `make dev-up`
```

- [ ] **步骤 2：验证当前仓库还无法满足这份约定**

运行：`test -f Makefile || echo "Makefile missing"`
预期：输出 `Makefile missing`

- [ ] **步骤 3：写入最小启动文件**

创建以下精确内容：

```makefile
.PHONY: test lint dev-up

test:
	./scripts/test.sh

lint:
	./scripts/lint.sh

dev-up:
	docker compose -f deploy/compose/compose.yml up -d
```

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "bootstrap placeholder"
```

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "repo tests not wired yet"
exit 1
```

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "repo linters not wired yet"
exit 1
```

```gitignore
.DS_Store
node_modules/
dist/
.venv/
__pycache__/
.pytest_cache/
.mypy_cache/
coverage/
*.log
.env
```

- [ ] **步骤 4：补上各服务的最小声明文件，让工具链能识别**

创建最小可用声明：

```go
module github.com/liwenjian/ai_gateway/gateway

go 1.22
```

```toml
[project]
name = "ai-gateway-rag-service"
version = "0.1.0"
requires-python = ">=3.11"
dependencies = []
```

```json
{
  "name": "ai-gateway-web",
  "version": "0.1.0",
  "private": true,
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "test": "vitest run"
  }
}
```

- [ ] **步骤 5：运行冒烟命令，证明骨架已存在**

运行：`make test`
预期：命令执行 `./scripts/test.sh`，并以 `repo tests not wired yet` 失败

- [ ] **步骤 6：提交**

```bash
git add README.md .gitignore Makefile scripts gateway/go.mod rag-service/pyproject.toml web/package.json deploy/compose/.env.example
git commit -m "chore: bootstrap ai gateway monorepo"
```

## 任务 2：搭建 Gateway 服务骨架与健康检查接口

**文件：**
- 创建：`gateway/cmd/server/main.go`
- 创建：`gateway/internal/config/config.go`
- 创建：`gateway/internal/http/router.go`
- 创建：`gateway/internal/http/handlers/health.go`
- 创建：`gateway/internal/telemetry/logger.go`
- 创建：`gateway/internal/http/router_test.go`
- 测试：`gateway/internal/http/router_test.go`

- [ ] **步骤 1：先写失败的网关路由测试**

```go
package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	apphttp "github.com/liwenjian/ai_gateway/gateway/internal/http"
)

func TestHealthRouteReturnsOK(t *testing.T) {
	app := apphttp.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
```

- [ ] **步骤 2：运行测试，确认它先失败**

运行：`cd gateway && go test ./internal/http -run TestHealthRouteReturnsOK -v`
预期：FAIL，因为 `NewRouter` 还不存在

- [ ] **步骤 3：写最小健康检查实现**

```go
package handlers

import "github.com/gofiber/fiber/v2"

func Health(c *fiber.Ctx) error {
	return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "ok"})
}
```

```go
package http

import (
	"github.com/gofiber/fiber/v2"
	"github.com/liwenjian/ai_gateway/gateway/internal/http/handlers"
)

func NewRouter() *fiber.App {
	app := fiber.New()
	app.Get("/healthz", handlers.Health)
	return app
}
```

```go
package main

import (
	"log"

	apphttp "github.com/liwenjian/ai_gateway/gateway/internal/http"
)

func main() {
	log.Fatal(apphttp.NewRouter().Listen(":8080"))
}
```

- [ ] **步骤 4：补上最小依赖**

运行：

```bash
cd gateway
go get github.com/gofiber/fiber/v2
```

预期：`go.mod` 和 `go.sum` 成功更新

- [ ] **步骤 5：重新运行测试，确认通过**

运行：`cd gateway && go test ./internal/http -run TestHealthRouteReturnsOK -v`
预期：PASS

- [ ] **步骤 6：提交**

```bash
git add gateway
git commit -m "feat: add gateway health endpoint"
```

## 任务 3：定义 Gateway Schema、迁移和仓储层

**文件：**
- 创建：`gateway/db/migrations/0001_init.sql`
- 创建：`gateway/db/query/api_keys.sql`
- 创建：`gateway/db/query/provider_credentials.sql`
- 创建：`gateway/db/query/tenants.sql`
- 创建：`gateway/sqlc.yaml`
- 创建：`gateway/internal/domain/auth.go`
- 创建：`gateway/internal/store/store_test.go`
- 测试：`gateway/internal/store/store_test.go`

- [ ] **步骤 1：先写失败的 API Key 查询仓储测试**

```go
package store_test

import "testing"

func TestLookupPlatformAPIKeyByHash(t *testing.T) {
	t.Fatal("write integration test with a seeded platform_api_keys row and assert lookup returns tenant_id and status")
}
```

在实现前先把这个 `t.Fatal` 替换成真实测试，使用 `testcontainers-go` 和临时 PostgreSQL 容器。测试必须插入：

```sql
insert into tenants (id, name, status) values ('tenant_demo', 'Demo', 'active');
insert into platform_api_keys (id, tenant_id, name, key_hash, status) values ('key_demo', 'tenant_demo', 'demo', 'sha256:demo', 'active');
```

然后断言查询结果返回 `tenant_demo`。

- [ ] **步骤 2：运行测试，确认它先失败**

运行：`cd gateway && go test ./internal/store -run TestLookupPlatformAPIKeyByHash -v`
预期：FAIL，因为迁移、查询和仓储代码都还不存在

- [ ] **步骤 3：编写初始 Schema**

使用下面这份迁移骨架：

```sql
create table tenants (
  id text primary key,
  name text not null,
  status text not null check (status in ('active', 'disabled')),
  created_at timestamptz not null default now()
);

create table platform_api_keys (
  id text primary key,
  tenant_id text not null references tenants(id),
  name text not null,
  key_hash text not null unique,
  status text not null check (status in ('active', 'disabled')),
  created_at timestamptz not null default now()
);

create table provider_credentials (
  id text primary key,
  provider text not null,
  display_name text not null,
  encrypted_secret text not null,
  status text not null check (status in ('active', 'disabled')),
  created_at timestamptz not null default now()
);

create table byok_credentials (
  id text primary key,
  tenant_id text not null references tenants(id),
  provider text not null,
  encrypted_secret text not null,
  status text not null check (status in ('active', 'disabled')),
  created_at timestamptz not null default now()
);
```

- [ ] **步骤 4：添加 sqlc 查询和仓储类型**

至少定义：

```sql
-- name: GetPlatformAPIKeyByHash :one
select id, tenant_id, name, key_hash, status
from platform_api_keys
where key_hash = $1;
```

```sql
-- name: ListActiveProviderCredentials :many
select id, provider, display_name, encrypted_secret, status
from provider_credentials
where status = 'active';
```

- [ ] **步骤 5：生成代码并让仓储测试通过**

运行：

```bash
cd gateway
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0
sqlc generate
go get github.com/testcontainers/testcontainers-go
go get github.com/jackc/pgx/v5
```

预期：生成的查询代码存在，测试可基于这些代码实现

- [ ] **步骤 6：重新运行仓储测试**

运行：`cd gateway && go test ./internal/store -run TestLookupPlatformAPIKeyByHash -v`
预期：PASS

- [ ] **步骤 7：提交**

```bash
git add gateway
git commit -m "feat: add gateway auth schema and repository layer"
```

## 任务 4：实现 Gateway 认证、配额拦截和路由解析

**文件：**
- 创建：`gateway/internal/http/middleware/auth.go`
- 创建：`gateway/internal/http/middleware/quota.go`
- 修改：`gateway/internal/http/router.go`
- 创建：`gateway/internal/store/auth_repository.go`
- 创建：`gateway/internal/service/auth_service.go`
- 创建：`gateway/internal/service/route_service.go`
- 创建：`gateway/internal/domain/routing.go`
- 创建：`gateway/internal/service/auth_service_test.go`
- 测试：`gateway/internal/service/auth_service_test.go`

- [ ] **步骤 1：先写失败的认证服务测试**

```go
package service_test

import "testing"

func TestResolveRequestContextUsesPlatformKeyAndProviderCredential(t *testing.T) {
	t.Fatal("assert a platform API key resolves to tenant context and selects a provider credential, not the raw client key")
}
```

把这个占位写法替换成真实的表驱动测试，覆盖：

- active platform key + active provider credential => success
- disabled platform key => unauthorized error
- quota exhausted => quota exceeded error

- [ ] **步骤 2：运行测试，确认它先失败**

运行：`cd gateway && go test ./internal/service -run TestResolveRequestContextUsesPlatformKeyAndProviderCredential -v`
预期：FAIL，因为认证服务和路由服务还不存在

- [ ] **步骤 3：实现最小认证与路由类型**

```go
type RequestContext struct {
	TenantID             string
	PlatformAPIKeyID     string
	SelectedProviderID   string
	SelectedProviderName string
}
```

```go
type AuthService interface {
	Resolve(rawKey string, requestedModel string) (RequestContext, error)
}
```

实现 `Resolve`，使其：

1. 对入站平台 key 做哈希
2. 通过手写的 `auth_repository.go` 边界加载匹配的 `platform_api_keys` 记录，而不是让 service 直接依赖 sqlc 生成的 row 类型
3. 校验租户和 key 状态
4. 在 Redis 中检查配额使用情况
5. 向 `RouteService` 请求供应商凭据

- [ ] **步骤 4：补上 HTTP 中间件接线**

使用下面这份中间件形状：

```go
func RequirePlatformAPIKey(authService service.AuthService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		raw := strings.TrimPrefix(c.Get("Authorization"), "Bearer ")
		ctx, err := authService.Resolve(raw, c.Query("model"))
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, err.Error())
		}
		c.Locals("requestContext", ctx)
		return c.Next()
	}
}
```

在这一任务中，允许对 `router.go` 做最小接线修改，用来证明认证中间件已经进入默认 HTTP 路径。保持 `/healthz` 兼容不变，并只增加一个最小受保护检查路由即可，例如 `/v1/auth-check`。

`quota.go` 在这一阶段不要求重复执行第二次配额判断；它只需要承担一个真实的中间件职责，例如校验经过认证与配额检查后的 `requestContext` 已经存在，再允许后续受保护处理器继续执行。

- [ ] **步骤 5：重新运行服务测试**

运行：`cd gateway && go test ./internal/service -run TestResolveRequestContextUsesPlatformKeyAndProviderCredential -v`
预期：PASS

- [ ] **步骤 6：提交**

```bash
git add gateway
git commit -m "feat: add platform key auth and route resolution"
```

## 任务 5：增加 Chat 和 Embedding 代理接口以及用量日志

**文件：**
- 创建：`gateway/internal/http/handlers/chat.go`
- 创建：`gateway/internal/http/handlers/embeddings.go`
- 创建：`gateway/internal/provider/openai_client.go`
- 创建：`gateway/internal/provider/dashscope_client.go`
- 创建：`gateway/internal/queue/usage_publisher.go`
- 创建：`gateway/tests/integration/proxy_test.go`
- 测试：`gateway/tests/integration/proxy_test.go`

- [ ] **步骤 1：先写失败的 Chat 代理集成测试**

测试必须做到：

- 启动网关和一个 stub 供应商服务
- 发送 `POST /v1/chat/completions`
- 使用平台 API Key 完成认证
- 断言供应商收到的是映射后的 provider credential
- 断言响应结构符合网关对外契约

使用下面这段响应断言：

```go
if got := body.Get("choices.0.message.content").String(); got != "stub-answer" {
	t.Fatalf("expected stub-answer, got %q", got)
}
```

- [ ] **步骤 2：运行集成测试，确认它先失败**

运行：`cd gateway && go test ./tests/integration -run TestChatCompletionProxy -v`
预期：FAIL，因为 handler 和 provider client 还不存在

- [ ] **步骤 3：实现 Chat Handler**

创建最小 handler 形状：

```go
func ChatCompletion(proxy service.ChatProxyService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req service.ChatRequest
		if err := c.BodyParser(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}
		resp, err := proxy.Complete(c.UserContext(), req, c.Locals("requestContext"))
		if err != nil {
			return err
		}
		return c.JSON(resp)
	}
}
```

- [ ] **步骤 4：实现用量消息发布**

每次请求成功或失败后，都向 RabbitMQ 发布如下结构：

```json
{
  "tenant_id": "tenant_demo",
  "platform_api_key_id": "key_demo",
  "provider_credential_id": "provider_qwen_primary",
  "endpoint": "/v1/chat/completions",
  "status_code": 200,
  "latency_ms": 123
}
```

- [ ] **步骤 5：运行集成测试**

运行：`cd gateway && go test ./tests/integration -run TestChatCompletionProxy -v`
预期：PASS

- [ ] **步骤 6：提交**

```bash
git add gateway
git commit -m "feat: add chat and embeddings proxy endpoints"
```

## 任务 6：构建 Python RAG 服务的文档导入与查询 API

**文件：**
- 创建：`rag-service/app/main.py`
- 创建：`rag-service/app/core/config.py`
- 创建：`rag-service/app/api/routes.py`
- 创建：`rag-service/app/models/document.py`
- 创建：`rag-service/app/services/chunker.py`
- 创建：`rag-service/app/services/retriever.py`
- 创建：`rag-service/app/services/answerer.py`
- 创建：`rag-service/tests/test_rag_query_api.py`
- 测试：`rag-service/tests/test_rag_query_api.py`

- [ ] **步骤 1：先写失败的 RAG API 测试**

```python
from fastapi.testclient import TestClient
from app.main import app

def test_rag_query_returns_answer_and_sources():
    client = TestClient(app)
    response = client.post("/internal/rag/query", json={
        "tenant_id": "tenant_demo",
        "knowledge_base_id": "kb_demo",
        "question": "What is AI Gateway?"
    })
    assert response.status_code == 200
    data = response.json()
    assert "answer" in data
    assert "sources" in data
```

- [ ] **步骤 2：运行测试，确认它先失败**

运行：`cd rag-service && pytest tests/test_rag_query_api.py -v`
预期：FAIL，因为 FastAPI app 和 route 还不存在

- [ ] **步骤 3：实现最小 FastAPI 应用**

使用下面这份精确的路由契约：

```python
@router.post("/internal/rag/query")
def rag_query(payload: RagQueryRequest) -> RagQueryResponse:
    return RagQueryResponse(
        answer="stub-answer",
        sources=[{"document_id": "doc_demo", "chunk_id": "chunk_1", "score": 0.91}],
    )
```

- [ ] **步骤 4：补上文档导入契约**

创建第二个路由：

```python
@router.post("/internal/rag/ingest")
def ingest_document(payload: IngestDocumentRequest) -> IngestDocumentResponse:
    return IngestDocumentResponse(document_id="doc_demo", status="queued")
```

- [ ] **步骤 5：重新运行 API 测试**

运行：`cd rag-service && pytest tests/test_rag_query_api.py -v`
预期：PASS

- [ ] **步骤 6：提交**

```bash
git add rag-service
git commit -m "feat: add rag service query and ingest APIs"
```

## 任务 7：暴露 Gateway RAG 接口并接通知识库链路

**文件：**
- 创建：`gateway/internal/http/handlers/rag.go`
- 创建：`gateway/internal/service/rag_proxy_service.go`
- 创建：`gateway/tests/integration/rag_proxy_test.go`
- 修改：`gateway/internal/http/router.go`
- 测试：`gateway/tests/integration/rag_proxy_test.go`

- [ ] **步骤 1：先写失败的 Gateway RAG 代理测试**

测试必须证明：

- 请求命中 `POST /v1/rag/query`
- 网关使用平台 API Key 完成认证
- 网关转发的 `tenant_id` 来自解析后的请求上下文，而不是客户端原始输入
- 响应中包含 `answer` 和 `sources`

使用下面这段断言：

```go
if got := body.Get("sources.0.document_id").String(); got == "" {
	t.Fatal("expected at least one source document")
}
```

- [ ] **步骤 2：运行测试，确认它先失败**

运行：`cd gateway && go test ./tests/integration -run TestRAGQueryProxy -v`
预期：FAIL，因为 handler 和 service 还不存在

- [ ] **步骤 3：实现 RAG 代理服务**

使用下面这份请求结构：

```go
type RAGQueryRequest struct {
	KnowledgeBaseID string `json:"knowledge_base_id"`
	Question        string `json:"question"`
}
```

在转发到 `rag-service` 之前，增加：

```go
forward := map[string]any{
	"tenant_id": resolved.TenantID,
	"knowledge_base_id": req.KnowledgeBaseID,
	"question": req.Question,
}
```

- [ ] **步骤 4：运行测试，确认通过**

运行：`cd gateway && go test ./tests/integration -run TestRAGQueryProxy -v`
预期：PASS

- [ ] **步骤 5：提交**

```bash
git add gateway
git commit -m "feat: add gateway rag proxy endpoint"
```

## 任务 8：搭建 React 控制台外壳和核心页面

**文件：**
- 创建：`web/index.html`
- 创建：`web/src/main.tsx`
- 创建：`web/src/app/router.tsx`
- 创建：`web/src/app/layout.tsx`
- 创建：`web/src/pages/dashboard.tsx`
- 创建：`web/src/pages/api-keys.tsx`
- 创建：`web/src/pages/routes.tsx`
- 创建：`web/src/pages/playground.tsx`
- 创建：`web/src/pages/knowledge-base.tsx`
- 创建：`web/src/pages/audit.tsx`
- 创建：`web/src/test/router.test.tsx`
- 测试：`web/src/test/router.test.tsx`

- [ ] **步骤 1：先写失败的路由冒烟测试**

```tsx
import { render, screen } from "@testing-library/react";
import { createAppRouter } from "../app/router";

test("renders dashboard route", async () => {
  render(createAppRouter());
  expect(await screen.findByText("Overview")).toBeInTheDocument();
});
```

- [ ] **步骤 2：运行测试，确认它先失败**

运行：`cd web && npm test -- --runInBand`
预期：FAIL，因为 Vite、React 和路由树都还没配置

- [ ] **步骤 3：实现页面外壳**

每个页面至少渲染一个稳定标题：

```tsx
export function DashboardPage() {
  return <h1>Overview</h1>;
}
```

```tsx
export function APIKeysPage() {
  return <h1>API Keys</h1>;
}
```

```tsx
export function PlaygroundPage() {
  return <h1>Playground</h1>;
}
```

- [ ] **步骤 4：增加路由定义**

使用下面这份路由表：

```tsx
[
  { path: "/", element: <DashboardPage /> },
  { path: "/api-keys", element: <APIKeysPage /> },
  { path: "/routes", element: <RoutesPage /> },
  { path: "/playground", element: <PlaygroundPage /> },
  { path: "/knowledge-base", element: <KnowledgeBasePage /> },
  { path: "/audit", element: <AuditPage /> },
]
```

- [ ] **步骤 5：重新运行路由测试**

运行：`cd web && npm test -- --runInBand`
预期：PASS

- [ ] **步骤 6：提交**

```bash
git add web
git commit -m "feat: add console shell and primary routes"
```

## 任务 9：打通控制台中的 API Keys、Playground 和 Knowledge Base 数据流

**文件：**
- 创建：`web/src/lib/api.ts`
- 创建：`web/src/features/api-keys/api.ts`
- 创建：`web/src/features/playground/api.ts`
- 创建：`web/src/features/knowledge-base/api.ts`
- 创建：`web/e2e/console-smoke.spec.ts`
- 修改：`web/src/pages/api-keys.tsx`
- 修改：`web/src/pages/playground.tsx`
- 修改：`web/src/pages/knowledge-base.tsx`
- 测试：`web/e2e/console-smoke.spec.ts`

- [ ] **步骤 1：先写失败的 Playwright 冒烟测试**

测试必须覆盖：

1. 打开 dashboard
2. 跳转到 API Keys 页面
3. 创建一个 demo key
4. 跳转到 Playground
5. 提交一个 chat prompt
6. 跳转到 Knowledge Base
7. 上传文档或使用预置的 demo knowledge base

使用下面这段断言：

```ts
await expect(page.getByText("stub-answer")).toBeVisible();
```

- [ ] **步骤 2：运行 e2e 测试，确认它先失败**

运行：`cd web && npx playwright test e2e/console-smoke.spec.ts`
预期：FAIL，因为表单和数据调用还不存在

- [ ] **步骤 3：实现最小数据客户端**

使用下面这两个精确函数：

```ts
export async function listAPIKeys() {
  return fetch("/api/admin/api-keys").then((r) => r.json());
}

export async function createPlaygroundCompletion(body: unknown) {
  return fetch("/api/v1/chat/completions", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }).then((r) => r.json());
}
```

- [ ] **步骤 4：添加可见的成功路径 UI 状态**

每个页面必须展示：

- loading state
- error state
- success state

对于 Playground 成功态，渲染：

```tsx
<pre aria-label="playground-response">{response.choices?.[0]?.message?.content}</pre>
```

- [ ] **步骤 5：重新运行 e2e 冒烟测试**

运行：`cd web && npx playwright test e2e/console-smoke.spec.ts`
预期：在预置 demo 环境下 PASS

- [ ] **步骤 6：提交**

```bash
git add web
git commit -m "feat: wire console data flows for api keys and playground"
```

## 任务 10：增加 Compose 部署、Demo 种子数据和可观测性

**文件：**
- 创建：`deploy/compose/compose.yml`
- 创建：`deploy/prometheus/prometheus.yml`
- 创建：`deploy/grafana/dashboards/gateway-overview.json`
- 创建：`scripts/seed-demo.sh`
- 创建：`docs/runbooks/local-dev.md`
- 创建：`docs/runbooks/deploy-compose.md`
- 创建：`docs/runbooks/troubleshooting.md`
- 测试：`deploy/compose/compose.yml`

- [ ] **步骤 1：先写失败的部署检查**

运行：

```bash
docker compose -f deploy/compose/compose.yml config
```

预期：FAIL，因为 compose 文件还不存在

- [ ] **步骤 2：创建部署栈**

compose 文件必须包含这些服务：

```yaml
services:
  postgres:
  redis:
  rabbitmq:
  minio:
  gateway:
  rag-service:
  web:
  prometheus:
  grafana:
```

- [ ] **步骤 3：增加 demo 种子数据**

创建 `scripts/seed-demo.sh`，至少完成以下动作：

```bash
insert demo tenant
insert demo platform api key
insert demo provider credential
insert demo route policy
insert demo knowledge base metadata
```

实际脚本应调用 `psql` 并指向 compose 中的 postgres 容器。

- [ ] **步骤 4：增加指标与 dashboard 契约**

Gateway 必须暴露 `/metrics`；Prometheus 必须每 15 秒抓取一次。Grafana dashboard 必须展示：

- request count
- p95 latency
- error rate
- quota exceeded count

- [ ] **步骤 5：重新运行部署校验**

运行：

```bash
docker compose -f deploy/compose/compose.yml config
```

预期：PASS，并输出渲染后的配置

- [ ] **步骤 6：提交**

```bash
git add deploy scripts docs/runbooks
git commit -m "feat: add compose deployment and observability stack"
```

## 任务 11：增加 Kubernetes 初始清单和 CI 脚本

**文件：**
- 创建：`deploy/kubernetes/gateway-deployment.yaml`
- 创建：`deploy/kubernetes/rag-service-deployment.yaml`
- 创建：`deploy/kubernetes/web-deployment.yaml`
- 创建：`deploy/kubernetes/ingress.yaml`
- 修改：`scripts/test.sh`
- 修改：`scripts/lint.sh`
- 创建：`.github/workflows/ci.yml`
- 测试：`.github/workflows/ci.yml`

- [ ] **步骤 1：先写失败的 manifest 校验**

运行：`test -f deploy/kubernetes/gateway-deployment.yaml || echo "missing k8s manifests"`
预期：输出 `missing k8s manifests`

- [ ] **步骤 2：增加 Kubernetes 起步清单**

每个 deployment 必须包含：

- `metadata.labels.app`
- `spec.replicas`
- `readinessProbe`
- `livenessProbe`
- 数据库和服务 URL 的环境变量引用

Gateway 使用下面这段 readiness probe：

```yaml
readinessProbe:
  httpGet:
    path: /healthz
    port: 8080
```

- [ ] **步骤 3：替换仓库中的占位脚本**

`scripts/test.sh` 必须运行：

```bash
go test ./...
pytest
npm test -- --runInBand
```

`scripts/lint.sh` 必须运行：

```bash
gofmt -w .
go test ./...
ruff check .
npm run build
```

- [ ] **步骤 4：增加 CI 工作流**

工作流必须：

- 在 push 和 pull_request 时运行
- 配置 Go、Python 和 Node 环境
- 运行 gateway 测试
- 运行 rag-service 测试
- 运行 web 测试
- 运行 `docker compose config`

- [ ] **步骤 5：重新运行快速校验**

运行：

```bash
test -f deploy/kubernetes/gateway-deployment.yaml && test -f .github/workflows/ci.yml && echo "k8s+ci ready"
```

预期：输出 `k8s+ci ready`

- [ ] **步骤 6：提交**

```bash
git add deploy/kubernetes scripts .github/workflows/ci.yml
git commit -m "chore: add ci pipeline and kubernetes starter manifests"
```

## 任务 12：最终验证和发布说明

**文件：**
- 修改：`README.md`
- 创建：`docs/runbooks/demo-script.md`
- 测试：`README.md`

- [ ] **步骤 1：写发布验证清单**

将下面这份精确清单加入 `README.md`：

```md
## MVP Verification

- gateway health endpoint returns 200
- chat completions proxy returns a stub or live answer
- RAG query returns answer plus sources
- console can create and view API keys
- Prometheus scrapes gateway metrics
```

- [ ] **步骤 2：执行完整验证套件**

运行：

```bash
./scripts/lint.sh
./scripts/test.sh
docker compose -f deploy/compose/compose.yml config
```

预期：所有命令都通过

- [ ] **步骤 3：编写 demo 演示脚本**

创建 `docs/runbooks/demo-script.md`，内容流程如下：

```md
1. Open the dashboard and show request metrics.
2. Create or reveal the demo platform API key.
3. Call `/v1/chat/completions` from Playground.
4. Upload or select a seeded document in Knowledge Base.
5. Call `/v1/rag/query` and show sources.
6. Open audit logs and explain platform key -> provider credential routing.
```

- [ ] **步骤 4：打 MVP 标签**

```bash
git add README.md docs/runbooks/demo-script.md
git commit -m "docs: add mvp verification and demo script"
git tag v0.1.0
```

## 自检

### 规格覆盖情况

- SaaS AI 网关核心：由任务 2-5 覆盖。
- 平台 API Key 与 provider credential 分离：由任务 3-5 覆盖。
- 面向未来 BYOK 的 schema：由任务 3 覆盖，并在任务 4 中被引用。
- RAG 示范服务：由任务 6-7 覆盖。
- React 控制台：由任务 8-9 覆盖。
- Compose 优先的部署与可观测性：由任务 10 覆盖。
- 面向 Kubernetes 的演进路径与 CI：由任务 11 覆盖。
- 可演示性与发布验证：由任务 12 覆盖。

### 占位检查

- 唯一有意保留的占位，是首轮实现阶段显式写出的临时 stub 响应；这些都出现在用于建立可运行纵向切片的任务中，并在后续由集成或验证步骤继续推进。
- 任务说明中不再保留 `TBD`、`TODO` 或 “implement later” 之类标记。

### 类型一致性

- `platform_api_keys`、`provider_credentials` 和 `byok_credentials` 命名与已批准规格保持一致。
- Gateway 对外公开接口仍然是 `/v1/chat/completions`、`/v1/embeddings` 和 `/v1/rag/query`。
- RAG 内部接口仍然是 `/internal/rag/query`。
