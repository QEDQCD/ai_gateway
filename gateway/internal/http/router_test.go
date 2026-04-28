package http_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/ai_gateway/gateway/internal/domain"
	apphttp "github.com/example/ai_gateway/gateway/internal/http"
	"github.com/example/ai_gateway/gateway/internal/service"
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	if string(body) != `{"status":"ok"}` {
		t.Fatalf("expected body %q, got %q", `{"status":"ok"}`, string(body))
	}
}

func TestRootRouteReturnsOK(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	if string(body) != `{"status":"ok"}` {
		t.Fatalf("expected body %q, got %q", `{"status":"ok"}`, string(body))
	}
}

func TestServiceBasicAuthDoesNotProtectRootOrHealthRoutes(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
	})

	rootReq := httptest.NewRequest(http.MethodGet, "/", nil)
	rootResp, err := app.Test(rootReq)
	if err != nil {
		t.Fatalf("app.Test root failed: %v", err)
	}
	if rootResp.StatusCode != http.StatusOK {
		t.Fatalf("expected root status %d, got %d", http.StatusOK, rootResp.StatusCode)
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthResp, err := app.Test(healthReq)
	if err != nil {
		t.Fatalf("app.Test health failed: %v", err)
	}
	if healthResp.StatusCode != http.StatusOK {
		t.Fatalf("expected health status %d, got %d", http.StatusOK, healthResp.StatusCode)
	}
}

func TestServiceBasicAuthProtectsAdminRoutes(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService:      stubConsoleService{},
	})

	unauthorizedReq := httptest.NewRequest(http.MethodGet, "/admin/api-keys", nil)
	unauthorizedResp, err := app.Test(unauthorizedReq)
	if err != nil {
		t.Fatalf("app.Test unauthorized failed: %v", err)
	}
	if unauthorizedResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized status %d, got %d", http.StatusUnauthorized, unauthorizedResp.StatusCode)
	}

	authorizedReq := httptest.NewRequest(http.MethodGet, "/admin/api-keys", nil)
	authorizedReq.SetBasicAuth("test-console-user", "test-console-password")
	authorizedResp, err := app.Test(authorizedReq)
	if err != nil {
		t.Fatalf("app.Test authorized failed: %v", err)
	}
	if authorizedResp.StatusCode != http.StatusOK {
		t.Fatalf("expected authorized status %d, got %d", http.StatusOK, authorizedResp.StatusCode)
	}
}

func TestConsoleLoginRouteReturnsSessionPayload(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		AuthService: stubConsoleAuthService{
			loginResult: service.ConsoleLoginResult{
				Token:     "console_session_token",
				UserID:    "user_member_a",
				Email:     "member-a@example.com",
				Name:      "租户用户 A",
				Role:      "member",
				TenantID:  "tenant_demo",
				ExpiresAt: "2026-04-29T00:00:00Z",
			},
		},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/console/session/login",
		strings.NewReader(`{"email":"member-a@example.com","password":"secret"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	expected := `{"token":"console_session_token","user_id":"user_member_a","email":"member-a@example.com","name":"租户用户 A","role":"member","tenant_id":"tenant_demo","expires_at":"2026-04-29T00:00:00Z"}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
}

func TestAdminRoutesRequireConsoleSessionWhenEnabled(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername:   "test-console-user",
		ServiceAuthPassword:   "test-console-password",
		ConsoleSessionEnabled: true,
		AuthService: stubConsoleAuthService{
			sessionPrincipal: service.ConsolePrincipal{
				UserID: "user_admin",
				Email:  "admin@example.com",
				Role:   "admin",
			},
		},
		ConsoleService: stubConsoleService{
			systemStatus: service.ConsoleSystemStatus{
				ConsoleStage: "控制台预览版",
			},
		},
	})

	missingSessionReq := httptest.NewRequest(http.MethodGet, "/admin/system/status", nil)
	missingSessionReq.SetBasicAuth("test-console-user", "test-console-password")

	missingSessionResp, err := app.Test(missingSessionReq)
	if err != nil {
		t.Fatalf("app.Test missing session failed: %v", err)
	}
	if missingSessionResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected missing session status %d, got %d", http.StatusUnauthorized, missingSessionResp.StatusCode)
	}

	authorizedReq := httptest.NewRequest(http.MethodGet, "/admin/system/status", nil)
	authorizedReq.SetBasicAuth("test-console-user", "test-console-password")
	authorizedReq.Header.Set("X-Console-Session", "console_session_token")

	authorizedResp, err := app.Test(authorizedReq)
	if err != nil {
		t.Fatalf("app.Test authorized failed: %v", err)
	}
	if authorizedResp.StatusCode != http.StatusOK {
		t.Fatalf("expected authorized status %d, got %d", http.StatusOK, authorizedResp.StatusCode)
	}
}

