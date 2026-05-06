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

func TestValidateDatabaseModeSecurityRequiresServiceAuthCredentials(t *testing.T) {
	t.Parallel()

	assertPanicContains(t, "GATEWAY_SERVICE_AUTH_USERNAME and GATEWAY_SERVICE_AUTH_PASSWORD are required in database mode", func() {
		validateDatabaseModeSecurity(config.Config{
			DatabaseURL: "postgres://gateway.example/db",
		})
	})
}

func TestValidateDatabaseModeSecurityRejectsDefaultWeakSecrets(t *testing.T) {
	t.Parallel()

	assertPanicContains(t, "GATEWAY_CONSOLE_SESSION_SECRET must be changed from the example value", func() {
		validateDatabaseModeSecurity(config.Config{
			DatabaseURL:             "postgres://gateway.example/db",
			ServiceAuthUsername:     "example-console-user",
			ServiceAuthPassword:     "strong-service-password",
			ConsoleSessionSecret:    "change-me-console-session-secret",
			SeedAdminPassword:       "strong-admin-password",
			SeedMemberPassword:      "strong-member-password",
			RAGServiceUsername:      "example-rag-user",
			RAGServicePassword:      "strong-rag-password",
			ProviderSecretKey:       "0123456789abcdef0123456789abcdef",
			SeedPlatformAPIKey:      "strong-platform-key",
			SeedProviderAPIKey:      "strong-provider-key",
			MIMOProviderAPIKey:      "strong-mimo-key",
			RabbitMQURL:             "amqp://example:strong-rabbit@rabbitmq:5672/ai_gateway",
			RedisURL:                "redis://example:strong-redis@redis:6379/0",
			PlatformAPIKeySecretKey: "abcdefghijklmnopqrstuvwxyz123456",
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
		bytes.NewBufferString(`{"messages":[{"role":"user","content":"请用一句话解释什么是 API Gateway。"}]}`),
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
		_, _ = io.WriteString(w, `{"model":"qwen-flash","choices":[{"message":{"content":"db-mode-answer"}}],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`)
	}))
	t.Cleanup(providerServer.Close)

	app := newServerApp(secureDatabaseConfig(config.Config{
		DatabaseURL:             dsn,
		RedisURL:                redisURL,
		SeedPlatformAPIKey:      "platform-live-key",
		SeedProviderBaseURL:     providerServer.URL + "/v1",
		SeedProviderAPIKey:      "provider-secret-key",
		SeedProvider:            "dashscope",
		SeedProviderDisplayName: "Qwen",
		MIMOProviderBaseURL:     "https://api.xiaomimimo.example/v1",
		MIMOProviderAPIKey:      "mimo-provider-secret-key",
		MIMOProviderDisplayName: "MIMO",
		ProviderSecretKey:       "0123456789abcdef0123456789abcdef",
		ChatFastModel:           "qwen-flash",
		ChatReasoningModel:      "mimo-v2.5-pro",
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"qwen-flash","messages":[{"role":"user","content":"hello"}]}`))
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
	if routeID != "route:provider_dashscope_primary:default" {
		t.Fatalf("expected route_id %q, got %q", "route:provider_dashscope_primary:default", routeID)
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
		  and provider_credential_id = 'provider_dashscope_primary'
		  and route_id = 'route:provider_dashscope_primary:default'
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

func TestNewServerAppDatabaseModeRoutesComplexChatToMIMO(t *testing.T) {
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

	qwenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("qwen provider should not receive complex coding chat requests, got %s", r.URL.Path)
	}))
	t.Cleanup(qwenServer.Close)

	var mimoModel string
	mimoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("json.NewDecoder failed: %v", err)
		}
		mimoModel = payload.Model

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"mimo-v2.5-pro","choices":[{"message":{"content":"mimo-answer"}}],"usage":{"prompt_tokens":21,"completion_tokens":9,"total_tokens":30}}`)
	}))
	t.Cleanup(mimoServer.Close)

	app := newServerApp(secureDatabaseConfig(config.Config{
		DatabaseURL:             dsn,
		RedisURL:                redisURL,
		SeedPlatformAPIKey:      "platform-live-key",
		SeedProviderBaseURL:     qwenServer.URL + "/v1",
		SeedProviderAPIKey:      "qwen-provider-secret-key",
		SeedProvider:            "dashscope",
		SeedProviderDisplayName: "Qwen",
		MIMOProviderBaseURL:     mimoServer.URL + "/v1",
		MIMOProviderAPIKey:      "mimo-provider-secret-key",
		MIMOProviderDisplayName: "MIMO",
		ProviderSecretKey:       "0123456789abcdef0123456789abcdef",
		ChatFastModel:           "qwen-flash",
		ChatReasoningModel:      "mimo-v2.5-pro",
	}))

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		bytes.NewBufferString("{\"messages\":[{\"role\":\"user\",\"content\":\"请帮我 debug 这段 Go panic，并给出修复代码 ```go\\npanic(\\\"x\\\")\\n```\"}]}"),
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
	if mimoModel != "mimo-v2.5-pro" {
		t.Fatalf("expected MIMO upstream model %q, got %q", "mimo-v2.5-pro", mimoModel)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	var routeID string
	var resolvedModel string
	if err := conn.QueryRow(ctx, `
		select route_id, resolved_model
		from llm_request_logs
		order by created_at desc
		limit 1
	`).Scan(&routeID, &resolvedModel); err != nil {
		t.Fatalf("QueryRow llm_request_logs failed: %v", err)
	}
	if routeID != "route:provider_mimo_primary:default" {
		t.Fatalf("expected route_id %q, got %q", "route:provider_mimo_primary:default", routeID)
	}
	if resolvedModel != "mimo-v2.5-pro" {
		t.Fatalf("expected resolved_model %q, got %q", "mimo-v2.5-pro", resolvedModel)
	}
}

