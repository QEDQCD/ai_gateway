package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	gatewaydb "github.com/example/ai_gateway/gateway/db"
	"github.com/example/ai_gateway/gateway/internal/secret"
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/bcrypt"
)

func TestPostgresConsoleServiceSystemStatus(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	payload, err := console.SystemStatus(ctx)
	if err != nil {
		t.Fatalf("SystemStatus failed: %v", err)
	}

	if payload.ConsoleStage != "控制台预览版" {
		t.Fatalf("expected console_stage 控制台预览版, got %q", payload.ConsoleStage)
	}
	if payload.RunMode != "数据库模式" {
		t.Fatalf("expected run_mode 数据库模式, got %q", payload.RunMode)
	}
	if payload.GatewayHealth != "告警" {
		t.Fatalf("expected gateway_health 告警, got %q", payload.GatewayHealth)
	}
	if payload.QuotaProtection != "已启用" {
		t.Fatalf("expected quota_protection 已启用, got %q", payload.QuotaProtection)
	}
	if payload.ConsoleEntry != "31873" {
		t.Fatalf("expected console_entry 31873, got %q", payload.ConsoleEntry)
	}
	if payload.GatewayAdminAPI != "32658" {
		t.Fatalf("expected gateway_admin_api 32658, got %q", payload.GatewayAdminAPI)
	}
	if len(payload.InternalServices) != 1 || payload.InternalServices[0] != "internal-search" {
		t.Fatalf("expected internal_services [internal-search], got %#v", payload.InternalServices)
	}
	if !containsString(payload.HiddenModules, "内部检索能力") || !containsString(payload.HiddenModules, "高级路由设置") {
		t.Fatalf("expected hidden_modules to include 内部检索能力 and 高级路由设置, got %#v", payload.HiddenModules)
	}
}

