package http_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liwenjian/ai_gateway/gateway/internal/domain"
	apphttp "github.com/liwenjian/ai_gateway/gateway/internal/http"
	"github.com/liwenjian/ai_gateway/gateway/internal/service"
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
			SelectedProviderName: "OpenAI Primary",
		},
	}
	app := apphttp.NewRouterWithAuth(stub)
	req := httptest.NewRequest(http.MethodGet, "/v1/auth-check?model=openai", nil)
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
	if stub.requestedModel != "openai" {
		t.Fatalf("expected requested model %q, got %q", "openai", stub.requestedModel)
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
