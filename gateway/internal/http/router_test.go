package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestAdminProviderModelsRouteReturnsProvidersAndModels(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			providerModels: service.ProviderModelsPageData{
				Providers: []service.ProviderItem{
					{ID: "provider_dashscope_primary", Provider: "qwen", DisplayName: "Qwen", SupportedModels: []string{"qwen-flash"}, CredentialMode: "encrypted", SecretRef: "", Status: "active"},
				},
				Models: []service.ProviderModelItem{
					{RequestedModel: "qwen-flash", Provider: "qwen", ProviderCredentialID: "provider_dashscope_primary", RouteLabel: "Qwen", HealthStatus: "healthy", LatencyMS: 218, RequestMode: "聊天"},
				},
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/provider-models", nil)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}

	var got service.ProviderModelsPageData
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("json.Unmarshal failed: %v, body=%q", err, string(body))
	}
	want := service.ProviderModelsPageData{
		Providers: []service.ProviderItem{
			{ID: "provider_dashscope_primary", Provider: "qwen", DisplayName: "Qwen", SupportedModels: []string{"qwen-flash"}, CredentialMode: "encrypted", SecretRef: "", Status: "active"},
		},
		Models: []service.ProviderModelItem{
			{RequestedModel: "qwen-flash", Provider: "qwen", ProviderCredentialID: "provider_dashscope_primary", RouteLabel: "Qwen", HealthStatus: "healthy", LatencyMS: 218, RequestMode: "聊天"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected payload %#v, got %#v", want, got)
	}
}

func TestAdminCreateProviderRoutePassesRequestBody(t *testing.T) {
	t.Parallel()

	var captured service.CreateProviderRequest
	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			createProviderReqRef: &captured,
			providerMutation: service.ProviderMutationResult{
				Item: service.ProviderItem{
					ID:             "provider_dashscope_primary",
					Provider:       "qwen",
					DisplayName:    "Qwen 主线路",
					CredentialMode: "secret_ref",
					SecretRef:      "TEST_QWEN_PROVIDER_SECRET",
					Status:         "active",
				},
			},
		},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/admin/providers",
		strings.NewReader(`{"provider":"dashscope","display_name":"Qwen 主线路","base_url":"https://dashscope.aliyuncs.com/compatible-mode/v1","credential_mode":"secret_ref","secret_ref":"TEST_QWEN_PROVIDER_SECRET"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	if captured.Provider != "dashscope" || captured.SecretRef != "TEST_QWEN_PROVIDER_SECRET" || captured.CredentialMode != "secret_ref" {
		t.Fatalf("unexpected captured request: %+v", captured)
	}
}

func TestAdminCreateProviderModelRoutePassesRequestBody(t *testing.T) {
	t.Parallel()

	var captured service.CreateProviderModelRequest
	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			createProviderModelReqRef: &captured,
			providerModelMutation: service.ProviderModelMutationResult{
				Item: service.ProviderModelItem{
					ID:                   "route:provider_dashscope_primary:qwen-flash",
					RequestedModel:       "qwen-flash",
					Provider:             "qwen",
					ProviderCredentialID: "provider_dashscope_primary",
					RouteLabel:           "Qwen 主线路",
					HealthStatus:         "healthy",
					RequestMode:          "聊天",
				},
			},
		},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/admin/provider-models",
		strings.NewReader(`{"requested_model":"qwen-flash","provider_credential_id":"provider_dashscope_primary","request_mode":"聊天","healthcheck_enabled":true}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	if captured.RequestedModel != "qwen-flash" || captured.ProviderCredentialID != "provider_dashscope_primary" || !captured.HealthcheckEnabled {
		t.Fatalf("unexpected captured request: %+v", captured)
	}
}

func TestAdminModelHealthRouteReturnsPayload(t *testing.T) {
	t.Parallel()

	var capturedWindow string
	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			modelHealthWindowRef: &capturedWindow,
			modelHealth: service.ModelHealthPageData{
				Items: []service.ModelHealthItem{
					{
						ID:                  "route:provider_dashscope_primary:qwen-flash",
						RequestedModel:      "qwen-flash",
						RouteLabel:          "Qwen 主线路",
						HealthStatus:        "healthy",
						LastHealthError:     "",
						RequestMode:         "聊天",
						LatencyMS:           218,
						FirstTokenLatencyMS: 82,
						LastHealthCheckedAt: "2026-05-06T12:00:00+08:00",
					},
				},
				Wall: service.ModelHealthWall{
					Window:      "7d",
					WindowLabel: "最近 7 天",
					Buckets:     []string{"05-01", "05-02"},
					Lanes: []service.ModelHealthWallLane{
						{
							Model:          "qwen-flash",
							RouteLabel:     "Qwen 主线路",
							SuccessRate:    "50%",
							AverageLatency: "200 ms",
							Cells: []service.ModelHealthWallCell{
								{BucketLabel: "05-01", Status: "降级", Latency: "300 ms", Requests: "1 次"},
								{BucketLabel: "05-02", Status: "健康", Latency: "100 ms", Requests: "1 次"},
							},
						},
					},
				},
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/model-health?window=7d", nil)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	if capturedWindow != "7d" {
		t.Fatalf("expected captured window %q, got %q", "7d", capturedWindow)
	}
}

func TestAdminTenantBillingRouteReturnsPayload(t *testing.T) {
	t.Parallel()

	var captured service.TenantBillingQuery
	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			tenantBillingQueryRef: &captured,
			tenantBilling: service.TenantBillingPageData{
				Summary: service.TenantBillingSummary{
					TenantID:     "tenant_demo",
					Month:        "2026-04",
					RequestCount: 12,
					SuccessCount: 10,
					FailureCount: 2,
					InputTokens:  1200,
					OutputTokens: 600,
					CachedTokens: 40,
					TotalTokens:  1840,
					InputCost:    "0.12 ￥",
					OutputCost:   "0.24 ￥",
					CachedCost:   "0.01 ￥",
					TotalCost:    "0.37 ￥",
				},
				Providers: []service.TenantBillingProviderItem{{ProviderCredentialID: "provider_qwen", Provider: "qwen", DisplayName: "Qwen", RequestCount: 12, TotalCost: "0.37 ￥"}},
				Models:    []service.TenantBillingModelItem{{Model: "qwen-flash", ProviderCredentialID: "provider_qwen", ProviderDisplayName: "Qwen", RequestCount: 12, TotalCost: "0.37 ￥"}},
				APIKeys:   []service.TenantBillingAPIKeyItem{{PlatformAPIKeyID: "pak_demo", Name: "Demo Key", RequestCount: 12, TotalCost: "0.37 ￥"}},
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/billing/tenant?tenant_id=tenant_demo&month=2026-04", nil)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	if captured.TenantID != "tenant_demo" || captured.Month != "2026-04" {
		t.Fatalf("unexpected captured query: %+v", captured)
	}

	var got service.TenantBillingPageData
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("json decode failed: %v", err)
	}
	if got.Summary.Month != "2026-04" || got.Summary.TenantID != "tenant_demo" {
		t.Fatalf("unexpected payload: %+v", got)
	}
}

func TestAdminTenantBillingRouteRejectsMissingOrInvalidMonth(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService:      stubConsoleService{},
	})

	for _, target := range []string{
		"/admin/billing/tenant?tenant_id=tenant_demo",
		"/admin/billing/tenant?tenant_id=tenant_demo&month=2026/04",
		"/admin/billing/tenant?tenant_id=tenant_demo&month=2026-13",
		"/admin/billing/tenant?month=2026-04",
	} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.SetBasicAuth("test-console-user", "test-console-password")

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test failed for %s: %v", target, err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 for %s, got %d", target, resp.StatusCode)
		}
	}
}