func TestPostgresConsoleServiceCreateAPIKey(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	result, err := console.CreateAPIKey(ctx, service.CreateAPIKeyRequest{
		TenantID: "tenant_demo",
		Name:     "prod-gateway-2",
		Scopes:   []string{"chat", "embeddings"},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	if strings.TrimSpace(result.RawKey) == "" {
		t.Fatal("expected created raw key")
	}
	if result.Item.Status != "启用" {
		t.Fatalf("expected status 启用, got %q", result.Item.Status)
	}

	var count int
	if err := conn.QueryRow(ctx, `select count(*) from platform_api_keys where id = $1 and tenant_id = $2 and status = 'active';`, result.Item.ID, "tenant_demo").Scan(&count); err != nil {
		t.Fatalf("QueryRow platform_api_keys failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected created key to persist, got count %d", count)
	}
}

func TestPostgresConsoleServiceRotateAPIKeyReturnsRawKeyOnce(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	result, err := console.RotateAPIKey(ctx, "pak_demo", service.RotateAPIKeyRequest{})
	if err != nil {
		t.Fatalf("RotateAPIKey failed: %v", err)
	}

	if strings.TrimSpace(result.RawKey) == "" {
		t.Fatal("expected rotated raw key")
	}
	if result.Item.Tenant != "tenant_demo" {
		t.Fatalf("expected tenant tenant_demo, got %q", result.Item.Tenant)
	}

	var oldStatus string
	if err := conn.QueryRow(ctx, `select status from platform_api_keys where id = 'pak_demo';`).Scan(&oldStatus); err != nil {
		t.Fatalf("QueryRow old platform_api_keys failed: %v", err)
	}
	if oldStatus != "disabled" {
		t.Fatalf("expected old key disabled, got %q", oldStatus)
	}
}

func TestPostgresConsoleServiceDeactivateAPIKey(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	result, err := console.DeactivateAPIKey(ctx, "pak_demo")
	if err != nil {
		t.Fatalf("DeactivateAPIKey failed: %v", err)
	}

	if result.Item.Status != "停用" {
		t.Fatalf("expected status 停用, got %q", result.Item.Status)
	}

	var status string
	if err := conn.QueryRow(ctx, `select status from platform_api_keys where id = 'pak_demo';`).Scan(&status); err != nil {
		t.Fatalf("QueryRow platform_api_keys failed: %v", err)
	}
	if status != "disabled" {
		t.Fatalf("expected disabled status in db, got %q", status)
	}
}

func TestPostgresConsoleServiceDeleteUnusedAPIKey(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	created, err := console.CreateAPIKey(ctx, service.CreateAPIKeyRequest{
		TenantID: "tenant_demo",
		Name:     "unused-delete-me",
		Scopes:   []string{"chat"},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	result, err := console.DeleteAPIKey(ctx, created.Item.ID)
	if err != nil {
		t.Fatalf("DeleteAPIKey failed: %v", err)
	}

	if result.Item.Status != "已删除" {
		t.Fatalf("expected status 已删除, got %q", result.Item.Status)
	}

	var count int
	if err := conn.QueryRow(ctx, `select count(*) from platform_api_keys where id = $1;`, created.Item.ID).Scan(&count); err != nil {
		t.Fatalf("QueryRow platform_api_keys failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected deleted key to be removed, got count %d", count)
	}
}

func TestPostgresConsoleServiceDeleteReferencedAPIKeyReturnsConflict(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	_, err := console.DeleteAPIKey(ctx, "pak_demo")
	if err == nil {
		t.Fatal("expected DeleteAPIKey to fail for referenced key")
	}

	var statusErr service.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Code != 409 {
		t.Fatalf("expected conflict status, got %d", statusErr.Code)
	}
}

func TestPostgresConsoleServiceRevealLegacyKeyMarksUnrecoverable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into platform_api_keys (
			id, tenant_id, name, key_hash, status, scopes, created_at, expires_at, secret_recoverable
		) values (
			'pak_legacy_only_hash',
			'tenant_demo',
			'legacy-only-hash',
			'sha256:legacy-only-hash',
			'active',
			ARRAY['chat'],
			now(),
			now() + interval '30 days',
			false
		);
	`); err != nil {
		t.Fatalf("seed legacy key failed: %v", err)
	}

	revealer, ok := console.(interface {
		RevealAPIKeySecret(context.Context, string) (service.APIKeySecretView, error)
	})
	if !ok {
		t.Fatal("expected console service to implement RevealAPIKeySecret")
	}

	secretView, err := revealer.RevealAPIKeySecret(ctx, "pak_legacy_only_hash")
	if err != nil {
		t.Fatalf("RevealAPIKeySecret failed: %v", err)
	}
	if secretView.Revealable {
		t.Fatal("expected legacy key to be unrecoverable")
	}
	if !secretView.LegacyUnrecoverable {
		t.Fatal("expected LegacyUnrecoverable to be true")
	}

	var action string
	var accessResult string
	if err := conn.QueryRow(ctx, `
		select action, access_result
		from api_key_secret_access_logs
		where api_key_id = 'pak_legacy_only_hash'
		order by created_at desc, id desc
		limit 1;
	`).Scan(&action, &accessResult); err != nil {
		t.Fatalf("QueryRow reveal api_key_secret_access_logs failed: %v", err)
	}
	if action != "reveal" {
		t.Fatalf("expected action %q, got %q", "reveal", action)
	}
	if accessResult != "allowed" {
		t.Fatalf("expected access_result %q, got %q", "allowed", accessResult)
	}
}

func TestPostgresConsoleServiceCopyAPIKeySecretWritesAllowedAuditLog(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	created, err := console.CreateAPIKey(service.ContextWithConsolePrincipal(ctx, service.ConsolePrincipal{
		UserID: "user_admin_demo",
		Role:   "admin",
	}), service.CreateAPIKeyRequest{
		TenantID: "tenant_demo",
		Name:     "copy-audit-admin",
		Scopes:   []string{"chat"},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	copier, ok := console.(interface {
		CopyAPIKeySecret(context.Context, string, string, string) (service.APIKeySecretView, error)
	})
	if !ok {
		t.Fatal("expected console service to implement CopyAPIKeySecret")
	}

	copyCtx := service.ContextWithConsolePrincipal(ctx, service.ConsolePrincipal{
		UserID: "user_admin_demo",
		Role:   "admin",
	})
	secretView, err := copier.CopyAPIKeySecret(copyCtx, created.Item.ID, "203.0.113.10", "console-copy-test")
	if err != nil {
		t.Fatalf("CopyAPIKeySecret failed: %v", err)
	}
	if !secretView.Revealable {
		t.Fatal("expected copied key to be revealable")
	}
	if secretView.FullKey == "" {
		t.Fatal("expected FullKey to be populated")
	}

	var actorUserID string
	var actorRole string
	var action string
	var accessResult string
	var ipAddress string
	var userAgent string
	if err := conn.QueryRow(ctx, `
		select actor_user_id, actor_role, action, access_result, ip_address, user_agent
		from api_key_secret_access_logs
		where api_key_id = $1
		order by created_at desc, id desc
		limit 1;
	`, created.Item.ID).Scan(&actorUserID, &actorRole, &action, &accessResult, &ipAddress, &userAgent); err != nil {
		t.Fatalf("QueryRow api_key_secret_access_logs failed: %v", err)
	}
	if actorUserID != "user_admin_demo" {
		t.Fatalf("expected actor_user_id %q, got %q", "user_admin_demo", actorUserID)
	}
	if actorRole != "admin" {
		t.Fatalf("expected actor_role %q, got %q", "admin", actorRole)
	}
	if action != "copy" {
		t.Fatalf("expected action %q, got %q", "copy", action)
	}
	if accessResult != "allowed" {
		t.Fatalf("expected access_result %q, got %q", "allowed", accessResult)
	}
	if ipAddress != "203.0.113.10" {
		t.Fatalf("expected ip_address %q, got %q", "203.0.113.10", ipAddress)
	}
	if userAgent != "console-copy-test" {
		t.Fatalf("expected user_agent %q, got %q", "console-copy-test", userAgent)
	}
}

func TestPostgresConsoleServiceApplicationsReturnsPendingRows(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `delete from account_applications`); err != nil {
		t.Fatalf("delete seeded applications failed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		insert into account_applications (
			id,
			email,
			name,
			company_name,
			use_case,
			status,
			reviewer_id,
			review_comment,
			reviewed_at,
			created_at
		) values
			(
				'app_demo_pending',
				'pending@example.com',
				'待审批用户',
				'Pending Co',
				'租户接入',
				'pending',
				null,
				'',
				null,
				timestamptz '2026-04-30T10:30:00Z'
			),
			(
				'app_demo_rejected',
				'rejected@example.com',
				'已拒绝用户',
				'Rejected Co',
				'压测脚本',
				'rejected',
				'user_admin_demo',
				'seed reject',
				timestamptz '2026-04-29T08:20:00Z',
				timestamptz '2026-04-29T08:15:00Z'
			),
			(
				'app_demo_approved',
				'approved@example.com',
				'已审批用户',
				'Approved Co',
				'内部知识问答',
				'approved',
				'user_admin_demo',
				'seed approve',
				timestamptz '2026-04-28T02:05:00Z',
				timestamptz '2026-04-28T02:00:00Z'
			)
	`); err != nil {
		t.Fatalf("insert applications failed: %v", err)
	}

	payload, err := console.Applications(ctx)
	if err != nil {
		t.Fatalf("Applications failed: %v", err)
	}

	if len(payload.Items) != 3 {
		t.Fatalf("expected 3 application rows, got %d", len(payload.Items))
	}

	if payload.Items[0].ID != "app_demo_pending" {
		t.Fatalf("expected newest application id %q, got %q", "app_demo_pending", payload.Items[0].ID)
	}
	if payload.Items[1].ID != "app_demo_rejected" {
		t.Fatalf("expected second application id %q, got %q", "app_demo_rejected", payload.Items[1].ID)
	}
	if payload.Items[2].ID != "app_demo_approved" {
		t.Fatalf("expected third application id %q, got %q", "app_demo_approved", payload.Items[2].ID)
	}

	first := payload.Items[0]
	if first.Email != "pending@example.com" {
		t.Fatalf("expected email %q, got %q", "pending@example.com", first.Email)
	}
	if first.Name != "待审批用户" {
		t.Fatalf("expected name %q, got %q", "待审批用户", first.Name)
	}
	if first.CompanyName != "Pending Co" {
		t.Fatalf("expected company_name %q, got %q", "Pending Co", first.CompanyName)
	}
	if first.UseCase != "租户接入" {
		t.Fatalf("expected use_case %q, got %q", "租户接入", first.UseCase)
	}
	if first.Status != "pending" {
		t.Fatalf("expected status %q, got %q", "pending", first.Status)
	}
	if first.CreatedAt != "2026-04-30T18:30:00+08:00" {
		t.Fatalf("expected created_at %q, got %q", "2026-04-30T18:30:00+08:00", first.CreatedAt)
	}
	if payload.Items[1].CreatedAt != "2026-04-29T16:15:00+08:00" {
		t.Fatalf("expected second created_at %q, got %q", "2026-04-29T16:15:00+08:00", payload.Items[1].CreatedAt)
	}
	if payload.Items[2].CreatedAt != "2026-04-28T10:00:00+08:00" {
		t.Fatalf("expected third created_at %q, got %q", "2026-04-28T10:00:00+08:00", payload.Items[2].CreatedAt)
	}
}

func TestPostgresConsoleServiceCreateApplicationPersistsPendingRow(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into captcha_challenges (
			id,
			answer_hash,
			status,
			pass_token_hash,
			expires_at
		) values (
			'cap_for_apply',
			$1,
			'verified',
			$2,
			now() + interval '5 minutes'
		)
	`, testHashCaptchaValue("ABCD"), testHashCaptchaValue("cp_valid")); err != nil {
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

	if result.Item.ID == "" {
		t.Fatal("expected application id to be generated")
	}
	if result.Item.Status != "pending" {
		t.Fatalf("expected status %q, got %q", "pending", result.Item.Status)
	}
	if result.Item.Email != "new-user@example.com" {
		t.Fatalf("expected email %q, got %q", "new-user@example.com", result.Item.Email)
	}
	if result.Item.Name != "新用户" {
		t.Fatalf("expected name %q, got %q", "新用户", result.Item.Name)
	}
	if result.Item.CompanyName != "New Co" {
		t.Fatalf("expected company_name %q, got %q", "New Co", result.Item.CompanyName)
	}
	if result.Item.UseCase != "测试接入" {
		t.Fatalf("expected use_case %q, got %q", "测试接入", result.Item.UseCase)
	}
	if result.Item.CreatedAt == "" {
		t.Fatal("expected created_at to be populated")
	}

	var status string
	var passwordHash *string
	var reviewerID *string
	var reviewedAt *time.Time
	if err := conn.QueryRow(ctx, `
		select status, password_hash, reviewer_id, reviewed_at
		from account_applications
		where id = $1
	`, result.Item.ID).Scan(&status, &passwordHash, &reviewerID, &reviewedAt); err != nil {
		t.Fatalf("select created application failed: %v", err)
	}
	if status != "pending" {
		t.Fatalf("expected stored status %q, got %q", "pending", status)
	}
	if passwordHash == nil || strings.TrimSpace(*passwordHash) == "" {
		t.Fatal("expected password_hash to be stored")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*passwordHash), []byte("Example1234")); err != nil {
		t.Fatalf("expected stored password_hash to match Example1234: %v", err)
	}
	if reviewerID != nil {
		t.Fatalf("expected reviewer_id to be null, got %q", *reviewerID)
	}
	if reviewedAt != nil {
		t.Fatal("expected reviewed_at to be null")
	}

	var captchaStatus string
	if err := conn.QueryRow(ctx, `
		select status
		from captcha_challenges
		where id = 'cap_for_apply'
	`).Scan(&captchaStatus); err != nil {
		t.Fatalf("select captcha challenge failed: %v", err)
	}
	if captchaStatus != "consumed" {
		t.Fatalf("expected captcha status %q, got %q", "consumed", captchaStatus)
	}
}

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
		Password:    "",
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

func TestPostgresConsoleServiceCreateApplicationRejectsExistingActiveUserEmail(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into captcha_challenges (
			id,
			answer_hash,
			status,
			pass_token_hash,
			expires_at
		) values (
			'cap_duplicate_user',
			$1,
			'verified',
			$2,
			now() + interval '5 minutes'
		)
	`, testHashCaptchaValue("EFGH"), testHashCaptchaValue("cp_existing_user")); err != nil {
		t.Fatalf("seed captcha challenge failed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		insert into users (id, email, name, role, status, password_hash)
		values ('user_existing_login', 'member@example.com', 'Existing', 'member', 'active', '$2a$10$abcdefghijklmnopqrstuv')
	`); err != nil {
		t.Fatalf("seed existing user failed: %v", err)
	}

	_, err := console.CreateApplication(ctx, service.CreateApplicationRequest{
		Email:            "member@example.com",
		Name:             "新用户",
		CompanyName:      "New Co",
		UseCase:          "测试接入",
		Password:         "Example1234",
		CaptchaPassToken: "cp_existing_user",
	})
	if err == nil {
		t.Fatal("expected duplicate active user email error")
	}

	var statusErr service.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", statusErr.Code)
	}
}

func TestPostgresConsoleServiceApproveApplicationRequiresTenantID(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into account_applications (
			id,
			email,
			email_normalized,
			name,
			company_name,
			use_case,
			status
		) values (
			'app_pending_missing_tenant',
			'missing-tenant@example.com',
			'missing-tenant@example.com',
			'缺少租户用户',
			'Missing Tenant Co',
			'租户接入',
			'pending'
		)
	`); err != nil {
		t.Fatalf("insert pending application failed: %v", err)
	}

	_, err := console.ApproveApplication(ctx, "app_pending_missing_tenant", service.ApproveApplicationRequest{
		ActorID: "user_admin_demo",
		Comment: "approved without tenant",
	})
	if err == nil {
		t.Fatal("expected ApproveApplication to require tenant_id")
	}

	var statusErr service.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Code != 400 {
		t.Fatalf("expected bad request status, got %d", statusErr.Code)
	}
	if statusErr.Message != "tenant_id is required" {
		t.Fatalf("expected message %q, got %q", "tenant_id is required", statusErr.Message)
	}

	var applicationStatus string
	if err := conn.QueryRow(ctx, `
		select status
		from account_applications
		where id = 'app_pending_missing_tenant'
	`).Scan(&applicationStatus); err != nil {
		t.Fatalf("select application status failed: %v", err)
	}
	if applicationStatus != "pending" {
		t.Fatalf("expected application to remain pending, got %q", applicationStatus)
	}
}

func TestPostgresConsoleServiceApproveApplicationCreatesUserMembershipAndAudit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("Example1234"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword failed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		insert into users (id, email, name, role, status)
		values (
			'user_pending_existing',
			'service-pending@example.com',
			'旧名字',
			'member',
			'disabled'
		)
	`); err != nil {
		t.Fatalf("seed existing user failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into tenant_memberships (id, tenant_id, user_id, role, status)
		values (
			'tm_pending_existing',
			'tenant_demo',
			'user_pending_existing',
			'member',
			'disabled'
		)
	`); err != nil {
		t.Fatalf("seed existing membership failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into account_applications (
			id,
			email,
			email_normalized,
			name,
			company_name,
			use_case,
			password_hash,
			status,
			created_at
		) values (
			'app_service_pending',
			'service-pending@example.com',
			'service-pending@example.com',
			'服务层待审批用户',
			'Service Co',
			'租户接入',
			$1,
			'pending',
			timestamptz '2026-04-25T01:02:03Z'
		)
	`, string(passwordHash)); err != nil {
		t.Fatalf("seed approve application failed: %v", err)
	}

	result, err := console.ApproveApplication(ctx, "app_service_pending", service.ApproveApplicationRequest{
		ActorID:  "user_admin_demo",
		Comment:  "approved via service",
		TenantID: "tenant_demo",
	})
	if err != nil {
		t.Fatalf("ApproveApplication failed: %v", err)
	}

	if result.Item.ID != "app_service_pending" {
		t.Fatalf("expected item id %q, got %q", "app_service_pending", result.Item.ID)
	}
	if result.Item.Email != "service-pending@example.com" {
		t.Fatalf("expected item email %q, got %q", "service-pending@example.com", result.Item.Email)
	}
	if result.Item.Name != "服务层待审批用户" {
		t.Fatalf("expected item name %q, got %q", "服务层待审批用户", result.Item.Name)
	}
	if result.Item.CompanyName != "Service Co" {
		t.Fatalf("expected item company_name %q, got %q", "Service Co", result.Item.CompanyName)
	}
	if result.Item.UseCase != "租户接入" {
		t.Fatalf("expected item use_case %q, got %q", "租户接入", result.Item.UseCase)
	}
	if result.Item.Status != "approved" {
		t.Fatalf("expected item status %q, got %q", "approved", result.Item.Status)
	}
	if result.Item.CreatedAt != "2026-04-25T09:02:03+08:00" {
		t.Fatalf("expected item created_at %q, got %q", "2026-04-25T09:02:03+08:00", result.Item.CreatedAt)
	}

	var applicationStatus string
	var applicationPasswordHash *string
	var reviewerID string
	var reviewComment string
	var reviewedAt time.Time
	if err := conn.QueryRow(ctx, `
		select status, password_hash, reviewer_id, review_comment, reviewed_at
		from account_applications
		where id = 'app_service_pending'
	`).Scan(&applicationStatus, &applicationPasswordHash, &reviewerID, &reviewComment, &reviewedAt); err != nil {
		t.Fatalf("select approved application failed: %v", err)
	}
	if applicationStatus != "approved" {
		t.Fatalf("expected application status %q, got %q", "approved", applicationStatus)
	}
	if applicationPasswordHash != nil {
		t.Fatal("expected approved application password_hash to be cleared")
	}
	if reviewerID != "user_admin_demo" {
		t.Fatalf("expected reviewer_id %q, got %q", "user_admin_demo", reviewerID)
	}
	if reviewComment != "approved via service" {
		t.Fatalf("expected review_comment %q, got %q", "approved via service", reviewComment)
	}
	if reviewedAt.IsZero() {
		t.Fatal("expected reviewed_at to be set")
	}

	var userID string
	var userName string
	var userRole string
	var userStatus string
	var userPasswordHash string
	if err := conn.QueryRow(ctx, `
		select id, name, role, status, password_hash
		from users
		where email = 'service-pending@example.com'
	`).Scan(&userID, &userName, &userRole, &userStatus, &userPasswordHash); err != nil {
		t.Fatalf("select approved user failed: %v", err)
	}
	if userID != "user_pending_existing" {
		t.Fatalf("expected existing user id %q, got %q", "user_pending_existing", userID)
	}
	if userName != "服务层待审批用户" {
		t.Fatalf("expected updated user name %q, got %q", "服务层待审批用户", userName)
	}
	if userRole != "member" {
		t.Fatalf("expected user role %q, got %q", "member", userRole)
	}
	if userStatus != "active" {
		t.Fatalf("expected user status %q, got %q", "active", userStatus)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(userPasswordHash), []byte("Example1234")); err != nil {
		t.Fatalf("expected approved user password hash to match Example1234: %v", err)
	}

	var membershipCount int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from tenant_memberships
		where tenant_id = 'tenant_demo'
			and user_id = 'user_pending_existing'
			and role = 'member'
			and status = 'active'
	`).Scan(&membershipCount); err != nil {
		t.Fatalf("select tenant membership failed: %v", err)
	}
	if membershipCount != 1 {
		t.Fatalf("expected 1 active membership, got %d", membershipCount)
	}

	var auditCount int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from audit_events
		where actor_type = 'admin'
			and actor_user_id = 'user_admin_demo'
			and tenant_id = 'tenant_demo'
			and event_type = 'application_approved'
			and target_type = 'account_application'
			and target_id = 'app_service_pending'
			and detail = 'approved via service'
	`).Scan(&auditCount); err != nil {
		t.Fatalf("select audit event count failed: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 approval audit event, got %d", auditCount)
	}
}

func TestPostgresConsoleServiceRejectApplicationUpdatesStatusAndAudit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("Example1234"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword failed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		insert into account_applications (
			id,
			email,
			email_normalized,
			name,
			company_name,
			use_case,
			password_hash,
			status,
			created_at
		) values (
			'app_service_reject',
			'reject@example.com',
			'reject@example.com',
			'待拒绝用户',
			'Reject Co',
			'测试接入',
			$1,
			'pending',
			timestamptz '2026-04-25T01:02:03Z'
		);
	`, string(passwordHash)); err != nil {
		t.Fatalf("seed reject application scenario failed: %v", err)
	}

	result, err := console.RejectApplication(ctx, "app_service_reject", service.RejectApplicationRequest{
		ActorID: "user_admin_demo",
		Comment: "rejected via service",
	})
	if err != nil {
		t.Fatalf("RejectApplication failed: %v", err)
	}

	if result.Item.Status != "rejected" {
		t.Fatalf("expected item status %q, got %q", "rejected", result.Item.Status)
	}

	var applicationStatus string
	var applicationPasswordHash *string
	var reviewerID string
	var reviewComment string
	var reviewedAt time.Time
	if err := conn.QueryRow(ctx, `
		select status, password_hash, reviewer_id, review_comment, reviewed_at
		from account_applications
		where id = 'app_service_reject'
	`).Scan(&applicationStatus, &applicationPasswordHash, &reviewerID, &reviewComment, &reviewedAt); err != nil {
		t.Fatalf("select rejected application failed: %v", err)
	}
	if applicationStatus != "rejected" {
		t.Fatalf("expected application status %q, got %q", "rejected", applicationStatus)
	}
	if applicationPasswordHash != nil {
		t.Fatal("expected rejected application password_hash to be cleared")
	}
	if reviewerID != "user_admin_demo" {
		t.Fatalf("expected reviewer_id %q, got %q", "user_admin_demo", reviewerID)
	}
	if reviewComment != "rejected via service" {
		t.Fatalf("expected review_comment %q, got %q", "rejected via service", reviewComment)
	}
	if reviewedAt.IsZero() {
		t.Fatal("expected reviewed_at to be set")
	}

	var auditCount int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from audit_events
		where actor_type = 'admin'
			and actor_user_id = 'user_admin_demo'
			and event_type = 'application_rejected'
			and target_type = 'account_application'
			and target_id = 'app_service_reject'
			and detail = 'rejected via service'
	`).Scan(&auditCount); err != nil {
		t.Fatalf("select rejection audit event count failed: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 rejection audit event, got %d", auditCount)
	}
}

func TestPostgresConsoleServiceAuditUsesUsageLogsAndEvents(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		update llm_request_logs
		set
			request_started_at = now() - interval '30 minutes',
			request_completed_at = now() - interval '30 minutes' + interval '182 milliseconds',
			created_at = now() - interval '30 minutes'
		where id = 'llmreq_demo_001';

		update llm_request_events
		set created_at = now() - interval '30 minutes' + interval '182 milliseconds'
		where id = 'llmevt_demo_001';
	`); err != nil {
		t.Fatalf("refresh recent usage seed failed: %v", err)
	}

	payload, err := console.Audit(ctx)
	if err != nil {
		t.Fatalf("Audit failed: %v", err)
	}

	if len(payload.Metrics) != 4 {
		t.Fatalf("expected 4 metrics, got %d", len(payload.Metrics))
	}
	if len(payload.Events) == 0 {
		t.Fatal("expected audit events from llm_request_events")
	}
	if len(payload.Items) == 0 {
		t.Fatal("expected audit items from llm_request_logs")
	}
	if payload.Items[0].RequestModel == "" {
		t.Fatal("expected request_model to be populated")
	}
	if payload.Items[0].UsageSource == "" {
		t.Fatal("expected usage_source to be populated")
	}
}

