# Registration Password And Local Captcha Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为公开申请链路增加“注册时设置密码 + 本地图形验证码先验证后提交”的完整闭环，并让 admin 审批通过后的用户可直接使用注册密码登录控制台。

**Architecture:** 后端继续使用 Go + PostgreSQL，在 `account_applications` 中短暂保存 `password_hash`，新建 `captcha_challenges` 管理一次性验证码挑战和通过凭证。公开申请链路拆成“获取验证码”“校验验证码”“提交申请”三段，审批通过时将申请密码哈希迁移到正式 `users.password_hash`，审批拒绝或通过后都清空申请侧残留密码哈希。

**Tech Stack:** Go 1.22, Fiber, PostgreSQL, bcrypt, React 18, Vite, Vitest, Docker Compose

---

## File Map

### 数据库与 seed

- Create: `gateway/db/migrations/0011_add_registration_password_and_captcha.sql`
- Modify: `gateway/db/runtime_test.go`

### 后端验证码与申请服务

- Modify: `gateway/internal/service/console_service.go`
- Create: `gateway/internal/service/captcha_service.go`
- Create: `gateway/internal/service/captcha_service_test.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/service/postgres_console_service_test.go`
- Modify: `gateway/internal/store/auth_repository.go`
- Modify: `gateway/internal/store/auth_repository_test.go`

### HTTP 路由与 handler

- Modify: `gateway/internal/http/handlers/admin.go`
- Create: `gateway/internal/http/handlers/captcha.go`
- Modify: `gateway/internal/http/router.go`
- Modify: `gateway/internal/http/router_test.go`

### 前端申请页

- Modify: `web/src/lib/console-api.ts`
- Modify: `web/src/pages/application-form.tsx`
- Modify: `web/src/test/router.test.tsx`

### 文档

- Modify: `README.md`
- Modify: `docs/specs/2026-04-28-registration-password-and-local-captcha-design.md`

## Task 1: 扩展 schema 与公开申请契约

**Files:**
- Create: `gateway/db/migrations/0011_add_registration_password_and_captcha.sql`
- Modify: `gateway/internal/service/console_service.go`
- Modify: `gateway/internal/service/postgres_console_service_test.go`

- [ ] **Step 1: 先写 service 层失败测试，锁定新增字段和约束**

```go
func TestPostgresConsoleServiceCreateApplicationRequiresPasswordAndCaptchaToken(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	_, err := console.CreateApplication(ctx, service.CreateApplicationRequest{
		Email:       "new-user@example.com",
		Name:        "新用户",
		CompanyName: "New Co",
		UseCase:     "测试接入",
	})
	if err == nil {
		t.Fatal("expected CreateApplication to require password and captcha_pass_token")
	}

	var statusErr service.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", statusErr.Code)
	}
}
```

- [ ] **Step 2: 运行测试，确认当前实现失败**

Run:

```bash
cd gateway && go test ./internal/service -run TestPostgresConsoleServiceCreateApplicationRequiresPasswordAndCaptchaToken -v
```

Expected:

```text
FAIL: expected CreateApplication to require password and captcha_pass_token
```

- [ ] **Step 3: 增加 migration 与请求结构**

```sql
alter table account_applications
  add column password_hash text,
  add column email_normalized text not null default '';

update account_applications
set email_normalized = lower(trim(email))
where email_normalized = '';

alter table account_applications
  alter column email_normalized drop default;

create unique index account_applications_pending_email_normalized_idx
  on account_applications (email_normalized)
  where status = 'pending';

create table captcha_challenges (
  id text primary key,
  answer_hash text not null,
  status text not null check (status in ('issued', 'verified', 'consumed', 'expired', 'failed')),
  verify_attempts integer not null default 0 check (verify_attempts >= 0),
  max_attempts integer not null default 5 check (max_attempts > 0),
  pass_token_hash text not null default '',
  issued_ip text not null default '',
  issued_user_agent text not null default '',
  verified_at timestamptz,
  consumed_at timestamptz,
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
```