func TestAdminRunProviderModelHealthcheckRoutePassesID(t *testing.T) {
	t.Parallel()

	var capturedID string
	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			runProviderModelHealthcheckIDRef: &capturedID,
			providerModelMutation: service.ProviderModelMutationResult{
				Item: service.ProviderModelItem{
					ID:                   "route:provider_dashscope_primary:qwen-flash",
					RequestedModel:       "qwen-flash",
					Provider:             "qwen",
					ProviderCredentialID: "provider_dashscope_primary",
					RouteLabel:           "Qwen 主线路",
					HealthStatus:         "healthy",
				},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/provider-models/route:provider_dashscope_primary:qwen-flash/health-check", nil)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	if capturedID != "route:provider_dashscope_primary:qwen-flash" {
		t.Fatalf("expected captured id %q, got %q", "route:provider_dashscope_primary:qwen-flash", capturedID)
	}
}

func TestAdminRunProviderModelHealthcheckRouteDecodesEncodedID(t *testing.T) {
	t.Parallel()

	var capturedID string
	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			runProviderModelHealthcheckIDRef: &capturedID,
			providerModelMutation: service.ProviderModelMutationResult{
				Item: service.ProviderModelItem{
					ID:                   "route:provider_dashscope_primary:default",
					RequestedModel:       "qwen-flash",
					Provider:             "qwen",
					ProviderCredentialID: "provider_dashscope_primary",
					RouteLabel:           "Qwen 主线路",
					HealthStatus:         "healthy",
				},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/provider-models/route%3Aprovider_dashscope_primary%3Adefault/health-check", nil)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	if capturedID != "route:provider_dashscope_primary:default" {
		t.Fatalf("expected captured id %q, got %q", "route:provider_dashscope_primary:default", capturedID)
	}
}

