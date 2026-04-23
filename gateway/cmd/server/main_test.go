package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liwenjian/ai_gateway/gateway/internal/config"
)

func TestNewServerAppAuthenticatesBootstrapRequest(t *testing.T) {
	t.Parallel()

	app := newServerApp(config.Config{
		BootstrapPlatformAPIKey:      "platform-live-key",
		BootstrapPlatformAPIKeyID:    "pak_bootstrap",
		BootstrapPlatformAPIKeyName:  "bootstrap key",
		BootstrapTenantID:            "tenant_bootstrap",
		BootstrapTenantName:          "Bootstrap Tenant",
		BootstrapProviderID:          "pc_bootstrap",
		BootstrapProvider:            "openai",
		BootstrapProviderDisplayName: "OpenAI Primary",
		BootstrapSupportedModels:     []string{"gpt-4o-mini"},
	})

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
}