func TestAdminSystemStatusRouteReturnsConsoleData(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			systemStatus: service.ConsoleSystemStatus{
				ConsoleStage:     "控制台预览版",
				RunMode:          "数据库模式",
				GatewayHealth:    "健康",
				QuotaProtection:  "已启用",
				ConsoleEntry:     "31873",
				GatewayAdminAPI:  "32658",
				InternalServices: []string{"internal-search"},
				HiddenModules:    []string{"内部检索能力", "高级路由设置"},
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/system/status", nil)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	expected := `{"console_stage":"控制台预览版","run_mode":"数据库模式","gateway_health":"健康","quota_protection":"已启用","console_entry":"31873","gateway_admin_api":"32658","internal_services":["internal-search"],"hidden_modules":["内部检索能力","高级路由设置"]}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
}

func TestAdminApplicationsRouteReturnsConsoleData(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			applications: service.ApplicationsPageData{
				Items: []service.ApplicationItem{
					{
						ID:          "app_demo_pending",
						Email:       "pending@example.com",
						Name:        "待审批用户",
						CompanyName: "Pending Co",
						UseCase:     "租户接入",
						Status:      "pending",
						CreatedAt:   "2026-04-30T18:30:00+08:00",
					},
					{
						ID:          "app_demo_rejected",
						Email:       "rejected@example.com",
						Name:        "已拒绝用户",
						CompanyName: "Rejected Co",
						UseCase:     "压测脚本",
						Status:      "rejected",
						CreatedAt:   "2026-04-24T17:43:00+08:00",
					},
				},
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/applications", nil)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	expected := `{"items":[{"id":"app_demo_pending","email":"pending@example.com","name":"待审批用户","company_name":"Pending Co","use_case":"租户接入","status":"pending","created_at":"2026-04-30T18:30:00+08:00"},{"id":"app_demo_rejected","email":"rejected@example.com","name":"已拒绝用户","company_name":"Rejected Co","use_case":"压测脚本","status":"rejected","created_at":"2026-04-24T17:43:00+08:00"}]}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
}

func TestAdminAPIKeysRouteReturnsConsoleData(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			apiKeys: service.APIKeysPageData{
				Items: []service.APIKeyItem{
					{
						ID:         "pak_demo",
						Name:       "prod-gateway",
						Tenant:     "tenant_alpha",
						Status:     "active",
						Scopes:     []string{"chat", "rag"},
						LastUsedAt: "2026-04-23T09:42:00Z",
					},
				},
				CredentialMode: "平台密钥与上游凭据分离，支持 BYOK 扩展",
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/api-keys", nil)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	expected := `{"items":[{"id":"pak_demo","name":"prod-gateway","tenant":"tenant_alpha","status":"active","scopes":["chat","rag"],"last_used_at":"2026-04-23T09:42:00Z"}],"credential_mode":"平台密钥与上游凭据分离，支持 BYOK 扩展"}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
}

func TestAdminCreateAPIKeyRouteReturnsConsoleData(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			apiKeyMutationResult: service.APIKeyMutationResult{
				Item: service.APIKeyItem{
					ID:         "pak_new",
					Name:       "prod-gateway-2",
					Tenant:     "tenant_alpha",
					Status:     "启用",
					Scopes:     []string{"chat", "embeddings"},
					LastUsedAt: "2026-04-24T12:00:00+08:00",
				},
				RawKey: "agw_secret_value",
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/api-keys", strings.NewReader(`{"tenant_id":"tenant_alpha","name":"prod-gateway-2","scopes":["chat","embeddings"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	expected := `{"item":{"id":"pak_new","name":"prod-gateway-2","tenant":"tenant_alpha","status":"启用","scopes":["chat","embeddings"],"last_used_at":"2026-04-24T12:00:00+08:00"},"raw_key":"agw_secret_value"}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
}

func TestAdminRotateAPIKeyRouteReturnsConsoleData(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			apiKeyMutationResult: service.APIKeyMutationResult{
				Item: service.APIKeyItem{
					ID:         "pak_rotated",
					Name:       "prod-gateway",
					Tenant:     "tenant_alpha",
					Status:     "启用",
					Scopes:     []string{"chat", "rag", "embeddings"},
					LastUsedAt: "2026-04-24T12:01:00+08:00",
				},
				RawKey: "agw_rotated_secret",
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/api-keys/pak_live_console/rotate", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	expected := `{"item":{"id":"pak_rotated","name":"prod-gateway","tenant":"tenant_alpha","status":"启用","scopes":["chat","rag","embeddings"],"last_used_at":"2026-04-24T12:01:00+08:00"},"raw_key":"agw_rotated_secret"}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
}

func TestAdminDeactivateAPIKeyRouteReturnsConsoleData(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			apiKeyMutationResult: service.APIKeyMutationResult{
				Item: service.APIKeyItem{
					ID:         "pak_live_console",
					Name:       "prod-gateway",
					Tenant:     "tenant_alpha",
					Status:     "停用",
					Scopes:     []string{"chat", "rag", "embeddings"},
					LastUsedAt: "2026-04-24T12:01:00+08:00",
				},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/api-keys/pak_live_console/deactivate", nil)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	expected := `{"item":{"id":"pak_live_console","name":"prod-gateway","tenant":"tenant_alpha","status":"停用","scopes":["chat","rag","embeddings"],"last_used_at":"2026-04-24T12:01:00+08:00"}}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
}

func TestAdminDeleteAPIKeyRouteReturnsConsoleData(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			apiKeyMutationResult: service.APIKeyMutationResult{
				Item: service.APIKeyItem{
					ID:         "pak_unused",
					Name:       "unused-key",
					Tenant:     "tenant_alpha",
					Status:     "已删除",
					Scopes:     []string{"chat"},
					LastUsedAt: "2026-04-24T12:01:00+08:00",
				},
			},
		},
	})

	req := httptest.NewRequest(http.MethodDelete, "/admin/api-keys/pak_unused", nil)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	expected := `{"item":{"id":"pak_unused","name":"unused-key","tenant":"tenant_alpha","status":"已删除","scopes":["chat"],"last_used_at":"2026-04-24T12:01:00+08:00"}}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
}

func TestAdminApproveApplicationCreatesUserMembershipAndAudit(t *testing.T) {
	t.Parallel()

	var capturedID string
	var capturedReq service.ApproveApplicationRequest

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			approveApplicationIDRef:  &capturedID,
			approveApplicationReqRef: &capturedReq,
			applicationMutation: service.ApplicationMutationResult{
				Item: service.ApplicationItem{
					ID:          "app_router_pending",
					Email:       "router-pending@example.com",
					Name:        "路由待审批用户",
					CompanyName: "Router Co",
					UseCase:     "租户接入",
					Status:      "approved",
					CreatedAt:   "2026-04-25T09:02:03+08:00",
				},
			},
		},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/admin/applications/app_router_pending/approve",
		strings.NewReader(`{"actor_id":"user_admin_demo","comment":"approved via route","tenant_id":"tenant_demo"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	expected := `{"item":{"id":"app_router_pending","email":"router-pending@example.com","name":"路由待审批用户","company_name":"Router Co","use_case":"租户接入","status":"approved","created_at":"2026-04-25T09:02:03+08:00"}}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
	if capturedID != "app_router_pending" {
		t.Fatalf("expected captured id %q, got %q", "app_router_pending", capturedID)
	}
	if capturedReq.ActorID != "user_admin_demo" {
		t.Fatalf("expected actor_id %q, got %q", "user_admin_demo", capturedReq.ActorID)
	}
	if capturedReq.Comment != "approved via route" {
		t.Fatalf("expected comment %q, got %q", "approved via route", capturedReq.Comment)
	}
	if capturedReq.TenantID != "tenant_demo" {
		t.Fatalf("expected tenant_id %q, got %q", "tenant_demo", capturedReq.TenantID)
	}
}

func TestAdminUsageOverviewRouteReturnsConsoleData(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			usageOverview: service.UsageOverviewData{
				TotalRequests:  12,
				SuccessRate:    "91.67%",
				TotalTokens:    "1,280",
				AverageLatency: "182 ms",
				EstimatedShare: "8.33%",
			},
		},
	})

	unauthorizedReq := httptest.NewRequest(http.MethodGet, "/admin/usage/overview", nil)
	unauthorizedResp, err := app.Test(unauthorizedReq)
	if err != nil {
		t.Fatalf("app.Test unauthorized failed: %v", err)
	}
	if unauthorizedResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized status %d, got %d", http.StatusUnauthorized, unauthorizedResp.StatusCode)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/overview", nil)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	expected := `{"total_requests":12,"success_rate":"91.67%","total_tokens":"1,280","average_latency":"182 ms","estimated_share":"8.33%"}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
}

func TestMemberOverviewRouteResolvesPrincipalAndReturnsMemberData(t *testing.T) {
	t.Parallel()

	captured := service.ConsolePrincipal{}
	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		AuthService: stubConsoleAuthService{
			principal: service.ConsolePrincipal{
				UserID:   "user_member_a",
				Email:    "member-a@example.com",
				Role:     "member",
				TenantID: "tenant_demo",
			},
		},
		MemberConsoleService: stubMemberConsoleService{
			overview: service.MemberOverviewPageData{
				TenantID:      "tenant_demo",
				TenantName:    "Demo Tenant",
				ActiveAPIKeys: 1,
			},
			principalRef: &captured,
		},
	})

	missingSubjectReq := httptest.NewRequest(http.MethodGet, "/me/overview", nil)
	missingSubjectReq.SetBasicAuth("test-console-user", "test-console-password")
	missingSubjectResp, err := app.Test(missingSubjectReq)
	if err != nil {
		t.Fatalf("app.Test missing subject failed: %v", err)
	}
	if missingSubjectResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected missing subject status %d, got %d", http.StatusUnauthorized, missingSubjectResp.StatusCode)
	}

	req := httptest.NewRequest(http.MethodGet, "/me/overview", nil)
	req.SetBasicAuth("test-console-user", "test-console-password")
	req.Header.Set("X-Console-Subject", "member-a@example.com")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	expected := `{"tenant_id":"tenant_demo","tenant_name":"Demo Tenant","active_api_keys":1}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
	if captured.UserID != "user_member_a" {
		t.Fatalf("expected captured principal user_id %q, got %q", "user_member_a", captured.UserID)
	}
	if captured.TenantID != "tenant_demo" {
		t.Fatalf("expected captured principal tenant_id %q, got %q", "tenant_demo", captured.TenantID)
	}
}

func TestMemberOverviewRouteRejectsAdminPrincipal(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		AuthService: stubConsoleAuthService{
			principal: service.ConsolePrincipal{
				UserID: "user_admin_demo",
				Email:  "admin@example.com",
				Role:   "admin",
			},
		},
		MemberConsoleService: stubMemberConsoleService{
			overview: service.MemberOverviewPageData{
				TenantID:      "tenant_demo",
				TenantName:    "Demo Tenant",
				ActiveAPIKeys: 1,
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/me/overview", nil)
	req.SetBasicAuth("test-console-user", "test-console-password")
	req.Header.Set("X-Console-Subject", "admin@example.com")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestAdminUsageTrendsRouteReturnsConsoleData(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			usageTrends: service.UsageTrendData{
				Requests: []service.UsageTrendPoint{{Label: "04-24 18:00", Value: "12"}},
				Tokens:   []service.UsageTrendPoint{{Label: "04-24 18:00", Value: "1,280"}},
				Success:  []service.UsageTrendPoint{{Label: "04-24 18:00", Value: "91.67%"}},
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/trends", nil)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	expected := `{"requests":[{"label":"04-24 18:00","value":"12"}],"tokens":[{"label":"04-24 18:00","value":"1,280"}],"success":[{"label":"04-24 18:00","value":"91.67%"}]}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
}

func TestAdminUsageLatencyWallRouteReturnsConsoleData(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			usageLatencyWall: service.UsageLatencyWallData{
				WindowLabel: "最近 24 小时",
				Buckets:     []string{"04-24 18:00"},
				Lanes: []service.UsageLatencyLane{
					{
						Model:          "qwen-flash",
						RouteLabel:     "default-route",
						SuccessRate:    "98.00%",
						AverageLatency: "182 ms",
						Cells: []service.UsageLatencyCell{
							{
								BucketLabel: "04-24 18:00",
								Latency:     "148 ms",
								Status:      "健康",
								Requests:    "12 次",
							},
						},
					},
				},
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/latency-wall?window=24h", nil)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	expected := `{"window_label":"最近 24 小时","buckets":["04-24 18:00"],"lanes":[{"model":"qwen-flash","route_label":"default-route","success_rate":"98.00%","average_latency":"182 ms","cells":[{"bucket_label":"04-24 18:00","latency":"148 ms","status":"健康","requests":"12 次"}]}]}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
}

func TestAdminUsageFailuresRouteReturnsConsoleData(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			usageFailures: service.UsageFailureData{
				Breakdown:    []service.UsageFailureBucket{{Label: "限流", Value: "2 次"}},
				RecentEvents: []string{"04-24 18:00 · 限流 · 请求失败（429）"},
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/failures", nil)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	expected := `{"breakdown":[{"label":"限流","value":"2 次"}],"recent_events":["04-24 18:00 · 限流 · 请求失败（429）"]}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
}

func TestAdminUsageRequestsRouteReturnsConsoleData(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			usageRequests: service.UsageRequestsPageData{
				Items: []service.UsageRequestItem{
					{
						RequestID:   "llmreq_demo_002",
						Tenant:      "tenant_demo",
						Endpoint:    "/v1/embeddings",
						Model:       "text-embedding-3-small",
						Status:      "限流",
						TotalTokens: "16",
						Latency:     "95 ms",
						UsageSource: "估算",
					},
				},
				Total:  1,
				Limit:  20,
				Offset: 0,
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/requests", nil)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	expected := `{"items":[{"request_id":"llmreq_demo_002","tenant":"tenant_demo","endpoint":"/v1/embeddings","model":"text-embedding-3-small","status":"限流","total_tokens":"16","latency":"95 ms","usage_source":"估算"}],"total":1,"limit":20,"offset":0}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
}

func TestAdminUsageOverviewRouteParsesAndForwardsUsageQuery(t *testing.T) {
	t.Parallel()

	var captured service.UsageQuery
	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			usageOverview: service.UsageOverviewData{
				TotalRequests: 1,
			},
			usageQueryRef: &captured,
		},
	})

	req := httptest.NewRequest(
		http.MethodGet,
		"/admin/usage/overview?from=2026-04-24T10:00:00Z&to=2026-04-24T11:00:00Z&tenant_id=tenant_demo&error_category=rate_limit&limit=10&offset=5",
		nil,
	)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if captured.TenantID != "tenant_demo" {
		t.Fatalf("expected tenant_id tenant_demo, got %q", captured.TenantID)
	}
	if captured.ErrorCategory != "rate_limit" {
		t.Fatalf("expected error_category rate_limit, got %q", captured.ErrorCategory)
	}
	if captured.Limit != 10 {
		t.Fatalf("expected limit 10, got %d", captured.Limit)
	}
	if captured.Offset != 5 {
		t.Fatalf("expected offset 5, got %d", captured.Offset)
	}
	if captured.From.Format(time.RFC3339) != "2026-04-24T10:00:00Z" {
		t.Fatalf("expected from 2026-04-24T10:00:00Z, got %s", captured.From.Format(time.RFC3339))
	}
	if captured.To.Format(time.RFC3339) != "2026-04-24T11:00:00Z" {
		t.Fatalf("expected to 2026-04-24T11:00:00Z, got %s", captured.To.Format(time.RFC3339))
	}
}

func TestAdminUsageOverviewRouteRejectsInvalidTimeRange(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService:      stubConsoleService{},
	})

	req := httptest.NewRequest(
		http.MethodGet,
		"/admin/usage/overview?from=2026-04-24T11:00:00Z&to=2026-04-24T10:00:00Z",
		nil,
	)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAdminUsageTrendsRouteParsesAndForwardsUsageQuery(t *testing.T) {
	t.Parallel()

	var captured service.UsageQuery
	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			usageTrends:   service.UsageTrendData{},
			usageQueryRef: &captured,
		},
	})

	req := httptest.NewRequest(
		http.MethodGet,
		"/admin/usage/trends?provider=dashscope&model=qwen-flash&status=failed&usage_source=estimated&limit=12&offset=24",
		nil,
	)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if captured.Provider != "dashscope" {
		t.Fatalf("expected provider dashscope, got %q", captured.Provider)
	}
	if captured.Model != "qwen-flash" {
		t.Fatalf("expected model qwen-flash, got %q", captured.Model)
	}
	if captured.Status != "failed" {
		t.Fatalf("expected status failed, got %q", captured.Status)
	}
	if captured.UsageSource != "estimated" {
		t.Fatalf("expected usage_source estimated, got %q", captured.UsageSource)
	}
	if captured.Limit != 12 {
		t.Fatalf("expected limit 12, got %d", captured.Limit)
	}
	if captured.Offset != 24 {
		t.Fatalf("expected offset 24, got %d", captured.Offset)
	}
}