func TestAdminPlaygroundStreamRouteReturnsSSE(t *testing.T) {
	t.Parallel()

	var captured service.PlaygroundRunRequest
	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			streamPlaygroundReqRef: &captured,
			streamPlaygroundSession: service.PlaygroundStreamSession{
				StatusCode:  http.StatusOK,
				ContentType: "text/event-stream; charset=utf-8",
				Run: func(emit func([]byte) error) (service.PlaygroundRunResponse, error) {
					if err := emit([]byte("event: meta\ndata: {\"request_id\":\"req_playground_stream\"}\n\n")); err != nil {
						return service.PlaygroundRunResponse{}, err
					}
					if err := emit([]byte("event: done\ndata: {\"status\":\"200 成功\"}\n\n")); err != nil {
						return service.PlaygroundRunResponse{}, err
					}
					return service.PlaygroundRunResponse{Status: "200 成功"}, nil
				},
			},
		},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/admin/playground/chat/stream",
		strings.NewReader(`{"model":"qwen-flash","prompt":"hello","stream":true}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("expected text/event-stream content type, got %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	if !strings.Contains(string(body), "event: meta") {
		t.Fatalf("expected SSE body to include meta event, got %q", string(body))
	}
	if !strings.Contains(string(body), "event: done") {
		t.Fatalf("expected SSE body to include done event, got %q", string(body))
	}
	if !captured.Stream {
		t.Fatalf("expected playground stream request to preserve stream=true, got %#v", captured)
	}
	if captured.Model != "qwen-flash" || captured.Prompt != "hello" {
		t.Fatalf("expected captured request to match body, got %#v", captured)
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

func TestConsoleLoginRouteReturnsChineseUnauthorizedMessage(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		AuthService: stubConsoleAuthService{
			err: fmt.Errorf("%w: invalid console credentials", service.ErrUnauthorized),
		},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/console/session/login",
		strings.NewReader(`{"email":"123@qq.com","password":"wrong-password"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	if string(body) != "邮箱或密码错误" {
		t.Fatalf("expected body %q, got %q", "邮箱或密码错误", string(body))
	}
}

func TestConsoleApplicationSubmitRouteReturnsPendingPayload(t *testing.T) {
	t.Parallel()

	expected := service.ApplicationMutationResult{
		Item: service.ApplicationItem{
			ID:          "app_public_pending",
			Email:       "new-user@example.com",
			Name:        "新用户",
			CompanyName: "New Co",
			UseCase:     "测试接入",
			Status:      "pending",
			CreatedAt:   "2026-04-28T16:00:00+08:00",
		},
	}
	var captured service.CreateApplicationRequest

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		AuthService: service.NewUnauthorizedAuthService(),
		ConsoleService: stubConsoleService{
			applicationMutation:     expected,
			createApplicationReqRef: &captured,
		},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/console/applications",
		strings.NewReader(`{"email":"new-user@example.com","name":"新用户","company_name":"New Co","use_case":"测试接入"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	if captured.Email != "new-user@example.com" {
		t.Fatalf("expected email %q, got %q", "new-user@example.com", captured.Email)
	}
	if captured.Name != "新用户" {
		t.Fatalf("expected name %q, got %q", "新用户", captured.Name)
	}
	if captured.CompanyName != "New Co" {
		t.Fatalf("expected company_name %q, got %q", "New Co", captured.CompanyName)
	}
	if captured.UseCase != "测试接入" {
		t.Fatalf("expected use_case %q, got %q", "测试接入", captured.UseCase)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	const want = `{"item":{"id":"app_public_pending","email":"new-user@example.com","name":"新用户","company_name":"New Co","use_case":"测试接入","status":"pending","created_at":"2026-04-28T16:00:00+08:00"}}`
	if string(body) != want {
		t.Fatalf("expected body %s, got %s", want, string(body))
	}
}

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

	verifyReq := httptest.NewRequest(
		http.MethodPost,
		"/console/captcha/verify",
		strings.NewReader(`{"captcha_id":"cap_demo","captcha_code":"A7KQ"}`),
	)
	verifyReq.Header.Set("Content-Type", "application/json")
	verifyResp, err := app.Test(verifyReq)
	if err != nil {
		t.Fatalf("POST captcha verify failed: %v", err)
	}
	if verifyResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", verifyResp.StatusCode)
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

func TestAdminRevealAPIKeySecretRouteReturnsConsoleData(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			apiKeySecretView: service.APIKeySecretView{
				APIKeyID:            "pak_demo",
				MaskedKey:           "agw_****demo",
				Revealable:          true,
				LegacyUnrecoverable: false,
				ExpiresAt:           "2026-05-01T12:00:00Z",
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/api-keys/pak_demo/secret", nil)
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
	expected := `{"api_key_id":"pak_demo","masked_key":"agw_****demo","revealable":true,"legacy_unrecoverable":false,"expires_at":"2026-05-01T12:00:00Z"}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
}

func TestAdminCopyAPIKeySecretRouteReturnsConsoleData(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			apiKeySecretView: service.APIKeySecretView{
				APIKeyID:            "pak_demo",
				MaskedKey:           "agw_****demo",
				Revealable:          true,
				LegacyUnrecoverable: false,
				ExpiresAt:           "2026-05-01T12:00:00Z",
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/api-keys/pak_demo/secret/copy", nil)
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
	expected := `{"api_key_id":"pak_demo","masked_key":"agw_****demo","revealable":true,"legacy_unrecoverable":false,"expires_at":"2026-05-01T12:00:00Z"}`
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
		strings.NewReader(`{"actor_id":"user_admin_demo","comment":"approved via route","tenant_id":"tenant_demo","token_limit":3456789,"cost_limit_microyuan":1234500000,"allowed_models":["qwen-flash","mimo-v2.5-pro"]}`),
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
	if capturedReq.TokenLimit != 3456789 {
		t.Fatalf("expected token_limit %d, got %d", 3456789, capturedReq.TokenLimit)
	}
	if capturedReq.CostLimitMicroyuan != 1234500000 {
		t.Fatalf("expected cost_limit_microyuan %d, got %d", int64(1234500000), capturedReq.CostLimitMicroyuan)
	}
	if !reflect.DeepEqual(capturedReq.AllowedModels, []string{"qwen-flash", "mimo-v2.5-pro"}) {
		t.Fatalf("expected allowed_models to be forwarded, got %#v", capturedReq.AllowedModels)
	}
}

func TestAdminRejectApplicationRouteReturnsConsoleData(t *testing.T) {
	t.Parallel()

	var capturedID string
	var capturedReq service.RejectApplicationRequest

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			rejectApplicationIDRef:  &capturedID,
			rejectApplicationReqRef: &capturedReq,
			applicationMutation: service.ApplicationMutationResult{
				Item: service.ApplicationItem{
					ID:          "app_router_reject",
					Email:       "router-reject@example.com",
					Name:        "路由待拒绝用户",
					CompanyName: "Reject Co",
					UseCase:     "测试接入",
					Status:      "rejected",
					CreatedAt:   "2026-04-25T09:02:03+08:00",
				},
			},
		},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/admin/applications/app_router_reject/reject",
		strings.NewReader(`{"actor_id":"user_admin_demo","comment":"rejected via route"}`),
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
	expected := `{"item":{"id":"app_router_reject","email":"router-reject@example.com","name":"路由待拒绝用户","company_name":"Reject Co","use_case":"测试接入","status":"rejected","created_at":"2026-04-25T09:02:03+08:00"}}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
	if capturedID != "app_router_reject" {
		t.Fatalf("expected captured id %q, got %q", "app_router_reject", capturedID)
	}
	if capturedReq.ActorID != "user_admin_demo" {
		t.Fatalf("expected actor_id %q, got %q", "user_admin_demo", capturedReq.ActorID)
	}
	if capturedReq.Comment != "rejected via route" {
		t.Fatalf("expected comment %q, got %q", "rejected via route", capturedReq.Comment)
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
				InputTokens:    "960",
				OutputTokens:   "320",
				CachedTokens:   "128",
				AverageLatency: "182 ms",
				EstimatedShare: "8.33%",
				InputCost:      "3.80 ￥",
				OutputCost:     "1.40 ￥",
				CachedCost:     "0.40 ￥",
				TotalCost:      "5.60 ￥",
				PricingModels: []service.PricingModelItem{
					{
						Model:       "gpt-4o-mini",
						InputPrice:  "2.50 ￥/M",
						OutputPrice: "5.00 ￥/M",
						CachedPrice: "0.50 ￥/M",
					},
				},
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
	expected := `{"total_requests":12,"success_rate":"91.67%","total_tokens":"1,280","input_tokens":"960","output_tokens":"320","cached_tokens":"128","average_latency":"182 ms","estimated_share":"8.33%","input_cost":"3.80 ￥","output_cost":"1.40 ￥","cached_cost":"0.40 ￥","total_cost":"5.60 ￥","pricing_models":[{"model":"gpt-4o-mini","input_price":"2.50 ￥/M","output_price":"5.00 ￥/M","cached_price":"0.50 ￥/M"}]}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
}

func TestAdminApproveAccountDeletionApplicationRouteReturnsConsoleData(t *testing.T) {
	t.Parallel()

	var capturedID string
	var capturedReq service.ReviewAccountDeletionApplicationRequest

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			accountDeletionMutation: service.AccountDeletionApplicationMutationResult{
				Item: service.AccountDeletionApplicationItem{
					ID:              "ada_router_pending",
					UserID:          "user_member_a",
					TenantID:        "tenant_demo",
					UserEmail:       "member-a@example.com",
					UserName:        "Member A",
					Reason:          "不再使用",
					Status:          "approved",
					DisabledAPIKeys: 2,
					CreatedAt:       "2026-05-06T09:02:03+08:00",
					ReviewedAt:      "2026-05-06T09:05:03+08:00",
				},
			},
			approveAccountDeletionIDRef:  &capturedID,
			approveAccountDeletionReqRef: &capturedReq,
		},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/admin/account-deletion-applications/ada_router_pending/approve",
		strings.NewReader(`{"actor_id":"user_admin_demo","comment":"同意注销"}`),
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
	expected := `{"item":{"id":"ada_router_pending","user_id":"user_member_a","tenant_id":"tenant_demo","user_email":"member-a@example.com","user_name":"Member A","reason":"不再使用","status":"approved","disabled_api_keys":2,"created_at":"2026-05-06T09:02:03+08:00","reviewed_at":"2026-05-06T09:05:03+08:00"}}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
	if capturedID != "ada_router_pending" {
		t.Fatalf("expected id %q, got %q", "ada_router_pending", capturedID)
	}
	if capturedReq.ActorID != "user_admin_demo" {
		t.Fatalf("expected actor_id %q, got %q", "user_admin_demo", capturedReq.ActorID)
	}
	if capturedReq.Comment != "同意注销" {
		t.Fatalf("expected comment %q, got %q", "同意注销", capturedReq.Comment)
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
	expected := `{"tenant_id":"tenant_demo","tenant_name":"Demo Tenant","active_api_keys":1,"quota":{"configured":false,"request_limit":0,"requests_used":0,"requests_remaining":0,"token_limit":0,"tokens_used":0,"tokens_remaining":0,"resets_at":""}}`
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

func TestMemberAccountDeletionApplicationRouteReturnsPendingPayload(t *testing.T) {
	t.Parallel()

	var capturedReq service.CreateAccountDeletionApplicationRequest
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
			accountDeletionMutation: service.AccountDeletionApplicationMutationResult{
				Item: service.AccountDeletionApplicationItem{
					ID:        "ada_router_pending",
					UserID:    "user_member_a",
					TenantID:  "tenant_demo",
					UserEmail: "member-a@example.com",
					UserName:  "Member A",
					Reason:    "不再使用",
					Status:    "pending",
					CreatedAt: "2026-05-06T09:02:03+08:00",
				},
			},
			createAccountDeletionReqRef: &capturedReq,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/me/account-deletion-applications", strings.NewReader(`{"reason":"不再使用"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Console-Subject", "member-a@example.com")
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
	expected := `{"item":{"id":"ada_router_pending","user_id":"user_member_a","tenant_id":"tenant_demo","user_email":"member-a@example.com","user_name":"Member A","reason":"不再使用","status":"pending","disabled_api_keys":0,"created_at":"2026-05-06T09:02:03+08:00"}}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
	if capturedReq.Reason != "不再使用" {
		t.Fatalf("expected reason %q, got %q", "不再使用", capturedReq.Reason)
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

func TestMemberRevealAPIKeySecretRouteReturnsConsoleData(t *testing.T) {
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
			apiKeySecretView: service.APIKeySecretView{
				APIKeyID:            "pak_demo",
				MaskedKey:           "agw_****demo",
				Revealable:          true,
				LegacyUnrecoverable: false,
				ExpiresAt:           "2026-05-01T12:00:00Z",
			},
			principalRef: &captured,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/me/api-keys/pak_demo/secret", nil)
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
	expected := `{"api_key_id":"pak_demo","masked_key":"agw_****demo","revealable":true,"legacy_unrecoverable":false,"expires_at":"2026-05-01T12:00:00Z"}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
	if captured.UserID != "user_member_a" {
		t.Fatalf("expected captured principal user_id %q, got %q", "user_member_a", captured.UserID)
	}
}

func TestMemberCopyAPIKeySecretRouteReturnsConsoleData(t *testing.T) {
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
			apiKeySecretView: service.APIKeySecretView{
				APIKeyID:            "pak_demo",
				MaskedKey:           "agw_****demo",
				FullKey:             "agw_secret_demo",
				Revealable:          true,
				LegacyUnrecoverable: false,
				ExpiresAt:           "2026-05-01T12:00:00Z",
			},
			principalRef: &captured,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/me/api-keys/pak_demo/secret/copy", nil)
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
	expected := `{"api_key_id":"pak_demo","masked_key":"agw_****demo","full_key":"agw_secret_demo","revealable":true,"legacy_unrecoverable":false,"expires_at":"2026-05-01T12:00:00Z"}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
	if captured.UserID != "user_member_a" {
		t.Fatalf("expected captured principal user_id %q, got %q", "user_member_a", captured.UserID)
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
				Costs:    []service.UsageTrendPoint{{Label: "04-24 18:00", Value: "5.60 ￥"}},
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
	expected := `{"requests":[{"label":"04-24 18:00","value":"12"}],"tokens":[{"label":"04-24 18:00","value":"1,280"}],"success":[{"label":"04-24 18:00","value":"91.67%"}],"costs":[{"label":"04-24 18:00","value":"5.60 ￥"}]}`
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
	expected := `{"window_label":"最近 24 小时","buckets":["04-24 18:00"],"lanes":[{"model":"qwen-flash","provider":"","source":"","route_label":"default-route","success_rate":"98.00%","average_latency":"182 ms","cells":[{"bucket_label":"04-24 18:00","latency":"148 ms","status":"健康","requests":"12 次"}]}]}`
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
				RecentEventItems: []service.UsageFailureEventItem{{
					Time:          "04-24 18:00",
					TenantID:      "tenant_demo",
					TenantName:    "Demo Tenant",
					RequestModel:  "qwen-flash",
					ResolvedModel: "qwen-flash",
					Provider:      "阿里云百炼",
					StatusCode:    429,
					Category:      "限流",
					Reason:        "上游返回 429，请求被限流",
				}},
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
	expected := `{"breakdown":[{"label":"限流","value":"2 次"}],"recent_events":["04-24 18:00 · 限流 · 请求失败（429）"],"recent_event_items":[{"time":"04-24 18:00","tenant_id":"tenant_demo","tenant_name":"Demo Tenant","request_model":"qwen-flash","resolved_model":"qwen-flash","provider":"阿里云百炼","status_code":429,"category":"限流","reason":"上游返回 429，请求被限流"}]}`
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
						RequestID:           "llmreq_demo_002",
						Tenant:              "tenant_demo",
						TenantID:            "tenant_demo",
						TenantName:          "Demo Tenant",
						Endpoint:            "/v1/embeddings",
						Model:               "text-embedding-3-small",
						ResolvedModel:       "text-embedding-3-small",
						TaskClass:           "embedding_simple",
						RoutingReason:       "model:direct",
						TargetModelTier:     "text-embedding-3-small",
						Status:              "限流",
						TotalTokens:         "16",
						InputTokens:         "16",
						OutputTokens:        "0",
						CachedTokens:        "5",
						Latency:             "95 ms",
						FirstTokenLatencyMS: 0,
						UsageSource:         "估算",
						InputCost:           "1.75 ￥",
						OutputCost:          "0.00 ￥",
						CachedCost:          "0.25 ￥",
						TotalCost:           "2.00 ￥",
						InputPrice:          "2.50 ￥/M",
						OutputPrice:         "0.00 ￥/M",
						CachedPrice:         "0.75 ￥/M",
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
	expected := `{"items":[{"request_id":"llmreq_demo_002","tenant":"tenant_demo","tenant_id":"tenant_demo","tenant_name":"Demo Tenant","endpoint":"/v1/embeddings","model":"text-embedding-3-small","resolved_model":"text-embedding-3-small","task_class":"embedding_simple","routing_reason":"model:direct","target_model_tier":"text-embedding-3-small","status":"限流","total_tokens":"16","input_tokens":"16","output_tokens":"0","cached_tokens":"5","latency":"95 ms","first_token_latency_ms":0,"usage_source":"估算","input_cost":"1.75 ￥","output_cost":"0.00 ￥","cached_cost":"0.25 ￥","total_cost":"2.00 ￥","input_price":"2.50 ￥/M","output_price":"0.00 ￥/M","cached_price":"0.75 ￥/M"}],"total":1,"limit":20,"offset":0}`
	if string(body) != expected {
		t.Fatalf("expected body %q, got %q", expected, string(body))
	}
}

func TestAdminUsageRequestDetailRouteReturnsConsoleData(t *testing.T) {
	t.Parallel()

	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "test-console-user",
		ServiceAuthPassword: "test-console-password",
		ConsoleService: stubConsoleService{
			usageRequestDetail: service.UsageRequestDetail{
				RequestID:           "llmreq_demo_002",
				TenantID:            "tenant_demo",
				TenantName:          "Demo Tenant",
				Endpoint:            "/v1/chat/completions",
				Model:               "qwen-flash",
				ResolvedModel:       "qwen-flash",
				TaskClass:           "chat_simple",
				RoutingReason:       "model:direct",
				TargetModelTier:     "fast",
				Status:              "成功",
				TotalTokens:         "18",
				InputTokens:         "12",
				OutputTokens:        "6",
				CachedTokens:        "0",
				Latency:             "120 ms",
				FirstTokenLatencyMS: 33,
				UsageSource:         "上游返回",
				InputCost:           "0.01 ￥",
				OutputCost:          "0.03 ￥",
				CachedCost:          "0.00 ￥",
				TotalCost:           "0.04 ￥",
				InputPrice:          "2.00 ￥/M",
				OutputPrice:         "20.00 ￥/M",
				CachedPrice:         "0.50 ￥/M",
				PromptExcerpt:       "你好，手机号 138XXXX0000",
				ResponseExcerpt:     "你好，请问有什么可以帮你？",
				ErrorCode:           "",
				ErrorMessage:        "",
				FailureEvents: []service.UsageFailureEventItem{{
					Time:          "04-24 18:00",
					TenantID:      "tenant_demo",
					TenantName:    "Demo Tenant",
					RequestModel:  "qwen-flash",
					ResolvedModel: "qwen-flash",
					Provider:      "阿里云百炼",
					StatusCode:    0,
					Category:      "",
					Reason:        "调用成功",
				}},
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/requests/llmreq_demo_002", nil)
	req.SetBasicAuth("test-console-user", "test-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
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
		"/admin/usage/requests?tenant_id=tenant_demo&platform_api_key_id=pak_demo&resolved_model=qwen-flash&status=success&from=2026-04-24T10:00:00Z&to=2026-04-24T11:00:00Z&limit=30&offset=60",
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
	if captured.ResolvedModel != "qwen-flash" {
		t.Fatalf("expected resolved_model qwen-flash, got %q", captured.ResolvedModel)
	}
	if captured.Status != "success" {
		t.Fatalf("expected status success, got %q", captured.Status)
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
			wantBody:   "认证失败：API Key 无效或已过期",
		},
		{
			name:       "quota exceeded returns generic quota exceeded",
			authErr:    fmt.Errorf("%w: tenant tenant_123", service.ErrQuotaExceeded),
			wantStatus: http.StatusTooManyRequests,
			wantBody:   "租户额度不足：请求次数或 Token 配额已耗尽",
		},
		{
			name:       "model not allowed returns chinese forbidden reason",
			authErr:    fmt.Errorf("%w: deepseek-r1-distill-qwen-7b", service.ErrModelNotAllowed),
			wantStatus: http.StatusForbidden,
			wantBody:   "模型未授权：当前租户不可使用该模型",
		},
		{
			name:       "route resolution failure returns bad gateway",
			authErr:    fmt.Errorf("%w: no provider for requested model", service.ErrRouteNotFound),
			wantStatus: http.StatusBadGateway,
			wantBody:   "路由解析失败：未找到可用的模型映射",
		},
		{
			name:       "internal failures return generic internal error",
			authErr:    errors.New("sql: connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   "服务暂时不可用，请稍后重试",
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

func TestChatCompletionRouteUsesSmartRoutingDecisionBeforeAuthResolve(t *testing.T) {
	t.Parallel()

	authService := &capturingAuthService{
		requestContext: domain.RequestContext{
			TenantID:             "tenant_123",
			PlatformAPIKeyID:     "pak_123",
			PlatformAPIKeyName:   "demo key",
			SelectedProviderID:   "pc_reasoning",
			SelectedProviderName: "Reasoning Route",
			RouteID:              "route:pc_reasoning:default",
			ProviderTarget: domain.ProviderTarget{
				CredentialID: "pc_reasoning",
				Provider:     "openai",
				BaseURL:      "https://example.com",
				APIKey:       "upstream-key",
			},
		},
	}
	chatProxy := &capturingChatProxyService{
		response: service.ChatResponse{
			Model: "gateway-chat-reasoning",
			Choices: []service.ChatChoice{
				{Message: service.ChatMessage{Role: "assistant", Content: "done"}},
			},
		},
	}
	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		AuthService: authService,
		ChatProxy:   chatProxy,
		SmartRouter: stubSmartRouter{
			decision: service.SmartRoutingDecision{
				TaskClass:       "coding_complex",
				TargetModelTier: "gateway-chat-reasoning",
				MatchedRules:    []string{"keyword:debug", "pattern:code_fence"},
			},
		},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		strings.NewReader("{\"messages\":[{\"role\":\"user\",\"content\":\"please debug this panic ```go\\npanic(\\\"x\\\")\\n```\"}]}"),
	)
	req.Header.Set("Authorization", "Bearer platform-live-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if authService.requestedModel != "gateway-chat-reasoning" {
		t.Fatalf("expected requested model %q, got %q", "gateway-chat-reasoning", authService.requestedModel)
	}
	if chatProxy.request.Model != "gateway-chat-reasoning" {
		t.Fatalf("expected proxy request model %q, got %q", "gateway-chat-reasoning", chatProxy.request.Model)
	}
	if chatProxy.requestContext.RequestedModel != "" {
		t.Fatalf("expected request context requested model %q, got %q", "", chatProxy.requestContext.RequestedModel)
	}
	if chatProxy.requestContext.ResolvedModel != "gateway-chat-reasoning" {
		t.Fatalf("expected resolved model %q, got %q", "gateway-chat-reasoning", chatProxy.requestContext.ResolvedModel)
	}
	if chatProxy.requestContext.TargetModelTier != "gateway-chat-reasoning" {
		t.Fatalf("expected target model tier %q, got %q", "gateway-chat-reasoning", chatProxy.requestContext.TargetModelTier)
	}
	if chatProxy.requestContext.TaskClass != "coding_complex" {
		t.Fatalf("expected task class %q, got %q", "coding_complex", chatProxy.requestContext.TaskClass)
	}
	if chatProxy.requestContext.RoutingReason != "keyword:debug,pattern:code_fence" {
		t.Fatalf("expected routing reason to be recorded, got %q", chatProxy.requestContext.RoutingReason)
	}
}

func TestChatCompletionRouteHonorsExplicitModelBeforeSmartRouting(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name              string
		requestModel      string
		wantResolvedModel string
	}{
		{
			name:              "qwen flash",
			requestModel:      "qwen-flash",
			wantResolvedModel: "qwen-flash",
		},
		{
			name:              "mimo alias",
			requestModel:      "mimo",
			wantResolvedModel: "mimo-v2.5-pro",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			authService := &capturingAuthService{
				requestContext: domain.RequestContext{
					TenantID:             "tenant_123",
					PlatformAPIKeyID:     "pak_123",
					PlatformAPIKeyName:   "demo key",
					SelectedProviderID:   "pc_explicit",
					SelectedProviderName: "Explicit Route",
					RouteID:              "route:pc_explicit:default",
					ProviderTarget: domain.ProviderTarget{
						CredentialID: "pc_explicit",
						Provider:     "openai",
						BaseURL:      "https://example.com",
						APIKey:       "upstream-key",
					},
				},
			}
			chatProxy := &capturingChatProxyService{
				response: service.ChatResponse{
					Model: tc.wantResolvedModel,
					Choices: []service.ChatChoice{
						{Message: service.ChatMessage{Role: "assistant", Content: "done"}},
					},
				},
			}
			app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
				AuthService: authService,
				ChatProxy:   chatProxy,
				SmartRouter: stubSmartRouter{
					decision: service.SmartRoutingDecision{
						TaskClass:       "coding_complex",
						TargetModelTier: "gateway-chat-reasoning",
						MatchedRules:    []string{"keyword:debug", "pattern:code_fence"},
					},
				},
			})

			req := httptest.NewRequest(
				http.MethodPost,
				"/v1/chat/completions",
				strings.NewReader(fmt.Sprintf("{\"model\":%q,\"messages\":[{\"role\":\"user\",\"content\":\"please debug this panic ```go\\npanic(\\\"x\\\")\\n```\"}]}", tc.requestModel)),
			)
			req.Header.Set("Authorization", "Bearer platform-live-key")
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test failed: %v", err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
			if authService.requestedModel != tc.wantResolvedModel {
				t.Fatalf("expected auth requested model %q, got %q", tc.wantResolvedModel, authService.requestedModel)
			}
			if chatProxy.request.Model != tc.wantResolvedModel {
				t.Fatalf("expected proxy request model %q, got %q", tc.wantResolvedModel, chatProxy.request.Model)
			}
			if chatProxy.requestContext.RequestedModel != tc.requestModel {
				t.Fatalf("expected request context requested model %q, got %q", tc.requestModel, chatProxy.requestContext.RequestedModel)
			}
			if chatProxy.requestContext.ResolvedModel != tc.wantResolvedModel {
				t.Fatalf("expected resolved model %q, got %q", tc.wantResolvedModel, chatProxy.requestContext.ResolvedModel)
			}
			if chatProxy.requestContext.TargetModelTier != tc.wantResolvedModel {
				t.Fatalf("expected target model tier %q, got %q", tc.wantResolvedModel, chatProxy.requestContext.TargetModelTier)
			}
			if chatProxy.requestContext.TaskClass != "explicit_model" {
				t.Fatalf("expected task class %q, got %q", "explicit_model", chatProxy.requestContext.TaskClass)
			}
			if chatProxy.requestContext.RoutingReason != "explicit_model:"+tc.requestModel {
				t.Fatalf("expected explicit routing reason, got %q", chatProxy.requestContext.RoutingReason)
			}
		})
	}
}

