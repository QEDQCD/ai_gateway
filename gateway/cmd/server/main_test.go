package main

import (
	"bytes"
	"encoding/json"
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

func TestNewServerAppRoutesRAGRequestsToDedicatedRAGService(t *testing.T) {
	t.Parallel()

	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("provider service should not receive rag requests")
	}))
	t.Cleanup(providerServer.Close)

	type ragRequest struct {
		Path     string
		TenantID string
	}
	var got ragRequest
	ragServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var payload struct {
			TenantID string `json:"tenant_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("json.NewDecoder failed: %v", err)
		}

		got = ragRequest{
			Path:     r.URL.Path,
			TenantID: payload.TenantID,
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"answer":"stub-answer","sources":[{"document_id":"doc_demo","chunk_id":"chunk_1","score":0.91}]}`)
	}))
	t.Cleanup(ragServer.Close)

	app := newServerApp(config.Config{
		BootstrapPlatformAPIKey:      "platform-live-key",
		BootstrapPlatformAPIKeyID:    "pak_bootstrap",
		BootstrapPlatformAPIKeyName:  "bootstrap key",
		BootstrapTenantID:            "tenant_bootstrap",
		BootstrapTenantName:          "Bootstrap Tenant",
		BootstrapProviderID:          "pc_bootstrap",
		BootstrapProvider:            "openai",
		BootstrapProviderDisplayName: "OpenAI Primary",
		BootstrapProviderBaseURL:     providerServer.URL + "/v1",
		BootstrapProviderAPIKey:      "provider-secret-key",
		BootstrapSupportedModels:     []string{"gpt-4o-mini"},
		RAGServiceBaseURL:            ragServer.URL,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/rag/query", bytes.NewBufferString(`{"tenant_id":"client_tenant","knowledge_base_id":"kb_demo","question":"Where is the answer?"}`))
	req.Header.Set("Authorization", "Bearer platform-live-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if got.Path != "/internal/rag/query" {
		t.Fatalf("expected rag path %q, got %q", "/internal/rag/query", got.Path)
	}
	if got.TenantID != "tenant_bootstrap" {
		t.Fatalf("expected tenant id %q, got %q", "tenant_bootstrap", got.TenantID)
	}
}