```go
type CreateApplicationRequest struct {
	Email            string `json:"email"`
	Name             string `json:"name"`
	CompanyName      string `json:"company_name"`
	UseCase          string `json:"use_case"`
	Password         string `json:"password"`
	CaptchaPassToken string `json:"captcha_pass_token"`
}

type CaptchaChallenge struct {
	CaptchaID string `json:"captcha_id"`
	ImageData string `json:"image_data"`
	ExpiresAt string `json:"expires_at"`
}

type VerifyCaptchaRequest struct {
	CaptchaID   string `json:"captcha_id"`
	CaptchaCode string `json:"captcha_code"`
}

type CaptchaPassResult struct {
	CaptchaPassToken string `json:"captcha_pass_token"`
	ExpiresAt        string `json:"expires_at"`
}
```

- [ ] **Step 4: 跑最小测试，确认契约已经收口**

Run:

```bash
cd gateway && go test ./internal/service -run TestPostgresConsoleServiceCreateApplicationRequiresPasswordAndCaptchaToken -v
```

Expected:

```text
PASS
```

- [ ] **Step 5: 提交**

```bash
git add gateway/db/migrations/0011_add_registration_password_and_captcha.sql \
  gateway/internal/service/console_service.go \
  gateway/internal/service/postgres_console_service_test.go
git commit -m "feat: add registration password and captcha schema"
```

## Task 2: 实现本地图形验证码服务与公开接口

**Files:**
- Create: `gateway/internal/service/captcha_service.go`
- Create: `gateway/internal/service/captcha_service_test.go`
- Create: `gateway/internal/http/handlers/captcha.go`
- Modify: `gateway/internal/http/router.go`
- Modify: `gateway/internal/http/router_test.go`
- Modify: `gateway/internal/service/console_service.go`

- [ ] **Step 1: 先写验证码 service 红灯测试**

```go
func TestPostgresCaptchaServiceIssueAndVerifyChallenge(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	challenge, err := console.IssueCaptcha(ctx, "127.0.0.1", "vitest")
	if err != nil {
		t.Fatalf("IssueCaptcha failed: %v", err)
	}
	if challenge.CaptchaID == "" || challenge.ImageData == "" || challenge.ExpiresAt == "" {
		t.Fatalf("expected populated challenge, got %+v", challenge)
	}

	_, err = console.VerifyCaptcha(ctx, service.VerifyCaptchaRequest{
		CaptchaID:   challenge.CaptchaID,
		CaptchaCode: "WRONG",
	})
	if err == nil {
		t.Fatal("expected VerifyCaptcha to reject wrong code")
	}
}
```

- [ ] **Step 2: 先写 HTTP 红灯测试**

```go
func TestConsoleCaptchaRoutesReturnChallengeAndPassToken(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ConsoleService: stubConsoleService{
			captchaChallenge: service.CaptchaChallenge{
				CaptchaID: "cap_demo",
				ImageData: "data:image/png;base64,AAAA",
				ExpiresAt: "2026-04-29T00:00:00Z",
			},
			captchaPassResult: service.CaptchaPassResult{
				CaptchaPassToken: "cp_demo",
				ExpiresAt:        "2026-04-29T00:00:00Z",
			},
		},
	})

	getReq := httptest.NewRequest(http.MethodGet, "/console/captcha", nil)
	getResp, err := app.Test(getReq)
	if err != nil {
		t.Fatalf("GET captcha failed: %v", err)
	}
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", getResp.StatusCode)
	}
}
```

- [ ] **Step 3: 运行测试，确认当前实现失败**

Run:

```bash
cd gateway && go test ./internal/service -run TestPostgresCaptchaServiceIssueAndVerifyChallenge -v
cd gateway && go test ./internal/http -run TestConsoleCaptchaRoutesReturnChallengeAndPassToken -v
```

Expected:

```text
FAIL: ConsoleService has no IssueCaptcha / VerifyCaptcha
FAIL: /console/captcha route missing
```