func TestAdminUsageFailuresRouteParsesAndForwardsUsageQuery(t *testing.T) {
	t.Parallel()

	var captured service.UsageQuery
	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			usageFailures: service.UsageFailureData{},
			usageQueryRef: &captured,
		},
	})

	req := httptest.NewRequest(
		http.MethodGet,
		"/admin/usage/failures?route_id=route_demo&request_path=/v1/chat/completions&error_category=上游超时",
		nil,
	)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if captured.RouteID != "route_demo" {
		t.Fatalf("expected route_id route_demo, got %q", captured.RouteID)
	}
	if captured.RequestPath != "/v1/chat/completions" {
		t.Fatalf("expected request_path /v1/chat/completions, got %q", captured.RequestPath)
	}
	if captured.ErrorCategory != "上游超时" {
		t.Fatalf("expected error_category 上游超时, got %q", captured.ErrorCategory)
	}
}

func TestAdminUsageRequestsRouteParsesAndForwardsUsageQuery(t *testing.T) {
	t.Parallel()

	var captured service.UsageQuery
	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			usageRequests: service.UsageRequestsPageData{},
			usageQueryRef: &captured,
		},
	})

	req := httptest.NewRequest(
		http.MethodGet,
		"/admin/usage/requests?tenant_id=tenant_demo&platform_api_key_id=pak_demo&from=2026-04-24T10:00:00Z&to=2026-04-24T11:00:00Z&limit=30&offset=60",
		nil,
	)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if captured.TenantID != "tenant_demo" {
		t.Fatalf("expected tenant_id tenant_demo, got %q", captured.TenantID)
	}
	if captured.PlatformAPIKeyID != "pak_demo" {
		t.Fatalf("expected platform_api_key_id pak_demo, got %q", captured.PlatformAPIKeyID)
	}
	if captured.Limit != 30 {
		t.Fatalf("expected limit 30, got %d", captured.Limit)
	}
	if captured.Offset != 60 {
		t.Fatalf("expected offset 60, got %d", captured.Offset)
	}
	if captured.From.Format(time.RFC3339) != "2026-04-24T10:00:00Z" {
		t.Fatalf("expected from 2026-04-24T10:00:00Z, got %s", captured.From.Format(time.RFC3339))
	}
	if captured.To.Format(time.RFC3339) != "2026-04-24T11:00:00Z" {
		t.Fatalf("expected to 2026-04-24T11:00:00Z, got %s", captured.To.Format(time.RFC3339))
	}
}