func TestPostgresConsoleServiceAuditFallsBackToAuditLogsWhenUsageDataMissing(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `delete from llm_request_events; delete from llm_request_logs;`); err != nil {
		t.Fatalf("delete usage tables failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into audit_logs (
			tenant_id,
			platform_api_key_id,
			requested_model,
			endpoint,
			status_code,
			provider_display_name,
			latency_ms,
			created_at
		) values (
			'tenant_demo',
			'pak_demo',
			'qwen-flash',
			'/v1/chat/completions',
			200,
			'DashScope 主路由',
			88,
			now()
		);
	`); err != nil {
		t.Fatalf("insert fallback audit log failed: %v", err)
	}

	payload, err := console.Audit(ctx)
	if err != nil {
		t.Fatalf("Audit failed: %v", err)
	}

	if len(payload.Items) == 0 {
		t.Fatal("expected fallback audit items")
	}
	if payload.Items[0].RequestModel != "-" {
		t.Fatalf("expected fallback request_model '-', got %q", payload.Items[0].RequestModel)
	}
	if payload.Items[0].UsageSource != "审计回退" {
		t.Fatalf("expected fallback usage_source 审计回退, got %q", payload.Items[0].UsageSource)
	}
}

func TestPostgresConsoleServiceUsageOverview(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	payload, err := console.UsageOverview(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T09:00:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageOverview failed: %v", err)
	}

	if payload.TotalRequests != 2 {
		t.Fatalf("expected total_requests 2, got %d", payload.TotalRequests)
	}
	if payload.SuccessRate != "50.00%" {
		t.Fatalf("expected success_rate 50.00%%, got %q", payload.SuccessRate)
	}
	if payload.TotalTokens != "188" {
		t.Fatalf("expected total_tokens 188, got %q", payload.TotalTokens)
	}
	if payload.AverageLatency != "139 ms" {
		t.Fatalf("expected average_latency 139 ms, got %q", payload.AverageLatency)
	}
	if payload.EstimatedShare != "50.00%" {
		t.Fatalf("expected estimated_share 50.00%%, got %q", payload.EstimatedShare)
	}

	var logCount int
	if err := conn.QueryRow(ctx, `select count(*) from llm_request_logs`).Scan(&logCount); err != nil {
		t.Fatalf("QueryRow llm_request_logs failed: %v", err)
	}
	if logCount < 2 {
		t.Fatalf("expected demo data to include llm_request_logs, got %d rows", logCount)
	}
}

func TestPostgresConsoleServiceUsageLatencyWall(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		update llm_request_logs
		set
			request_started_at = now() - interval '30 minutes',
			request_completed_at = now() - interval '30 minutes' + interval '182 milliseconds',
			created_at = now() - interval '30 minutes'
		where id = 'llmreq_demo_001';

		update llm_request_logs
		set
			request_started_at = now() - interval '20 minutes',
			request_completed_at = now() - interval '20 minutes' + interval '95 milliseconds',
			created_at = now() - interval '20 minutes'
		where id = 'llmreq_demo_002';
	`); err != nil {
		t.Fatalf("refresh latency wall seed failed: %v", err)
	}

	payload, err := console.UsageLatencyWall(ctx, service.UsageQuery{Window: "24h"})
	if err != nil {
		t.Fatalf("UsageLatencyWall failed: %v", err)
	}

	if payload.WindowLabel != "最近 24 小时" {
		t.Fatalf("expected 最近 24 小时, got %q", payload.WindowLabel)
	}
	if len(payload.Buckets) == 0 {
		t.Fatal("expected latency wall buckets")
	}
	if len(payload.Lanes) == 0 {
		t.Fatal("expected latency wall lanes")
	}
	if payload.Lanes[0].Model == "" {
		t.Fatal("expected lane model to be populated")
	}
	if len(payload.Lanes[0].Cells) != len(payload.Buckets) {
		t.Fatalf("expected cells to align with buckets, got %d cells and %d buckets", len(payload.Lanes[0].Cells), len(payload.Buckets))
	}
}