func TestNewServerAppDatabaseModeRoutesEmbeddingsToQwen(t *testing.T) {
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

	var qwenPath string
	var qwenModel string
	qwenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("json.NewDecoder failed: %v", err)
		}
		qwenPath = r.URL.Path
		qwenModel = payload.Model

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"embedding":[0.1,0.2,0.3]}]}`)
	}))
	t.Cleanup(qwenServer.Close)

	mimoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("mimo provider should not receive embeddings requests, got %s", r.URL.Path)
	}))
	t.Cleanup(mimoServer.Close)

	app := newServerApp(secureDatabaseConfig(config.Config{
		DatabaseURL:             dsn,
		RedisURL:                redisURL,
		SeedPlatformAPIKey:      "platform-live-key",
		SeedProviderBaseURL:     qwenServer.URL + "/v1",
		SeedProviderAPIKey:      "qwen-provider-secret-key",
		SeedProvider:            "dashscope",
		SeedProviderDisplayName: "Qwen",
		MIMOProviderBaseURL:     mimoServer.URL + "/v1",
		MIMOProviderAPIKey:      "mimo-provider-secret-key",
		MIMOProviderDisplayName: "MIMO",
		ProviderSecretKey:       "0123456789abcdef0123456789abcdef",
		ChatFastModel:           "qwen-flash",
		ChatReasoningModel:      "mimo-v2.5-pro",
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewBufferString(`{"model":"text-embedding-v4","input":"hello"}`))
	req.Header.Set("Authorization", "Bearer platform-live-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if qwenPath != "/v1/embeddings" {
		t.Fatalf("expected qwen path %q, got %q", "/v1/embeddings", qwenPath)
	}
	if qwenModel != "text-embedding-v4" {
		t.Fatalf("expected qwen model %q, got %q", "text-embedding-v4", qwenModel)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	var routeID string
	var providerCredentialID string
	if err := conn.QueryRow(ctx, `
		select route_id, provider_credential_id
		from llm_request_logs
		order by created_at desc
		limit 1
	`).Scan(&routeID, &providerCredentialID); err != nil {
		t.Fatalf("QueryRow llm_request_logs failed: %v", err)
	}
	if routeID != "route:provider_dashscope_primary:text-embedding-v4" {
		t.Fatalf("expected route_id %q, got %q", "route:provider_dashscope_primary:text-embedding-v4", routeID)
	}
	if providerCredentialID != "provider_dashscope_primary" {
		t.Fatalf("expected provider_credential_id %q, got %q", "provider_dashscope_primary", providerCredentialID)
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

	app := newServerApp(secureDatabaseConfig(config.Config{
		DatabaseURL:             dsn,
		RedisURL:                redisURL,
		SeedPlatformAPIKey:      "platform-live-key",
		SeedProviderBaseURL:     "https://dashscope.aliyuncs.com/compatible-mode/v1",
		SeedProviderAPIKey:      "provider-secret-key",
		SeedProvider:            "dashscope",
		SeedProviderDisplayName: "Qwen",
		MIMOProviderBaseURL:     "https://api.xiaomimimo.example/v1",
		MIMOProviderAPIKey:      "mimo-provider-secret-key",
		MIMOProviderDisplayName: "MIMO",
		ProviderSecretKey:       "0123456789abcdef0123456789abcdef",
	}))

	loginReq := httptest.NewRequest(http.MethodPost, "/console/session/login", bytes.NewBufferString(`{"email":"member-a@example.com","password":"test-member-password"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginResp, err := app.Test(loginReq)
	if err != nil {
		t.Fatalf("login app.Test failed: %v", err)
	}
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("expected login 200, got %d", loginResp.StatusCode)
	}
	var loginPayload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(loginResp.Body).Decode(&loginPayload); err != nil {
		t.Fatalf("json.NewDecoder login failed: %v", err)
	}
	if loginPayload.Token == "" {
		t.Fatal("expected login token")
	}

	req := httptest.NewRequest(http.MethodGet, "/me/overview", nil)
	req.Header.Set("X-Service-User", "test-service-user")
	req.Header.Set("X-Service-Password", "test-service-password")
	req.Header.Set("X-Console-Session", loginPayload.Token)

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

func secureDatabaseConfig(cfg config.Config) config.Config {
	cfg.ServiceAuthUsername = "test-service-user"
	cfg.ServiceAuthPassword = "test-service-password"
	cfg.ConsoleSessionSecret = "test-console-session-secret"
	cfg.SeedAdminPassword = "test-admin-password"
	cfg.SeedMemberPassword = "test-member-password"
	return cfg
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