func TestAuthCheckRouteMapsAuthFailuresToHTTPStatuses(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		authErr    error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "unauthorized returns generic unauthorized",
			authErr:    fmt.Errorf("%w: platform API key not found", service.ErrUnauthorized),
			wantStatus: http.StatusUnauthorized,
			wantBody:   "unauthorized",
		},
		{
			name:       "quota exceeded returns generic quota exceeded",
			authErr:    fmt.Errorf("%w: tenant tenant_123", service.ErrQuotaExceeded),
			wantStatus: http.StatusTooManyRequests,
			wantBody:   "quota exceeded",
		},
		{
			name:       "route resolution failure returns bad gateway",
			authErr:    fmt.Errorf("%w: no provider for requested model", service.ErrRouteNotFound),
			wantStatus: http.StatusBadGateway,
			wantBody:   "route resolution failed",
		},
		{
			name:       "internal failures return generic internal error",
			authErr:    errors.New("sql: connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   "internal server error",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			app := apphttp.NewRouterWithAuth(stubAuthService{err: tc.authErr})
			req := httptest.NewRequest(http.MethodGet, "/v1/auth-check", nil)
			req.Header.Set("Authorization", "Bearer platform-live-key")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test failed: %v", err)
			}
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("expected %d, got %d", tc.wantStatus, resp.StatusCode)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("io.ReadAll failed: %v", err)
			}
			if string(body) != tc.wantBody {
				t.Fatalf("expected body %q, got %q", tc.wantBody, string(body))
			}
		})
	}
}