func TestPostgresConsoleServiceUsageOverviewFiltersByErrorCategory(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	payload, err := console.UsageOverview(ctx, service.UsageQuery{
		From:          mustParseUsageTime(t, "2026-04-24T09:00:00Z"),
		To:            mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
		ErrorCategory: "rate_limit",
	})
	if err != nil {
		t.Fatalf("UsageOverview failed: %v", err)
	}

	if payload.TotalRequests != 1 {
		t.Fatalf("expected total_requests 1, got %d", payload.TotalRequests)
	}
	if payload.SuccessRate != "0.00%" {
		t.Fatalf("expected success_rate 0.00%%, got %q", payload.SuccessRate)
	}
	if payload.TotalTokens != "16" {
		t.Fatalf("expected total_tokens 16, got %q", payload.TotalTokens)
	}
	if payload.AverageLatency != "95 ms" {
		t.Fatalf("expected average_latency 95 ms, got %q", payload.AverageLatency)
	}
	if payload.EstimatedShare != "100.00%" {
		t.Fatalf("expected estimated_share 100.00%%, got %q", payload.EstimatedShare)
	}
}

func TestPostgresConsoleServiceUsageOverviewHonorsPartialHourWindow(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	payload, err := console.UsageOverview(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T10:05:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T10:30:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageOverview failed: %v", err)
	}

	if payload.TotalRequests != 1 {
		t.Fatalf("expected total_requests 1, got %d", payload.TotalRequests)
	}
	if payload.TotalTokens != "16" {
		t.Fatalf("expected total_tokens 16, got %q", payload.TotalTokens)
	}
	if payload.AverageLatency != "95 ms" {
		t.Fatalf("expected average_latency 95 ms, got %q", payload.AverageLatency)
	}
}

