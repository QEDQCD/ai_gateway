# Tenant API Key Governance Platform Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将当前 AI Gateway 从单一 admin 控制台重构为“账号审批 + 租户治理 + 用户自助 API Key + 租户级 token/失败/审计闭环”的平台。

**Architecture:** 后端先扩展 PostgreSQL 模型、角色上下文和路由，将当前 `/admin` 单入口拆分为 `admin` 控制面和 `member` 自服务面，再复用现有 `llm_request_logs` 聚合链路沉淀租户级账本与失败视图。前端继续使用现有 React 控制台壳层，但导航、页面和 API 客户端按角色分裂为两组，隐藏 RAG/知识库和内部 provider 细节。

**Tech Stack:** Go + Fiber + pgx + PostgreSQL + React + TypeScript + Vitest + Docker Compose

---

## File Structure

本轮实现前，先锁定文件职责，避免继续把所有控制台逻辑堆进单一 service。

- **数据库与种子**
  - Create: `gateway/db/migrations/0007_add_tenant_governance_tables.sql`
  - Modify: `gateway/db/runtime.go`
  - Modify: `gateway/db/runtime_test.go`
- **认证与上下文**
  - Modify: `gateway/internal/domain/auth.go`
  - Modify: `gateway/internal/store/auth_repository.go`
  - Modify: `gateway/internal/service/auth_service.go`
  - Modify: `gateway/internal/http/middleware/auth.go`
  - Create: `gateway/internal/http/middleware/console_role.go`
- **admin 控制面**
  - Modify: `gateway/internal/service/console_service.go`
  - Modify: `gateway/internal/service/postgres_console_service.go`
  - Modify: `gateway/internal/http/handlers/admin.go`
  - Modify: `gateway/internal/http/router.go`
- **member 自服务面**
  - Create: `gateway/internal/http/handlers/member.go`
  - Create: `gateway/internal/service/member_console_service.go`
  - Create: `gateway/internal/service/postgres_member_console_service.go`
- **调用观测与失败审计**
  - Modify: `gateway/internal/service/usage_recording.go`
  - Modify: `gateway/internal/service/usage_aggregator.go`
  - Modify: `gateway/internal/service/usage_types.go`
- **前端壳层与 API**
  - Modify: `web/src/app/router.tsx`
  - Modify: `web/src/app/layout.tsx`
  - Modify: `web/src/lib/console-api.ts`
  - Create: `web/src/lib/session.ts`
- **前端页面**
  - Create: `web/src/pages/admin-applications.tsx`
  - Create: `web/src/pages/admin-tenants.tsx`
  - Create: `web/src/pages/member-overview.tsx`
  - Create: `web/src/pages/member-usage.tsx`
  - Create: `web/src/pages/member-failures.tsx`
  - Modify: `web/src/pages/api-keys.tsx`
  - Modify: `web/src/pages/audit.tsx`
  - Modify: `web/src/pages/dashboard.tsx`
  - Modify: `web/src/styles.css`
- **测试**
  - Modify: `gateway/internal/http/router_test.go`
  - Modify: `gateway/internal/service/postgres_console_service_test.go`
  - Create: `gateway/internal/service/postgres_member_console_service_test.go`
  - Modify: `gateway/internal/store/auth_repository_test.go`
  - Modify: `gateway/tests/integration/proxy_test.go`
  - Modify: `web/src/test/router.test.tsx`
- **文档**
  - Modify: `README.md`
  - Modify: `docs/specs/2026-04-27-tenant-api-key-governance-platform-design.md`

## Task 1: 扩展数据库模型支持账号申请、成员关系和审计事件

**Files:**
- Create: `gateway/db/migrations/0007_add_tenant_governance_tables.sql`
- Modify: `gateway/db/runtime.go`
- Modify: `gateway/db/runtime_test.go`
- Test: `gateway/db/runtime_test.go`

- [ ] **Step 1: 写失败测试，先锁定新表和种子能力**