func TestChatCompletionRouteReusesMiddlewareResolutionForExplicitModel(t *testing.T) {
	t.Parallel()

	authService := &countingAuthService{
		requestContext: domain.RequestContext{
			TenantID:             "tenant_123",
			PlatformAPIKeyID:     "pak_123",
			PlatformAPIKeyName:   "demo key",
			SelectedProviderID:   "pc_explicit",
			SelectedProviderName: "Explicit Route",
			RouteID:              "route:pc_explicit:deepseek-r1-distill-qwen-7b",
			ProviderTarget: domain.ProviderTarget{
				CredentialID: "pc_explicit",
				Provider:     "openai",
				BaseURL:      "https://example.com",
				APIKey:       "upstream-key",
			},
		},
		failAfterFirst: fmt.Errorf("%w: deepseek-r1-distill-qwen-7b", service.ErrModelNotAllowed),
	}
	chatProxy := &capturingChatProxyService{
		response: service.ChatResponse{
			Model: "deepseek-r1-distill-qwen-7b",
			Choices: []service.ChatChoice{
				{Message: service.ChatMessage{Role: "assistant", Content: "done"}},
			},
		},
	}
	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		AuthService: authService,
		ChatProxy:   chatProxy,
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		strings.NewReader("{\"model\":\"deepseek-r1-distill-qwen-7b\",\"messages\":[{\"role\":\"user\",\"content\":\"你好\"}]}"),
	)
	req.Header.Set("Authorization", "Bearer platform-live-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if authService.calls != 1 {
		t.Fatalf("expected auth resolve to be called once, got %d", authService.calls)
	}
	if chatProxy.requestContext.ResolvedModel != "deepseek-r1-distill-qwen-7b" {
		t.Fatalf("expected resolved model to remain explicit model, got %q", chatProxy.requestContext.ResolvedModel)
	}
}