func TestPostgresConsoleServiceUsageOverviewUsesRequestStartedAtWindow(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_delayed',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'success',
			200,
			120,
			18,
			6,
			24,
			'',
			'',
			timestamptz '2026-04-24T10:15:00Z',
			timestamptz '2026-04-24T10:15:00.120Z',
			timestamptz '2026-04-24T12:15:00Z'
		)
	`); err != nil {
		t.Fatalf("insert delayed llm_request_logs failed: %v", err)
	}

	payload, err := console.UsageOverview(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T10:10:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T10:20:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageOverview failed: %v", err)
	}

	if payload.TotalRequests != 1 {
		t.Fatalf("expected total_requests 1, got %d", payload.TotalRequests)
	}
	if payload.TotalTokens != "24" {
		t.Fatalf("expected total_tokens 24, got %q", payload.TotalTokens)
	}
}

func TestPostgresConsoleServiceUsageTrends(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_011',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'success',
			200,
			205,
			90,
			30,
			120,
			'',
			'',
			timestamptz '2026-04-24T11:00:00Z',
			timestamptz '2026-04-24T11:00:00.205Z',
			timestamptz '2026-04-24T11:00:01Z'
		)
	`); err != nil {
		t.Fatalf("insert llm_request_logs failed: %v", err)
	}

	payload, err := console.UsageTrends(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T09:00:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T12:00:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageTrends failed: %v", err)
	}

	if len(payload.Requests) != 2 {
		t.Fatalf("expected 2 request trend points, got %d", len(payload.Requests))
	}
	if payload.Requests[0].Value != "2" {
		t.Fatalf("expected first request trend value 2, got %q", payload.Requests[0].Value)
	}
	if payload.Tokens[0].Value != "188" {
		t.Fatalf("expected first token trend value 188, got %q", payload.Tokens[0].Value)
	}
	if payload.Success[0].Value != "50.00%" {
		t.Fatalf("expected first success trend value 50.00%%, got %q", payload.Success[0].Value)
	}
	if payload.Requests[1].Value != "1" {
		t.Fatalf("expected second request trend value 1, got %q", payload.Requests[1].Value)
	}
	if payload.Tokens[1].Value != "120" {
		t.Fatalf("expected second token trend value 120, got %q", payload.Tokens[1].Value)
	}
	if payload.Success[1].Value != "100.00%" {
		t.Fatalf("expected second success trend value 100.00%%, got %q", payload.Success[1].Value)
	}
}