func TestAuthCheckRouteWithInjectedAuthServiceReturnsSuccess(t *testing.T) {
	t.Parallel()

	stub := &capturingAuthService{
		requestContext: domain.RequestContext{
			TenantID:             "tenant_123",
			PlatformAPIKeyID:     "pak_123",
			SelectedProviderID:   "pc_123",
			SelectedProviderName: "Platform Default Route",
		},
	}
	app := apphttp.NewRouterWithAuth(stub)
	req := httptest.NewRequest(http.MethodGet, "/v1/auth-check?model=gpt-4o-mini", nil)
	req.Header.Set("Authorization", "Bearer platform-live-key")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	if string(body) != `{"status":"ok"}` {
		t.Fatalf("expected body %q, got %q", `{"status":"ok"}`, string(body))
	}
	if stub.rawKey != "platform-live-key" {
		t.Fatalf("expected raw key %q, got %q", "platform-live-key", stub.rawKey)
	}
	if stub.requestedModel != "gpt-4o-mini" {
		t.Fatalf("expected requested model %q, got %q", "gpt-4o-mini", stub.requestedModel)
	}
	if stub.ctx == nil {
		t.Fatal("expected auth service to receive a request context")
	}
	if stub.ctx == context.Background() {
		t.Fatal("expected auth service to receive a request-scoped context, got context.Background")
	}
}

