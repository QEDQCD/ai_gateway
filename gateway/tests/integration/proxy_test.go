package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/ai_gateway/gateway/internal/domain"
	apphttp "github.com/example/ai_gateway/gateway/internal/http"
	"github.com/example/ai_gateway/gateway/internal/provider"
	"github.com/example/ai_gateway/gateway/internal/queue"
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/example/ai_gateway/gateway/internal/store"
	"github.com/gofiber/fiber/v2"
)

func TestChatCompletionProxy(t *testing.T) {
	t.Parallel()

	type stubProviderRequest struct {
		Authorization string
		Model         string
		Message       string
	}

	var gotProviderRequest stubProviderRequest
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var payload struct {
			Model    string `json:"model"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("json.NewDecoder failed: %v", err)
		}

		gotProviderRequest = stubProviderRequest{
			Authorization: r.Header.Get("Authorization"),
			Model:         payload.Model,
		}
		if len(payload.Messages) > 0 {
			gotProviderRequest.Message = payload.Messages[0].Content
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"stub-answer"}}],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`)
	}))
	t.Cleanup(providerServer.Close)

	app, usagePublisher := newGatewayApp(t, providerServer.URL+"/v1", providerServer.URL)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`))
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
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("json.NewDecoder failed: %v", err)
	}
	if got := body.Choices[0].Message.Content; got != "stub-answer" {
		t.Fatalf("expected stub-answer, got %q", got)
	}

	if gotProviderRequest.Authorization != "Bearer provider-secret-key" {
		t.Fatalf("expected provider credential auth header, got %q", gotProviderRequest.Authorization)
	}
	if gotProviderRequest.Authorization == "Bearer platform-live-key" {
		t.Fatal("expected upstream auth to use provider credential instead of platform key")
	}
	if gotProviderRequest.Model != "gpt-4o-mini" {
		t.Fatalf("expected upstream model %q, got %q", "gpt-4o-mini", gotProviderRequest.Model)
	}

	events := usagePublisher.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 usage event, got %d", len(events))
	}
	if events[0].TenantID != "tenant_demo" {
		t.Fatalf("expected tenant id %q, got %q", "tenant_demo", events[0].TenantID)
	}
	if events[0].PlatformAPIKeyID != "pak_demo" {
		t.Fatalf("expected platform api key id %q, got %q", "pak_demo", events[0].PlatformAPIKeyID)
	}
	if events[0].PlatformAPIKeyName != "demo key" {
		t.Fatalf("expected platform api key name %q, got %q", "demo key", events[0].PlatformAPIKeyName)
	}
	if events[0].ProviderCredentialID != "provider_qwen_primary" {
		t.Fatalf("expected provider credential id %q, got %q", "provider_qwen_primary", events[0].ProviderCredentialID)
	}
	if events[0].RouteID != "route:provider_qwen_primary:default" {
		t.Fatalf("expected route id %q, got %q", "route:provider_qwen_primary:default", events[0].RouteID)
	}
	if events[0].RequestID == "" {
		t.Fatal("expected request id to be populated")
	}
	if events[0].Provider != "openai" {
		t.Fatalf("expected provider %q, got %q", "openai", events[0].Provider)
	}
	if events[0].Model != "gpt-4o-mini" {
		t.Fatalf("expected model %q, got %q", "gpt-4o-mini", events[0].Model)
	}
	if events[0].Status != "success" {
		t.Fatalf("expected status %q, got %q", "success", events[0].Status)
	}
	if events[0].UsageSource != "upstream" {
		t.Fatalf("expected usage source %q, got %q", "upstream", events[0].UsageSource)
	}
	if events[0].PromptTokens != 11 || events[0].CompletionTokens != 7 || events[0].TotalTokens != 18 {
		t.Fatalf("expected upstream usage 11/7/18, got %d/%d/%d", events[0].PromptTokens, events[0].CompletionTokens, events[0].TotalTokens)
	}
	if events[0].Endpoint != "/v1/chat/completions" {
		t.Fatalf("expected endpoint %q, got %q", "/v1/chat/completions", events[0].Endpoint)
	}
	if events[0].StatusCode != http.StatusOK {
		t.Fatalf("expected status code %d, got %d", http.StatusOK, events[0].StatusCode)
	}
	if events[0].LatencyMS <= 0 {
		t.Fatalf("expected latency_ms > 0, got %d", events[0].LatencyMS)
	}
}

func TestChatCompletionProxyPublishesUsageOnInvalidBody(t *testing.T) {
	t.Parallel()

	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("provider should not be called when request body is invalid")
	}))
	t.Cleanup(providerServer.Close)

	app, usagePublisher := newGatewayApp(t, providerServer.URL+"/v1", providerServer.URL)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":`))
	req.Header.Set("Authorization", "Bearer platform-live-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	events := usagePublisher.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 usage event, got %d", len(events))
	}
	if events[0].Endpoint != "/v1/chat/completions" {
		t.Fatalf("expected endpoint %q, got %q", "/v1/chat/completions", events[0].Endpoint)
	}
	if events[0].StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status code %d, got %d", http.StatusBadRequest, events[0].StatusCode)
	}
	if events[0].TenantID != "tenant_demo" {
		t.Fatalf("expected tenant id %q, got %q", "tenant_demo", events[0].TenantID)
	}
	if events[0].PlatformAPIKeyName != "demo key" {
		t.Fatalf("expected platform api key name %q, got %q", "demo key", events[0].PlatformAPIKeyName)
	}
	if events[0].ProviderCredentialID != "provider_qwen_primary" {
		t.Fatalf("expected provider credential id %q, got %q", "provider_qwen_primary", events[0].ProviderCredentialID)
	}
	if events[0].RouteID != "route:provider_qwen_primary:default" {
		t.Fatalf("expected route id %q, got %q", "route:provider_qwen_primary:default", events[0].RouteID)
	}
	if events[0].RequestID == "" {
		t.Fatal("expected request id to be populated")
	}
	if events[0].Status != "failed" {
		t.Fatalf("expected status %q, got %q", "failed", events[0].Status)
	}
	if events[0].UsageSource != "estimated" {
		t.Fatalf("expected usage source %q, got %q", "estimated", events[0].UsageSource)
	}
}