func TestPostgresConsoleServiceUsageTrendsFiltersByErrorCategory(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	payload, err := console.UsageTrends(ctx, service.UsageQuery{
		From:          mustParseUsageTime(t, "2026-04-24T09:00:00Z"),
		To:            mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
		ErrorCategory: "rate_limit",
	})
	if err != nil {
		t.Fatalf("UsageTrends failed: %v", err)
	}

	if len(payload.Requests) != 1 {
		t.Fatalf("expected 1 request trend point, got %d", len(payload.Requests))
	}
	if payload.Requests[0].Value != "1" {
		t.Fatalf("expected request trend value 1, got %q", payload.Requests[0].Value)
	}
	if payload.Tokens[0].Value != "16" {
		t.Fatalf("expected token trend value 16, got %q", payload.Tokens[0].Value)
	}
	if payload.Success[0].Value != "0.00%" {
		t.Fatalf("expected success trend value 0.00%%, got %q", payload.Success[0].Value)
	}
}

func TestPostgresConsoleServiceUsageTrendsHonorsPartialHourWindow(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	payload, err := console.UsageTrends(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T10:05:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T10:30:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageTrends failed: %v", err)
	}

	if len(payload.Requests) != 1 {
		t.Fatalf("expected 1 request trend point, got %d", len(payload.Requests))
	}
	if payload.Requests[0].Value != "1" {
		t.Fatalf("expected request trend value 1, got %q", payload.Requests[0].Value)
	}
	if payload.Tokens[0].Value != "16" {
		t.Fatalf("expected token trend value 16, got %q", payload.Tokens[0].Value)
	}
	if payload.Success[0].Value != "0.00%" {
		t.Fatalf("expected success trend value 0.00%%, got %q", payload.Success[0].Value)
	}
}

func TestPostgresConsoleServiceUsageFailures(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_003',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'upstream_error',
			502,
			410,
			32,
			8,
			40,
			'bad_gateway',
			'upstream returned 502',
			timestamptz '2026-04-24T10:30:00Z',
			timestamptz '2026-04-24T10:30:00.410Z',
			timestamptz '2026-04-24T10:30:01Z'
		)
	`); err != nil {
		t.Fatalf("insert llm_request_logs failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into llm_request_events (
			id,
			request_log_id,
			tenant_id,
			event_type,
			usage_source,
			usage_status,
			status_code,
			detail,
			created_at
		) values (
			'llmevt_demo_003',
			'llmreq_demo_003',
			'tenant_demo',
			'request_failed',
			'upstream',
			'upstream_error',
			502,
			'demo upstream 502',
			timestamptz '2026-04-24T10:30:00.410Z'
		)
	`); err != nil {
		t.Fatalf("insert llm_request_events failed: %v", err)
	}

	payload, err := console.UsageFailures(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T09:00:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageFailures failed: %v", err)
	}

	if len(payload.Breakdown) < 2 {
		t.Fatalf("expected at least 2 failure buckets, got %d", len(payload.Breakdown))
	}
	if !containsFailureBucket(payload.Breakdown, "上游服务异常", "1 次") {
		t.Fatalf("expected breakdown to contain 上游服务异常=1 次, got %#v", payload.Breakdown)
	}
	if len(payload.RecentEvents) == 0 {
		t.Fatal("expected recent_events to be returned")
	}
	if payload.RecentEvents[0] == "" {
		t.Fatal("expected recent event summary to be non-empty")
	}
	if !strings.Contains(payload.RecentEvents[0], "上游服务异常") {
		t.Fatalf("expected recent event summary to use failure-category label, got %q", payload.RecentEvents[0])
	}
}