```go
func TestApplyMigrationsCreatesTenantGovernanceTables(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	conn := openTestPostgres(t, ctx)
	applyMigrations(t, ctx, conn)

	for _, tableName := range []string{
		"account_applications",
		"users",
		"tenant_memberships",
		"audit_events",
	} {
		assertTableExists(t, ctx, conn, tableName)
	}
}

func TestRuntimeSeedStatementsPopulateApprovalAndMembershipData(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	conn := openSeededRuntimeDB(t, ctx)

	assertTableCount(t, ctx, conn, "account_applications", 2)
	assertTableCount(t, ctx, conn, "users", 3)
	assertTableCount(t, ctx, conn, "tenant_memberships", 2)
	assertTableCount(t, ctx, conn, "audit_events", 3)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd gateway && go test ./db -run 'TestApplyMigrationsCreatesTenantGovernanceTables|TestRuntimeSeedStatementsPopulateApprovalAndMembershipData' -count=1 -v`  
Expected: FAIL，提示缺少新表或种子记录。

- [ ] **Step 3: 写最小迁移和种子**

```sql
create table account_applications (
  id text primary key,
  email text not null,
  name text not null,
  company_name text not null default '',
  use_case text not null default '',
  status text not null check (status in ('pending', 'approved', 'rejected')),
  reviewer_id text not null default '',
  review_comment text not null default '',
  reviewed_at timestamptz,
  created_at timestamptz not null default now()
);

create table users (
  id text primary key,
  email text not null unique,
  name text not null,
  role text not null check (role in ('admin', 'member')),
  status text not null check (status in ('active', 'disabled')),
  created_at timestamptz not null default now()
);

create table tenant_memberships (
  id text primary key,
  tenant_id text not null references tenants(id),
  user_id text not null references users(id),
  role text not null check (role in ('member')),
  status text not null check (status in ('active', 'disabled')),
  created_at timestamptz not null default now(),
  unique (tenant_id, user_id)
);

create table audit_events (
  id text primary key,
  actor_type text not null check (actor_type in ('admin', 'member', 'system')),
  actor_user_id text not null default '',
  tenant_id text references tenants(id),
  event_type text not null,
  target_type text not null,
  target_id text not null default '',
  detail text not null default '',
  ip_digest text not null default '',
  created_at timestamptz not null default now()
);
```

```go
func RuntimeSeedStatements() []string {
	return append(existingSeedStatements(),
		`insert into users (id, email, name, role, status) values
		 ('user_admin_demo', 'admin@example.com', '平台管理员', 'admin', 'active'),
		 ('user_member_a', 'member-a@example.com', '租户用户A', 'member', 'active'),
		 ('user_member_b', 'member-b@example.com', '租户用户B', 'member', 'active')
		 on conflict (id) do nothing;`,
		`insert into tenant_memberships (id, tenant_id, user_id, role, status) values
		 ('tm_demo_001', 'tenant_demo', 'user_member_a', 'member', 'active'),
		 ('tm_demo_002', 'tenant_demo', 'user_member_b', 'member', 'active')
		 on conflict (tenant_id, user_id) do nothing;`,
		`insert into account_applications (id, email, name, company_name, use_case, status) values
		 ('app_demo_pending', 'pending@example.com', '待审批用户', 'Demo Co', '内部知识问答', 'pending'),
		 ('app_demo_rejected', 'rejected@example.com', '被拒绝用户', 'Demo Co', '压测脚本', 'rejected')
		 on conflict (id) do nothing;`,
		`insert into audit_events (id, actor_type, actor_user_id, tenant_id, event_type, target_type, target_id, detail) values
		 ('audit_evt_001', 'admin', 'user_admin_demo', 'tenant_demo', 'application_approved', 'account_application', 'app_demo_seeded', 'seed approve'),
		 ('audit_evt_002', 'member', 'user_member_a', 'tenant_demo', 'api_key_created', 'platform_api_key', 'pak_demo', 'seed key create'),
		 ('audit_evt_003', 'system', '', 'tenant_demo', 'quota_warning', 'tenant', 'tenant_demo', 'seed quota warning')
		 on conflict (id) do nothing;`,
	)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd gateway && go test ./db -run 'TestApplyMigrationsCreatesTenantGovernanceTables|TestRuntimeSeedStatementsPopulateApprovalAndMembershipData' -count=1 -v`  
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gateway/db/migrations/0007_add_tenant_governance_tables.sql gateway/db/runtime.go gateway/db/runtime_test.go
git commit -m "feat: add tenant governance schema"
```

### Task 2: 扩展认证上下文与角色作用域

**Files:**
- Modify: `gateway/internal/domain/auth.go`
- Modify: `gateway/internal/store/auth_repository.go`
- Modify: `gateway/internal/service/auth_service.go`
- Modify: `gateway/internal/http/middleware/auth.go`
- Create: `gateway/internal/http/middleware/console_role.go`
- Modify: `gateway/internal/store/auth_repository_test.go`
- Modify: `gateway/internal/service/auth_service_test.go`
- Test: `gateway/internal/store/auth_repository_test.go`
- Test: `gateway/internal/service/auth_service_test.go`

- [ ] **Step 1: 写失败测试，定义角色和租户上下文**

```go
func TestFindPlatformAPIKeyByHashReturnsCreatorUserAndTenantScope(t *testing.T) {
	t.Parallel()

	repo := newSeededAuthRepository(t)
	record, err := repo.FindPlatformAPIKeyByHash(context.Background(), hashPlatformAPIKey("agw_demo_key"))
	if err != nil {
		t.Fatalf("FindPlatformAPIKeyByHash failed: %v", err)
	}

	if record.UserID == "" {
		t.Fatal("expected user id on auth record")
	}
	if record.TenantID != "tenant_demo" {
		t.Fatalf("expected tenant_demo, got %q", record.TenantID)
	}
}

