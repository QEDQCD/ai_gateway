package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRAGQueryProxy(t *testing.T) {
	t.Parallel()

	type upstreamRequest struct {
		Path            string
		TenantID        string
		KnowledgeBaseID string
		Question        string
	}

	var got upstreamRequest
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var payload struct {
			TenantID        string `json:"tenant_id"`
			KnowledgeBaseID string `json:"knowledge_base_id"`
			Question        string `json:"question"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("json.NewDecoder failed: %v", err)
		}

		got = upstreamRequest{
			Path:            r.URL.Path,
			TenantID:        payload.TenantID,
			KnowledgeBaseID: payload.KnowledgeBaseID,
			Question:        payload.Question,
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"answer":"stub-answer","sources":[{"document_id":"doc_demo","chunk_id":"chunk_1","score":0.91}]}`)
	}))
	t.Cleanup(providerServer.Close)

	app, _ := newGatewayApp(t, providerServer.URL+"/v1", providerServer.URL)

	req := httptest.NewRequest(http.MethodPost, "/v1/internal-search", bytes.NewBufferString(`{"tenant_id":"tenant_client","knowledge_base_id":"kb_demo","question":"Where is the answer?"}`))
	req.Header.Set("Authorization", "Bearer platform-live-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Answer  string `json:"answer"`
		Sources []struct {
			DocumentID string  `json:"document_id"`
			ChunkID    string  `json:"chunk_id"`
			Score      float64 `json:"score"`
		} `json:"sources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("json.NewDecoder failed: %v", err)
	}
	if body.Answer != "stub-answer" {
		t.Fatalf("expected answer %q, got %q", "stub-answer", body.Answer)
	}
	if len(body.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(body.Sources))
	}
	if body.Sources[0].DocumentID == "" {
		t.Fatal("expected response to include sources")
	}

	if got.Path != "/internal/rag/query" {
		t.Fatalf("expected upstream path %q, got %q", "/internal/rag/query", got.Path)
	}
	if got.TenantID != "tenant_demo" {
		t.Fatalf("expected upstream tenant id %q, got %q", "tenant_demo", got.TenantID)
	}
	if got.TenantID == "tenant_client" {
		t.Fatal("expected upstream tenant id to come from request context instead of client input")
	}
	if got.KnowledgeBaseID != "kb_demo" {
		t.Fatalf("expected upstream knowledge base id %q, got %q", "kb_demo", got.KnowledgeBaseID)
	}
	if got.Question != "Where is the answer?" {
		t.Fatalf("expected upstream question %q, got %q", "Where is the answer?", got.Question)
	}

	unauthorizedReq := httptest.NewRequest(http.MethodPost, "/v1/internal-search", bytes.NewBufferString(`{"knowledge_base_id":"kb_demo","question":"Where is the answer?"}`))
	unauthorizedReq.Header.Set("Authorization", "Bearer platform-wrong-key")
	unauthorizedReq.Header.Set("Content-Type", "application/json")

	unauthorizedResp, err := app.Test(unauthorizedReq)
	if err != nil {
		t.Fatalf("app.Test unauthorized request failed: %v", err)
	}
	if unauthorizedResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized request to return 401, got %d", unauthorizedResp.StatusCode)
	}
}