type stubAuthService struct {
	err error
}

func (s stubAuthService) Resolve(context.Context, string, string) (domain.RequestContext, error) {
	return domain.RequestContext{}, s.err
}

type capturingAuthService struct {
	ctx            context.Context
	rawKey         string
	requestedModel string
	requestContext domain.RequestContext
}

func (s *capturingAuthService) Resolve(ctx context.Context, rawKey string, requestedModel string) (domain.RequestContext, error) {
	s.ctx = ctx
	s.rawKey = rawKey
	s.requestedModel = requestedModel
	return s.requestContext, nil
}

type stubConsoleAuthService struct {
	principal        service.ConsolePrincipal
	sessionPrincipal service.ConsolePrincipal
	loginResult      service.ConsoleLoginResult
	err              error
}

func (s stubConsoleAuthService) Resolve(context.Context, string, string) (domain.RequestContext, error) {
	return domain.RequestContext{}, nil
}

func (s stubConsoleAuthService) ResolvePlatformAPIKey(context.Context, string) (domain.RequestContext, error) {
	return domain.RequestContext{}, nil
}

func (s stubConsoleAuthService) ResolveConsolePrincipal(context.Context, string) (service.ConsolePrincipal, error) {
	return s.principal, s.err
}

func (s stubConsoleAuthService) AuthenticateConsoleSession(context.Context, string, string) (service.ConsoleLoginResult, error) {
	return s.loginResult, s.err
}