- [ ] **Step 4: 写最小实现**

```go
type CaptchaService interface {
	IssueCaptcha(ctx context.Context, ip string, userAgent string) (CaptchaChallenge, error)
	VerifyCaptcha(ctx context.Context, req VerifyCaptchaRequest) (CaptchaPassResult, error)
}

func (s postgresConsoleService) IssueCaptcha(ctx context.Context, ip string, userAgent string) (CaptchaChallenge, error) {
	answer := randomCaptchaCode(4)
	imageData, err := renderCaptchaDataURL(answer)
	if err != nil {
		return CaptchaChallenge{}, err
	}
	id := newCaptchaID()
	_, err = s.db.Exec(ctx, `
		insert into captcha_challenges (
			id, answer_hash, status, issued_ip, issued_user_agent, expires_at
		) values ($1, $2, 'issued', $3, $4, now() + interval '5 minutes')
	`, id, hashCaptchaValue(answer), ip, userAgent)
	if err != nil {
		return CaptchaChallenge{}, err
	}
	return CaptchaChallenge{
		CaptchaID: id,
		ImageData: imageData,
		ExpiresAt: time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339),
	}, nil
}

func ConsoleIssueCaptcha(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := console.IssueCaptcha(c.UserContext(), c.IP(), c.Get(fiber.HeaderUserAgent))
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}
```

```go
app.Get("/console/captcha", handlers.ConsoleIssueCaptcha(deps.ConsoleService))
app.Post("/console/captcha/verify", handlers.ConsoleVerifyCaptcha(deps.ConsoleService))
```

- [ ] **Step 5: 跑测试转绿**

Run:

```bash
cd gateway && go test ./internal/service -run TestPostgresCaptchaServiceIssueAndVerifyChallenge -v
cd gateway && go test ./internal/http -run TestConsoleCaptchaRoutesReturnChallengeAndPassToken -v
```

Expected:

```text
PASS
PASS
```

- [ ] **Step 6: 提交**

```bash
git add gateway/internal/service/console_service.go \
  gateway/internal/service/captcha_service.go \
  gateway/internal/service/captcha_service_test.go \
  gateway/internal/http/handlers/captcha.go \
  gateway/internal/http/router.go \
  gateway/internal/http/router_test.go
git commit -m "feat: add local captcha issue and verify flow"
```

## Task 3: 扩展申请创建逻辑，写入密码哈希并消费验证码通过凭证