func TestPostgresConsoleServiceUsageFailuresIncludesUsagePublishFailedEvents(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_publish_fail',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'success',
			200,
			188,
			60,
			20,
			80,
			'',
			'',
			timestamptz '2026-04-24T10:20:00Z',
			timestamptz '2026-04-24T10:20:00.188Z',
			timestamptz '2026-04-24T10:20:01Z'
		)
	`); err != nil {
		t.Fatalf("insert publish-fail llm_request_logs failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into llm_request_events (
			id,
			request_log_id,
			tenant_id,
			event_type,
			usage_source,
			usage_status,
			status_code,
			detail,
			created_at
		) values (
			'llmevt_demo_publish_fail',
			'llmreq_demo_publish_fail',
			'tenant_demo',
			'usage_publish_failed',
			'upstream',
			'success',
			200,
			'demo publish retry exhausted',
			timestamptz '2026-04-24T10:20:00.188Z'
		)
	`); err != nil {
		t.Fatalf("insert publish-fail llm_request_events failed: %v", err)
	}

	payload, err := console.UsageFailures(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T10:00:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageFailures failed: %v", err)
	}

	if !containsRecentEvent(payload.RecentEvents, "计量事件投递失败") {
		t.Fatalf("expected recent_events to include usage publish failure, got %#v", payload.RecentEvents)
	}
	if !containsRecentEvent(payload.RecentEvents, "网关内部错误 · 用户调用 gpt-4o-mini 已完成，但计量事件投递失败") {
		t.Fatalf("expected recent_events to categorize usage publish failure with meaningful detail, got %#v", payload.RecentEvents)
	}
}

func TestPostgresConsoleServiceUsageFailuresUsesRequestStartedAtWindow(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_delayed_fail',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'upstream_error',
			502,
			320,
			20,
			4,
			24,
			'bad_gateway',
			'delayed persistence',
			timestamptz '2026-04-24T10:12:00Z',
			timestamptz '2026-04-24T10:12:00.320Z',
			timestamptz '2026-04-24T12:12:00Z'
		)
	`); err != nil {
		t.Fatalf("insert delayed failure log failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into llm_request_events (
			id,
			request_log_id,
			tenant_id,
			event_type,
			usage_source,
			usage_status,
			status_code,
			detail,
			created_at
		) values (
			'llmevt_demo_delayed_fail',
			'llmreq_demo_delayed_fail',
			'tenant_demo',
			'request_failed',
			'upstream',
			'upstream_error',
			502,
			'delayed event persistence',
			timestamptz '2026-04-24T12:12:00.320Z'
		)
	`); err != nil {
		t.Fatalf("insert delayed failure event failed: %v", err)
	}

	payload, err := console.UsageFailures(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T10:10:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T10:20:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageFailures failed: %v", err)
	}

	if !containsRecentEvent(payload.RecentEvents, "delayed event persistence") {
		t.Fatalf("expected recent_events to use request_started_at window, got %#v", payload.RecentEvents)
	}
}

func TestPostgresConsoleServiceUsageFailuresMergesEquivalentLabels(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_rate_limited',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'rate_limited',
			429,
			140,
			10,
			0,
			10,
			'rate_limited',
			'provider throttled',
			timestamptz '2026-04-24T10:25:00Z',
			timestamptz '2026-04-24T10:25:00.140Z',
			timestamptz '2026-04-24T10:25:01Z'
		)
	`); err != nil {
		t.Fatalf("insert rate-limited failure log failed: %v", err)
	}

	payload, err := console.UsageFailures(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T10:00:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageFailures failed: %v", err)
	}

	if !containsFailureBucket(payload.Breakdown, "限流", "2 次") {
		t.Fatalf("expected equivalent rate-limit labels to merge, got %#v", payload.Breakdown)
	}
}

func TestPostgresConsoleServiceUsageOverviewFiltersByMergedFailureCategory(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_rate_limit_alias',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'rate_limited',
			429,
			140,
			10,
			0,
			10,
			'rate_limited',
			'provider throttled',
			timestamptz '2026-04-24T10:25:00Z',
			timestamptz '2026-04-24T10:25:00.140Z',
			timestamptz '2026-04-24T10:25:01Z'
		)
	`); err != nil {
		t.Fatalf("insert aliased failure log failed: %v", err)
	}

	payload, err := console.UsageOverview(ctx, service.UsageQuery{
		From:          mustParseUsageTime(t, "2026-04-24T10:00:00Z"),
		To:            mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
		ErrorCategory: "rate_limit",
	})
	if err != nil {
		t.Fatalf("UsageOverview failed: %v", err)
	}

	if payload.TotalRequests != 2 {
		t.Fatalf("expected merged failure category to include 2 requests, got %d", payload.TotalRequests)
	}
	if payload.TotalTokens != "26" {
		t.Fatalf("expected merged failure category total_tokens 26, got %q", payload.TotalTokens)
	}
}

func TestPostgresConsoleServiceUsageFailuresFiltersByMergedFailureCategory(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_rate_limit_filter_alias',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'rate_limited',
			429,
			140,
			10,
			0,
			10,
			'rate_limited',
			'provider throttled',
			timestamptz '2026-04-24T10:25:00Z',
			timestamptz '2026-04-24T10:25:00.140Z',
			timestamptz '2026-04-24T10:25:01Z'
		)
	`); err != nil {
		t.Fatalf("insert aliased failure log failed: %v", err)
	}

	payload, err := console.UsageFailures(ctx, service.UsageQuery{
		From:          mustParseUsageTime(t, "2026-04-24T10:00:00Z"),
		To:            mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
		ErrorCategory: "rate_limit",
	})
	if err != nil {
		t.Fatalf("UsageFailures failed: %v", err)
	}

	if !containsFailureBucket(payload.Breakdown, "限流", "2 次") {
		t.Fatalf("expected merged failure filter to keep breakdown consistent, got %#v", payload.Breakdown)
	}
}

func TestPostgresConsoleServiceUsageOverviewFiltersByDisplayFailureCategory(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values
			(
				'llmreq_demo_timeout_display',
				'tenant_demo',
				'pak_demo',
				'demo key',
				'provider_openai_demo',
				'route:provider_openai_demo:default',
				'/v1/chat/completions',
				'gpt-4o-mini',
				'gpt-4o-mini',
				'upstream',
				'timeout',
				504,
				900,
				10,
				0,
				10,
				'',
				'timed out',
				timestamptz '2026-04-24T10:35:00Z',
				timestamptz '2026-04-24T10:35:00.900Z',
				timestamptz '2026-04-24T10:35:01Z'
			),
			(
				'llmreq_demo_upstream_rate_limited_display',
				'tenant_demo',
				'pak_demo',
				'demo key',
				'provider_openai_demo',
				'route:provider_openai_demo:default',
				'/v1/chat/completions',
				'gpt-4o-mini',
				'gpt-4o-mini',
				'upstream',
				'failed',
				429,
				180,
				12,
				0,
				12,
				'upstream_rate_limited',
				'provider throttled',
				timestamptz '2026-04-24T10:36:00Z',
				timestamptz '2026-04-24T10:36:00.180Z',
				timestamptz '2026-04-24T10:36:01Z'
			)
	`); err != nil {
		t.Fatalf("insert display-category logs failed: %v", err)
	}

	timeoutPayload, err := console.UsageOverview(ctx, service.UsageQuery{
		From:          mustParseUsageTime(t, "2026-04-24T10:30:00Z"),
		To:            mustParseUsageTime(t, "2026-04-24T10:40:00Z"),
		ErrorCategory: "上游超时",
	})
	if err != nil {
		t.Fatalf("UsageOverview timeout filter failed: %v", err)
	}
	if timeoutPayload.TotalRequests != 1 {
		t.Fatalf("expected 上游超时 filter to include timeout status rows, got %d", timeoutPayload.TotalRequests)
	}

	rateLimitedPayload, err := console.UsageOverview(ctx, service.UsageQuery{
		From:          mustParseUsageTime(t, "2026-04-24T10:30:00Z"),
		To:            mustParseUsageTime(t, "2026-04-24T10:40:00Z"),
		ErrorCategory: "上游限流",
	})
	if err != nil {
		t.Fatalf("UsageOverview upstream rate limit filter failed: %v", err)
	}
	if rateLimitedPayload.TotalRequests != 1 {
		t.Fatalf("expected 上游限流 filter to include upstream_rate_limited rows, got %d", rateLimitedPayload.TotalRequests)
	}
}