func (s stubConsoleAuthService) ResolveConsoleSession(context.Context, string) (service.ConsolePrincipal, error) {
	if s.sessionPrincipal.UserID != "" || s.sessionPrincipal.Email != "" || s.sessionPrincipal.Role != "" {
		return s.sessionPrincipal, s.err
	}
	return s.principal, s.err
}

type stubConsoleService struct {
	systemStatus             service.ConsoleSystemStatus
	apiKeys                  service.APIKeysPageData
	apiKeyMutationResult     service.APIKeyMutationResult
	applications             service.ApplicationsPageData
	applicationMutation      service.ApplicationMutationResult
	approveApplicationIDRef  *string
	approveApplicationReqRef *service.ApproveApplicationRequest
	usageOverview            service.UsageOverviewData
	usageTrends              service.UsageTrendData
	usageLatencyWall         service.UsageLatencyWallData
	usageFailures            service.UsageFailureData
	usageRequests            service.UsageRequestsPageData
	usageQueryRef            *service.UsageQuery
}

type stubMemberConsoleService struct {
	overview      service.MemberOverviewPageData
	apiKeys       service.MemberAPIKeysPageData
	apiKeyResult  service.APIKeyMutationResult
	usageOverview service.UsageOverviewData
	usageRequests service.UsageRequestsPageData
	failures      service.MemberFailurePageData
	auditEvents   service.MemberAuditPageData
	principalRef  *service.ConsolePrincipal
}

func (s stubMemberConsoleService) capturePrincipal(ctx context.Context) {
	if s.principalRef == nil {
		return
	}
	if principal, ok := service.ConsolePrincipalFromContext(ctx); ok {
		*s.principalRef = principal
	}
}

func (s stubMemberConsoleService) Overview(ctx context.Context) (service.MemberOverviewPageData, error) {
	s.capturePrincipal(ctx)
	return s.overview, nil
}

func (s stubMemberConsoleService) APIKeys(ctx context.Context) (service.MemberAPIKeysPageData, error) {
	s.capturePrincipal(ctx)
	return s.apiKeys, nil
}

func (s stubMemberConsoleService) CreateAPIKey(ctx context.Context, _ service.CreateAPIKeyRequest) (service.APIKeyMutationResult, error) {
	s.capturePrincipal(ctx)
	return s.apiKeyResult, nil
}

func (s stubMemberConsoleService) RotateAPIKey(ctx context.Context, _ string, _ service.RotateAPIKeyRequest) (service.APIKeyMutationResult, error) {
	s.capturePrincipal(ctx)
	return s.apiKeyResult, nil
}