**Files:**
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/service/postgres_console_service_test.go`
- Modify: `gateway/internal/store/auth_repository.go`
- Modify: `gateway/internal/store/auth_repository_test.go`

- [ ] **Step 1: 先写申请写库与冲突测试**

```go
func TestPostgresConsoleServiceCreateApplicationStoresPasswordHashAndConsumesCaptcha(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into captcha_challenges (
			id, answer_hash, status, pass_token_hash, expires_at
		) values (
			'cap_for_apply', 'sha256:ignored', 'verified', $1, now() + interval '5 minutes'
		)
	`, hashCaptchaValue("cp_valid")); err != nil {
		t.Fatalf("seed captcha challenge failed: %v", err)
	}

	result, err := console.CreateApplication(ctx, service.CreateApplicationRequest{
		Email:            "new-user@example.com",
		Name:             "新用户",
		CompanyName:      "New Co",
		UseCase:          "测试接入",
		Password:         "Example1234",
		CaptchaPassToken: "cp_valid",
	})
	if err != nil {
		t.Fatalf("CreateApplication failed: %v", err)
	}
	if result.Item.Status != "pending" {
		t.Fatalf("expected pending, got %q", result.Item.Status)
	}
}
```

```go
func TestPostgresConsoleServiceCreateApplicationRejectsExistingActiveUserEmail(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into users (id, email, name, role, status, password_hash)
		values ('user_existing', 'member@example.com', 'Existing', 'member', 'active', '$2a$10$abcdef');
	`); err != nil {
		t.Fatalf("seed user failed: %v", err)
	}

	_, err := console.CreateApplication(ctx, service.CreateApplicationRequest{
		Email:            "member@example.com",
		Name:             "新用户",
		CompanyName:      "New Co",
		UseCase:          "测试接入",
		Password:         "Example1234",
		CaptchaPassToken: "cp_valid",
	})
	if err == nil {
		t.Fatal("expected duplicate user email error")
	}
}
```

- [ ] **Step 2: 运行测试，确认当前实现失败**

Run:

```bash
cd gateway && go test ./internal/service -run 'TestPostgresConsoleServiceCreateApplicationStoresPasswordHashAndConsumesCaptcha|TestPostgresConsoleServiceCreateApplicationRejectsExistingActiveUserEmail' -v
```

Expected:

```text
FAIL: account_applications has no password_hash persistence
FAIL: captcha_pass_token not validated
FAIL: duplicate active user email not rejected
```

- [ ] **Step 3: 写最小实现**

```go
func (s postgresConsoleService) CreateApplication(ctx context.Context, req CreateApplicationRequest) (ApplicationMutationResult, error) {
	email := normalizeConsoleSubject(req.Email)
	password := strings.TrimSpace(req.Password)
	passToken := strings.TrimSpace(req.CaptchaPassToken)
	if password == "" {
		return ApplicationMutationResult{}, StatusError{Code: http.StatusBadRequest, Message: "password is required"}
	}
	if passToken == "" {
		return ApplicationMutationResult{}, StatusError{Code: http.StatusBadRequest, Message: "captcha_pass_token is required"}
	}
	if err := validateConsolePassword(password); err != nil {
		return ApplicationMutationResult{}, err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return ApplicationMutationResult{}, err
	}

	row := s.db.QueryRow(ctx, `
		with consumed_captcha as (
			update captcha_challenges
			set status = 'consumed', consumed_at = now(), updated_at = now()
			where pass_token_hash = $6
			  and status = 'verified'
			  and expires_at > now()
			returning id
		),
		inserted_application as (
			insert into account_applications (
				id, email, email_normalized, name, company_name, use_case, password_hash, status
			)
			select $1, $2, $3, $4, $5, $7, $8, 'pending'
			where exists (select 1 from consumed_captcha)
			returning id, email, name, company_name, use_case, status, created_at
		)
		select id, email, name, company_name, use_case, status, created_at
		from inserted_application;
	`, newApplicationID(), req.Email, email, req.Name, req.CompanyName, hashCaptchaValue(passToken), req.UseCase, string(passwordHash))

	item, err := scanApplicationItem(row)
	return ApplicationMutationResult{Item: item}, mapCreateApplicationError(err)
}
```

- [ ] **Step 4: 跑测试转绿**

Run:

```bash
cd gateway && go test ./internal/service -run 'TestPostgresConsoleServiceCreateApplicationStoresPasswordHashAndConsumesCaptcha|TestPostgresConsoleServiceCreateApplicationRejectsExistingActiveUserEmail' -v
```

Expected:

```text
PASS
PASS
```

- [ ] **Step 5: 提交**

```bash
git add gateway/internal/service/postgres_console_service.go \
  gateway/internal/service/postgres_console_service_test.go \
  gateway/internal/store/auth_repository.go \
  gateway/internal/store/auth_repository_test.go
git commit -m "feat: store registration password hash in applications"
```

## Task 4: 调整审批逻辑，迁移或清理申请密码哈希

**Files:**
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/service/postgres_console_service_test.go`
- Modify: `gateway/internal/service/auth_service_test.go`

- [ ] **Step 1: 先写审批通过与拒绝的红灯测试**