func TestEmbeddingsProxyReturnsSuccess(t *testing.T) {
	t.Parallel()

	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("expected upstream path %q, got %q", "/v1/embeddings", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer provider-secret-key" {
			t.Fatalf("expected provider credential auth header, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"embedding":[0.1,0.2]}]}`)
	}))
	t.Cleanup(providerServer.Close)

	app, usagePublisher := newGatewayApp(t, providerServer.URL+"/v1", providerServer.URL)

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewBufferString(`{"model":"text-embedding-3-small","input":"hello"}`))
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
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("json.NewDecoder failed: %v", err)
	}
	if len(body.Data) != 1 || len(body.Data[0].Embedding) != 2 {
		t.Fatalf("expected one embedding vector, got %#v", body.Data)
	}

	events := usagePublisher.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 usage event, got %d", len(events))
	}
	if events[0].Endpoint != "/v1/embeddings" {
		t.Fatalf("expected endpoint %q, got %q", "/v1/embeddings", events[0].Endpoint)
	}
	if events[0].StatusCode != http.StatusOK {
		t.Fatalf("expected status code %d, got %d", http.StatusOK, events[0].StatusCode)
	}
	if events[0].PlatformAPIKeyName != "demo key" {
		t.Fatalf("expected platform api key name %q, got %q", "demo key", events[0].PlatformAPIKeyName)
	}
	if events[0].RouteID != "route:provider_qwen_primary:text-embedding-3-small" {
		t.Fatalf("expected route id %q, got %q", "route:provider_qwen_primary:text-embedding-3-small", events[0].RouteID)
	}
	if events[0].RequestID == "" {
		t.Fatal("expected request id to be populated")
	}
	if events[0].Provider != "openai" {
		t.Fatalf("expected provider %q, got %q", "openai", events[0].Provider)
	}
	if events[0].Model != "text-embedding-3-small" {
		t.Fatalf("expected model %q, got %q", "text-embedding-3-small", events[0].Model)
	}
	if events[0].Status != "success" {
		t.Fatalf("expected status %q, got %q", "success", events[0].Status)
	}
	if events[0].UsageSource != "estimated" {
		t.Fatalf("expected usage source %q, got %q", "estimated", events[0].UsageSource)
	}
	if events[0].PromptTokens <= 0 || events[0].TotalTokens <= 0 {
		t.Fatalf("expected estimated tokens to be populated, got %d/%d/%d", events[0].PromptTokens, events[0].CompletionTokens, events[0].TotalTokens)
	}
}

