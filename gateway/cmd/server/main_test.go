package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/ai_gateway/gateway/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestNewQuotaGuardFallsBackToStaticWhenRedisURLMissing(t *testing.T) {
	t.Parallel()

	guard := newQuotaGuard(config.Config{
		DatabaseURL: "postgres://gateway.example/db",
	})
	if guard == nil {
		t.Fatal("expected quota guard")
	}
}

func TestNewQuotaGuardPanicsInDatabaseModeWithInvalidRedisURL(t *testing.T) {
	t.Parallel()

	assertPanicContains(t, "invalid GATEWAY_REDIS_URL", func() {
		_ = newQuotaGuard(config.Config{
			DatabaseURL: "postgres://gateway.example/db",
			RedisURL:    "://not-a-redis-url",
		})
	})
}

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

func TestNewServerAppUsesConfiguredSmartRoutingTiersForChat(t *testing.T) {
	t.Parallel()

	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"qwen-flash","choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	t.Cleanup(providerServer.Close)

	app := newServerApp(config.Config{
		BootstrapPlatformAPIKey:         "platform-live-key",
		BootstrapPlatformAPIKeyID:       "pak_bootstrap",
		BootstrapPlatformAPIKeyName:     "bootstrap key",
		BootstrapTenantID:               "tenant_bootstrap",
		BootstrapTenantName:             "Bootstrap Tenant",
		BootstrapProviderID:             "pc_bootstrap",
		BootstrapProvider:               "openai",
		BootstrapProviderDisplayName:    "OpenAI Primary",
		BootstrapProviderBaseURL:        providerServer.URL + "/v1",
		BootstrapProviderAPIKey:         "provider-secret-key",
		BootstrapSupportedModels:        []string{"qwen-flash", "qwen-plus"},
		ChatFastModel:                   "qwen-flash",
		ChatReasoningModel:              "qwen-plus",
		SmartRoutingCodingKeywords:      []string{"debug", "写代码"},
		SmartRoutingLongPromptThreshold: 240,
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		bytes.NewBufferString(`{"model":"gateway-public","messages":[{"role":"user","content":"请用一句话解释什么是 API Gateway。"}]}`),
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

	req := httptest.NewRequest(http.MethodPost, "/v1/internal-search", bytes.NewBufferString(`{"tenant_id":"client_tenant","knowledge_base_id":"kb_demo","question":"Where is the answer?"}`))
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

func TestNewServerAppDatabaseModeWritesUsageObservability(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(ctx, t)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})
	redisContainer, redisURL := startRedisContainer(ctx, t)
	t.Cleanup(func() {
		_ = redisContainer.Terminate(context.Background())
	})

	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"gpt-4o-mini","choices":[{"message":{"content":"db-mode-answer"}}],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`)
	}))
	t.Cleanup(providerServer.Close)

	app := newServerApp(config.Config{
		DatabaseURL:             dsn,
		RedisURL:                redisURL,
		SeedPlatformAPIKey:      "platform-live-key",
		SeedProviderBaseURL:     providerServer.URL + "/v1",
		SeedProviderAPIKey:      "provider-secret-key",
		SeedProvider:            "openai",
		SeedProviderDisplayName: "OpenAI Primary",
		ProviderSecretKey:       "0123456789abcdef0123456789abcdef",
		ChatFastModel:           "gpt-4o-mini",
		ChatReasoningModel:      "gpt-4o-mini",
	})

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

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	var requestLogID string
	var routeID string
	var requestCompletedAt time.Time
	if err := conn.QueryRow(ctx, `
		select id, route_id, request_completed_at
		from llm_request_logs
		order by created_at desc
		limit 1
	`).Scan(&requestLogID, &routeID, &requestCompletedAt); err != nil {
		t.Fatalf("QueryRow llm_request_logs failed: %v", err)
	}
	if routeID != "route:provider_openai_primary:default" {
		t.Fatalf("expected route_id %q, got %q", "route:provider_openai_primary:default", routeID)
	}

	var eventType string
	if err := conn.QueryRow(ctx, `
		select event_type
		from llm_request_events
		where request_log_id = $1
		order by created_at asc
		limit 1
	`, requestLogID).Scan(&eventType); err != nil {
		t.Fatalf("QueryRow llm_request_events failed: %v", err)
	}
	if eventType != "response_received" {
		t.Fatalf("expected lifecycle event_type %q, got %q", "response_received", eventType)
	}

	var requestCount int
	var totalTokens int
	if err := conn.QueryRow(ctx, `
		select request_count, total_tokens
		from llm_usage_agg_hourly
		where bucket_start = date_trunc('hour', $1::timestamptz)
		  and tenant_id = 'tenant_alpha'
		  and platform_api_key_id = 'pak_live_console'
		  and provider_credential_id = 'provider_openai_primary'
		  and route_id = 'route:provider_openai_primary:default'
		  and request_path = '/v1/chat/completions'
		  and usage_source = 'upstream'
		  and usage_status = 'success'
	`, requestCompletedAt).Scan(&requestCount, &totalTokens); err != nil {
		t.Fatalf("QueryRow llm_usage_agg_hourly failed: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected request_count 1, got %d", requestCount)
	}
	if totalTokens != 18 {
		t.Fatalf("expected total_tokens 18, got %d", totalTokens)
	}
}

func TestNewServerAppDatabaseModeWiresMemberOverview(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(ctx, t)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})
	redisContainer, redisURL := startRedisContainer(ctx, t)
	t.Cleanup(func() {
		_ = redisContainer.Terminate(context.Background())
	})

	app := newServerApp(config.Config{
		DatabaseURL:             dsn,
		RedisURL:                redisURL,
		SeedPlatformAPIKey:      "platform-live-key",
		SeedProviderBaseURL:     "https://api.openai.example/v1",
		SeedProviderAPIKey:      "provider-secret-key",
		SeedProvider:            "openai",
		SeedProviderDisplayName: "OpenAI Primary",
		ProviderSecretKey:       "0123456789abcdef0123456789abcdef",
	})

	req := httptest.NewRequest(http.MethodGet, "/me/overview", nil)
	req.Header.Set("X-Console-Subject", "member-a@example.com")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var payload struct {
		TenantID      string `json:"tenant_id"`
		TenantName    string `json:"tenant_name"`
		ActiveAPIKeys int    `json:"active_api_keys"`
		Quota         struct {
			Configured   bool  `json:"configured"`
			RequestLimit int64 `json:"request_limit"`
			TokenLimit   int64 `json:"token_limit"`
		} `json:"quota"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("json.NewDecoder failed: %v", err)
	}
	if payload.TenantID != "tenant_alpha" {
		t.Fatalf("expected tenant_id %q, got %q", "tenant_alpha", payload.TenantID)
	}
	if payload.TenantName != "Alpha 租户" {
		t.Fatalf("expected tenant_name %q, got %q", "Alpha 租户", payload.TenantName)
	}
	if payload.ActiveAPIKeys != 0 {
		t.Fatalf("expected active_api_keys %d, got %d", 0, payload.ActiveAPIKeys)
	}
	if !payload.Quota.Configured {
		t.Fatal("expected quota to be configured")
	}
	if payload.Quota.RequestLimit == 0 || payload.Quota.TokenLimit == 0 {
		t.Fatalf("expected quota limits to be populated, got %+v", payload.Quota)
	}
}

func startPostgresContainer(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
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
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("container.MappedPort failed: %v", err)
	}

	dsn := fmt.Sprintf("postgres://postgres:postgres@%s:%s/gateway_test?sslmode=disable", host, port.Port())
	return container, dsn
}

func startRedisContainer(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
	t.Helper()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor: wait.ForLog("Ready to accept connections").
				WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("GenericContainer redis failed: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("redis container.Host failed: %v", err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatalf("redis container.MappedPort failed: %v", err)
	}

	return container, fmt.Sprintf("redis://%s:%s/0", host, port.Port())
}

func assertPanicContains(t *testing.T, want string, fn func()) {
	t.Helper()

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("expected panic containing %q", want)
		}
		if !strings.Contains(fmt.Sprint(recovered), want) {
			t.Fatalf("expected panic containing %q, got %v", want, recovered)
		}
	}()

	fn()
}