func TestNonChatV1RoutesStillUsePlatformAuthMiddleware(t *testing.T) {
	t.Parallel()

	baseRequestContext := domain.RequestContext{
		TenantID:             "tenant_123",
		PlatformAPIKeyID:     "pak_123",
		PlatformAPIKeyName:   "demo key",
		SelectedProviderID:   "pc_123",
		SelectedProviderName: "Default Route",
		RouteID:              "route:pc_123:default",
		ProviderTarget: domain.ProviderTarget{
			CredentialID: "pc_123",
			Provider:     "openai",
			BaseURL:      "https://example.com",
			APIKey:       "upstream-key",
		},
	}

	t.Run("auth-check", func(t *testing.T) {
		t.Parallel()

		authService := &capturingAuthService{requestContext: baseRequestContext}
		app := apphttp.NewRouterWithAuth(authService)
		req := httptest.NewRequest(http.MethodGet, "/v1/auth-check?model=gpt-4o-mini", nil)
		req.Header.Set("Authorization", "Bearer platform-live-key")

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if authService.requestedModel != "gpt-4o-mini" {
			t.Fatalf("expected requested model %q, got %q", "gpt-4o-mini", authService.requestedModel)
		}
	})

	t.Run("embeddings", func(t *testing.T) {
		t.Parallel()

		authService := &capturingAuthService{requestContext: baseRequestContext}
		embeddingProxy := &capturingEmbeddingProxyService{
			response: service.EmbeddingsResponse{
				Model: "text-embedding-3-small",
				Data:  []service.EmbeddingsDatum{{Embedding: []float64{0.1, 0.2}}},
			},
		}
		app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
			AuthService:    authService,
			EmbeddingProxy: embeddingProxy,
		})
		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/embeddings",
			strings.NewReader(`{"model":"text-embedding-3-small","input":"hello"}`),
		)
		req.Header.Set("Authorization", "Bearer platform-live-key")
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if authService.requestedModel != "text-embedding-3-small" {
			t.Fatalf("expected requested model %q, got %q", "text-embedding-3-small", authService.requestedModel)
		}
		if embeddingProxy.requestContext != baseRequestContext {
			t.Fatalf("expected middleware request context %+v, got %+v", baseRequestContext, embeddingProxy.requestContext)
		}
	})

	t.Run("internal-search", func(t *testing.T) {
		t.Parallel()

		authService := &capturingAuthService{requestContext: baseRequestContext}
		ragProxy := &capturingRAGProxyService{
			response: service.RAGQueryResponse{Answer: "ok"},
		}
		app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
			AuthService: authService,
			RAGProxy:    ragProxy,
		})
		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/internal-search",
			strings.NewReader(`{"knowledge_base_id":"kb_demo","question":"hello"}`),
		)
		req.Header.Set("Authorization", "Bearer platform-live-key")
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if authService.requestedModel != "" {
			t.Fatalf("expected requested model %q, got %q", "", authService.requestedModel)
		}
		if ragProxy.requestContext != baseRequestContext {
			t.Fatalf("expected middleware request context %+v, got %+v", baseRequestContext, ragProxy.requestContext)
		}
	})
}