```go
func TestPostgresConsoleServiceApproveApplicationMovesPasswordHashIntoUser(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into account_applications (
			id, email, email_normalized, name, company_name, use_case, password_hash, status
		) values (
			'app_password_pending', 'pending@example.com', 'pending@example.com',
			'待审批用户', 'Pending Co', '测试接入', '$2a$10$abcdefghijklmnopqrstuv', 'pending'
		)
	`); err != nil {
		t.Fatalf("seed application failed: %v", err)
	}

	_, err := console.ApproveApplication(ctx, "app_password_pending", service.ApproveApplicationRequest{
		ActorID:  "user_admin_demo",
		Comment:  "approved",
		TenantID: "tenant_demo",
	})
	if err != nil {
		t.Fatalf("ApproveApplication failed: %v", err)
	}
}
```

```go
func TestPostgresConsoleServiceRejectApplicationClearsApplicationPasswordHash(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into account_applications (
			id, email, email_normalized, name, company_name, use_case, password_hash, status
		) values (
			'app_password_reject', 'reject@example.com', 'reject@example.com',
			'待拒绝用户', 'Reject Co', '测试接入', '$2a$10$abcdefghijklmnopqrstuv', 'pending'
		)
	`); err != nil {
		t.Fatalf("seed application failed: %v", err)
	}

	_, err := console.RejectApplication(ctx, "app_password_reject", service.RejectApplicationRequest{
		ActorID: "user_admin_demo",
		Comment: "rejected",
	})
	if err != nil {
		t.Fatalf("RejectApplication failed: %v", err)
	}
}
```

- [ ] **Step 2: 运行测试，确认当前实现失败**

Run:

```bash
cd gateway && go test ./internal/service -run 'TestPostgresConsoleServiceApproveApplicationMovesPasswordHashIntoUser|TestPostgresConsoleServiceRejectApplicationClearsApplicationPasswordHash' -v
```

Expected:

```text
FAIL: users.password_hash not populated from application
FAIL: application password_hash not cleared on reject
```

- [ ] **Step 3: 写最小实现**

```go
with updated_application as (
	update account_applications
	set
		status = 'approved',
		reviewer_id = $2,
		review_comment = $3,
		reviewed_at = now()
	where id = $1
	  and status = 'pending'
	  and password_hash is not null
	returning id, email, name, company_name, use_case, password_hash, status, created_at
),
upserted_user as (
	insert into users (id, email, name, role, status, password_hash)
	select $4, email, name, 'member', 'active', password_hash
	from updated_application
	on conflict (email) do update
	set
		name = excluded.name,
		status = 'active',
		password_hash = excluded.password_hash
	returning id
),
cleared_application_secret as (
	update account_applications
	set password_hash = null
	where id in (select id from updated_application)
)
```

```go
with updated_application as (
	update account_applications
	set
		status = 'rejected',
		reviewer_id = $2,
		review_comment = $3,
		reviewed_at = now(),
		password_hash = null
	where id = $1
	  and status = 'pending'
	returning id, email, name, company_name, use_case, status, created_at
)
```

- [ ] **Step 4: 增加一个登录级回归测试**

```go
func TestAuthenticateConsoleSessionUsesApprovedApplicationPasswordHash(t *testing.T) {
	authService := newTestAuthService(t, "console-session-secret")

	result, err := authService.AuthenticateConsoleSession(context.Background(), "pending@example.com", "Example1234")
	if err != nil {
		t.Fatalf("AuthenticateConsoleSession failed: %v", err)
	}
	if result.Email != "pending@example.com" {
		t.Fatalf("expected pending@example.com, got %q", result.Email)
	}
}
```

- [ ] **Step 5: 跑测试转绿**

Run:

```bash
cd gateway && go test ./internal/service -run 'TestPostgresConsoleServiceApproveApplicationMovesPasswordHashIntoUser|TestPostgresConsoleServiceRejectApplicationClearsApplicationPasswordHash' -v
cd gateway && go test ./internal/service -run TestAuthenticateConsoleSessionUsesApprovedApplicationPasswordHash -v
```

Expected:

```text
PASS
PASS
```

- [ ] **Step 6: 提交**

```bash
git add gateway/internal/service/postgres_console_service.go \
  gateway/internal/service/postgres_console_service_test.go \
  gateway/internal/service/auth_service_test.go
git commit -m "feat: migrate registration password on application review"
```

## Task 5: 改造前端申请页，先验证验证码再允许提交

**Files:**
- Modify: `web/src/lib/console-api.ts`
- Modify: `web/src/pages/application-form.tsx`
- Modify: `web/src/test/router.test.tsx`

- [ ] **Step 1: 先写前端红灯测试**

```tsx
test("申请页需要先验证验证码后才能提交", async () => {
  mockAnonymousSession();
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.toString();

    if (url === "/api/console/captcha" && !init?.method) {
      return new Response(JSON.stringify({
        captcha_id: "cap_demo",
        image_data: "data:image/png;base64,AAAA",
        expires_at: "2026-04-29T00:00:00Z",
      }), { status: 200, headers: { "Content-Type": "application/json" } });
    }

    if (url === "/api/console/captcha/verify" && init?.method === "POST") {
      return new Response(JSON.stringify({
        captcha_pass_token: "cp_demo",
        expires_at: "2026-04-29T00:00:00Z",
      }), { status: 200, headers: { "Content-Type": "application/json" } });
    }

    if (url === "/api/console/applications" && init?.method === "POST") {
      expect(JSON.parse(String(init.body))).toEqual({
        email: "new-user@example.com",
        name: "新用户",
        company_name: "New Co",
        use_case: "测试接入",
        password: "Example1234",
        captcha_pass_token: "cp_demo",
      });
      return new Response(JSON.stringify({
        item: {
          id: "app_new_pending",
          email: "new-user@example.com",
          name: "新用户",
          company_name: "New Co",
          use_case: "测试接入",
          status: "pending",
          created_at: "2026-04-28T16:00:00+08:00",
        },
      }), { status: 200, headers: { "Content-Type": "application/json" } });
    }

    throw new Error(`Unexpected fetch url: ${url}`);
  });

  vi.stubGlobal("fetch", fetchMock);
  render(<RouterProvider router={createTestRouter(["/apply"])} future={{ v7_startTransition: true }} />);

  expect(await screen.findByAltText("图形验证码")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "提交申请" })).toBeDisabled();
});
```

- [ ] **Step 2: 运行测试，确认当前实现失败**

Run:

```bash
cd web && npm test -- --runInBand src/test/router.test.tsx -t "申请页需要先验证验证码后才能提交"
```

Expected:

```text
FAIL: unable to find captcha controls
FAIL: submit button is not disabled before captcha verification
```

- [ ] **Step 3: 写最小实现**

```ts
export type CaptchaChallenge = {
  captcha_id: string;
  image_data: string;
  expires_at: string;
};

export type VerifyCaptchaPayload = {
  captcha_id: string;
  captcha_code: string;
};

export type CaptchaPassResult = {
  captcha_pass_token: string;
  expires_at: string;
};

export type CreateApplicationPayload = {
  email: string;
  name: string;
  company_name: string;
  use_case: string;
  password: string;
  captcha_pass_token: string;
};

export function issueCaptcha() {
  return requestJson<CaptchaChallenge>("/api/console/captcha");
}

export function verifyCaptcha(payload: VerifyCaptchaPayload) {
  return requestJson<CaptchaPassResult>("/api/console/captcha/verify", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}
```

```tsx
const [password, setPassword] = useState("");
const [confirmPassword, setConfirmPassword] = useState("");
const [captchaID, setCaptchaID] = useState("");
const [captchaImage, setCaptchaImage] = useState("");
const [captchaCode, setCaptchaCode] = useState("");
const [captchaPassToken, setCaptchaPassToken] = useState("");
const [captchaVerified, setCaptchaVerified] = useState(false);

const canSubmit =
  !submitting &&
  email.trim() !== "" &&
  name.trim() !== "" &&
  companyName.trim() !== "" &&
  useCase.trim() !== "" &&
  password.length >= 8 &&
  password === confirmPassword &&
  captchaVerified &&
  captchaPassToken !== "";
```

- [ ] **Step 4: 跑测试转绿**

Run:

```bash
cd web && npm test -- --runInBand src/test/router.test.tsx -t "申请页需要先验证验证码后才能提交"
```

Expected:

```text
PASS
```

- [ ] **Step 5: 提交**

```bash
git add web/src/lib/console-api.ts \
  web/src/pages/application-form.tsx \
  web/src/test/router.test.tsx
git commit -m "feat: add captcha gated registration form"
```

## Task 6: 全链路回归、文档更新与部署验证

**Files:**
- Modify: `README.md`
- Modify: `docs/specs/2026-04-28-registration-password-and-local-captcha-design.md`

- [ ] **Step 1: 补 README 中的公开注册说明**

```md
### 公开申请注册

未登录用户现在需要按以下流程申请：

1. 打开 `/apply`
2. 获取并验证本地图形验证码
3. 填写邮箱、姓名、公司、用途、密码
4. 提交申请进入 `pending`
5. admin 审批通过后，用户可直接用注册密码登录
```

- [ ] **Step 2: 如果实现偏离 spec，回写 spec**

```md
- 若最终实现把验证码最大尝试次数从 `5` 改为其他值，必须同步更新本 spec
- 若最终密码复杂度规则有调整，必须同步更新“密码规则”章节
```

- [ ] **Step 3: 跑后端全量验证**

Run:

```bash
cd gateway && go test ./...
```

Expected:

```text
ok  	github.com/example/ai_gateway/gateway/internal/http
ok  	github.com/example/ai_gateway/gateway/internal/service
ok  	github.com/example/ai_gateway/gateway/internal/store
```

- [ ] **Step 4: 跑前端回归与构建**

Run:

```bash
cd web && npm test -- --runInBand && npm run build
```

Expected:

```text
✓ src/test/router.test.tsx
✓ built in
```

- [ ] **Step 5: 跑真实部署验证**

Run:

```bash
docker compose --env-file deploy/compose/.env.local -f deploy/compose/compose.yml up -d --build gateway web
curl -sS http://127.0.0.1:32658/healthz
curl -sS http://127.0.0.1:31873/api/console/captcha
```

Expected:

```text
{"status":"ok"}
{"captcha_id":"...","image_data":"data:image/png;base64,...","expires_at":"..."}
```

- [ ] **Step 6: 做一条人工验收脚本**

```bash
1. 打开 http://127.0.0.1:31873/apply
2. 输入邮箱、姓名、公司、用途、密码、确认密码
3. 验证验证码，确认“提交申请”按钮从禁用变为可点击
4. 提交申请，确认页面显示 pending
5. admin 登录后在 /applications 审批通过
6. 回到登录页，用刚注册的邮箱和密码登录成功
```

- [ ] **Step 7: 提交**

```bash
git add README.md docs/specs/2026-04-28-registration-password-and-local-captcha-design.md
git commit -m "docs: finalize registration password and captcha rollout"
```

## Self-Review

- Spec coverage:
  - 密码在申请时保存：Task 1, Task 3
  - 本地图形验证码：Task 2, Task 5
  - 先验证验证码再允许注册：Task 2, Task 5
  - 审批通过直接登录：Task 4, Task 6
  - 审批通过或拒绝后清理申请密码哈希：Task 4
  - 同邮箱冲突与 `rejected` 可重申请：Task 3
- Placeholder scan:
  - 未使用占位符标记
  - 每个任务都给了明确文件、命令和最小代码片段
- Type consistency:
  - 公开申请请求统一使用 `password` 与 `captcha_pass_token`
  - 验证码对象统一使用 `CaptchaChallenge`、`VerifyCaptchaRequest`、`CaptchaPassResult`
  - 申请审批接口仍沿用现有 `ApproveApplicationRequest` / `RejectApplicationRequest`