func TestChatCompletionProxyPassesPlatformKeyNameAndRouteIDThroughRequestContext(t *testing.T) {
	t.Parallel()

	chatProxy := &capturingChatProxyService{}
	app := newGatewayAppWithChatProxy(t, chatProxy)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Authorization", "Bearer platform-live-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if chatProxy.requestContext.PlatformAPIKeyName != "demo key" {
		t.Fatalf("expected platform api key name %q, got %q", "demo key", chatProxy.requestContext.PlatformAPIKeyName)
	}
	if chatProxy.requestContext.RouteID != "route:provider_qwen_primary:default" {
		t.Fatalf("expected route id %q, got %q", "route:provider_qwen_primary:default", chatProxy.requestContext.RouteID)
	}
	if chatProxy.requestContext.RouteID == chatProxy.requestContext.SelectedProviderID {
		t.Fatalf("expected route id to differ from provider credential id, got %q", chatProxy.requestContext.RouteID)
	}
}

func newGatewayApp(t *testing.T, providerBaseURL string, ragBaseURL string) (*fiber.App, *queue.RecordingUsagePublisher) {
	t.Helper()

	repository := store.NewBootstrapAuthRepository(store.BootstrapAuthConfig{
		RawPlatformAPIKey:    "platform-live-key",
		PlatformAPIKeyID:     "pak_demo",
		PlatformAPIKeyName:   "demo key",
		TenantID:             "tenant_demo",
		TenantName:           "Demo Tenant",
		ProviderCredentialID: "provider_qwen_primary",
		Provider:             "openai",
		ProviderDisplayName:  "OpenAI Primary",
		SupportedModels:      []string{"gpt-4o-mini", "text-embedding-3-small"},
		ProviderBaseURL:      providerBaseURL,
		ProviderAPIKey:       "provider-secret-key",
	})

	authService := service.NewAuthService(
		repository,
		service.NewRedisQuotaGuard(staticQuotaClient{}),
		service.NewRouteService(repository),
	)
	usagePublisher := queue.NewRecordingUsagePublisher()
	chatProxy := service.NewChatProxyService(provider.NewOpenAIClient(http.DefaultClient), usagePublisher)
	embeddingProxy := service.NewEmbeddingProxyService(provider.NewOpenAIClient(http.DefaultClient), usagePublisher)
	ragProxy := service.NewRAGProxyService(ragBaseURL, "", "", http.DefaultClient)
	return apphttp.NewRouterWithServices(authService, chatProxy, embeddingProxy, ragProxy), usagePublisher
}

func newGatewayAppWithChatProxy(t *testing.T, chatProxy service.ChatProxyService) *fiber.App {
	t.Helper()

	repository := store.NewBootstrapAuthRepository(store.BootstrapAuthConfig{
		RawPlatformAPIKey:    "platform-live-key",
		PlatformAPIKeyID:     "pak_demo",
		PlatformAPIKeyName:   "demo key",
		TenantID:             "tenant_demo",
		TenantName:           "Demo Tenant",
		ProviderCredentialID: "provider_qwen_primary",
		Provider:             "openai",
		ProviderDisplayName:  "OpenAI Primary",
		SupportedModels:      []string{"gpt-4o-mini", "text-embedding-3-small"},
		ProviderBaseURL:      "https://provider.example/v1",
		ProviderAPIKey:       "provider-secret-key",
	})

	authService := service.NewAuthService(
		repository,
		service.NewRedisQuotaGuard(staticQuotaClient{}),
		service.NewRouteService(repository),
	)

	return apphttp.NewRouterWithServices(
		authService,
		chatProxy,
		service.NewUnavailableEmbeddingProxyService(),
		service.NewUnavailableRAGProxyService(),
	)
}

type staticQuotaClient struct{}

func (staticQuotaClient) Exists(context.Context, string) (bool, error) {
	return false, nil
}

type capturingChatProxyService struct {
	requestContext domain.RequestContext
}

func (s *capturingChatProxyService) Complete(_ context.Context, _ service.ChatRequest, resolved any) (service.ChatResponse, error) {
	requestContext, ok := resolved.(domain.RequestContext)
	if !ok {
		return service.ChatResponse{}, service.StatusError{Code: http.StatusUnauthorized, Message: "unauthorized"}
	}
	s.requestContext = requestContext
	return service.ChatResponse{
		Choices: []service.ChatChoice{
			{Message: service.ChatMessage{Role: "assistant", Content: "captured"}},
		},
	}, nil
}

func (*capturingChatProxyService) RecordFailure(context.Context, any, int) {}