func TestChatCompletionRouteRejectsInvalidPayloadBeforeUpstream(t *testing.T) {
	t.Parallel()

	authService := &capturingAuthService{}
	chatProxy := &capturingChatProxyService{}
	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		AuthService: authService,
		ChatProxy:   chatProxy,
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		strings.NewReader(`{"messages":[]}`),
	)
	req.Header.Set("Authorization", "Bearer platform-live-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if authService.rawKey != "" {
		t.Fatalf("expected auth not to run, got rawKey %q", authService.rawKey)
	}
	if len(chatProxy.request.Messages) != 0 {
		t.Fatalf("expected proxy not to receive request, got %+v", chatProxy.request)
	}
}

func TestEmbeddingsRouteRejectsInvalidPayloadBeforeUpstream(t *testing.T) {
	t.Parallel()

	authService := &capturingAuthService{requestContext: domain.RequestContext{
		TenantID:           "tenant_123",
		PlatformAPIKeyID:   "pak_123",
		PlatformAPIKeyName: "demo key",
		ProviderTarget: domain.ProviderTarget{
			CredentialID: "pc_123",
			Provider:     "openai",
			BaseURL:      "https://example.com",
			APIKey:       "upstream-key",
		},
	}}
	embeddingProxy := &capturingEmbeddingProxyService{}
	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		AuthService:    authService,
		EmbeddingProxy: embeddingProxy,
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/embeddings",
		strings.NewReader(`{"model":"text-embedding-3-small","input":""}`),
	)
	req.Header.Set("Authorization", "Bearer platform-live-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if embeddingProxy.request.Model != "" || embeddingProxy.request.Input != nil {
		t.Fatalf("expected proxy not to receive request, got %+v", embeddingProxy.request)
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

type countingAuthService struct {
	calls          int
	requestContext domain.RequestContext
	failAfterFirst error
}

func (s *countingAuthService) Resolve(context.Context, string, string) (domain.RequestContext, error) {
	s.calls++
	if s.calls > 1 && s.failAfterFirst != nil {
		return domain.RequestContext{}, s.failAfterFirst
	}
	return s.requestContext, nil
}

type stubSmartRouter struct {
	decision service.SmartRoutingDecision
}

func (s stubSmartRouter) Decide(service.ChatRequest) service.SmartRoutingDecision {
	return s.decision
}

type capturingChatProxyService struct {
	request        service.ChatRequest
	requestContext domain.RequestContext
	response       service.ChatResponse
}

func (s *capturingChatProxyService) Complete(_ context.Context, req service.ChatRequest, resolved any) (service.ChatResponse, error) {
	s.request = req
	if requestContext, ok := resolved.(domain.RequestContext); ok {
		s.requestContext = requestContext
	}
	return s.response, nil
}

func (s *capturingChatProxyService) Stream(context.Context, service.ChatRequest, any) (service.ChatCompletionStream, error) {
	return service.ChatCompletionStream{}, nil
}

func (s *capturingChatProxyService) RecordFailure(context.Context, any, int) {}

type capturingEmbeddingProxyService struct {
	request        service.EmbeddingsRequest
	requestContext domain.RequestContext
	response       service.EmbeddingsResponse
}

func (s *capturingEmbeddingProxyService) Create(_ context.Context, req service.EmbeddingsRequest, resolved any) (service.EmbeddingsResponse, error) {
	s.request = req
	if requestContext, ok := resolved.(domain.RequestContext); ok {
		s.requestContext = requestContext
	}
	return s.response, nil
}

func (s *capturingEmbeddingProxyService) RecordFailure(context.Context, any, int) {}

type capturingRAGProxyService struct {
	request        service.RAGQueryRequest
	requestContext domain.RequestContext
	response       service.RAGQueryResponse
}

func (s *capturingRAGProxyService) Query(_ context.Context, req service.RAGQueryRequest, resolved any) (service.RAGQueryResponse, error) {
	s.request = req
	if requestContext, ok := resolved.(domain.RequestContext); ok {
		s.requestContext = requestContext
	}
	return s.response, nil
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
	systemStatus                     service.ConsoleSystemStatus
	captchaChallenge                 service.CaptchaChallenge
	captchaPassResult                service.CaptchaPassResult
	providerModels                   service.ProviderModelsPageData
	modelHealth                      service.ModelHealthPageData
	apiKeys                          service.APIKeysPageData
	apiKeyMutationResult             service.APIKeyMutationResult
	apiKeySecretView                 service.APIKeySecretView
	applications                     service.ApplicationsPageData
	applicationMutation              service.ApplicationMutationResult
	accountDeletions                 service.AccountDeletionApplicationsPageData
	accountDeletionMutation          service.AccountDeletionApplicationMutationResult
	createApplicationReqRef          *service.CreateApplicationRequest
	createProviderReqRef             *service.CreateProviderRequest
	createProviderModelReqRef        *service.CreateProviderModelRequest
	runProviderModelHealthcheckIDRef *string
	approveApplicationIDRef          *string
	approveApplicationReqRef         *service.ApproveApplicationRequest
	rejectApplicationIDRef           *string
	rejectApplicationReqRef          *service.RejectApplicationRequest
	approveAccountDeletionIDRef      *string
	approveAccountDeletionReqRef     *service.ReviewAccountDeletionApplicationRequest
	rejectAccountDeletionIDRef       *string
	rejectAccountDeletionReqRef      *service.ReviewAccountDeletionApplicationRequest
	providerMutation                 service.ProviderMutationResult
	providerModelMutation            service.ProviderModelMutationResult
	usageOverview                    service.UsageOverviewData
	usageTrends                      service.UsageTrendData
	usageLatencyWall                 service.UsageLatencyWallData
	usageFailures                    service.UsageFailureData
	usageRequests                    service.UsageRequestsPageData
	usageRequestDetail               service.UsageRequestDetail
	tenantBilling                    service.TenantBillingPageData
	usageQueryRef                    *service.UsageQuery
	modelHealthWindowRef             *string
	tenantBillingQueryRef            *service.TenantBillingQuery
	streamPlaygroundReqRef           *service.PlaygroundRunRequest
	streamPlaygroundSession          service.PlaygroundStreamSession
}

type stubMemberConsoleService struct {
	overview                    service.MemberOverviewPageData
	apiKeys                     service.MemberAPIKeysPageData
	apiKeyResult                service.APIKeyMutationResult
	apiKeySecretView            service.APIKeySecretView
	usageOverview               service.UsageOverviewData
	usageRequests               service.UsageRequestsPageData
	failures                    service.MemberFailurePageData
	auditEvents                 service.MemberAuditPageData
	principalRef                *service.ConsolePrincipal
	accountDeletionMutation     service.AccountDeletionApplicationMutationResult
	createAccountDeletionReqRef *service.CreateAccountDeletionApplicationRequest
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

func (s stubMemberConsoleService) RevealAPIKeySecret(ctx context.Context, _ string) (service.APIKeySecretView, error) {
	s.capturePrincipal(ctx)
	return s.apiKeySecretView, nil
}

func (s stubMemberConsoleService) CopyAPIKeySecret(ctx context.Context, _ string, _, _ string) (service.APIKeySecretView, error) {
	s.capturePrincipal(ctx)
	return s.apiKeySecretView, nil
}

func (s stubMemberConsoleService) CreateAccountDeletionApplication(ctx context.Context, req service.CreateAccountDeletionApplicationRequest) (service.AccountDeletionApplicationMutationResult, error) {
	s.capturePrincipal(ctx)
	if s.createAccountDeletionReqRef != nil {
		*s.createAccountDeletionReqRef = req
	}
	return s.accountDeletionMutation, nil
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

func (s stubConsoleService) IssueCaptcha(context.Context, string, string) (service.CaptchaChallenge, error) {
	return s.captchaChallenge, nil
}

func (s stubConsoleService) VerifyCaptcha(context.Context, service.VerifyCaptchaRequest) (service.CaptchaPassResult, error) {
	return s.captchaPassResult, nil
}

func (s stubConsoleService) APIKeys(context.Context) (service.APIKeysPageData, error) {
	return s.apiKeys, nil
}

func (s stubConsoleService) Applications(context.Context) (service.ApplicationsPageData, error) {
	return s.applications, nil
}

func (s stubConsoleService) AccountDeletionApplications(context.Context) (service.AccountDeletionApplicationsPageData, error) {
	return s.accountDeletions, nil
}

func (s stubConsoleService) CreateApplication(_ context.Context, req service.CreateApplicationRequest) (service.ApplicationMutationResult, error) {
	if s.createApplicationReqRef != nil {
		*s.createApplicationReqRef = req
	}
	return s.applicationMutation, nil
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

func (s stubConsoleService) RejectApplication(_ context.Context, id string, req service.RejectApplicationRequest) (service.ApplicationMutationResult, error) {
	if s.rejectApplicationIDRef != nil {
		*s.rejectApplicationIDRef = id
	}
	if s.rejectApplicationReqRef != nil {
		*s.rejectApplicationReqRef = req
	}
	return s.applicationMutation, nil
}

func (s stubConsoleService) ApproveAccountDeletionApplication(_ context.Context, id string, req service.ReviewAccountDeletionApplicationRequest) (service.AccountDeletionApplicationMutationResult, error) {
	if s.approveAccountDeletionIDRef != nil {
		*s.approveAccountDeletionIDRef = id
	}
	if s.approveAccountDeletionReqRef != nil {
		*s.approveAccountDeletionReqRef = req
	}
	return s.accountDeletionMutation, nil
}

func (s stubConsoleService) RejectAccountDeletionApplication(_ context.Context, id string, req service.ReviewAccountDeletionApplicationRequest) (service.AccountDeletionApplicationMutationResult, error) {
	if s.rejectAccountDeletionIDRef != nil {
		*s.rejectAccountDeletionIDRef = id
	}
	if s.rejectAccountDeletionReqRef != nil {
		*s.rejectAccountDeletionReqRef = req
	}
	return s.accountDeletionMutation, nil
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

func (s stubConsoleService) RevealAPIKeySecret(context.Context, string) (service.APIKeySecretView, error) {
	return s.apiKeySecretView, nil
}

func (s stubConsoleService) CopyAPIKeySecret(context.Context, string, string, string) (service.APIKeySecretView, error) {
	return s.apiKeySecretView, nil
}

func (s stubConsoleService) ProviderModels(context.Context) (service.ProviderModelsPageData, error) {
	return s.providerModels, nil
}

func (s stubConsoleService) CreateProvider(_ context.Context, req service.CreateProviderRequest) (service.ProviderMutationResult, error) {
	if s.createProviderReqRef != nil {
		*s.createProviderReqRef = req
	}
	return s.providerMutation, nil
}

func (s stubConsoleService) CreateProviderModel(_ context.Context, req service.CreateProviderModelRequest) (service.ProviderModelMutationResult, error) {
	if s.createProviderModelReqRef != nil {
		*s.createProviderModelReqRef = req
	}
	return s.providerModelMutation, nil
}

func (s stubConsoleService) RunProviderModelHealthcheck(_ context.Context, id string) (service.ProviderModelMutationResult, error) {
	if s.runProviderModelHealthcheckIDRef != nil {
		*s.runProviderModelHealthcheckIDRef = id
	}
	return s.providerModelMutation, nil
}

func (s stubConsoleService) ModelHealth(_ context.Context, window string) (service.ModelHealthPageData, error) {
	if s.modelHealthWindowRef != nil {
		*s.modelHealthWindowRef = window
	}
	return s.modelHealth, nil
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

func (s stubConsoleService) StreamPlayground(_ context.Context, req service.PlaygroundRunRequest) (service.PlaygroundStreamSession, error) {
	if s.streamPlaygroundReqRef != nil {
		*s.streamPlaygroundReqRef = req
	}
	return s.streamPlaygroundSession, nil
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

func (s stubConsoleService) TenantBilling(_ context.Context, query service.TenantBillingQuery) (service.TenantBillingPageData, error) {
	if s.tenantBillingQueryRef != nil {
		*s.tenantBillingQueryRef = query
	}
	return s.tenantBilling, nil
}

func (s stubConsoleService) UsageRequests(_ context.Context, query service.UsageQuery) (service.UsageRequestsPageData, error) {
	if s.usageQueryRef != nil {
		*s.usageQueryRef = query
	}
	return s.usageRequests, nil
}

func (s stubConsoleService) UsageRequestDetail(_ context.Context, _ string) (service.UsageRequestDetail, error) {
	return s.usageRequestDetail, nil
}