func TestPostgresConsoleServiceUsageOverviewDoesNotLeakUpstreamErrorIntoServiceExceptionFilter(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_upstream_error_display',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'upstream_error',
			502,
			220,
			10,
			0,
			10,
			'',
			'generic upstream error',
			timestamptz '2026-04-24T10:37:00Z',
			timestamptz '2026-04-24T10:37:00.220Z',
			timestamptz '2026-04-24T10:37:01Z'
		)
	`); err != nil {
		t.Fatalf("insert upstream_error log failed: %v", err)
	}

	payload, err := console.UsageOverview(ctx, service.UsageQuery{
		From:          mustParseUsageTime(t, "2026-04-24T10:30:00Z"),
		To:            mustParseUsageTime(t, "2026-04-24T10:40:00Z"),
		ErrorCategory: "上游服务异常",
	})
	if err != nil {
		t.Fatalf("UsageOverview failed: %v", err)
	}
	if payload.TotalRequests != 0 {
		t.Fatalf("expected 上游服务异常 filter not to include generic upstream_error rows, got %d", payload.TotalRequests)
	}
}

func TestPostgresConsoleServiceUsageRequests(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	payload, err := console.UsageRequests(ctx, service.UsageQuery{
		From:   mustParseUsageTime(t, "2026-04-24T09:00:00Z"),
		To:     mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
		Status: "rate_limited",
	})
	if err != nil {
		t.Fatalf("UsageRequests failed: %v", err)
	}

	if len(payload.Items) != 1 {
		t.Fatalf("expected 1 request item, got %d", len(payload.Items))
	}
	item := payload.Items[0]
	if item.RequestID != "llmreq_demo_002" {
		t.Fatalf("expected request_id llmreq_demo_002, got %q", item.RequestID)
	}
	if item.Tenant != "tenant_demo" {
		t.Fatalf("expected tenant tenant_demo, got %q", item.Tenant)
	}
	if item.Endpoint != "/v1/embeddings" {
		t.Fatalf("expected endpoint /v1/embeddings, got %q", item.Endpoint)
	}
	if item.Model != "text-embedding-3-small" {
		t.Fatalf("expected model text-embedding-3-small, got %q", item.Model)
	}
	if item.Status != "限流" {
		t.Fatalf("expected status 限流, got %q", item.Status)
	}
	if item.TotalTokens != "16" {
		t.Fatalf("expected total_tokens 16, got %q", item.TotalTokens)
	}
	if item.Latency != "95 ms" {
		t.Fatalf("expected latency 95 ms, got %q", item.Latency)
	}
	if item.UsageSource != "估算" {
		t.Fatalf("expected usage_source 估算, got %q", item.UsageSource)
	}
}

func TestPostgresConsoleServiceUsageRequestsUsesRequestStartedAtWindow(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_delayed_request',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'success',
			200,
			90,
			14,
			3,
			17,
			'',
			'',
			timestamptz '2026-04-24T10:18:00Z',
			timestamptz '2026-04-24T10:18:00.090Z',
			timestamptz '2026-04-24T12:18:00Z'
		)
	`); err != nil {
		t.Fatalf("insert delayed request log failed: %v", err)
	}

	payload, err := console.UsageRequests(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T10:15:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T10:20:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageRequests failed: %v", err)
	}

	if len(payload.Items) != 1 {
		t.Fatalf("expected 1 request item, got %d", len(payload.Items))
	}
	if payload.Items[0].RequestID != "llmreq_demo_delayed_request" {
		t.Fatalf("expected delayed request to be selected by request_started_at, got %q", payload.Items[0].RequestID)
	}
}

func newUsageConsoleService(t *testing.T, ctx context.Context) (service.ConsoleService, *pgx.Conn) {
	t.Helper()

	container, dsn := startUsagePostgresContainer(ctx, t)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	pool, err := gatewaydb.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("OpenPostgres failed: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := gatewaydb.ApplyMigrations(ctx, pool); err != nil {
		t.Fatalf("ApplyMigrations failed: %v", err)
	}
	for _, statement := range gatewaydb.RuntimeSeedStatements() {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("conn.Exec seed failed: %v", err)
		}
	}

	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("secret.NewCodec failed: %v", err)
	}

	return service.NewPostgresConsoleService(conn, nil, nil, nil, "", codec), conn
}

func mustParseUsageTime(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("time.Parse failed: %v", err)
	}
	return parsed
}

func testHashCaptchaValue(value string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(value))))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func startUsagePostgresContainer(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
	t.Helper()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       "gateway_test",
				"POSTGRES_USER":     "postgres",
				"POSTGRES_PASSWORD": "postgres",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("GenericContainer failed: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container.Host failed: %v", err)
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("container.MappedPort failed: %v", err)
	}

	dsn := "postgres://postgres:postgres@" + host + ":" + port.Port() + "/gateway_test?sslmode=disable"
	return container, dsn
}

func containsFailureBucket(items []service.UsageFailureBucket, label string, value string) bool {
	for _, item := range items {
		if item.Label == label && item.Value == value {
			return true
		}
	}
	return false
}

func containsRecentEvent(items []string, fragment string) bool {
	for _, item := range items {
		if strings.Contains(item, fragment) {
			return true
		}
	}
	return false
}

func containsString(items []string, expected string) bool {
	for _, item := range items {
		if item == expected {
			return true
		}
	}
	return false
}