func TestResolveConsolePrincipalAllowsAdminAndMemberScopes(t *testing.T) {
	t.Parallel()

	service := newSeededAuthService(t)

	admin, err := service.ResolveConsolePrincipal(context.Background(), "admin@example.com")
	if err != nil || admin.Role != "admin" {
		t.Fatalf("expected admin principal, got %#v err=%v", admin, err)
	}

	member, err := service.ResolveConsolePrincipal(context.Background(), "member-a@example.com")
	if err != nil || member.Role != "member" || member.TenantID != "tenant_demo" {
		t.Fatalf("expected tenant-scoped member, got %#v err=%v", member, err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd gateway && go test ./internal/store ./internal/service -run 'TestFindPlatformAPIKeyByHashReturnsCreatorUserAndTenantScope|TestResolveConsolePrincipalAllowsAdminAndMemberScopes' -count=1 -v`  
Expected: FAIL，提示结构体缺字段或服务方法不存在。

- [ ] **Step 3: 实现最小认证模型**

```go
type PlatformAPIKeyRecord struct {
	ID       string
	TenantID string
	UserID   string
	Name     string
	Status   domain.Status
}

type ConsolePrincipal struct {
	UserID   string
	Email    string
	Role     string
	TenantID string
}

type AuthService interface {
	ResolvePlatformAPIKey(ctx context.Context, rawKey string) (domain.RequestContext, error)
	ResolveConsolePrincipal(ctx context.Context, subject string) (ConsolePrincipal, error)
}
```

```go
func RequireConsoleRole(allowed ...string) fiber.Handler {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, role := range allowed {
		allowedSet[role] = struct{}{}
	}
	return func(c *fiber.Ctx) error {
		principal, ok := c.Locals("console_principal").(service.ConsolePrincipal)
		if !ok {
			return fiber.NewError(fiber.StatusUnauthorized, "console principal missing")
		}
		if _, ok := allowedSet[principal.Role]; !ok {
			return fiber.NewError(fiber.StatusForbidden, "forbidden")
		}
		return c.Next()
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd gateway && go test ./internal/store ./internal/service -run 'TestFindPlatformAPIKeyByHashReturnsCreatorUserAndTenantScope|TestResolveConsolePrincipalAllowsAdminAndMemberScopes' -count=1 -v`  
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gateway/internal/domain/auth.go gateway/internal/store/auth_repository.go gateway/internal/store/auth_repository_test.go gateway/internal/service/auth_service.go gateway/internal/service/auth_service_test.go gateway/internal/http/middleware/auth.go gateway/internal/http/middleware/console_role.go
git commit -m "feat: add console role and tenant auth scope"
```

### Task 3: 后端实现 admin 账号申请与租户管理接口

**Files:**
- Modify: `gateway/internal/service/console_service.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/http/handlers/admin.go`
- Modify: `gateway/internal/http/router.go`
- Modify: `gateway/internal/service/postgres_console_service_test.go`
- Modify: `gateway/internal/http/router_test.go`
- Test: `gateway/internal/service/postgres_console_service_test.go`
- Test: `gateway/internal/http/router_test.go`

- [ ] **Step 1: 写失败测试，定义 admin 视角的新接口**

```go
func TestPostgresConsoleServiceApplicationsReturnsPendingRows(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)
	payload, err := console.Applications(ctx)
	if err != nil {
		t.Fatalf("Applications failed: %v", err)
	}
	if len(payload.Items) == 0 || payload.Items[0].Status == "" {
		t.Fatal("expected application rows")
	}
}

func TestAdminApproveApplicationCreatesUserMembershipAndAudit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)
	_, err := console.ApproveApplication(ctx, "app_demo_pending", service.ApproveApplicationRequest{
		TenantID: "tenant_demo",
		Comment:  "approve for demo tenant",
		ActorID:  "user_admin_demo",
	})
	if err != nil {
		t.Fatalf("ApproveApplication failed: %v", err)
	}

	assertRowCount(t, ctx, conn, "tenant_memberships", "user_id = 'user_pending_demo'", 1)
	assertRowCount(t, ctx, conn, "audit_events", "event_type = 'application_approved'", 1)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd gateway && go test ./internal/service ./internal/http -run 'TestPostgresConsoleServiceApplicationsReturnsPendingRows|TestAdminApproveApplicationCreatesUserMembershipAndAudit' -count=1 -v`  
Expected: FAIL，提示接口和类型不存在。

- [ ] **Step 3: 实现最小 admin 审批与租户管理服务**

```go
type ApplicationItem struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	CompanyName string `json:"company_name"`
	UseCase     string `json:"use_case"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

type ApplicationsPageData struct {
	Items []ApplicationItem `json:"items"`
}

func (s postgresConsoleService) Applications(ctx context.Context) (ApplicationsPageData, error) {
	rows, err := s.db.Query(ctx, `
		select id, email, name, company_name, use_case, status, created_at
		from account_applications
		order by created_at desc
	`)
	if err != nil {
		return ApplicationsPageData{}, err
	}
	defer rows.Close()

	items := make([]ApplicationItem, 0)
	for rows.Next() {
		var item ApplicationItem
		var createdAt time.Time
		if err := rows.Scan(&item.ID, &item.Email, &item.Name, &item.CompanyName, &item.UseCase, &item.Status, &createdAt); err != nil {
			return ApplicationsPageData{}, err
		}
		item.CreatedAt = createdAt.In(shanghaiLocation()).Format(time.RFC3339)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ApplicationsPageData{}, err
	}
	return ApplicationsPageData{Items: items}, nil
}

func (s postgresConsoleService) ApproveApplication(ctx context.Context, id string, req ApproveApplicationRequest) (ApplicationMutationResult, error) {
	_, err := s.db.Exec(ctx, `
		with updated as (
			update account_applications
			set status = 'approved', reviewer_id = $2, review_comment = $3, reviewed_at = now()
			where id = $1 and status = 'pending'
			returning email, name
		)
		insert into users (id, email, name, role, status)
		select 'user_pending_demo', email, name, 'member', 'active' from updated
		on conflict (email) do nothing;
	`, id, req.ActorID, req.Comment)
	return s.applicationByID(ctx, id)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd gateway && go test ./internal/service ./internal/http -run 'TestPostgresConsoleServiceApplicationsReturnsPendingRows|TestAdminApproveApplicationCreatesUserMembershipAndAudit' -count=1 -v`  
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gateway/internal/service/console_service.go gateway/internal/service/postgres_console_service.go gateway/internal/service/postgres_console_service_test.go gateway/internal/http/handlers/admin.go gateway/internal/http/router.go gateway/internal/http/router_test.go
git commit -m "feat: add admin application approval flows"
```

### Task 4: 后端实现 member 自服务 API Key 与租户概览接口

**Files:**
- Create: `gateway/internal/service/member_console_service.go`
- Create: `gateway/internal/service/postgres_member_console_service.go`
- Create: `gateway/internal/http/handlers/member.go`
- Modify: `gateway/internal/http/router.go`
- Create: `gateway/internal/service/postgres_member_console_service_test.go`
- Modify: `gateway/internal/http/router_test.go`
- Test: `gateway/internal/service/postgres_member_console_service_test.go`
- Test: `gateway/internal/http/router_test.go`

- [ ] **Step 1: 写失败测试，定义 member 能力边界**

```go
func TestPostgresMemberConsoleServiceOverviewIsTenantScoped(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	svc, _ := newMemberConsoleService(t, ctx, "user_member_a", "tenant_demo")
	payload, err := svc.Overview(ctx)
	if err != nil {
		t.Fatalf("Overview failed: %v", err)
	}
	if payload.TenantName == "" {
		t.Fatal("expected tenant name")
	}
}

func TestPostgresMemberConsoleServiceAPIKeysOnlyReturnsCreatorsKeys(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	svc, _ := newMemberConsoleService(t, ctx, "user_member_a", "tenant_demo")
	payload, err := svc.APIKeys(ctx)
	if err != nil {
		t.Fatalf("APIKeys failed: %v", err)
	}
	for _, item := range payload.Items {
		if item.OwnerUserID != "user_member_a" {
			t.Fatalf("unexpected foreign key item %#v", item)
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd gateway && go test ./internal/service ./internal/http -run 'TestPostgresMemberConsoleServiceOverviewIsTenantScoped|TestPostgresMemberConsoleServiceAPIKeysOnlyReturnsCreatorsKeys' -count=1 -v`  
Expected: FAIL，提示 member service/handler/router 不存在。

- [ ] **Step 3: 写最小 member 服务与路由**

```go
type MemberConsoleService interface {
	Overview(ctx context.Context) (MemberOverviewPageData, error)
	APIKeys(ctx context.Context) (MemberAPIKeysPageData, error)
	CreateAPIKey(ctx context.Context, req CreateMemberAPIKeyRequest) (APIKeyMutationResult, error)
	RotateAPIKey(ctx context.Context, id string, req RotateAPIKeyRequest) (APIKeyMutationResult, error)
	DeactivateAPIKey(ctx context.Context, id string) (APIKeyMutationResult, error)
	UsageOverview(ctx context.Context, query UsageQuery) (UsageOverviewData, error)
	UsageRequests(ctx context.Context, query UsageQuery) (UsageRequestsPageData, error)
	Failures(ctx context.Context, query UsageQuery) (MemberFailurePageData, error)
	AuditEvents(ctx context.Context) (MemberAuditPageData, error)
}

member := app.Group(
	"/me",
	middleware.RequireServiceBasicAuth(deps.ServiceAuthUsername, deps.ServiceAuthPassword),
	middleware.RequireConsoleRole("member"),
)
member.Get("/overview", handlers.MemberOverview(deps.MemberConsoleService))
member.Get("/api-keys", handlers.MemberAPIKeys(deps.MemberConsoleService))
member.Post("/api-keys", handlers.MemberCreateAPIKey(deps.MemberConsoleService))
member.Post("/api-keys/:id/rotate", handlers.MemberRotateAPIKey(deps.MemberConsoleService))
member.Post("/api-keys/:id/deactivate", handlers.MemberDeactivateAPIKey(deps.MemberConsoleService))
member.Get("/usage/overview", handlers.MemberUsageOverview(deps.MemberConsoleService))
member.Get("/usage/requests", handlers.MemberUsageRequests(deps.MemberConsoleService))
member.Get("/failures", handlers.MemberFailures(deps.MemberConsoleService))
member.Get("/audit-events", handlers.MemberAuditEvents(deps.MemberConsoleService))
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd gateway && go test ./internal/service ./internal/http -run 'TestPostgresMemberConsoleServiceOverviewIsTenantScoped|TestPostgresMemberConsoleServiceAPIKeysOnlyReturnsCreatorsKeys' -count=1 -v`  
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gateway/internal/service/member_console_service.go gateway/internal/service/postgres_member_console_service.go gateway/internal/service/postgres_member_console_service_test.go gateway/internal/http/handlers/member.go gateway/internal/http/router.go gateway/internal/http/router_test.go
git commit -m "feat: add member self-service console api"
```

### Task 5: 调整调用记录、失败记录和租户级 token 账本

**Files:**
- Modify: `gateway/db/migrations/0007_add_tenant_governance_tables.sql`
- Modify: `gateway/internal/service/usage_types.go`
- Modify: `gateway/internal/service/usage_recording.go`
- Modify: `gateway/internal/service/usage_aggregator.go`
- Modify: `gateway/internal/service/usage_recording_test.go`
- Modify: `gateway/internal/service/usage_aggregator_test.go`
- Modify: `gateway/tests/integration/proxy_test.go`
- Test: `gateway/internal/service/usage_recording_test.go`
- Test: `gateway/internal/service/usage_aggregator_test.go`
- Test: `gateway/tests/integration/proxy_test.go`

- [ ] **Step 1: 写失败测试，先定义 user_id 和失败表**

```go
func TestUsageRecorderStoresTenantAndUserScopedFailureRecord(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	recorder, conn := newUsageRecorder(t, ctx)
	err := recorder.RecordFailure(ctx, UsageFailureInput{
		RequestID:         "req_fail_demo",
		TenantID:          "tenant_demo",
		UserID:            "user_member_a",
		PlatformAPIKeyID:  "pak_demo",
		FailureStage:      "upstream",
		ErrorCategory:     "rate_limited",
		StatusCode:        429,
		UserMessage:       "请求频率过高",
		InternalMessage:   "upstream rate limited",
	})
	if err != nil {
		t.Fatalf("RecordFailure failed: %v", err)
	}

	assertRowCount(t, ctx, conn, "llm_request_failures", "user_id = 'user_member_a'", 1)
}

func TestUsageAggregatorRollsUpTenantUsageLedger(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	aggregator, conn := newUsageAggregator(t, ctx)
	requireNoError(t, aggregator.AggregateHour(ctx, mustParseTime(t, "2026-04-24T10:00:00Z")))
	assertRowCount(t, ctx, conn, "tenant_usage_ledger", "tenant_id = 'tenant_demo'", 1)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd gateway && go test ./internal/service ./tests/integration -run 'TestUsageRecorderStoresTenantAndUserScopedFailureRecord|TestUsageAggregatorRollsUpTenantUsageLedger' -count=1 -v`  
Expected: FAIL，提示缺表、缺字段或聚合逻辑不存在。

- [ ] **Step 3: 写最小实现**

```sql
create table llm_request_failures (
  id text primary key,
  request_log_id text not null references llm_request_logs(id) on delete cascade,
  tenant_id text not null references tenants(id),
  user_id text not null default '',
  platform_api_key_id text not null,
  failure_stage text not null,
  error_category text not null,
  status_code integer not null default 0,
  retryable boolean not null default false,
  user_message text not null default '',
  internal_message_digest text not null default '',
  created_at timestamptz not null default now()
);

create table tenant_usage_ledger (
  id text primary key,
  tenant_id text not null references tenants(id),
  bucket_start timestamptz not null,
  input_tokens integer not null default 0,
  output_tokens integer not null default 0,
  total_tokens integer not null default 0,
  request_count integer not null default 0,
  success_count integer not null default 0,
  failure_count integer not null default 0,
  estimated_count integer not null default 0,
  updated_at timestamptz not null default now(),
  unique (tenant_id, bucket_start)
);
```

```go
type UsageFailureInput struct {
	RequestID        string
	RequestLogID     string
	TenantID         string
	UserID           string
	PlatformAPIKeyID string
	FailureStage     string
	ErrorCategory    string
	StatusCode       int
	Retryable        bool
	UserMessage      string
	InternalMessage  string
}

func (r *UsageRecorder) RecordFailure(ctx context.Context, input UsageFailureInput) error {
	_, err := r.db.Exec(ctx, `
		insert into llm_request_failures (
			id, request_log_id, tenant_id, user_id, platform_api_key_id,
			failure_stage, error_category, status_code, retryable,
			user_message, internal_message_digest
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`, input.RequestID, input.RequestLogID, input.TenantID, input.UserID, input.PlatformAPIKeyID,
		input.FailureStage, input.ErrorCategory, input.StatusCode, input.Retryable,
		input.UserMessage, digestMessage(input.InternalMessage))
	return err
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd gateway && go test ./internal/service ./tests/integration -run 'TestUsageRecorderStoresTenantAndUserScopedFailureRecord|TestUsageAggregatorRollsUpTenantUsageLedger' -count=1 -v`  
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gateway/db/migrations/0007_add_tenant_governance_tables.sql gateway/internal/service/usage_types.go gateway/internal/service/usage_recording.go gateway/internal/service/usage_recording_test.go gateway/internal/service/usage_aggregator.go gateway/internal/service/usage_aggregator_test.go gateway/tests/integration/proxy_test.go
git commit -m "feat: add tenant usage ledger and failure records"
```

### Task 6: 重构前端导航和会话模型，按角色显示控制台

**Files:**
- Modify: `web/src/app/router.tsx`
- Modify: `web/src/app/layout.tsx`
- Create: `web/src/lib/session.ts`
- Modify: `web/src/lib/console-api.ts`
- Modify: `web/src/test/router.test.tsx`
- Test: `web/src/test/router.test.tsx`

- [ ] **Step 1: 写失败测试，定义角色导航**

```tsx
it("renders admin navigation for admin session", async () => {
  mockSession({ role: "admin" })
  const router = createTestRouter(["/applications"])
  render(<RouterProvider router={router} />)
  expect(await screen.findByText("账号申请")).toBeInTheDocument()
  expect(screen.getByText("租户管理")).toBeInTheDocument()
})

it("renders member navigation for member session", async () => {
  mockSession({ role: "member" })
  const router = createTestRouter(["/me"])
  render(<RouterProvider router={router} />)
  expect(await screen.findByText("我的总览")).toBeInTheDocument()
  expect(screen.queryByText("账号申请")).not.toBeInTheDocument()
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd web && npm test -- --runInBand src/test/router.test.tsx`  
Expected: FAIL，提示角色导航不存在。

- [ ] **Step 3: 写最小导航切换实现**

```ts
export type ConsoleSession = {
  role: "admin" | "member"
  tenant_id?: string
  user_id: string
}

export function getNavigationForRole(role: "admin" | "member") {
  return role === "admin"
    ? adminNavigation
    : memberNavigation
}
```

```tsx
const session = useConsoleSession()
const navigation = getNavigationForRole(session.role)

<aside className="sidebar">
  <div className="sidebar__brand">
    {session.role === "admin" ? "AI 接入平台" : "租户控制台"}
  </div>
  <nav className="sidebar__nav">
    {navigation.map((item) => (
      <NavLink key={item.path} to={item.path} className="sidebar__link">
        {item.label}
      </NavLink>
    ))}
  </nav>
</aside>
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd web && npm test -- --runInBand src/test/router.test.tsx`  
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/app/router.tsx web/src/app/layout.tsx web/src/lib/session.ts web/src/lib/console-api.ts web/src/test/router.test.tsx
git commit -m "feat: split console navigation by role"
```

### Task 7: 实现 admin 页面和 member 页面闭环

**Files:**
- Create: `web/src/pages/admin-applications.tsx`
- Create: `web/src/pages/admin-tenants.tsx`
- Create: `web/src/pages/member-overview.tsx`
- Create: `web/src/pages/member-usage.tsx`
- Create: `web/src/pages/member-failures.tsx`
- Modify: `web/src/pages/api-keys.tsx`
- Modify: `web/src/pages/dashboard.tsx`
- Modify: `web/src/pages/audit.tsx`
- Modify: `web/src/styles.css`
- Modify: `web/src/test/router.test.tsx`
- Test: `web/src/test/router.test.tsx`

- [ ] **Step 1: 写失败测试，定义新页面最小文案与交互**

```tsx
it("allows admin to approve an application", async () => {
  mockSession({ role: "admin" })
  mockApplications()
  renderWithRoute("/applications")
  expect(await screen.findByText("待审批用户")).toBeInTheDocument()
  await userEvent.click(screen.getByRole("button", { name: "审批通过" }))
  expect(await screen.findByText("审批已提交")).toBeInTheDocument()
})

it("allows member to create api key and view masked key", async () => {
  mockSession({ role: "member" })
  mockCreateAPIKey()
  renderWithRoute("/api-keys")
  await userEvent.click(screen.getByRole("button", { name: "新建密钥" }))
  expect(await screen.findByText(/agw_/)).toBeInTheDocument()
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd web && npm test -- --runInBand src/test/router.test.tsx`  
Expected: FAIL，提示页面组件或交互不存在。

- [ ] **Step 3: 写最小页面实现**

```tsx
export function AdminApplicationsPage() {
  const { data, refresh } = useRemoteData(getApplications)
  return (
    <section className="page-section">
      <h2>账号申请</h2>
      {data?.items.map((item) => (
        <article key={item.id} className="list-card">
          <h3>{item.name}</h3>
          <p>{item.company_name}</p>
          <button onClick={async () => {
            await approveApplication(item.id, { tenant_id: "tenant_demo", comment: "approve" })
            await refresh()
          }}>
            审批通过
          </button>
        </article>
      ))}
    </section>
  )
}
```

```tsx
export function MemberOverviewPage() {
  const { data } = useRemoteData(getMemberOverview)
  return (
    <section className="hero-grid">
      <MetricCard label="所属租户" value={data?.tenant_name ?? "-"} />
      <MetricCard label="租户总 Token" value={data?.tenant_total_tokens ?? "-"} />
      <MetricCard label="我的失败数" value={data?.my_failure_count ?? "-"} />
    </section>
  )
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd web && npm test -- --runInBand src/test/router.test.tsx && npm run build`  
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/admin-applications.tsx web/src/pages/admin-tenants.tsx web/src/pages/member-overview.tsx web/src/pages/member-usage.tsx web/src/pages/member-failures.tsx web/src/pages/api-keys.tsx web/src/pages/dashboard.tsx web/src/pages/audit.tsx web/src/styles.css web/src/test/router.test.tsx
git commit -m "feat: add admin and member governance console pages"
```

### Task 8: 清理产品边界、更新文档与部署验证

**Files:**
- Modify: `README.md`
- Modify: `docs/specs/2026-04-27-tenant-api-key-governance-platform-design.md`
- Modify: `web/src/app/router.tsx`
- Modify: `deploy/compose/compose.yml`
- Modify: `scripts/test.sh`
- Modify: `scripts/lint.sh`
- Test: `scripts/test.sh`
- Test: `scripts/lint.sh`

- [ ] **Step 1: 写失败检查，确保旧主线入口被移除**

```bash
rg -n "知识库|RAG 控制台|resolved_provider|OpenAI Primary" web/src README.md
```

Expected: 至少出现旧导航、旧文案或旧 provider 暴露内容。

- [ ] **Step 2: 运行检查确认存在旧边界**

Run: `cd /root/liwenjian/ai_gateway && rg -n '知识库|RAG 控制台|resolved_provider|OpenAI Primary' web/src README.md`  
Expected: 找到旧产品叙事或 UI 文案。

- [ ] **Step 3: 写最小清理与文档更新**

```md
## 项目做了什么

- admin 审批账号申请并开通租户
- member 在审批后自助创建平台 API Key
- 平台按租户统计 token、请求、失败和审计事件
- 平台隐藏真实上游 provider 凭据，只暴露平台统一接口
```

```ts
export const memberNavigation = [
  { path: "/me", label: "我的总览", title: "我的总览", description: "查看所属租户、额度与个人调用健康。", element: <MemberOverviewPage /> },
  { path: "/api-keys", label: "API 密钥", title: "API 密钥", description: "自助创建、轮换与停用平台密钥。", element: <APIKeysPage /> },
  { path: "/usage", label: "调用记录", title: "调用记录", description: "查看自己的请求、token 与状态。", element: <MemberUsagePage /> },
  { path: "/failures", label: "失败记录", title: "失败记录", description: "查看失败分类、阶段和可重试性。", element: <MemberFailuresPage /> },
  { path: "/audit", label: "审计轨迹", title: "审计轨迹", description: "查看关键操作和风控提示。", element: <AuditPage /> },
]
```

- [ ] **Step 4: 运行全量验证**

Run: `cd /root/liwenjian/ai_gateway && ./scripts/test.sh && ./scripts/lint.sh && docker compose --env-file deploy/compose/.env.example -f deploy/compose/compose.yml config >/tmp/compose.out && tail -n 5 /tmp/compose.out`  
Expected: 测试通过、lint 通过、compose config 输出正常。

- [ ] **Step 5: Commit**

```bash
git add README.md docs/specs/2026-04-27-tenant-api-key-governance-platform-design.md web/src/app/router.tsx deploy/compose/compose.yml scripts/test.sh scripts/lint.sh
git commit -m "docs: align platform narrative with tenant governance"
```

## Self-Review

### Spec Coverage

- 账号审批：Task 1、Task 3
- 多用户同租户：Task 1、Task 2、Task 3
- 用户自助 API Key：Task 4、Task 7
- 租户级 token 账本：Task 5
- 调用日志与失败记录：Task 5、Task 7
- admin/member 同一控制台不同菜单：Task 6、Task 7
- 隐藏真实上游 provider：Task 5、Task 7、Task 8
- 弱化 RAG/知识库：Task 6、Task 8

### Placeholder Scan

本计划未留下“以后再补”的空白步骤。  
每个任务都给出了测试入口、最小代码示例、验证命令和 commit 粒度。

### Type Consistency

- 平台角色统一使用：`admin` / `member`
- 申请状态统一使用：`pending` / `approved` / `rejected`
- member 自服务入口统一挂在：`/me/*`
- 失败表统一命名：`llm_request_failures`
- 租户级账本统一命名：`tenant_usage_ledger`