func (s stubMemberConsoleService) DeactivateAPIKey(ctx context.Context, _ string) (service.APIKeyMutationResult, error) {
	s.capturePrincipal(ctx)
	return s.apiKeyResult, nil
}

func (s stubMemberConsoleService) UsageOverview(ctx context.Context, _ service.UsageQuery) (service.UsageOverviewData, error) {
	s.capturePrincipal(ctx)
	return s.usageOverview, nil
}

func (s stubMemberConsoleService) UsageRequests(ctx context.Context, _ service.UsageQuery) (service.UsageRequestsPageData, error) {
	s.capturePrincipal(ctx)
	return s.usageRequests, nil
}

func (s stubMemberConsoleService) Failures(ctx context.Context, _ service.UsageQuery) (service.MemberFailurePageData, error) {
	s.capturePrincipal(ctx)
	return s.failures, nil
}

func (s stubMemberConsoleService) AuditEvents(ctx context.Context) (service.MemberAuditPageData, error) {
	s.capturePrincipal(ctx)
	return s.auditEvents, nil
}

func (s stubConsoleService) Overview(context.Context) (service.OverviewPageData, error) {
	return service.OverviewPageData{}, nil
}

func (s stubConsoleService) SystemStatus(context.Context) (service.ConsoleSystemStatus, error) {
	return s.systemStatus, nil
}

func (s stubConsoleService) APIKeys(context.Context) (service.APIKeysPageData, error) {
	return s.apiKeys, nil
}

func (s stubConsoleService) Applications(context.Context) (service.ApplicationsPageData, error) {
	return s.applications, nil
}

func (s stubConsoleService) CreateAPIKey(context.Context, service.CreateAPIKeyRequest) (service.APIKeyMutationResult, error) {
	return s.apiKeyMutationResult, nil
}

func (s stubConsoleService) ApproveApplication(_ context.Context, id string, req service.ApproveApplicationRequest) (service.ApplicationMutationResult, error) {
	if s.approveApplicationIDRef != nil {
		*s.approveApplicationIDRef = id
	}
	if s.approveApplicationReqRef != nil {
		*s.approveApplicationReqRef = req
	}
	return s.applicationMutation, nil
}

func (s stubConsoleService) RotateAPIKey(context.Context, string, service.RotateAPIKeyRequest) (service.APIKeyMutationResult, error) {
	return s.apiKeyMutationResult, nil
}

func (s stubConsoleService) DeactivateAPIKey(context.Context, string) (service.APIKeyMutationResult, error) {
	return s.apiKeyMutationResult, nil
}

func (s stubConsoleService) DeleteAPIKey(context.Context, string) (service.APIKeyMutationResult, error) {
	return s.apiKeyMutationResult, nil
}

func (s stubConsoleService) Routes(context.Context) (service.RoutesPageData, error) {
	return service.RoutesPageData{}, nil
}

func (s stubConsoleService) Playground(context.Context) (service.PlaygroundPageData, error) {
	return service.PlaygroundPageData{}, nil
}

func (s stubConsoleService) RunPlayground(context.Context, service.PlaygroundRunRequest) (service.PlaygroundRunResponse, error) {
	return service.PlaygroundRunResponse{}, nil
}

func (s stubConsoleService) Audit(context.Context) (service.AuditPageData, error) {
	return service.AuditPageData{}, nil
}

func (s stubConsoleService) UsageOverview(_ context.Context, query service.UsageQuery) (service.UsageOverviewData, error) {
	if s.usageQueryRef != nil {
		*s.usageQueryRef = query
	}
	return s.usageOverview, nil
}

func (s stubConsoleService) UsageTrends(_ context.Context, query service.UsageQuery) (service.UsageTrendData, error) {
	if s.usageQueryRef != nil {
		*s.usageQueryRef = query
	}
	return s.usageTrends, nil
}

func (s stubConsoleService) UsageLatencyWall(_ context.Context, query service.UsageQuery) (service.UsageLatencyWallData, error) {
	if s.usageQueryRef != nil {
		*s.usageQueryRef = query
	}
	return s.usageLatencyWall, nil
}

func (s stubConsoleService) UsageFailures(_ context.Context, query service.UsageQuery) (service.UsageFailureData, error) {
	if s.usageQueryRef != nil {
		*s.usageQueryRef = query
	}
	return s.usageFailures, nil
}

func (s stubConsoleService) UsageRequests(_ context.Context, query service.UsageQuery) (service.UsageRequestsPageData, error) {
	if s.usageQueryRef != nil {
		*s.usageQueryRef = query
	}
	return s.usageRequests, nil
}
