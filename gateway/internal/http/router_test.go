package http_test

import (
	"io"
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	if string(body) != `{"status":"ok"}` {
		t.Fatalf("expected body %q, got %q", `{"status":"ok"}`, string(body))
	}
}
