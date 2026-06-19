package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	gatewaydb "github.com/example/ai_gateway/gateway/db"
	"github.com/example/ai_gateway/gateway/internal/domain"
	"github.com/example/ai_gateway/gateway/internal/secret"
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/bcrypt"
)

func TestPostgresConsoleServiceSystemStatus(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	payload, err := console.SystemStatus(ctx)
	if err != nil {
		t.Fatalf("SystemStatus failed: %v", err)
	}

	if payload.ConsoleStage != "控制台预览版" {
		t.Fatalf("expected console_stage 控制台预览版, got %q", payload.ConsoleStage)
	}
	if payload.RunMode != "数据库模式" {
		t.Fatalf("expected run_mode 数据库模式, got %q", payload.RunMode)
	}
	if payload.GatewayHealth != "告警" {
		t.Fatalf("expected gateway_health 告警, got %q", payload.GatewayHealth)
	}
	if payload.QuotaProtection != "已启用" {
		t.Fatalf("expected quota_protection 已启用, got %q", payload.QuotaProtection)
	}
	if payload.ConsoleEntry != "31873" {
		t.Fatalf("expected console_entry 31873, got %q", payload.ConsoleEntry)
	}
	if payload.GatewayAdminAPI != "32658" {
		t.Fatalf("expected gateway_admin_api 32658, got %q", payload.GatewayAdminAPI)
	}
	if len(payload.InternalServices) != 1 || payload.InternalServices[0] != "internal-search" {
		t.Fatalf("expected internal_services [internal-search], got %#v", payload.InternalServices)
	}
	if !containsString(payload.HiddenModules, "内部检索能力") || !containsString(payload.HiddenModules, "高级路由设置") {
		t.Fatalf("expected hidden_modules to include 内部检索能力 and 高级路由设置, got %#v", payload.HiddenModules)
	}
}

func TestPostgresConsoleServiceRoutesGroupsItemsByProvider(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		delete from llm_request_events;
		delete from llm_usage_agg_hourly;
		delete from llm_request_logs;
		delete from route_catalog;
		delete from provider_credentials;

		insert into provider_credentials (id, provider, display_name, supported_models, base_url, encrypted_secret, status) values
			('provider_dashscope_primary', 'dashscope', 'Qwen', '{"qwen-flash","text-embedding-v4"}', 'https://dashscope.aliyuncs.com/compatible-mode/v1', '', 'active'),
			('provider_mimo_primary', 'mimo', 'MIMO', '{"mimo-v2.5-pro"}', 'https://api.xiaomimimo.com/v1', '', 'active'),
			('provider_rag_service', 'rag', 'RAG', '{"rag-query"}', 'http://rag-service:8000', '', 'active');

		insert into route_catalog (id, requested_model, resolved_provider, provider_credential_id, endpoint, latency_ms, health_status, request_mode, updated_at) values
			('route:provider_dashscope_primary:default', 'qwen-flash', 'Qwen', 'provider_dashscope_primary', '/v1/chat/completions', 218, 'healthy', '聊天', now()),
			('route:provider_dashscope_primary:text-embedding-v4', 'text-embedding-v4', 'Qwen', 'provider_dashscope_primary', '/v1/embeddings', 64, 'healthy', '向量', now()),
			('route:provider_mimo_primary:default', 'mimo-v2.5-pro', 'MIMO', 'provider_mimo_primary', '/v1/chat/completions', 286, 'warning', '聊天', now()),
			('route:provider_rag_service:default', 'rag-query', 'RAG', 'provider_rag_service', '/v1/internal-search', 312, 'warning', '知识库', now());
	`); err != nil {
		t.Fatalf("seed routes failed: %v", err)
	}

	payload, err := console.Routes(ctx)
	if err != nil {
		t.Fatalf("Routes failed: %v", err)
	}

	if len(payload.Items) != 4 {
		t.Fatalf("expected 4 route items, got %d", len(payload.Items))
	}

	gotGroups := map[string]string{}
	for _, item := range payload.Items {
		gotGroups[item.RequestedModel] = item.ProviderGroup
	}

	wantGroups := map[string]string{
		"qwen-flash":        "qwen",
		"text-embedding-v4": "qwen",
		"mimo-v2.5-pro":     "mimo",
		"rag-query":         "other",
	}

	for requestedModel, wantGroup := range wantGroups {
		if gotGroups[requestedModel] != wantGroup {
			t.Fatalf("expected provider_group %q for %s, got %q", wantGroup, requestedModel, gotGroups[requestedModel])
		}
	}

	if payload.Items[0].RequestedModel != "qwen-flash" || payload.Items[0].ProviderGroup != "qwen" {
		t.Fatalf("expected first route item to be qwen-flash/qwen, got %+v", payload.Items[0])
	}
	if payload.Items[2].RequestedModel != "mimo-v2.5-pro" || payload.Items[2].ProviderGroup != "mimo" {
		t.Fatalf("expected third route item to be mimo-v2.5-pro/mimo, got %+v", payload.Items[2])
	}
	if payload.Items[3].RequestedModel != "rag-query" || payload.Items[3].ProviderGroup != "other" {
		t.Fatalf("expected fourth route item to be rag-query/other, got %+v", payload.Items[3])
	}
}

func TestPostgresConsoleServiceProviderModelsReturnsChatProvidersAndModels(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		delete from llm_request_events;
		delete from llm_usage_agg_hourly;
		delete from llm_request_logs;
		delete from route_catalog;
		delete from provider_credentials;

		insert into provider_credentials (
			id,
			provider,
			display_name,
			supported_models,
			base_url,
			encrypted_secret,
			secret_ref,
			credential_mode,
			status
		) values
			('provider_dashscope_primary', 'dashscope', 'Qwen', '{"qwen-flash","text-embedding-v4"}', 'https://dashscope.aliyuncs.com/compatible-mode/v1', '', 'secret/qwen', 'secret_ref', 'active'),
			('provider_mimo_primary', 'mimo', 'MIMO', '{"mimo-v2.5-pro"}', 'https://api.xiaomimimo.com/v1', '', '', 'encrypted', 'active'),
			('provider_rag_service', 'rag', 'RAG', '{"rag-query"}', 'http://rag-service:8000', '', '', 'encrypted', 'disabled');

		insert into route_catalog (
			id,
			requested_model,
			resolved_provider,
			provider_credential_id,
			endpoint,
			latency_ms,
			health_status,
			request_mode,
			updated_at
		) values
			('route:provider_dashscope_primary:default', 'qwen-flash', 'Qwen', 'provider_dashscope_primary', '/v1/chat/completions', 218, 'healthy', '聊天', now()),
			('route:provider_dashscope_primary:text-embedding-v4', 'text-embedding-v4', 'Qwen', 'provider_dashscope_primary', '/v1/embeddings', 64, 'healthy', '向量', now()),
			('route:provider_mimo_primary:default', 'mimo-v2.5-pro', 'MIMO', 'provider_mimo_primary', '/v1/chat/completions', 286, 'warning', '推理', now()),
			('route:provider_rag_service:default', 'rag-query', 'RAG', 'provider_rag_service', '/v1/internal-search', 312, 'warning', '知识库', now());
	`); err != nil {
		t.Fatalf("seed provider-models failed: %v", err)
	}

	payload, err := console.ProviderModels(ctx)
	if err != nil {
		t.Fatalf("ProviderModels failed: %v", err)
	}

	if len(payload.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(payload.Providers))
	}
	if len(payload.Models) != 2 {
		t.Fatalf("expected 2 chat models, got %d", len(payload.Models))
	}

	gotProviders := map[string]service.ProviderItem{}
	for _, item := range payload.Providers {
		gotProviders[item.ID] = item
	}
	if gotProviders["provider_dashscope_primary"].SecretRef != "secret/qwen" || gotProviders["provider_dashscope_primary"].CredentialMode != "secret_ref" {
		t.Fatalf("expected dashscope provider to expose secret_ref/credential_mode, got %+v", gotProviders["provider_dashscope_primary"])
	}
	if len(gotProviders["provider_dashscope_primary"].SupportedModels) != 2 {
		t.Fatalf("expected dashscope provider supported_models to round-trip, got %+v", gotProviders["provider_dashscope_primary"])
	}
	if gotProviders["provider_dashscope_primary"].Provider != "qwen" {
		t.Fatalf("expected dashscope provider to normalize to qwen, got %+v", gotProviders["provider_dashscope_primary"])
	}
	if gotProviders["provider_mimo_primary"].Provider != "mimo" {
		t.Fatalf("expected mimo provider to normalize to mimo, got %+v", gotProviders["provider_mimo_primary"])
	}

	gotModels := map[string]service.ProviderModelItem{}
	for _, item := range payload.Models {
		gotModels[item.RequestedModel] = item
	}
	if _, ok := gotModels["text-embedding-v4"]; ok {
		t.Fatalf("expected embedding model to be excluded, got %+v", gotModels["text-embedding-v4"])
	}
	if _, ok := gotModels["rag-query"]; ok {
		t.Fatalf("expected internal-search model to be excluded, got %+v", gotModels["rag-query"])
	}
	if gotModels["qwen-flash"].Provider != "qwen" {
		t.Fatalf("expected qwen-flash provider qwen, got %+v", gotModels["qwen-flash"])
	}
	if gotModels["mimo-v2.5-pro"].Provider != "mimo" {
		t.Fatalf("expected mimo-v2.5-pro provider mimo, got %+v", gotModels["mimo-v2.5-pro"])
	}
	if gotModels["mimo-v2.5-pro"].RequestMode != "推理" {
		t.Fatalf("expected request_mode 推理 preserved, got %+v", gotModels["mimo-v2.5-pro"])
	}
}

func TestPostgresConsoleServiceProviderModelsIncludesProvidersWithoutChatRoutesAndDeduplicatesProviders(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		delete from llm_request_events;
		delete from llm_usage_agg_hourly;
		delete from llm_request_logs;
		delete from route_catalog;
		delete from provider_credentials;

		insert into provider_credentials (
			id, provider, display_name, supported_models, base_url, encrypted_secret, secret_ref, credential_mode, status
		) values
			('provider_dashscope_primary', 'dashscope', 'Qwen 主线路', '{"qwen-flash","qwen-plus"}', 'https://dashscope.aliyuncs.com/compatible-mode/v1', '', 'secret/qwen', 'secret_ref', 'active'),
			('provider_embeddings_only', 'dashscope', 'Qwen 向量', '{"text-embedding-v4"}', 'https://dashscope.aliyuncs.com/compatible-mode/v1', '', 'secret/embed', 'secret_ref', 'active'),
			('provider_no_route', 'mimo', 'MIMO 预留', '{"mimo-v2.5-pro"}', 'https://api.xiaomimimo.com/v1', '', '', 'encrypted', 'active');

		insert into route_catalog (
			id, requested_model, resolved_provider, provider_credential_id, endpoint, latency_ms, health_status, request_mode, updated_at
		) values
			('route:provider_dashscope_primary:qwen-flash', 'qwen-flash', 'Qwen 主线路', 'provider_dashscope_primary', '/v1/chat/completions', 218, 'healthy', '聊天', now()),
			('route:provider_dashscope_primary:qwen-plus', 'qwen-plus', 'Qwen 主线路', 'provider_dashscope_primary', '/v1/chat/completions', 286, 'healthy', '推理', now()),
			('route:provider_embeddings_only:text-embedding-v4', 'text-embedding-v4', 'Qwen 向量', 'provider_embeddings_only', '/v1/embeddings', 64, 'healthy', '向量', now());
	`); err != nil {
		t.Fatalf("seed provider-models dedup failed: %v", err)
	}

	payload, err := console.ProviderModels(ctx)
	if err != nil {
		t.Fatalf("ProviderModels failed: %v", err)
	}

	if len(payload.Providers) != 3 {
		t.Fatalf("expected 3 providers including no-route provider, got %d", len(payload.Providers))
	}
	if len(payload.Models) != 2 {
		t.Fatalf("expected 2 chat models, got %d", len(payload.Models))
	}

	providerCount := map[string]int{}
	for _, item := range payload.Providers {
		providerCount[item.ID]++
	}
	if providerCount["provider_dashscope_primary"] != 1 {
		t.Fatalf("expected provider_dashscope_primary once, got %d", providerCount["provider_dashscope_primary"])
	}
	if providerCount["provider_no_route"] != 1 {
		t.Fatalf("expected provider_no_route to be present once, got %d", providerCount["provider_no_route"])
	}
	if providerCount["provider_embeddings_only"] != 1 {
		t.Fatalf("expected provider_embeddings_only to be present once, got %d", providerCount["provider_embeddings_only"])
	}
}

func TestPostgresConsoleServiceTenantBillingAggregatesMonthData(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		delete from llm_request_events;
		delete from llm_usage_agg_hourly;
		delete from llm_request_logs;
		delete from tenant_usage_ledger;
		delete from platform_api_keys where id in ('pak_billing_a', 'pak_billing_b');
		delete from provider_credentials where id in ('provider_qwen_primary', 'provider_mimo_primary');

		insert into provider_credentials (id, provider, display_name, supported_models, base_url, encrypted_secret, status) values
			('provider_qwen_primary', 'dashscope', 'Qwen 主线路', '{"qwen-flash"}', 'https://dashscope.aliyuncs.com/compatible-mode/v1', '', 'active'),
			('provider_mimo_primary', 'mimo', 'MIMO 线路', '{"mimo-v2.5-pro"}', 'https://api.xiaomimimo.com/v1', '', 'active');

		insert into platform_api_keys (id, tenant_id, name, key_hash, status) values
			('pak_billing_a', 'tenant_demo', 'Tenant Demo A', 'sha256:billing-a', 'active'),
			('pak_billing_b', 'tenant_demo', 'Tenant Demo B', 'sha256:billing-b', 'active');

		insert into tenant_usage_ledger (
			bucket_start, tenant_id,
			input_tokens, output_tokens, total_tokens, cached_tokens,
			input_cost_microyuan, output_cost_microyuan, cached_cost_microyuan, total_cost_microyuan,
			request_count, success_count, failure_count, estimated_count, created_at, updated_at
		) values
			('2026-04-03T00:00:00Z', 'tenant_demo', 1000, 400, 1450, 50, 120000, 240000, 10000, 370000, 9, 8, 1, 0, now(), now()),
			('2026-04-18T00:00:00Z', 'tenant_demo', 500, 200, 730, 30, 60000, 120000, 5000, 185000, 3, 2, 1, 0, now(), now()),
			('2026-05-01T00:00:00Z', 'tenant_demo', 999, 999, 1998, 0, 1, 1, 0, 2, 1, 1, 0, 0, now(), now());

		insert into llm_request_logs (
			id, tenant_id, platform_api_key_id, platform_api_key_name, provider_credential_id, route_id,
			request_path, request_model, upstream_model, resolved_model, usage_source, usage_status, status_code,
			latency_ms, prompt_tokens, completion_tokens, total_tokens, cached_tokens,
			input_price_microyuan_per_million, output_price_microyuan_per_million, cached_price_microyuan_per_million,
			input_cost_microyuan, output_cost_microyuan, cached_cost_microyuan, total_cost_microyuan,
			request_started_at, request_completed_at, created_at
		) values
			('bill_req_1', 'tenant_demo', 'pak_billing_a', 'Tenant Demo A', 'provider_qwen_primary', 'route_qwen',
			 '/v1/chat/completions', 'qwen-flash', 'qwen-flash', 'qwen-flash', 'upstream', 'success', 200,
			 180, 400, 100, 520, 20,
			 100, 200, 50,
			 40000, 20000, 1000, 61000,
			 '2026-04-10T01:00:00Z', '2026-04-10T01:00:02Z', '2026-04-10T01:00:02Z'),
			('bill_req_2', 'tenant_demo', 'pak_billing_a', 'Tenant Demo A', 'provider_qwen_primary', 'route_qwen',
			 '/v1/chat/completions', 'qwen-flash', 'qwen-flash', 'qwen-flash', 'upstream', 'failed', 500,
			 220, 200, 80, 300, 10,
			 100, 200, 50,
			 20000, 16000, 500, 36500,
			 '2026-04-11T01:00:00Z', '2026-04-11T01:00:03Z', '2026-04-11T01:00:03Z'),
			('bill_req_3', 'tenant_demo', 'pak_billing_b', 'Tenant Demo B', 'provider_mimo_primary', 'route_mimo',
			 '/v1/chat/completions', 'mimo-v2.5-pro', 'mimo-v2.5-pro', 'mimo-v2.5-pro', 'upstream', 'success', 200,
			 260, 300, 120, 450, 5,
			 100, 200, 50,
			 30000, 24000, 250, 54250,
			 '2026-04-20T01:00:00Z', '2026-04-20T01:00:04Z', '2026-04-20T01:00:04Z'),
			('bill_req_4', 'tenant_demo', 'pak_billing_b', 'Tenant Demo B', 'provider_mimo_primary', 'route_mimo',
			 '/v1/chat/completions', 'mimo-v2.5-pro', 'mimo-v2.5-pro', 'mimo-v2.5-pro', 'upstream', 'success', 200,
			 150, 999, 999, 1998, 0,
			 1, 1, 0,
			 1, 1, 0, 2,
			 '2026-05-03T01:00:00Z', '2026-05-03T01:00:04Z', '2026-05-03T01:00:04Z');
	`); err != nil {
		t.Fatalf("seed tenant billing failed: %v", err)
	}

	payload, err := console.TenantBilling(ctx, service.TenantBillingQuery{TenantID: "tenant_demo", Month: "2026-04"})
	if err != nil {
		t.Fatalf("TenantBilling failed: %v", err)
	}

	if payload.Summary.Month != "2026-04" || payload.Summary.TenantID != "tenant_demo" {
		t.Fatalf("unexpected summary identity: %+v", payload.Summary)
	}
	if payload.Summary.RequestCount != 12 || payload.Summary.SuccessCount != 10 || payload.Summary.FailureCount != 2 {
		t.Fatalf("unexpected ledger counts: %+v", payload.Summary)
	}
	if payload.Summary.InputTokens != 1500 || payload.Summary.OutputTokens != 600 || payload.Summary.CachedTokens != 80 || payload.Summary.TotalTokens != 2180 {
		t.Fatalf("unexpected ledger tokens: %+v", payload.Summary)
	}
	if payload.Summary.TotalCost != "0.56 ￥" {
		t.Fatalf("expected total cost 0.56 ￥, got %+v", payload.Summary)
	}
	if len(payload.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(payload.Providers))
	}
	if payload.Providers[0].DisplayName != "default-route" || payload.Providers[0].RequestCount != 2 {
		t.Fatalf("unexpected first provider row: %+v", payload.Providers[0])
	}
	if payload.Providers[1].DisplayName != "shared-route" || payload.Providers[1].RequestCount != 1 {
		t.Fatalf("unexpected second provider row: %+v", payload.Providers[1])
	}
	if len(payload.Models) != 2 || payload.Models[0].Model != "qwen-flash" || payload.Models[1].Model != "mimo-v2.5-pro" {
		t.Fatalf("unexpected model rows: %+v", payload.Models)
	}
	if len(payload.APIKeys) != 2 || payload.APIKeys[0].PlatformAPIKeyID != "pak_billing_a" || payload.APIKeys[1].PlatformAPIKeyID != "pak_billing_b" {
		t.Fatalf("unexpected api key rows: %+v", payload.APIKeys)
	}
}

func TestPostgresConsoleServiceTenantBillingRejectsInvalidMonth(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	_, err := console.TenantBilling(ctx, service.TenantBillingQuery{TenantID: "tenant_demo", Month: "2026-13"})
	if err == nil {
		t.Fatal("expected invalid month error")
	}
	var statusErr service.StatusError
	if !errors.As(err, &statusErr) || statusErr.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request status error, got %v", err)
	}
}

func TestPostgresConsoleServiceCreateProviderPersistsSecretRefCredential(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	result, err := console.CreateProvider(ctx, service.CreateProviderRequest{
		Provider:       "dashscope",
		DisplayName:    "Qwen Secret Ref",
		BaseURL:        "https://dashscope.aliyuncs.com/compatible-mode/v1",
		CredentialMode: "secret_ref",
		SecretRef:      "TEST_QWEN_PROVIDER_SECRET",
	})
	if err != nil {
		t.Fatalf("CreateProvider failed: %v", err)
	}

	if result.Item.ID == "" {
		t.Fatal("expected created provider id")
	}
	if result.Item.CredentialMode != "secret_ref" {
		t.Fatalf("expected credential_mode secret_ref, got %q", result.Item.CredentialMode)
	}
	if result.Item.SecretRef != "TEST_QWEN_PROVIDER_SECRET" {
		t.Fatalf("expected secret_ref to round-trip, got %q", result.Item.SecretRef)
	}

	var encryptedSecret string
	var secretRef string
	var credentialMode string
	var status string
	if err := conn.QueryRow(ctx, `
		select encrypted_secret, secret_ref, credential_mode, status
		from provider_credentials
		where id = $1;
	`, result.Item.ID).Scan(&encryptedSecret, &secretRef, &credentialMode, &status); err != nil {
		t.Fatalf("query provider_credentials failed: %v", err)
	}
	if encryptedSecret != "" {
		t.Fatalf("expected encrypted_secret to stay empty for secret_ref mode, got %q", encryptedSecret)
	}
	if secretRef != "TEST_QWEN_PROVIDER_SECRET" {
		t.Fatalf("expected persisted secret_ref %q, got %q", "TEST_QWEN_PROVIDER_SECRET", secretRef)
	}
	if credentialMode != "secret_ref" {
		t.Fatalf("expected persisted credential_mode secret_ref, got %q", credentialMode)
	}
	if status != "active" {
		t.Fatalf("expected persisted status active, got %q", status)
	}
}

func TestPostgresConsoleServiceCreateProviderModelRunsImmediateHealthcheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Setenv("TEST_QWEN_PROVIDER_SECRET", "provider-secret")

	_, conn := newUsageConsoleService(t, ctx)

	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("secret.NewCodec failed: %v", err)
	}

	upstream := &stubConsoleUpstreamChatClient{
		stream: service.ChatCompletionStream{
			StatusCode:  http.StatusOK,
			ContentType: "text/event-stream; charset=utf-8",
			Run: func(emit func([]byte) error, onFirstToken func()) (service.ChatStreamResult, error) {
				if onFirstToken != nil {
					onFirstToken()
				}
				if err := emit([]byte("data: first-content\n\n")); err != nil {
					return service.ChatStreamResult{}, err
				}
				return service.ChatStreamResult{
					SawContentToken: true,
					Response:        service.ChatResponse{Model: "qwen-plus-health"},
				}, nil
			},
		},
	}
	console := service.NewPostgresConsoleService(
		conn,
		nil,
		service.NewChatProxyService(upstream, nil),
		nil,
		"",
		codec,
	)

	providerResult, err := console.CreateProvider(ctx, service.CreateProviderRequest{
		Provider:       "dashscope",
		DisplayName:    "Qwen Health",
		BaseURL:        "https://dashscope.aliyuncs.com/compatible-mode/v1",
		CredentialMode: "secret_ref",
		SecretRef:      "TEST_QWEN_PROVIDER_SECRET",
	})
	if err != nil {
		t.Fatalf("CreateProvider failed: %v", err)
	}

	modelResult, err := console.CreateProviderModel(ctx, service.CreateProviderModelRequest{
		RequestedModel:       "qwen-plus-health",
		ProviderCredentialID: providerResult.Item.ID,
		RequestMode:          "聊天",
		HealthcheckEnabled:   true,
	})
	if err != nil {
		t.Fatalf("CreateProviderModel failed: %v", err)
	}

	if modelResult.Item.HealthStatus != "healthy" {
		t.Fatalf("expected health status healthy, got %+v", modelResult.Item)
	}

	var healthStatus string
	var lastHealthError string
	var lastHealthCheckedAt time.Time
	if err := conn.QueryRow(ctx, `
		select health_status, last_health_error, last_health_checked_at
		from route_catalog
		where id = $1;
	`, modelResult.Item.ID).Scan(&healthStatus, &lastHealthError, &lastHealthCheckedAt); err != nil {
		t.Fatalf("query route_catalog failed: %v", err)
	}
	if healthStatus != "healthy" {
		t.Fatalf("expected persisted health_status healthy, got %q", healthStatus)
	}
	if lastHealthError != "" {
		t.Fatalf("expected empty last_health_error, got %q", lastHealthError)
	}
	if lastHealthCheckedAt.IsZero() {
		t.Fatal("expected last_health_checked_at to be populated")
	}

	healthPayload, err := console.ModelHealth(ctx, "24h")
	if err != nil {
		t.Fatalf("ModelHealth failed: %v", err)
	}
	if len(healthPayload.Items) == 0 {
		t.Fatal("expected model health items")
	}

	found := false
	for _, item := range healthPayload.Items {
		if item.ID == modelResult.Item.ID {
			found = true
			if item.HealthStatus != "healthy" {
				t.Fatalf("expected model health item to be healthy, got %+v", item)
			}
		}
	}
	if !found {
		t.Fatalf("expected model health to include route %q", modelResult.Item.ID)
	}
	if len(healthPayload.Wall.Buckets) == 0 {
		t.Fatal("expected model health wall buckets")
	}
	if len(healthPayload.Wall.Lanes) == 0 {
		t.Fatal("expected model health wall lanes")
	}
}

func TestPostgresConsoleServiceCreateProviderModelSyncsProviderSupportedModels(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Setenv("TEST_QWEN_PROVIDER_SECRET", "provider-secret")

	_, conn := newUsageConsoleService(t, ctx)

	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("secret.NewCodec failed: %v", err)
	}

	console := service.NewPostgresConsoleService(
		conn,
		nil,
		service.NewChatProxyService(&stubConsoleUpstreamChatClient{}, nil),
		nil,
		"",
		codec,
	)

	providerResult, err := console.CreateProvider(ctx, service.CreateProviderRequest{
		Provider:       "dashscope",
		DisplayName:    "Qwen Sync",
		BaseURL:        "https://dashscope.aliyuncs.com/compatible-mode/v1",
		CredentialMode: "secret_ref",
		SecretRef:      "TEST_QWEN_PROVIDER_SECRET",
	})
	if err != nil {
		t.Fatalf("CreateProvider failed: %v", err)
	}

	if _, err := console.CreateProviderModel(ctx, service.CreateProviderModelRequest{
		RequestedModel:       "deepseek-r1-distill-qwen-7b",
		ProviderCredentialID: providerResult.Item.ID,
		RequestMode:          "聊天",
		HealthcheckEnabled:   false,
	}); err != nil {
		t.Fatalf("CreateProviderModel failed: %v", err)
	}

	var supportedModels []string
	if err := conn.QueryRow(ctx, `
		select supported_models
		from provider_credentials
		where id = $1;
	`, providerResult.Item.ID).Scan(&supportedModels); err != nil {
		t.Fatalf("query provider_credentials failed: %v", err)
	}

	if !slices.Contains(supportedModels, "deepseek-r1-distill-qwen-7b") {
		t.Fatalf("expected supported_models to include requested model, got %#v", supportedModels)
	}
}

func TestPostgresConsoleServiceCreateProviderModelHealthcheckAcceptsCompletionTokensOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Setenv("TEST_MIMO_PROVIDER_SECRET", "provider-secret")

	_, conn := newUsageConsoleService(t, ctx)

	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("secret.NewCodec failed: %v", err)
	}

	upstream := &stubConsoleUpstreamChatClient{
		stream: service.ChatCompletionStream{
			StatusCode:  http.StatusOK,
			ContentType: "text/event-stream; charset=utf-8",
			Run: func(emit func([]byte) error, onFirstToken func()) (service.ChatStreamResult, error) {
				return service.ChatStreamResult{
					Response: service.ChatResponse{
						Model: "mimo-v2.5-pro",
						Usage: &service.TokenUsage{
							PromptTokens:     252,
							CompletionTokens: 1,
							TotalTokens:      253,
						},
					},
				}, nil
			},
		},
	}
	console := service.NewPostgresConsoleService(
		conn,
		nil,
		service.NewChatProxyService(upstream, nil),
		nil,
		"",
		codec,
	)

	providerResult, err := console.CreateProvider(ctx, service.CreateProviderRequest{
		Provider:       "mimo",
		DisplayName:    "Xiaomi MIMO",
		BaseURL:        "https://api.xiaomimimo.com/v1",
		CredentialMode: "secret_ref",
		SecretRef:      "TEST_MIMO_PROVIDER_SECRET",
	})
	if err != nil {
		t.Fatalf("CreateProvider failed: %v", err)
	}

	modelResult, err := console.CreateProviderModel(ctx, service.CreateProviderModelRequest{
		RequestedModel:       "mimo-v2.5-pro",
		ProviderCredentialID: providerResult.Item.ID,
		RequestMode:          "推理",
		HealthcheckEnabled:   true,
	})
	if err != nil {
		t.Fatalf("CreateProviderModel failed: %v", err)
	}

	if modelResult.Item.HealthStatus != "healthy" {
		t.Fatalf("expected health status healthy, got %+v", modelResult.Item)
	}
}

func TestPostgresConsoleServiceCreateProviderModelDoesNotRollbackOnHealthcheckFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Setenv("TEST_QWEN_PROVIDER_SECRET", "provider-secret")

	_, conn := newUsageConsoleService(t, ctx)

	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("secret.NewCodec failed: %v", err)
	}

	upstream := &stubConsoleUpstreamChatClient{
		streamErr: errors.New("upstream failed"),
	}
	console := service.NewPostgresConsoleService(
		conn,
		nil,
		service.NewChatProxyService(upstream, nil),
		nil,
		"",
		codec,
	)

	providerResult, err := console.CreateProvider(ctx, service.CreateProviderRequest{
		Provider:       "dashscope",
		DisplayName:    "Qwen Failure",
		BaseURL:        "https://dashscope.aliyuncs.com/compatible-mode/v1",
		CredentialMode: "secret_ref",
		SecretRef:      "TEST_QWEN_PROVIDER_SECRET",
	})
	if err != nil {
		t.Fatalf("CreateProvider failed: %v", err)
	}

	modelResult, err := console.CreateProviderModel(ctx, service.CreateProviderModelRequest{
		RequestedModel:       "qwen-plus-health-fail",
		ProviderCredentialID: providerResult.Item.ID,
		RequestMode:          "聊天",
		HealthcheckEnabled:   true,
	})
	if err != nil {
		t.Fatalf("CreateProviderModel should not rollback on healthcheck failure: %v", err)
	}

	var routeCount int
	var healthStatus string
	var lastHealthError string
	if err := conn.QueryRow(ctx, `
		select count(*), max(health_status), max(last_health_error)
		from route_catalog
		where id = $1;
	`, modelResult.Item.ID).Scan(&routeCount, &healthStatus, &lastHealthError); err != nil {
		t.Fatalf("query route_catalog failed: %v", err)
	}
	if routeCount != 1 {
		t.Fatalf("expected route to remain persisted, got count %d", routeCount)
	}
	if healthStatus != "degraded" {
		t.Fatalf("expected degraded health status after failed healthcheck, got %q", healthStatus)
	}
	if !strings.Contains(lastHealthError, "upstream failed") {
		t.Fatalf("expected last_health_error to contain upstream failure, got %q", lastHealthError)
	}
}

func TestPostgresConsoleServiceCreateProviderModelGeneratesURLSafeOpaqueID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Setenv("TEST_QWEN_PROVIDER_SECRET", "provider-secret")

	_, conn := newUsageConsoleService(t, ctx)

	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("secret.NewCodec failed: %v", err)
	}

	upstream := &stubConsoleUpstreamChatClient{
		stream: service.ChatCompletionStream{
			StatusCode:  http.StatusOK,
			ContentType: "text/event-stream; charset=utf-8",
			Run: func(emit func([]byte) error, onFirstToken func()) (service.ChatStreamResult, error) {
				if onFirstToken != nil {
					onFirstToken()
				}
				if err := emit([]byte("data: first-content\n\n")); err != nil {
					return service.ChatStreamResult{}, err
				}
				return service.ChatStreamResult{
					SawContentToken: true,
					Response:        service.ChatResponse{Model: "folder/model v1"},
				}, nil
			},
		},
	}
	console := service.NewPostgresConsoleService(
		conn,
		nil,
		service.NewChatProxyService(upstream, nil),
		nil,
		"",
		codec,
	)

	providerResult, err := console.CreateProvider(ctx, service.CreateProviderRequest{
		Provider:       "dashscope",
		DisplayName:    "Qwen Safe Route",
		BaseURL:        "https://dashscope.aliyuncs.com/compatible-mode/v1",
		CredentialMode: "secret_ref",
		SecretRef:      "TEST_QWEN_PROVIDER_SECRET",
	})
	if err != nil {
		t.Fatalf("CreateProvider failed: %v", err)
	}

	modelResult, err := console.CreateProviderModel(ctx, service.CreateProviderModelRequest{
		RequestedModel:       "folder/model v1",
		ProviderCredentialID: providerResult.Item.ID,
		RequestMode:          "聊天",
		HealthcheckEnabled:   false,
	})
	if err != nil {
		t.Fatalf("CreateProviderModel failed: %v", err)
	}

	if !strings.HasPrefix(modelResult.Item.ID, "route:"+providerResult.Item.ID+":") {
		t.Fatalf("expected route id prefix for provider, got %q", modelResult.Item.ID)
	}
	if strings.Contains(modelResult.Item.ID, "/") || strings.Contains(modelResult.Item.ID, " ") {
		t.Fatalf("expected route id to be URL-safe, got %q", modelResult.Item.ID)
	}
	if strings.Contains(modelResult.Item.ID, "folder/model v1") {
		t.Fatalf("expected opaque route id suffix, got %q", modelResult.Item.ID)
	}

	healthcheckResult, err := console.RunProviderModelHealthcheck(ctx, modelResult.Item.ID)
	if err != nil {
		t.Fatalf("RunProviderModelHealthcheck failed: %v", err)
	}
	if healthcheckResult.Item.ID != modelResult.Item.ID {
		t.Fatalf("expected healthcheck to use created opaque id %q, got %q", modelResult.Item.ID, healthcheckResult.Item.ID)
	}
	if healthcheckResult.Item.HealthStatus != "healthy" {
		t.Fatalf("expected healthcheck result healthy, got %+v", healthcheckResult.Item)
	}
}

func TestPostgresConsoleServiceDeleteProviderModelRemovesChatRouteFromProviderModels(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Setenv("TEST_QWEN_PROVIDER_SECRET", "provider-secret")

	console, conn := newUsageConsoleService(t, ctx)

	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("secret.NewCodec failed: %v", err)
	}

	console = service.NewPostgresConsoleService(
		conn,
		nil,
		service.NewChatProxyService(&stubConsoleUpstreamChatClient{}, nil),
		nil,
		"",
		codec,
	)

	providerResult, err := console.CreateProvider(ctx, service.CreateProviderRequest{
		Provider:       "dashscope",
		DisplayName:    "Qwen Delete",
		BaseURL:        "https://dashscope.aliyuncs.com/compatible-mode/v1",
		CredentialMode: "secret_ref",
		SecretRef:      "TEST_QWEN_PROVIDER_SECRET",
	})
	if err != nil {
		t.Fatalf("CreateProvider failed: %v", err)
	}

	modelResult, err := console.CreateProviderModel(ctx, service.CreateProviderModelRequest{
		RequestedModel:       "qwen-delete-me",
		ProviderCredentialID: providerResult.Item.ID,
		RequestMode:          "聊天",
		HealthcheckEnabled:   false,
	})
	if err != nil {
		t.Fatalf("CreateProviderModel failed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id, tenant_id, platform_api_key_id, platform_api_key_name, provider_credential_id, route_id,
			request_path, request_model, upstream_model, usage_source, usage_status, status_code,
			latency_ms, prompt_tokens, completion_tokens, total_tokens, error_code, error_message,
			request_started_at, request_completed_at
		) values (
			'llmreq_delete_provider_model', 'tenant_demo', 'pak_demo', 'demo key', $1, $2,
			'/v1/chat/completions', 'qwen-delete-me', 'qwen-delete-me', 'upstream', 'success', 200,
			10, 10, 5, 15, '', '', now() - interval '1 hour', now() - interval '1 hour'
		);
	`, providerResult.Item.ID, modelResult.Item.ID); err != nil {
		t.Fatalf("seed llm_request_logs failed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		insert into llm_usage_agg_hourly (
			bucket_start, tenant_id, platform_api_key_id, provider_credential_id, route_id,
			request_path, usage_source, usage_status, request_count, prompt_tokens, completion_tokens, total_tokens
		) values (
			date_trunc('hour', now() - interval '1 hour'),
			'tenant_demo', 'pak_demo', $1, $2,
			'/v1/chat/completions', 'upstream', 'success', 1, 10, 5, 15
		);
	`, providerResult.Item.ID, modelResult.Item.ID); err != nil {
		t.Fatalf("seed llm_usage_agg_hourly failed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		insert into model_healthcheck_history (
			id, route_id, requested_model, provider_credential_id, route_label, health_status,
			last_health_error, request_mode, latency_ms, first_token_latency_ms, checked_at
		) values (
			'mhh_delete_provider_model', $2, 'qwen-delete-me', $1, 'Qwen Delete', 'healthy',
			'', '聊天', 10, 3, now() - interval '1 hour'
		);
	`, providerResult.Item.ID, modelResult.Item.ID); err != nil {
		t.Fatalf("seed delete provider model history failed: %v", err)
	}

	deleteResult, err := console.DeleteProviderModel(ctx, modelResult.Item.ID)
	if err != nil {
		t.Fatalf("DeleteProviderModel failed: %v", err)
	}
	if deleteResult.DeletedID != modelResult.Item.ID {
		t.Fatalf("expected deleted_id %q, got %q", modelResult.Item.ID, deleteResult.DeletedID)
	}

	payload, err := console.ProviderModels(ctx)
	if err != nil {
		t.Fatalf("ProviderModels failed: %v", err)
	}
	for _, item := range payload.Models {
		if item.ID == modelResult.Item.ID || item.RequestedModel == "qwen-delete-me" {
			t.Fatalf("expected deleted model to disappear from provider models, got %+v", item)
		}
	}

	var routeCount int
	if err := conn.QueryRow(ctx, `select count(*) from route_catalog where id = $1;`, modelResult.Item.ID).Scan(&routeCount); err != nil {
		t.Fatalf("query route_catalog failed: %v", err)
	}
	if routeCount != 0 {
		t.Fatalf("expected route_catalog row deleted, got count %d", routeCount)
	}

	var logCount int
	if err := conn.QueryRow(ctx, `select count(*) from llm_request_logs where route_id = $1;`, modelResult.Item.ID).Scan(&logCount); err != nil {
		t.Fatalf("query llm_request_logs failed: %v", err)
	}
	if logCount != 1 {
		t.Fatalf("expected llm_request_logs to remain, got count %d", logCount)
	}

	var aggCount int
	if err := conn.QueryRow(ctx, `select count(*) from llm_usage_agg_hourly where route_id = $1;`, modelResult.Item.ID).Scan(&aggCount); err != nil {
		t.Fatalf("query llm_usage_agg_hourly failed: %v", err)
	}
	if aggCount != 1 {
		t.Fatalf("expected llm_usage_agg_hourly to remain, got count %d", aggCount)
	}

	var historyCount int
	if err := conn.QueryRow(ctx, `select count(*) from model_healthcheck_history where route_id = $1;`, modelResult.Item.ID).Scan(&historyCount); err != nil {
		t.Fatalf("query model_healthcheck_history failed: %v", err)
	}
	if historyCount != 1 {
		t.Fatalf("expected model_healthcheck_history to remain, got count %d", historyCount)
	}
}

func TestPostgresConsoleServiceDeleteProviderModelReturnsNotFound(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	_, err := console.DeleteProviderModel(ctx, "route:missing:model")
	if err == nil {
		t.Fatal("expected not found error")
	}

	statusErr, ok := err.(service.StatusError)
	if !ok {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", statusErr.Code)
	}
}

func TestPostgresConsoleServiceDeleteProviderModelRejectsNonChatEndpoint(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		delete from route_catalog where id = 'route:provider_openai_demo:embedding-delete';
		insert into route_catalog (
			id, requested_model, resolved_provider, provider_credential_id, endpoint,
			latency_ms, health_status, request_mode, updated_at
		) values (
			'route:provider_openai_demo:embedding-delete',
			'text-embedding-delete',
			'OpenAI Primary',
			'provider_openai_demo',
			'/v1/embeddings',
			0,
			'healthy',
			'向量',
			now()
		);
	`); err != nil {
		t.Fatalf("seed non-chat route failed: %v", err)
	}

	_, err := console.DeleteProviderModel(ctx, "route:provider_openai_demo:embedding-delete")
	if err == nil {
		t.Fatal("expected bad request error")
	}

	statusErr, ok := err.(service.StatusError)
	if !ok {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", statusErr.Code)
	}
}

func TestPostgresConsoleServiceModelHealthWallAggregatesWindowData(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		delete from model_healthcheck_history;
		delete from llm_request_events;
		delete from llm_usage_agg_hourly;
		delete from llm_request_logs;
		delete from route_catalog;
		delete from provider_credentials;

		insert into provider_credentials (
			id, provider, display_name, supported_models, base_url, encrypted_secret, credential_mode, secret_ref, status
		) values (
			'provider_dashscope_primary', 'dashscope', 'Qwen 主线路', '{"qwen-flash"}', 'https://dashscope.aliyuncs.com/compatible-mode/v1', '', 'secret_ref', 'TEST_QWEN_PROVIDER_SECRET', 'active'
		);

		insert into route_catalog (
			id, requested_model, resolved_provider, provider_credential_id, endpoint, latency_ms, health_status, request_mode, status, healthcheck_enabled, updated_at
		) values (
			'route:provider_dashscope_primary:qwen-flash',
			'qwen-flash',
			'Qwen 主线路',
			'provider_dashscope_primary',
			'/v1/chat/completions',
			200,
			'healthy',
			'聊天',
			'active',
			true,
			now()
		);

		insert into model_healthcheck_history (
			id,
			route_id,
			requested_model,
			provider_credential_id,
			route_label,
			health_status,
			last_health_error,
			request_mode,
			latency_ms,
			first_token_latency_ms,
			checked_at
		) values
			(
				'mh_24h_success',
				'route:provider_dashscope_primary:qwen-flash',
				'qwen-flash',
				'provider_dashscope_primary',
				'Qwen 主线路',
				'healthy',
				'',
				'聊天',
				180,
				60,
				now() - interval '90 minutes'
			),
			(
				'mh_6h_fail',
				'route:provider_dashscope_primary:qwen-flash',
				'qwen-flash',
				'provider_dashscope_primary',
				'Qwen 主线路',
				'degraded',
				'upstream timeout',
				'聊天',
				420,
				0,
				now() - interval '30 minutes'
			),
			(
				'mh_7d_old',
				'route:provider_dashscope_primary:qwen-flash',
				'qwen-flash',
				'provider_dashscope_primary',
				'Qwen 主线路',
				'warning',
				'transient error',
				'聊天',
				260,
				0,
				now() - interval '3 days'
			),
			(
				'mh_8d_outside',
				'route:provider_dashscope_primary:qwen-flash',
				'qwen-flash',
				'provider_dashscope_primary',
				'Qwen 主线路',
				'healthy',
				'',
				'聊天',
				160,
				40,
				now() - interval '8 days'
			);
	`); err != nil {
		t.Fatalf("seed model_healthcheck_history failed: %v", err)
	}

	payload24h, err := console.ModelHealth(ctx, "24h")
	if err != nil {
		t.Fatalf("ModelHealth(24h) failed: %v", err)
	}
	if payload24h.Wall.Window != "24h" {
		t.Fatalf("expected wall.window 24h, got %q", payload24h.Wall.Window)
	}
	if payload24h.Wall.WindowLabel != "最近 24 小时" {
		t.Fatalf("expected wall.window_label 最近 24 小时, got %q", payload24h.Wall.WindowLabel)
	}
	if len(payload24h.Wall.Buckets) != 12 {
		t.Fatalf("expected 24h wall 12 buckets, got %d", len(payload24h.Wall.Buckets))
	}
	if len(payload24h.Wall.Lanes) == 0 {
		t.Fatalf("expected 24h wall lanes, got %+v", payload24h.Wall)
	}
	if !containsModelHealthWallStatus(payload24h.Wall, "降级") {
		t.Fatalf("expected 24h wall to contain 降级 status, got %+v", payload24h.Wall)
	}

	payload6h, err := console.ModelHealth(ctx, "6h")
	if err != nil {
		t.Fatalf("ModelHealth(6h) failed: %v", err)
	}
	if payload6h.Wall.Window != "6h" {
		t.Fatalf("expected wall.window 6h, got %q", payload6h.Wall.Window)
	}
	if payload6h.Wall.WindowLabel != "最近 6 小时" {
		t.Fatalf("expected wall.window_label 最近 6 小时, got %q", payload6h.Wall.WindowLabel)
	}
	if len(payload6h.Wall.Buckets) != 12 {
		t.Fatalf("expected 6h wall 12 buckets, got %d", len(payload6h.Wall.Buckets))
	}
	if len(payload6h.Wall.Lanes) == 0 {
		t.Fatalf("expected 6h wall lanes, got %+v", payload6h.Wall)
	}
	if !containsModelHealthWallStatus(payload6h.Wall, "降级") {
		t.Fatalf("expected 6h wall to contain 降级 status, got %+v", payload6h.Wall)
	}

	payload7d, err := console.ModelHealth(ctx, "7d")
	if err != nil {
		t.Fatalf("ModelHealth(7d) failed: %v", err)
	}
	if payload7d.Wall.Window != "7d" {
		t.Fatalf("expected wall.window 7d, got %q", payload7d.Wall.Window)
	}
	if payload7d.Wall.WindowLabel != "最近 7 天" {
		t.Fatalf("expected wall.window_label 最近 7 天, got %q", payload7d.Wall.WindowLabel)
	}
	if len(payload7d.Wall.Buckets) != 14 {
		t.Fatalf("expected 7d wall 14 buckets, got %d", len(payload7d.Wall.Buckets))
	}
	if len(payload7d.Wall.Lanes) == 0 {
		t.Fatalf("expected 7d wall lanes, got %+v", payload7d.Wall)
	}
	if !containsModelHealthWallStatus(payload7d.Wall, "告警") {
		t.Fatalf("expected 7d wall to contain 告警 status, got %+v", payload7d.Wall)
	}
}

func TestPostgresConsoleServiceModelHealthWallReturnsEmptyForUnknownWindow(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	payload, err := console.ModelHealth(ctx, "unsupported-window")
	if err != nil {
		t.Fatalf("ModelHealth unsupported window failed: %v", err)
	}

	if payload.Wall.Window != "24h" {
		t.Fatalf("expected fallback wall.window 24h, got %q", payload.Wall.Window)
	}
	if payload.Wall.WindowLabel != "最近 24 小时" {
		t.Fatalf("expected fallback wall.window_label 最近 24 小时, got %q", payload.Wall.WindowLabel)
	}
	if len(payload.Wall.Buckets) != 12 {
		t.Fatalf("expected fallback wall buckets 12, got %d", len(payload.Wall.Buckets))
	}
}

func TestPostgresConsoleServiceOverviewIncludesTenantPostureAndPlatformMetrics(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into tenants (id, name, status)
		values ('tenant_alpha', 'Alpha Tenant', 'active')
		on conflict (id) do update set name = excluded.name, status = excluded.status;

		insert into users (id, email, name, role, status)
		values ('user_alpha_member', 'alpha-member@example.com', 'Alpha Member', 'member', 'active')
		on conflict (id) do nothing;

		insert into tenant_memberships (id, tenant_id, user_id, role, status)
		values ('tm_alpha_member', 'tenant_alpha', 'user_alpha_member', 'member', 'active')
		on conflict (tenant_id, user_id) do update set status = excluded.status;

		insert into tenant_quota_policies (tenant_id, period_type, request_limit, token_limit, effective_from)
		values ('tenant_alpha', 'monthly', 120000, 3456789, now())
		on conflict (tenant_id) do update set
			request_limit = excluded.request_limit,
			token_limit = excluded.token_limit,
			effective_from = excluded.effective_from,
			updated_at = now();
	`); err != nil {
		t.Fatalf("seed overview tenant posture failed: %v", err)
	}

	payload, err := console.Overview(ctx)
	if err != nil {
		t.Fatalf("Overview failed: %v", err)
	}

	if len(payload.PlatformMetrics) == 0 {
		t.Fatal("expected platform metrics to be populated")
	}
	if len(payload.TenantPosture) < 2 {
		t.Fatalf("expected at least 2 tenant posture rows, got %d", len(payload.TenantPosture))
	}
	if !containsMetric(payload.PlatformMetrics, "活跃租户数") {
		t.Fatalf("expected 活跃租户数 metric, got %#v", payload.PlatformMetrics)
	}
	if !containsTableRowValue(payload.TenantPosture, "tenant_alpha") {
		t.Fatalf("expected tenant posture to include tenant_alpha, got %#v", payload.TenantPosture)
	}
}

func TestPostgresConsoleServiceOverviewMarksMissingTenantTokenLimitAsUnconfigured(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into tenants (id, name, status)
		values ('tenant_123', 'Tenant 123', 'active')
		on conflict (id) do update set name = excluded.name, status = excluded.status;

		insert into tenant_quota_usage_periods (
			tenant_id,
			period_start,
			period_end,
			requests_used,
			tokens_used,
			last_aggregated_at
		) values (
			'tenant_123',
			(date_trunc('month', now() at time zone 'Asia/Shanghai') at time zone 'Asia/Shanghai'),
			((date_trunc('month', now() at time zone 'Asia/Shanghai') + interval '1 month') at time zone 'Asia/Shanghai'),
			10,
			1754,
			now()
		)
		on conflict (tenant_id, period_start) do update set
			requests_used = excluded.requests_used,
			tokens_used = excluded.tokens_used,
			last_aggregated_at = now();
	`); err != nil {
		t.Fatalf("seed tenant_123 overview posture failed: %v", err)
	}

	payload, err := console.Overview(ctx)
	if err != nil {
		t.Fatalf("Overview failed: %v", err)
	}

	row := findTableRowByFirstColumn(payload.TenantPosture, "tenant_123")
	if row == nil {
		t.Fatalf("expected tenant posture to include tenant_123, got %#v", payload.TenantPosture)
	}
	if len(row.Columns) < 6 {
		t.Fatalf("expected tenant_123 posture row to have 6 columns, got %#v", row.Columns)
	}
	if row.Columns[4] != "未配置" {
		t.Fatalf("expected tenant_123 token limit to be 未配置, got %q", row.Columns[4])
	}
	if row.Columns[5] != "1754 / 未配置" {
		t.Fatalf("expected tenant_123 token usage to be %q, got %q", "1754 / 未配置", row.Columns[5])
	}
}

func TestPostgresConsoleServiceOverviewUsesLLMLogsWithoutAuditLogs(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		delete from audit_logs;

		insert into platform_api_keys (
			id,
			tenant_id,
			name,
			key_hash,
			status,
			created_at,
			expires_at
		)
		values (
			'pak_overview_extra',
			'tenant_demo',
			'overview-extra',
			'sha256:overview-extra',
			'active',
			now() - interval '20 minutes',
			now() + interval '29 days'
		)
		on conflict (id) do update set
			status = excluded.status,
			expires_at = excluded.expires_at;

		update llm_request_logs
		set
			request_started_at = now() - interval '10 minutes',
			request_completed_at = now() - interval '10 minutes' + interval '182 milliseconds',
			created_at = now() - interval '10 minutes',
			status_code = 200,
			usage_status = 'success';
	`); err != nil {
		t.Fatalf("prepare overview fallback data failed: %v", err)
	}

	var recentLogCount int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from llm_request_logs
		where request_started_at >= now() - interval '24 hours'
	`).Scan(&recentLogCount); err != nil {
		t.Fatalf("count recent llm_request_logs failed: %v", err)
	}

	payload, err := console.Overview(ctx)
	if err != nil {
		t.Fatalf("Overview failed: %v", err)
	}

	if metricValue(payload.Stats, "24 小时请求量") != strconv.Itoa(recentLogCount) {
		t.Fatalf("expected 24 小时请求量 %q, got %#v", strconv.Itoa(recentLogCount), payload.Stats)
	}
	if metricValue(payload.Stats, "成功率") != "100.00%" {
		t.Fatalf("expected 成功率 %q, got %#v", "100.00%", payload.Stats)
	}
	if metricValue(payload.Stats, "活跃 API 密钥") != "2" {
		t.Fatalf("expected 活跃 API 密钥 %q, got %#v", "2", payload.Stats)
	}
	if len(payload.AuditSnapshot) == 0 {
		t.Fatal("expected audit snapshot to be populated from llm_request_logs")
	}
	if !containsTableRowValue(payload.AuditSnapshot, "tenant_demo") {
		t.Fatalf("expected audit snapshot to include tenant_demo, got %#v", payload.AuditSnapshot)
	}
}

func TestPostgresConsoleServiceOverviewRouteHealthIncludesConfiguredMIMOWithoutUsage(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		delete from llm_request_events;
		delete from llm_usage_agg_hourly;
		delete from llm_request_logs;
		delete from route_catalog;
		delete from provider_credentials;

		insert into provider_credentials (id, provider, display_name, supported_models, base_url, encrypted_secret, status) values
			('provider_dashscope_primary', 'dashscope', 'Qwen', '{"qwen-flash"}', 'https://dashscope.aliyuncs.com/compatible-mode/v1', '', 'active'),
			('provider_mimo_primary', 'mimo', 'MIMO', '{"mimo-v2.5-pro"}', 'https://api.xiaomimimo.com/v1', '', 'active');

		insert into route_catalog (id, requested_model, resolved_provider, provider_credential_id, endpoint, latency_ms, health_status, request_mode, updated_at) values
			('route:provider_dashscope_primary:default', 'qwen-flash', 'Qwen', 'provider_dashscope_primary', '/v1/chat/completions', 218, 'healthy', '聊天', now()),
			('route:provider_mimo_primary:default', 'mimo-v2.5-pro', 'MIMO', 'provider_mimo_primary', '/v1/chat/completions', 286, 'warning', '聊天', now());

		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			resolved_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			cached_tokens,
			input_price_microyuan_per_million,
			output_price_microyuan_per_million,
			cached_price_microyuan_per_million,
			input_cost_microyuan,
			output_cost_microyuan,
			cached_cost_microyuan,
			total_cost_microyuan,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_overview_qwen_only',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_dashscope_primary',
			'route:provider_dashscope_primary:default',
			'/v1/chat/completions',
			'gateway-public',
			'qwen-flash',
			'qwen-flash',
			'upstream',
			'success',
			200,
			182,
			12,
			6,
			18,
			0,
			2000000,
			20000000,
			500000,
			24,
			120,
			0,
			144,
			'',
			'',
			now() - interval '20 minutes',
			now() - interval '20 minutes' + interval '182 milliseconds',
			now() - interval '20 minutes'
		);
	`); err != nil {
		t.Fatalf("seed overview route health fallback failed: %v", err)
	}

	payload, err := console.Overview(ctx)
	if err != nil {
		t.Fatalf("Overview failed: %v", err)
	}

	if !containsTableRowValue(payload.RouteHealth, "qwen-flash") {
		t.Fatalf("expected route health to include qwen-flash, got %#v", payload.RouteHealth)
	}
	if !containsTableRowValue(payload.RouteHealth, "mimo-v2.5-pro") {
		t.Fatalf("expected route health to include mimo-v2.5-pro, got %#v", payload.RouteHealth)
	}
}

func TestPostgresConsoleServiceStreamPlaygroundRejectsEmbeddingsRoute(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_, conn := newUsageConsoleService(t, ctx)
	if _, err := conn.Exec(ctx, `
		update route_catalog
		set endpoint = '/v1/embeddings'
		where requested_model = 'gpt-4o-mini';
	`); err != nil {
		t.Fatalf("update route_catalog failed: %v", err)
	}

	chatProxy := &stubConsoleChatProxy{}
	console := service.NewPostgresConsoleService(
		conn,
		stubConsoleResolveAuthService{
			requestContext: domain.RequestContext{
				TenantID:             "tenant_demo",
				PlatformAPIKeyID:     "pak_demo",
				SelectedProviderID:   "provider_openai_demo",
				SelectedProviderName: "OpenAI Demo",
				RouteID:              "route:provider_openai_demo:default",
				ProviderTarget: domain.ProviderTarget{
					CredentialID: "provider_openai_demo",
					Provider:     "openai-compatible",
					BaseURL:      "https://example.com/v1",
					APIKey:       "test-key",
				},
			},
		},
		chatProxy,
		nil,
		"seed-key",
	)

	_, err := console.StreamPlayground(ctx, service.PlaygroundRunRequest{
		Model:  "gpt-4o-mini",
		Prompt: "hello",
		Stream: true,
	})
	if err == nil {
		t.Fatal("expected StreamPlayground to reject embeddings route")
	}

	var statusErr service.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, statusErr.Code)
	}
	if !strings.Contains(statusErr.Message, "only supports chat routes") {
		t.Fatalf("expected chat route error message, got %q", statusErr.Message)
	}
	if chatProxy.streamCalls != 0 {
		t.Fatalf("expected chat proxy stream not to be called, got %d calls", chatProxy.streamCalls)
	}
}

func TestPostgresConsoleServiceRunPlaygroundRejectsEmbeddingsRoute(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_, conn := newUsageConsoleService(t, ctx)
	if _, err := conn.Exec(ctx, `
		update route_catalog
		set endpoint = '/v1/embeddings'
		where requested_model = 'gpt-4o-mini';
	`); err != nil {
		t.Fatalf("update route_catalog failed: %v", err)
	}

	chatProxy := &stubConsoleChatProxy{}
	console := service.NewPostgresConsoleService(
		conn,
		stubConsoleResolveAuthService{
			requestContext: domain.RequestContext{
				TenantID:             "tenant_demo",
				PlatformAPIKeyID:     "pak_demo",
				SelectedProviderID:   "provider_openai_demo",
				SelectedProviderName: "OpenAI Demo",
				RouteID:              "route:provider_openai_demo:default",
				ProviderTarget: domain.ProviderTarget{
					CredentialID: "provider_openai_demo",
					Provider:     "openai-compatible",
					BaseURL:      "https://example.com/v1",
					APIKey:       "test-key",
				},
			},
		},
		chatProxy,
		nil,
		"seed-key",
	)

	_, err := console.RunPlayground(ctx, service.PlaygroundRunRequest{
		Model:  "gpt-4o-mini",
		Prompt: "hello",
	})
	if err == nil {
		t.Fatal("expected RunPlayground to reject embeddings route")
	}

	var statusErr service.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, statusErr.Code)
	}
	if !strings.Contains(statusErr.Message, "only supports chat or rag routes") {
		t.Fatalf("expected route rejection message, got %q", statusErr.Message)
	}
	if chatProxy.completeCalls != 0 {
		t.Fatalf("expected chat proxy complete not to be called, got %d calls", chatProxy.completeCalls)
	}
}

func TestPostgresConsoleServiceStreamPlaygroundWritesSSEAndPersistsRun(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_, conn := newUsageConsoleService(t, ctx)
	uniquePrompt := "stream-success-prompt"

	var auditCountBefore int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from audit_logs
		where platform_api_key_id = 'pak_demo'
		  and requested_model = 'gpt-4o-mini'
		  and endpoint = '/v1/chat/completions';
	`).Scan(&auditCountBefore); err != nil {
		t.Fatalf("count audit_logs before failed: %v", err)
	}

	chatProxy := &stubConsoleChatProxy{
		streamResult: service.ChatCompletionStream{
			StatusCode:  http.StatusOK,
			ContentType: "text/plain",
			Run: func(emit func([]byte) error, onFirstToken func()) (service.ChatStreamResult, error) {
				if onFirstToken != nil {
					onFirstToken()
				}
				if err := emit([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\n")); err != nil {
					return service.ChatStreamResult{}, err
				}
				if err := emit([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\n")); err != nil {
					return service.ChatStreamResult{}, err
				}
				return service.ChatStreamResult{
					Response: service.ChatResponse{
						Model: "gpt-4o-mini",
						Choices: []service.ChatChoice{
							{
								Message: service.ChatMessage{
									Role:    "assistant",
									Content: "你好",
								},
							},
						},
					},
					SawContentToken: true,
				}, nil
			},
		},
	}
	console := service.NewPostgresConsoleService(
		conn,
		stubConsoleResolveAuthService{
			requestContext: domain.RequestContext{
				TenantID:             "tenant_demo",
				PlatformAPIKeyID:     "pak_demo",
				SelectedProviderID:   "provider_openai_demo",
				SelectedProviderName: "OpenAI Demo",
				RouteID:              "route:provider_openai_demo:default",
				ProviderTarget: domain.ProviderTarget{
					CredentialID: "provider_openai_demo",
					Provider:     "openai-compatible",
					BaseURL:      "https://example.com/v1",
					APIKey:       "test-key",
				},
			},
		},
		chatProxy,
		nil,
		"seed-key",
	)

	session, err := console.StreamPlayground(ctx, service.PlaygroundRunRequest{
		Model:  "gpt-4o-mini",
		Prompt: uniquePrompt,
		Stream: true,
	})
	if err != nil {
		t.Fatalf("StreamPlayground failed: %v", err)
	}
	if session.ContentType != "text/event-stream; charset=utf-8" {
		t.Fatalf("expected fixed SSE content type, got %q", session.ContentType)
	}

	var emitted strings.Builder
	response, err := session.Run(func(chunk []byte) error {
		emitted.Write(chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("session.Run failed: %v", err)
	}
	if response.Status != "200 成功" {
		t.Fatalf("expected success status, got %q", response.Status)
	}
	if response.Response != "你好" {
		t.Fatalf("expected response text %q, got %q", "你好", response.Response)
	}

	output := emitted.String()
	for _, fragment := range []string{"event: meta", "event: token", "event: stats", "event: done"} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("expected emitted stream to include %q, got %q", fragment, output)
		}
	}

	var storedExcerpt string
	var storedStatusCode int
	if err := conn.QueryRow(ctx, `
		select response_excerpt, status_code
		from playground_runs
		where prompt = $1
		order by created_at desc
		limit 1;
	`, uniquePrompt).Scan(&storedExcerpt, &storedStatusCode); err != nil {
		t.Fatalf("query playground_runs failed: %v", err)
	}
	if storedExcerpt != "你好" {
		t.Fatalf("expected stored response_excerpt %q, got %q", "你好", storedExcerpt)
	}
	if storedStatusCode != http.StatusOK {
		t.Fatalf("expected stored status_code %d, got %d", http.StatusOK, storedStatusCode)
	}

	var auditCountAfter int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from audit_logs
		where platform_api_key_id = 'pak_demo'
		  and requested_model = 'gpt-4o-mini'
		  and endpoint = '/v1/chat/completions';
	`).Scan(&auditCountAfter); err != nil {
		t.Fatalf("count audit_logs after failed: %v", err)
	}
	if auditCountAfter != auditCountBefore+1 {
		t.Fatalf("expected audit log count to grow by 1, got before=%d after=%d", auditCountBefore, auditCountAfter)
	}
	if chatProxy.streamCalls != 1 {
		t.Fatalf("expected chat proxy stream to be called once, got %d", chatProxy.streamCalls)
	}
}

func TestPostgresConsoleServiceStreamPlaygroundEmitsFailureStatsWhenPersistenceFails(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_, conn := newUsageConsoleService(t, ctx)
	chatProxy := &stubConsoleChatProxy{
		streamResult: service.ChatCompletionStream{
			StatusCode:  http.StatusOK,
			ContentType: "text/event-stream; charset=utf-8",
			Run: func(emit func([]byte) error, onFirstToken func()) (service.ChatStreamResult, error) {
				if onFirstToken != nil {
					onFirstToken()
				}
				if err := emit([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"部\"}}]}\n\n")); err != nil {
					return service.ChatStreamResult{}, err
				}
				return service.ChatStreamResult{
					Response: service.ChatResponse{
						Model: "gpt-4o-mini",
						Choices: []service.ChatChoice{
							{
								Message: service.ChatMessage{
									Role:    "assistant",
									Content: "部分结果",
								},
							},
						},
					},
					SawContentToken: true,
				}, nil
			},
		},
	}
	console := service.NewPostgresConsoleService(
		conn,
		stubConsoleResolveAuthService{
			requestContext: domain.RequestContext{
				TenantID:             "tenant_demo",
				PlatformAPIKeyID:     "pak_demo",
				SelectedProviderID:   "provider_openai_demo",
				SelectedProviderName: "OpenAI Demo",
				RouteID:              "route:provider_openai_demo:default",
				ProviderTarget: domain.ProviderTarget{
					CredentialID: "provider_openai_demo",
					Provider:     "openai-compatible",
					BaseURL:      "https://example.com/v1",
					APIKey:       "test-key",
				},
			},
		},
		chatProxy,
		nil,
		"seed-key",
	)

	session, err := console.StreamPlayground(ctx, service.PlaygroundRunRequest{
		Model:  "gpt-4o-mini",
		Prompt: "stream-persist-failure",
		Stream: true,
	})
	if err != nil {
		t.Fatalf("StreamPlayground failed: %v", err)
	}

	if err := conn.Close(ctx); err != nil {
		t.Fatalf("conn.Close failed: %v", err)
	}

	var emitted strings.Builder
	response, err := session.Run(func(chunk []byte) error {
		emitted.Write(chunk)
		return nil
	})
	if err == nil {
		t.Fatal("expected session.Run to fail after persistence loss")
	}
	if response.Status != "500 失败" {
		t.Fatalf("expected failure status after persistence error, got %q", response.Status)
	}

	output := emitted.String()
	if !strings.Contains(output, "event: stats") {
		t.Fatalf("expected failure path to emit stats event, got %q", output)
	}
	if strings.Contains(output, "event: done") {
		t.Fatalf("expected failure path to omit done event, got %q", output)
	}
}

func TestPostgresConsoleServiceCreateAPIKey(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	result, err := console.CreateAPIKey(ctx, service.CreateAPIKeyRequest{
		TenantID: "tenant_demo",
		Name:     "prod-gateway-2",
		Scopes:   []string{"chat", "embeddings"},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	if strings.TrimSpace(result.RawKey) == "" {
		t.Fatal("expected created raw key")
	}
	if result.Item.Status != "启用" {
		t.Fatalf("expected status 启用, got %q", result.Item.Status)
	}

	var count int
	if err := conn.QueryRow(ctx, `select count(*) from platform_api_keys where id = $1 and tenant_id = $2 and status = 'active';`, result.Item.ID, "tenant_demo").Scan(&count); err != nil {
		t.Fatalf("QueryRow platform_api_keys failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected created key to persist, got count %d", count)
	}
}

func TestPostgresConsoleServiceRotateAPIKeyReturnsRawKeyOnce(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	result, err := console.RotateAPIKey(ctx, "pak_demo", service.RotateAPIKeyRequest{})
	if err != nil {
		t.Fatalf("RotateAPIKey failed: %v", err)
	}

	if strings.TrimSpace(result.RawKey) == "" {
		t.Fatal("expected rotated raw key")
	}
	if result.Item.Tenant != "tenant_demo" {
		t.Fatalf("expected tenant tenant_demo, got %q", result.Item.Tenant)
	}

	var oldStatus string
	if err := conn.QueryRow(ctx, `select status from platform_api_keys where id = 'pak_demo';`).Scan(&oldStatus); err != nil {
		t.Fatalf("QueryRow old platform_api_keys failed: %v", err)
	}
	if oldStatus != "disabled" {
		t.Fatalf("expected old key disabled, got %q", oldStatus)
	}
}

func TestPostgresConsoleServiceDeactivateAPIKey(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	result, err := console.DeactivateAPIKey(ctx, "pak_demo")
	if err != nil {
		t.Fatalf("DeactivateAPIKey failed: %v", err)
	}

	if result.Item.Status != "停用" {
		t.Fatalf("expected status 停用, got %q", result.Item.Status)
	}

	var status string
	if err := conn.QueryRow(ctx, `select status from platform_api_keys where id = 'pak_demo';`).Scan(&status); err != nil {
		t.Fatalf("QueryRow platform_api_keys failed: %v", err)
	}
	if status != "disabled" {
		t.Fatalf("expected disabled status in db, got %q", status)
	}
}

func TestPostgresConsoleServiceDeleteUnusedAPIKey(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	created, err := console.CreateAPIKey(ctx, service.CreateAPIKeyRequest{
		TenantID: "tenant_demo",
		Name:     "unused-delete-me",
		Scopes:   []string{"chat"},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	result, err := console.DeleteAPIKey(ctx, created.Item.ID)
	if err != nil {
		t.Fatalf("DeleteAPIKey failed: %v", err)
	}

	if result.Item.Status != "已删除" {
		t.Fatalf("expected status 已删除, got %q", result.Item.Status)
	}

	var count int
	if err := conn.QueryRow(ctx, `select count(*) from platform_api_keys where id = $1;`, created.Item.ID).Scan(&count); err != nil {
		t.Fatalf("QueryRow platform_api_keys failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected deleted key to be removed, got count %d", count)
	}
}

func TestPostgresConsoleServiceDeleteReferencedAPIKeyReturnsConflict(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	_, err := console.DeleteAPIKey(ctx, "pak_demo")
	if err == nil {
		t.Fatal("expected DeleteAPIKey to fail for referenced key")
	}

	var statusErr service.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Code != 409 {
		t.Fatalf("expected conflict status, got %d", statusErr.Code)
	}
}

func TestPostgresConsoleServiceRevealLegacyKeyMarksUnrecoverable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into platform_api_keys (
			id, tenant_id, name, key_hash, status, scopes, created_at, expires_at, secret_recoverable
		) values (
			'pak_legacy_only_hash',
			'tenant_demo',
			'legacy-only-hash',
			'sha256:legacy-only-hash',
			'active',
			ARRAY['chat'],
			now(),
			now() + interval '30 days',
			false
		);
	`); err != nil {
		t.Fatalf("seed legacy key failed: %v", err)
	}

	revealer, ok := console.(interface {
		RevealAPIKeySecret(context.Context, string) (service.APIKeySecretView, error)
	})
	if !ok {
		t.Fatal("expected console service to implement RevealAPIKeySecret")
	}

	secretView, err := revealer.RevealAPIKeySecret(ctx, "pak_legacy_only_hash")
	if err != nil {
		t.Fatalf("RevealAPIKeySecret failed: %v", err)
	}
	if secretView.Revealable {
		t.Fatal("expected legacy key to be unrecoverable")
	}
	if !secretView.LegacyUnrecoverable {
		t.Fatal("expected LegacyUnrecoverable to be true")
	}

	var action string
	var accessResult string
	if err := conn.QueryRow(ctx, `
		select action, access_result
		from api_key_secret_access_logs
		where api_key_id = 'pak_legacy_only_hash'
		order by created_at desc, id desc
		limit 1;
	`).Scan(&action, &accessResult); err != nil {
		t.Fatalf("QueryRow reveal api_key_secret_access_logs failed: %v", err)
	}
	if action != "reveal" {
		t.Fatalf("expected action %q, got %q", "reveal", action)
	}
	if accessResult != "allowed" {
		t.Fatalf("expected access_result %q, got %q", "allowed", accessResult)
	}
}

func TestPostgresConsoleServiceCopyAPIKeySecretWritesAllowedAuditLog(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	created, err := console.CreateAPIKey(service.ContextWithConsolePrincipal(ctx, service.ConsolePrincipal{
		UserID: "user_admin_demo",
		Role:   "admin",
	}), service.CreateAPIKeyRequest{
		TenantID: "tenant_demo",
		Name:     "copy-audit-admin",
		Scopes:   []string{"chat"},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	copier, ok := console.(interface {
		CopyAPIKeySecret(context.Context, string, string, string) (service.APIKeySecretView, error)
	})
	if !ok {
		t.Fatal("expected console service to implement CopyAPIKeySecret")
	}

	copyCtx := service.ContextWithConsolePrincipal(ctx, service.ConsolePrincipal{
		UserID: "user_admin_demo",
		Role:   "admin",
	})
	secretView, err := copier.CopyAPIKeySecret(copyCtx, created.Item.ID, "203.0.113.10", "console-copy-test")
	if err != nil {
		t.Fatalf("CopyAPIKeySecret failed: %v", err)
	}
	if !secretView.Revealable {
		t.Fatal("expected copied key to be revealable")
	}
	if secretView.FullKey == "" {
		t.Fatal("expected FullKey to be populated")
	}

	var actorUserID string
	var actorRole string
	var action string
	var accessResult string
	var ipAddress string
	var userAgent string
	if err := conn.QueryRow(ctx, `
		select actor_user_id, actor_role, action, access_result, ip_address, user_agent
		from api_key_secret_access_logs
		where api_key_id = $1
		order by created_at desc, id desc
		limit 1;
	`, created.Item.ID).Scan(&actorUserID, &actorRole, &action, &accessResult, &ipAddress, &userAgent); err != nil {
		t.Fatalf("QueryRow api_key_secret_access_logs failed: %v", err)
	}
	if actorUserID != "user_admin_demo" {
		t.Fatalf("expected actor_user_id %q, got %q", "user_admin_demo", actorUserID)
	}
	if actorRole != "admin" {
		t.Fatalf("expected actor_role %q, got %q", "admin", actorRole)
	}
	if action != "copy" {
		t.Fatalf("expected action %q, got %q", "copy", action)
	}
	if accessResult != "allowed" {
		t.Fatalf("expected access_result %q, got %q", "allowed", accessResult)
	}
	if ipAddress != "203.0.113.10" {
		t.Fatalf("expected ip_address %q, got %q", "203.0.113.10", ipAddress)
	}
	if userAgent != "console-copy-test" {
		t.Fatalf("expected user_agent %q, got %q", "console-copy-test", userAgent)
	}
}

func TestPostgresConsoleServiceApplicationsReturnsPendingRows(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `delete from account_applications`); err != nil {
		t.Fatalf("delete seeded applications failed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		insert into account_applications (
			id,
			email,
			email_normalized,
			name,
			company_name,
			use_case,
			status,
			reviewer_id,
			review_comment,
			reviewed_at,
			created_at
		) values
			(
				'app_demo_pending',
				'pending@example.com',
				'pending@example.com',
				'待审批用户',
				'Pending Co',
				'租户接入',
				'pending',
				null,
				'',
				null,
				timestamptz '2026-04-30T10:30:00Z'
			),
			(
				'app_demo_rejected',
				'rejected@example.com',
				'rejected@example.com',
				'已拒绝用户',
				'Rejected Co',
				'压测脚本',
				'rejected',
				'user_admin_demo',
				'seed reject',
				timestamptz '2026-04-29T08:20:00Z',
				timestamptz '2026-04-29T08:15:00Z'
			),
			(
				'app_demo_approved',
				'approved@example.com',
				'approved@example.com',
				'已审批用户',
				'Approved Co',
				'内部知识问答',
				'approved',
				'user_admin_demo',
				'seed approve',
				timestamptz '2026-04-28T02:05:00Z',
				timestamptz '2026-04-28T02:00:00Z'
			)
	`); err != nil {
		t.Fatalf("insert applications failed: %v", err)
	}

	payload, err := console.Applications(ctx)
	if err != nil {
		t.Fatalf("Applications failed: %v", err)
	}

	if len(payload.Items) != 3 {
		t.Fatalf("expected 3 application rows, got %d", len(payload.Items))
	}

	if payload.Items[0].ID != "app_demo_pending" {
		t.Fatalf("expected newest application id %q, got %q", "app_demo_pending", payload.Items[0].ID)
	}
	if payload.Items[1].ID != "app_demo_rejected" {
		t.Fatalf("expected second application id %q, got %q", "app_demo_rejected", payload.Items[1].ID)
	}
	if payload.Items[2].ID != "app_demo_approved" {
		t.Fatalf("expected third application id %q, got %q", "app_demo_approved", payload.Items[2].ID)
	}

	first := payload.Items[0]
	if first.Email != "pending@example.com" {
		t.Fatalf("expected email %q, got %q", "pending@example.com", first.Email)
	}
	if first.Name != "待审批用户" {
		t.Fatalf("expected name %q, got %q", "待审批用户", first.Name)
	}
	if first.CompanyName != "Pending Co" {
		t.Fatalf("expected company_name %q, got %q", "Pending Co", first.CompanyName)
	}
	if first.UseCase != "租户接入" {
		t.Fatalf("expected use_case %q, got %q", "租户接入", first.UseCase)
	}
	if first.Status != "pending" {
		t.Fatalf("expected status %q, got %q", "pending", first.Status)
	}
	if first.CreatedAt != "2026-04-30T18:30:00+08:00" {
		t.Fatalf("expected created_at %q, got %q", "2026-04-30T18:30:00+08:00", first.CreatedAt)
	}
	if payload.Items[1].CreatedAt != "2026-04-29T16:15:00+08:00" {
		t.Fatalf("expected second created_at %q, got %q", "2026-04-29T16:15:00+08:00", payload.Items[1].CreatedAt)
	}
	if payload.Items[2].CreatedAt != "2026-04-28T10:00:00+08:00" {
		t.Fatalf("expected third created_at %q, got %q", "2026-04-28T10:00:00+08:00", payload.Items[2].CreatedAt)
	}
}

func TestPostgresConsoleServiceCreateApplicationPersistsPendingRow(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into captcha_challenges (
			id,
			answer_hash,
			status,
			pass_token_hash,
			expires_at
		) values (
			'cap_for_apply',
			$1,
			'verified',
			$2,
			now() + interval '5 minutes'
		)
	`, testHashCaptchaValue("ABCD"), testHashCaptchaValue("cp_valid")); err != nil {
		t.Fatalf("seed captcha challenge failed: %v", err)
	}

	result, err := console.CreateApplication(ctx, service.CreateApplicationRequest{
		Email:            "new-user@example.com",
		Name:             "新用户",
		CompanyName:      "New Co",
		UseCase:          "测试接入",
		Password:         "Example1234",
		CaptchaPassToken: "cp_valid",
	})
	if err != nil {
		t.Fatalf("CreateApplication failed: %v", err)
	}

	if result.Item.ID == "" {
		t.Fatal("expected application id to be generated")
	}
	if result.Item.Status != "pending" {
		t.Fatalf("expected status %q, got %q", "pending", result.Item.Status)
	}
	if result.Item.Email != "new-user@example.com" {
		t.Fatalf("expected email %q, got %q", "new-user@example.com", result.Item.Email)
	}
	if result.Item.Name != "新用户" {
		t.Fatalf("expected name %q, got %q", "新用户", result.Item.Name)
	}
	if result.Item.CompanyName != "New Co" {
		t.Fatalf("expected company_name %q, got %q", "New Co", result.Item.CompanyName)
	}
	if result.Item.UseCase != "测试接入" {
		t.Fatalf("expected use_case %q, got %q", "测试接入", result.Item.UseCase)
	}
	if result.Item.CreatedAt == "" {
		t.Fatal("expected created_at to be populated")
	}

	var status string
	var passwordHash *string
	var reviewerID *string
	var reviewedAt *time.Time
	if err := conn.QueryRow(ctx, `
		select status, password_hash, reviewer_id, reviewed_at
		from account_applications
		where id = $1
	`, result.Item.ID).Scan(&status, &passwordHash, &reviewerID, &reviewedAt); err != nil {
		t.Fatalf("select created application failed: %v", err)
	}
	if status != "pending" {
		t.Fatalf("expected stored status %q, got %q", "pending", status)
	}
	if passwordHash == nil || strings.TrimSpace(*passwordHash) == "" {
		t.Fatal("expected password_hash to be stored")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*passwordHash), []byte("Example1234")); err != nil {
		t.Fatalf("expected stored password_hash to match Example1234: %v", err)
	}
	if reviewerID != nil {
		t.Fatalf("expected reviewer_id to be null, got %q", *reviewerID)
	}
	if reviewedAt != nil {
		t.Fatal("expected reviewed_at to be null")
	}

	var captchaStatus string
	if err := conn.QueryRow(ctx, `
		select status
		from captcha_challenges
		where id = 'cap_for_apply'
	`).Scan(&captchaStatus); err != nil {
		t.Fatalf("select captcha challenge failed: %v", err)
	}
	if captchaStatus != "consumed" {
		t.Fatalf("expected captcha status %q, got %q", "consumed", captchaStatus)
	}
}

func TestPostgresConsoleServiceCreateApplicationRequiresPasswordAndCaptchaToken(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	_, err := console.CreateApplication(ctx, service.CreateApplicationRequest{
		Email:       "new-user@example.com",
		Name:        "新用户",
		CompanyName: "New Co",
		UseCase:     "测试接入",
		Password:    "",
	})
	if err == nil {
		t.Fatal("expected CreateApplication to require password and captcha_pass_token")
	}

	var statusErr service.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", statusErr.Code)
	}
}

func TestPostgresConsoleServiceCreateApplicationRejectsEmailWithoutAt(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	_, err := console.CreateApplication(ctx, service.CreateApplicationRequest{
		Email:            "invalid-email",
		Name:             "新用户",
		CompanyName:      "New Co",
		UseCase:          "测试接入",
		Password:         "Example1234",
		CaptchaPassToken: "cp_demo",
	})
	if err == nil {
		t.Fatal("expected CreateApplication to reject email without @")
	}

	var statusErr service.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", statusErr.Code)
	}
	if statusErr.Message != "邮箱格式不合法，请输入包含 @ 的邮箱地址。" {
		t.Fatalf("expected chinese email validation message, got %q", statusErr.Message)
	}
}

func TestPostgresConsoleServiceCreateApplicationReturnsChinesePasswordRuleError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	_, err := console.CreateApplication(ctx, service.CreateApplicationRequest{
		Email:            "new-user@example.com",
		Name:             "新用户",
		CompanyName:      "New Co",
		UseCase:          "测试接入",
		Password:         "12345678",
		CaptchaPassToken: "cp_demo",
	})
	if err == nil {
		t.Fatal("expected CreateApplication to reject password without letters")
	}

	var statusErr service.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", statusErr.Code)
	}
	if statusErr.Message != "密码需同时包含字母和数字。" {
		t.Fatalf("expected chinese password validation message, got %q", statusErr.Message)
	}
}

func TestPostgresConsoleServiceCreateApplicationRejectsExistingActiveUserEmail(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into captcha_challenges (
			id,
			answer_hash,
			status,
			pass_token_hash,
			expires_at
		) values (
			'cap_duplicate_user',
			$1,
			'verified',
			$2,
			now() + interval '5 minutes'
		)
	`, testHashCaptchaValue("EFGH"), testHashCaptchaValue("cp_existing_user")); err != nil {
		t.Fatalf("seed captcha challenge failed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		insert into users (id, email, name, role, status, password_hash)
		values ('user_existing_login', 'member@example.com', 'Existing', 'member', 'active', '$2a$10$abcdefghijklmnopqrstuv')
	`); err != nil {
		t.Fatalf("seed existing user failed: %v", err)
	}

	_, err := console.CreateApplication(ctx, service.CreateApplicationRequest{
		Email:            "member@example.com",
		Name:             "新用户",
		CompanyName:      "New Co",
		UseCase:          "测试接入",
		Password:         "Example1234",
		CaptchaPassToken: "cp_existing_user",
	})
	if err == nil {
		t.Fatal("expected duplicate active user email error")
	}

	var statusErr service.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", statusErr.Code)
	}
}

func TestPostgresConsoleServiceApproveApplicationRequiresTenantID(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into account_applications (
			id,
			email,
			email_normalized,
			name,
			company_name,
			use_case,
			status
		) values (
			'app_pending_missing_tenant',
			'missing-tenant@example.com',
			'missing-tenant@example.com',
			'缺少租户用户',
			'Missing Tenant Co',
			'租户接入',
			'pending'
		)
	`); err != nil {
		t.Fatalf("insert pending application failed: %v", err)
	}

	_, err := console.ApproveApplication(ctx, "app_pending_missing_tenant", service.ApproveApplicationRequest{
		ActorID: "user_admin_demo",
		Comment: "approved without tenant",
	})
	if err == nil {
		t.Fatal("expected ApproveApplication to require tenant_id")
	}

	var statusErr service.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Code != 400 {
		t.Fatalf("expected bad request status, got %d", statusErr.Code)
	}
	if statusErr.Message != "tenant_id is required" {
		t.Fatalf("expected message %q, got %q", "tenant_id is required", statusErr.Message)
	}

	var applicationStatus string
	if err := conn.QueryRow(ctx, `
		select status
		from account_applications
		where id = 'app_pending_missing_tenant'
	`).Scan(&applicationStatus); err != nil {
		t.Fatalf("select application status failed: %v", err)
	}
	if applicationStatus != "pending" {
		t.Fatalf("expected application to remain pending, got %q", applicationStatus)
	}
}

func TestPostgresConsoleServiceApproveApplicationRequiresTokenLimit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("Example1234"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword failed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		insert into account_applications (
			id,
			email,
			email_normalized,
			name,
			company_name,
			use_case,
			password_hash,
			status,
			created_at
		) values (
			'app_pending_missing_token_limit',
			'missing-token-limit@example.com',
			'missing-token-limit@example.com',
			'缺少额度用户',
			'Token Co',
			'租户接入',
			$1,
			'pending',
			timestamptz '2026-04-25T01:02:03Z'
		)
	`, string(passwordHash)); err != nil {
		t.Fatalf("seed approve application failed: %v", err)
	}

	_, err = console.ApproveApplication(ctx, "app_pending_missing_token_limit", service.ApproveApplicationRequest{
		ActorID:  "user_admin_demo",
		Comment:  "approved without token limit",
		TenantID: "tenant_demo",
	})
	if err == nil {
		t.Fatal("expected ApproveApplication to require token_limit")
	}

	var statusErr service.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Code != 400 {
		t.Fatalf("expected bad request status, got %d", statusErr.Code)
	}
}

func TestPostgresConsoleServiceApproveApplicationRequiresCostLimit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("Example1234"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword failed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		insert into account_applications (
			id,
			email,
			email_normalized,
			name,
			company_name,
			use_case,
			password_hash,
			status,
			created_at
		) values (
			'app_pending_missing_cost_limit',
			'missing-cost-limit@example.com',
			'missing-cost-limit@example.com',
			'缺少金额额度用户',
			'Cost Co',
			'租户接入',
			$1,
			'pending',
			timestamptz '2026-04-25T01:02:03Z'
		)
	`, string(passwordHash)); err != nil {
		t.Fatalf("seed approve application failed: %v", err)
	}

	_, err = console.ApproveApplication(ctx, "app_pending_missing_cost_limit", service.ApproveApplicationRequest{
		ActorID:    "user_admin_demo",
		Comment:    "approved without cost limit",
		TenantID:   "tenant_demo",
		TokenLimit: 123456,
	})
	if err == nil {
		t.Fatal("expected ApproveApplication to require cost_limit_microyuan")
	}

	var statusErr service.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.Code != 400 {
		t.Fatalf("expected bad request status, got %d", statusErr.Code)
	}
	if statusErr.Message != "cost_limit_microyuan is required" {
		t.Fatalf("expected message %q, got %q", "cost_limit_microyuan is required", statusErr.Message)
	}
}

func TestPostgresConsoleServiceApproveApplicationCreatesUserMembershipAndAudit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("Example1234"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword failed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		insert into users (id, email, name, role, status)
		values (
			'user_pending_existing',
			'service-pending@example.com',
			'旧名字',
			'member',
			'disabled'
		)
	`); err != nil {
		t.Fatalf("seed existing user failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into tenant_memberships (id, tenant_id, user_id, role, status)
		values (
			'tm_pending_existing',
			'tenant_demo',
			'user_pending_existing',
			'member',
			'disabled'
		)
	`); err != nil {
		t.Fatalf("seed existing membership failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into account_applications (
			id,
			email,
			email_normalized,
			name,
			company_name,
			use_case,
			password_hash,
			status,
			created_at
		) values (
			'app_service_pending',
			'service-pending@example.com',
			'service-pending@example.com',
			'服务层待审批用户',
			'Service Co',
			'租户接入',
			$1,
			'pending',
			timestamptz '2026-04-25T01:02:03Z'
		)
	`, string(passwordHash)); err != nil {
		t.Fatalf("seed approve application failed: %v", err)
	}

	result, err := console.ApproveApplication(ctx, "app_service_pending", service.ApproveApplicationRequest{
		ActorID:            "user_admin_demo",
		Comment:            "approved via service",
		TenantID:           "tenant_demo",
		TokenLimit:         23456789,
		CostLimitMicroyuan: 4567000000,
		AllowedModels:      []string{"qwen-flash", "mimo-v2.5-pro"},
	})
	if err != nil {
		t.Fatalf("ApproveApplication failed: %v", err)
	}

	if result.Item.ID != "app_service_pending" {
		t.Fatalf("expected item id %q, got %q", "app_service_pending", result.Item.ID)
	}
	if result.Item.Email != "service-pending@example.com" {
		t.Fatalf("expected item email %q, got %q", "service-pending@example.com", result.Item.Email)
	}
	if result.Item.Name != "服务层待审批用户" {
		t.Fatalf("expected item name %q, got %q", "服务层待审批用户", result.Item.Name)
	}
	if result.Item.CompanyName != "Service Co" {
		t.Fatalf("expected item company_name %q, got %q", "Service Co", result.Item.CompanyName)
	}
	if result.Item.UseCase != "租户接入" {
		t.Fatalf("expected item use_case %q, got %q", "租户接入", result.Item.UseCase)
	}
	if result.Item.Status != "approved" {
		t.Fatalf("expected item status %q, got %q", "approved", result.Item.Status)
	}
	if result.Item.CreatedAt != "2026-04-25T09:02:03+08:00" {
		t.Fatalf("expected item created_at %q, got %q", "2026-04-25T09:02:03+08:00", result.Item.CreatedAt)
	}

	var applicationStatus string
	var applicationPasswordHash *string
	var reviewerID string
	var reviewComment string
	var reviewedAt time.Time
	if err := conn.QueryRow(ctx, `
		select status, password_hash, reviewer_id, review_comment, reviewed_at
		from account_applications
		where id = 'app_service_pending'
	`).Scan(&applicationStatus, &applicationPasswordHash, &reviewerID, &reviewComment, &reviewedAt); err != nil {
		t.Fatalf("select approved application failed: %v", err)
	}
	if applicationStatus != "approved" {
		t.Fatalf("expected application status %q, got %q", "approved", applicationStatus)
	}
	if applicationPasswordHash != nil {
		t.Fatal("expected approved application password_hash to be cleared")
	}
	if reviewerID != "user_admin_demo" {
		t.Fatalf("expected reviewer_id %q, got %q", "user_admin_demo", reviewerID)
	}
	if reviewComment != "approved via service" {
		t.Fatalf("expected review_comment %q, got %q", "approved via service", reviewComment)
	}
	if reviewedAt.IsZero() {
		t.Fatal("expected reviewed_at to be set")
	}

	var userID string
	var userName string
	var userRole string
	var userStatus string
	var userPasswordHash string
	if err := conn.QueryRow(ctx, `
		select id, name, role, status, password_hash
		from users
		where email = 'service-pending@example.com'
	`).Scan(&userID, &userName, &userRole, &userStatus, &userPasswordHash); err != nil {
		t.Fatalf("select approved user failed: %v", err)
	}
	if userID != "user_pending_existing" {
		t.Fatalf("expected existing user id %q, got %q", "user_pending_existing", userID)
	}
	if userName != "服务层待审批用户" {
		t.Fatalf("expected updated user name %q, got %q", "服务层待审批用户", userName)
	}
	if userRole != "member" {
		t.Fatalf("expected user role %q, got %q", "member", userRole)
	}
	if userStatus != "active" {
		t.Fatalf("expected user status %q, got %q", "active", userStatus)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(userPasswordHash), []byte("Example1234")); err != nil {
		t.Fatalf("expected approved user password hash to match Example1234: %v", err)
	}

	var membershipCount int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from tenant_memberships
		where tenant_id = 'tenant_demo'
			and user_id = 'user_pending_existing'
			and role = 'member'
			and status = 'active'
	`).Scan(&membershipCount); err != nil {
		t.Fatalf("select tenant membership failed: %v", err)
	}
	if membershipCount != 1 {
		t.Fatalf("expected 1 active membership, got %d", membershipCount)
	}

	var auditCount int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from audit_events
		where actor_type = 'admin'
			and actor_user_id = 'user_admin_demo'
			and tenant_id = 'tenant_demo'
			and event_type = 'application_approved'
			and target_type = 'account_application'
			and target_id = 'app_service_pending'
			and detail = 'approved via service'
	`).Scan(&auditCount); err != nil {
		t.Fatalf("select audit event count failed: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 approval audit event, got %d", auditCount)
	}

	var tokenLimit int64
	var costLimitMicroyuan int64
	var allowedModels []string
	if err := conn.QueryRow(ctx, `
		select token_limit, cost_limit_microyuan, allowed_models
		from tenant_quota_policies
		where tenant_id = 'tenant_demo'
	`).Scan(&tokenLimit, &costLimitMicroyuan, &allowedModels); err != nil {
		t.Fatalf("select tenant quota policy failed: %v", err)
	}
	if tokenLimit != 23456789 {
		t.Fatalf("expected token_limit %d, got %d", 23456789, tokenLimit)
	}
	if costLimitMicroyuan != 4567000000 {
		t.Fatalf("expected cost_limit_microyuan %d, got %d", int64(4567000000), costLimitMicroyuan)
	}
	if !slices.Equal(allowedModels, []string{"qwen-flash", "mimo-v2.5-pro"}) {
		t.Fatalf("expected allowed_models to be persisted, got %#v", allowedModels)
	}
}

func TestPostgresConsoleServiceApproveApplicationCreatesTenantWhenMissing(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("Example1234"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword failed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		insert into account_applications (
			id,
			email,
			email_normalized,
			name,
			company_name,
			use_case,
			password_hash,
			status,
			created_at
		) values (
			'app_create_tenant_pending',
			'create-tenant@example.com',
			'create-tenant@example.com',
			'新租户申请用户',
			'Create Tenant Co',
			'新租户接入',
			$1,
			'pending',
			timestamptz '2026-04-25T01:02:03Z'
		)
	`, string(passwordHash)); err != nil {
		t.Fatalf("seed create tenant application failed: %v", err)
	}

	result, err := console.ApproveApplication(ctx, "app_create_tenant_pending", service.ApproveApplicationRequest{
		ActorID:            "user_admin_demo",
		Comment:            "approved with new tenant",
		TenantID:           "tenant_create_tenant_co",
		TokenLimit:         8765432,
		CostLimitMicroyuan: 1234500000,
		AllowedModels:      []string{"qwen-flash"},
	})
	if err != nil {
		t.Fatalf("ApproveApplication failed: %v", err)
	}

	if result.Item.Status != "approved" {
		t.Fatalf("expected item status %q, got %q", "approved", result.Item.Status)
	}

	var tenantName string
	var tenantStatus string
	if err := conn.QueryRow(ctx, `
		select name, status
		from tenants
		where id = 'tenant_create_tenant_co'
	`).Scan(&tenantName, &tenantStatus); err != nil {
		t.Fatalf("select created tenant failed: %v", err)
	}
	if tenantName != "Create Tenant Co" {
		t.Fatalf("expected tenant name %q, got %q", "Create Tenant Co", tenantName)
	}
	if tenantStatus != "active" {
		t.Fatalf("expected tenant status %q, got %q", "active", tenantStatus)
	}

	var membershipCount int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from tenant_memberships
		where tenant_id = 'tenant_create_tenant_co'
			and role = 'member'
			and status = 'active'
	`).Scan(&membershipCount); err != nil {
		t.Fatalf("select created tenant membership failed: %v", err)
	}
	if membershipCount != 1 {
		t.Fatalf("expected 1 active membership for created tenant, got %d", membershipCount)
	}

	var requestLimit int64
	var tokenLimit int64
	var costLimitMicroyuan int64
	var allowedModels []string
	if err := conn.QueryRow(ctx, `
		select request_limit, token_limit, cost_limit_microyuan, allowed_models
		from tenant_quota_policies
		where tenant_id = 'tenant_create_tenant_co'
	`).Scan(&requestLimit, &tokenLimit, &costLimitMicroyuan, &allowedModels); err != nil {
		t.Fatalf("select created tenant quota policy failed: %v", err)
	}
	if requestLimit <= 0 {
		t.Fatalf("expected positive request_limit, got %d", requestLimit)
	}
	if tokenLimit != 8765432 {
		t.Fatalf("expected token_limit %d, got %d", 8765432, tokenLimit)
	}
	if costLimitMicroyuan != 1234500000 {
		t.Fatalf("expected cost_limit_microyuan %d, got %d", int64(1234500000), costLimitMicroyuan)
	}
	if !slices.Equal(allowedModels, []string{"qwen-flash"}) {
		t.Fatalf("expected created tenant allowed_models qwen-flash, got %#v", allowedModels)
	}
}

func TestPostgresConsoleServiceRejectApplicationUpdatesStatusAndAudit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("Example1234"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword failed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		insert into account_applications (
			id,
			email,
			email_normalized,
			name,
			company_name,
			use_case,
			password_hash,
			status,
			created_at
		) values (
			'app_service_reject',
			'reject@example.com',
			'reject@example.com',
			'待拒绝用户',
			'Reject Co',
			'测试接入',
			$1,
			'pending',
			timestamptz '2026-04-25T01:02:03Z'
		);
	`, string(passwordHash)); err != nil {
		t.Fatalf("seed reject application scenario failed: %v", err)
	}

	result, err := console.RejectApplication(ctx, "app_service_reject", service.RejectApplicationRequest{
		ActorID: "user_admin_demo",
		Comment: "rejected via service",
	})
	if err != nil {
		t.Fatalf("RejectApplication failed: %v", err)
	}

	if result.Item.Status != "rejected" {
		t.Fatalf("expected item status %q, got %q", "rejected", result.Item.Status)
	}

	var applicationStatus string
	var applicationPasswordHash *string
	var reviewerID string
	var reviewComment string
	var reviewedAt time.Time
	if err := conn.QueryRow(ctx, `
		select status, password_hash, reviewer_id, review_comment, reviewed_at
		from account_applications
		where id = 'app_service_reject'
	`).Scan(&applicationStatus, &applicationPasswordHash, &reviewerID, &reviewComment, &reviewedAt); err != nil {
		t.Fatalf("select rejected application failed: %v", err)
	}
	if applicationStatus != "rejected" {
		t.Fatalf("expected application status %q, got %q", "rejected", applicationStatus)
	}
	if applicationPasswordHash != nil {
		t.Fatal("expected rejected application password_hash to be cleared")
	}
	if reviewerID != "user_admin_demo" {
		t.Fatalf("expected reviewer_id %q, got %q", "user_admin_demo", reviewerID)
	}
	if reviewComment != "rejected via service" {
		t.Fatalf("expected review_comment %q, got %q", "rejected via service", reviewComment)
	}
	if reviewedAt.IsZero() {
		t.Fatal("expected reviewed_at to be set")
	}

	var auditCount int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from audit_events
		where actor_type = 'admin'
			and actor_user_id = 'user_admin_demo'
			and event_type = 'application_rejected'
			and target_type = 'account_application'
			and target_id = 'app_service_reject'
			and detail = 'rejected via service'
	`).Scan(&auditCount); err != nil {
		t.Fatalf("select rejection audit event count failed: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 rejection audit event, got %d", auditCount)
	}
}

func TestPostgresConsoleServiceApproveAccountDeletionApplicationDisablesUserMembershipAndKeys(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into platform_api_keys (
			id,
			tenant_id,
			name,
			key_hash,
			status,
			scopes,
			created_by_user_id,
			created_at,
			expires_at
		) values (
			'pak_delete_member_owned',
			'tenant_demo',
			'delete-member-owned',
			'sha256:delete-member-owned',
			'active',
			ARRAY['chat'],
			'user_member_a',
			now(),
			now() + interval '30 days'
		), (
			'pak_delete_other_member',
			'tenant_demo',
			'delete-other-member',
			'sha256:delete-other-member',
			'active',
			ARRAY['chat'],
			'user_member_b',
			now(),
			now() + interval '30 days'
		);

		insert into account_deletion_applications (
			id,
			user_id,
			tenant_id,
			reason,
			status,
			created_at
		) values (
			'ada_service_pending',
			'user_member_a',
			'tenant_demo',
			'不再使用',
			'pending',
			timestamptz '2026-05-06T01:02:03Z'
		);
	`); err != nil {
		t.Fatalf("seed account deletion application failed: %v", err)
	}

	result, err := console.ApproveAccountDeletionApplication(ctx, "ada_service_pending", service.ReviewAccountDeletionApplicationRequest{
		ActorID: "user_admin_demo",
		Comment: "同意注销",
	})
	if err != nil {
		t.Fatalf("ApproveAccountDeletionApplication failed: %v", err)
	}

	if result.Item.ID != "ada_service_pending" {
		t.Fatalf("expected item id %q, got %q", "ada_service_pending", result.Item.ID)
	}
	if result.Item.Status != "approved" {
		t.Fatalf("expected status %q, got %q", "approved", result.Item.Status)
	}
	if result.Item.DisabledAPIKeys != 1 {
		t.Fatalf("expected disabled_api_keys 1, got %d", result.Item.DisabledAPIKeys)
	}

	var userStatus string
	if err := conn.QueryRow(ctx, `select status from users where id = 'user_member_a';`).Scan(&userStatus); err != nil {
		t.Fatalf("QueryRow user status failed: %v", err)
	}
	if userStatus != "disabled" {
		t.Fatalf("expected user status disabled, got %q", userStatus)
	}

	var membershipStatus string
	if err := conn.QueryRow(ctx, `
		select status
		from tenant_memberships
		where tenant_id = 'tenant_demo'
		  and user_id = 'user_member_a';
	`).Scan(&membershipStatus); err != nil {
		t.Fatalf("QueryRow membership status failed: %v", err)
	}
	if membershipStatus != "disabled" {
		t.Fatalf("expected membership status disabled, got %q", membershipStatus)
	}

	statuses := map[string]string{}
	rows, err := conn.Query(ctx, `
		select id, status
		from platform_api_keys
		where id in ('pak_delete_member_owned', 'pak_delete_other_member');
	`)
	if err != nil {
		t.Fatalf("Query platform_api_keys failed: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var status string
		if err := rows.Scan(&id, &status); err != nil {
			t.Fatalf("Scan platform_api_keys failed: %v", err)
		}
		statuses[id] = status
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("platform_api_keys rows error: %v", err)
	}
	if statuses["pak_delete_member_owned"] != "disabled" {
		t.Fatalf("expected owned key disabled, got %q", statuses["pak_delete_member_owned"])
	}
	if statuses["pak_delete_other_member"] != "active" {
		t.Fatalf("expected other key active, got %q", statuses["pak_delete_other_member"])
	}
}

func TestPostgresConsoleServiceAuditUsesUsageLogsAndEvents(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		update llm_request_logs
		set
			request_started_at = now() - interval '30 minutes',
			request_completed_at = now() - interval '30 minutes' + interval '182 milliseconds',
			created_at = now() - interval '30 minutes',
			first_token_latency_ms = 41,
			task_class = 'coding_complex',
			routing_reason = 'keyword:debug,pattern:code_fence',
			target_model_tier = 'gateway-chat-reasoning',
			resolved_model = 'qwen-plus',
			input_cost_microyuan = 1200000,
			output_cost_microyuan = 1000000,
			cached_cost_microyuan = 300000,
			total_cost_microyuan = 2500000
		where id = 'llmreq_demo_001';

		update llm_request_events
		set created_at = now() - interval '30 minutes' + interval '182 milliseconds'
		where id = 'llmevt_demo_001';
	`); err != nil {
		t.Fatalf("refresh recent usage seed failed: %v", err)
	}

	payload, err := console.Audit(ctx)
	if err != nil {
		t.Fatalf("Audit failed: %v", err)
	}

	if len(payload.Metrics) != 4 {
		t.Fatalf("expected 4 metrics, got %d", len(payload.Metrics))
	}
	if len(payload.Events) == 0 {
		t.Fatal("expected audit events from llm_request_events")
	}
	if len(payload.Items) == 0 {
		t.Fatal("expected audit items from llm_request_logs")
	}
	if payload.Items[0].RequestModel == "" {
		t.Fatal("expected request_model to be populated")
	}
	if payload.Items[0].UsageSource == "" {
		t.Fatal("expected usage_source to be populated")
	}
	if payload.Items[0].FirstTokenLatencyMS != 41 {
		t.Fatalf("expected first_token_latency_ms 41, got %#v", payload.Items[0].FirstTokenLatencyMS)
	}
	if payload.Items[0].TotalCost != "2.50 ￥" {
		t.Fatalf("expected total_cost 2.50 ￥, got %q", payload.Items[0].TotalCost)
	}
	if payload.Items[0].TaskClass != "coding_complex" {
		t.Fatalf("expected task_class %q, got %q", "coding_complex", payload.Items[0].TaskClass)
	}
	if payload.Items[0].RoutingReason != "keyword:debug,pattern:code_fence" {
		t.Fatalf("expected routing_reason to be populated, got %q", payload.Items[0].RoutingReason)
	}
	if payload.Items[0].TargetModelTier != "gateway-chat-reasoning" {
		t.Fatalf("expected target_model_tier %q, got %q", "gateway-chat-reasoning", payload.Items[0].TargetModelTier)
	}
	if payload.Items[0].ResolvedModel != "qwen-plus" {
		t.Fatalf("expected resolved_model %q, got %q", "qwen-plus", payload.Items[0].ResolvedModel)
	}
}

func TestPostgresConsoleServiceAuditFallsBackToAuditLogsWhenUsageDataMissing(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `delete from llm_request_events; delete from llm_request_logs;`); err != nil {
		t.Fatalf("delete usage tables failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into audit_logs (
			tenant_id,
			platform_api_key_id,
			requested_model,
			endpoint,
			status_code,
			provider_display_name,
			latency_ms,
			created_at
		) values (
			'tenant_demo',
			'pak_demo',
			'qwen-flash',
			'/v1/chat/completions',
			200,
			'DashScope 主路由',
			88,
			now()
		);
	`); err != nil {
		t.Fatalf("insert fallback audit log failed: %v", err)
	}

	payload, err := console.Audit(ctx)
	if err != nil {
		t.Fatalf("Audit failed: %v", err)
	}

	if len(payload.Items) == 0 {
		t.Fatal("expected fallback audit items")
	}
	if payload.Items[0].RequestModel != "-" {
		t.Fatalf("expected fallback request_model '-', got %q", payload.Items[0].RequestModel)
	}
	if payload.Items[0].UsageSource != "审计回退" {
		t.Fatalf("expected fallback usage_source 审计回退, got %q", payload.Items[0].UsageSource)
	}
	if payload.Items[0].TotalCost != "--" {
		t.Fatalf("expected fallback total_cost --, got %q", payload.Items[0].TotalCost)
	}
}

func TestPostgresConsoleServiceUsageOverview(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		update llm_request_logs
		set
			input_cost_microyuan = case
				when id = 'llmreq_demo_001' then 3200000
				when id = 'llmreq_demo_002' then 600000
				else input_cost_microyuan
			end,
			output_cost_microyuan = case
				when id = 'llmreq_demo_001' then 1400000
				when id = 'llmreq_demo_002' then 0
				else output_cost_microyuan
			end,
			cached_cost_microyuan = case
				when id = 'llmreq_demo_001' then 300000
				when id = 'llmreq_demo_002' then 100000
				else cached_cost_microyuan
			end,
			total_cost_microyuan = case
				when id = 'llmreq_demo_001' then 4900000
				when id = 'llmreq_demo_002' then 700000
				else total_cost_microyuan
			end
		where id in ('llmreq_demo_001', 'llmreq_demo_002');
	`); err != nil {
		t.Fatalf("seed usage overview pricing failed: %v", err)
	}

	payload, err := console.UsageOverview(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T09:00:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageOverview failed: %v", err)
	}

	if payload.TotalRequests != 2 {
		t.Fatalf("expected total_requests 2, got %d", payload.TotalRequests)
	}
	if payload.SuccessRate != "50.00%" {
		t.Fatalf("expected success_rate 50.00%%, got %q", payload.SuccessRate)
	}
	if payload.TotalTokens != "188" {
		t.Fatalf("expected total_tokens 188, got %q", payload.TotalTokens)
	}
	if payload.InputTokens != "140" {
		t.Fatalf("expected input_tokens 140, got %q", payload.InputTokens)
	}
	if payload.OutputTokens != "48" {
		t.Fatalf("expected output_tokens 48, got %q", payload.OutputTokens)
	}
	if payload.CachedTokens != "24" {
		t.Fatalf("expected cached_tokens 24, got %q", payload.CachedTokens)
	}
	if payload.AverageLatency != "139 ms" {
		t.Fatalf("expected average_latency 139 ms, got %q", payload.AverageLatency)
	}
	if payload.InputCost != "3.80 ￥" {
		t.Fatalf("expected input_cost 3.80 ￥, got %q", payload.InputCost)
	}
	if payload.OutputCost != "1.40 ￥" {
		t.Fatalf("expected output_cost 1.40 ￥, got %q", payload.OutputCost)
	}
	if payload.CachedCost != "0.40 ￥" {
		t.Fatalf("expected cached_cost 0.40 ￥, got %q", payload.CachedCost)
	}
	if payload.TotalCost != "5.60 ￥" {
		t.Fatalf("expected total_cost 5.60 ￥, got %q", payload.TotalCost)
	}
	if payload.EstimatedShare != "50.00%" {
		t.Fatalf("expected estimated_share 50.00%%, got %q", payload.EstimatedShare)
	}
	if !containsPricingModel(payload.PricingModels, "gpt-4o-mini", "2.50 ￥/M", "5.00 ￥/M", "0.50 ￥/M") {
		t.Fatalf("expected pricing_models to include gpt-4o-mini pricing, got %#v", payload.PricingModels)
	}
	if !containsPricingModel(payload.PricingModels, "text-embedding-3-small", "1.25 ￥/M", "0.00 ￥/M", "0.00 ￥/M") {
		t.Fatalf("expected pricing_models to include demo models, got %#v", payload.PricingModels)
	}

	var logCount int
	if err := conn.QueryRow(ctx, `select count(*) from llm_request_logs`).Scan(&logCount); err != nil {
		t.Fatalf("QueryRow llm_request_logs failed: %v", err)
	}
	if logCount < 2 {
		t.Fatalf("expected demo data to include llm_request_logs, got %d rows", logCount)
	}
}

func TestPostgresConsoleServiceUsageOverviewIncludesDistinctPricingVariantsPerModel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			cached_tokens,
			input_price_microyuan_per_million,
			output_price_microyuan_per_million,
			cached_price_microyuan_per_million,
			input_cost_microyuan,
			output_cost_microyuan,
			cached_cost_microyuan,
			total_cost_microyuan,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_pricing_variant',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'success',
			200,
			120,
			20,
			10,
			30,
			4,
			3500000,
			6500000,
			800000,
			56,
			65,
			3,
			124,
			'',
			'',
			timestamptz '2026-04-24T10:20:00Z',
			timestamptz '2026-04-24T10:20:00.120Z',
			timestamptz '2026-04-24T10:20:01Z'
		);
	`); err != nil {
		t.Fatalf("insert pricing variant request log failed: %v", err)
	}

	payload, err := console.UsageOverview(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T09:00:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageOverview failed: %v", err)
	}

	if !containsPricingModel(payload.PricingModels, "gpt-4o-mini", "2.50 ￥/M", "5.00 ￥/M", "0.50 ￥/M") {
		t.Fatalf("expected pricing_models to include base gpt-4o-mini pricing, got %#v", payload.PricingModels)
	}
	if !containsPricingModel(payload.PricingModels, "gpt-4o-mini", "3.50 ￥/M", "6.50 ￥/M", "0.80 ￥/M") {
		t.Fatalf("expected pricing_models to include variant gpt-4o-mini pricing, got %#v", payload.PricingModels)
	}
	if !containsPricingModel(payload.PricingModels, "text-embedding-3-small", "1.25 ￥/M", "0.00 ￥/M", "0.00 ￥/M") {
		t.Fatalf("expected pricing_models to retain embedding pricing, got %#v", payload.PricingModels)
	}
	if len(payload.PricingModels) != 3 {
		t.Fatalf("expected 3 pricing model variants, got %d (%#v)", len(payload.PricingModels), payload.PricingModels)
	}
}

func TestPostgresConsoleServiceUsageOverviewIncludesConfiguredMIMOPricingWithoutUsage(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		delete from llm_request_events;
		delete from llm_usage_agg_hourly;
		delete from llm_request_logs;
		delete from route_catalog;
		delete from provider_credentials;

		insert into provider_credentials (id, provider, display_name, supported_models, base_url, encrypted_secret, status) values
			('provider_dashscope_primary', 'dashscope', 'Qwen', '{"qwen-flash"}', 'https://dashscope.aliyuncs.com/compatible-mode/v1', '', 'active'),
			('provider_mimo_primary', 'mimo', 'MIMO', '{"mimo-v2.5-pro"}', 'https://api.xiaomimimo.com/v1', '', 'active');

		insert into route_catalog (id, requested_model, resolved_provider, provider_credential_id, endpoint, latency_ms, health_status, request_mode, updated_at) values
			('route:provider_dashscope_primary:default', 'qwen-flash', 'Qwen', 'provider_dashscope_primary', '/v1/chat/completions', 218, 'healthy', '聊天', now()),
			('route:provider_mimo_primary:default', 'mimo-v2.5-pro', 'MIMO', 'provider_mimo_primary', '/v1/chat/completions', 286, 'warning', '聊天', now());

		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			cached_tokens,
			input_price_microyuan_per_million,
			output_price_microyuan_per_million,
			cached_price_microyuan_per_million,
			input_cost_microyuan,
			output_cost_microyuan,
			cached_cost_microyuan,
			total_cost_microyuan,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_pricing_qwen_only',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_dashscope_primary',
			'route:provider_dashscope_primary:default',
			'/v1/chat/completions',
			'qwen-flash',
			'qwen-flash',
			'upstream',
			'success',
			200,
			182,
			12,
			6,
			18,
			0,
			2000000,
			20000000,
			500000,
			24,
			120,
			0,
			144,
			'',
			'',
			now() - interval '20 minutes',
			now() - interval '20 minutes' + interval '182 milliseconds',
			now() - interval '20 minutes'
		);
	`); err != nil {
		t.Fatalf("seed configured pricing fallback failed: %v", err)
	}

	payload, err := console.UsageOverview(ctx, service.UsageQuery{Window: "24h"})
	if err != nil {
		t.Fatalf("UsageOverview failed: %v", err)
	}

	if !containsPricingModel(payload.PricingModels, "qwen-flash", "2.00 ￥/M", "20.00 ￥/M", "0.50 ￥/M") {
		t.Fatalf("expected pricing_models to include qwen-flash pricing, got %#v", payload.PricingModels)
	}
	if !containsPricingModel(payload.PricingModels, "mimo-v2.5-pro", "2.00 ￥/M", "20.00 ￥/M", "0.50 ￥/M") {
		t.Fatalf("expected pricing_models to include mimo-v2.5-pro fallback pricing, got %#v", payload.PricingModels)
	}
}

func TestPostgresConsoleServiceUsageLatencyWall(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		update llm_request_logs
		set
			request_model = 'gateway-public',
			resolved_model = 'qwen-flash',
			request_started_at = now() - interval '30 minutes',
			request_completed_at = now() - interval '30 minutes' + interval '182 milliseconds',
			created_at = now() - interval '30 minutes'
		where id = 'llmreq_demo_001';

		update llm_request_logs
		set
			request_model = 'gateway-public',
			resolved_model = 'mimo-v2.5-pro',
			request_started_at = now() - interval '20 minutes',
			request_completed_at = now() - interval '20 minutes' + interval '95 milliseconds',
			created_at = now() - interval '20 minutes'
		where id = 'llmreq_demo_002';
	`); err != nil {
		t.Fatalf("refresh latency wall seed failed: %v", err)
	}

	payload, err := console.UsageLatencyWall(ctx, service.UsageQuery{Window: "24h"})
	if err != nil {
		t.Fatalf("UsageLatencyWall failed: %v", err)
	}

	if payload.WindowLabel != "最近 24 小时" {
		t.Fatalf("expected 最近 24 小时, got %q", payload.WindowLabel)
	}
	if len(payload.Buckets) == 0 {
		t.Fatal("expected latency wall buckets")
	}
	if len(payload.Lanes) == 0 {
		t.Fatal("expected latency wall lanes")
	}
	if payload.Lanes[0].Model == "" {
		t.Fatal("expected lane model to be populated")
	}
	if len(payload.Lanes[0].Cells) != len(payload.Buckets) {
		t.Fatalf("expected cells to align with buckets, got %d cells and %d buckets", len(payload.Lanes[0].Cells), len(payload.Buckets))
	}
	models := make([]string, 0, len(payload.Lanes))
	for _, lane := range payload.Lanes {
		models = append(models, lane.Model)
	}
	if !slices.Contains(models, "gateway-public -> qwen-flash") {
		t.Fatalf("expected gateway-public -> qwen-flash lane, got %v", models)
	}
	if !slices.Contains(models, "gateway-public -> mimo-v2.5-pro") {
		t.Fatalf("expected gateway-public -> mimo-v2.5-pro lane, got %v", models)
	}
}

func TestPostgresConsoleServiceUsageLatencyWallIncludesConfiguredChatRoutesWithoutUsage(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		delete from llm_request_events;
		delete from llm_usage_agg_hourly;
		delete from llm_request_logs;
		delete from route_catalog;
		delete from provider_credentials;

		insert into provider_credentials (id, provider, display_name, supported_models, base_url, encrypted_secret, status) values
			('provider_dashscope_primary', 'dashscope', 'Qwen', '{"qwen-flash"}', 'https://dashscope.aliyuncs.com/compatible-mode/v1', '', 'active'),
			('provider_mimo_primary', 'mimo', 'MIMO', '{"mimo-v2.5-pro"}', 'https://api.xiaomimimo.com/v1', '', 'active');

		insert into route_catalog (id, requested_model, resolved_provider, provider_credential_id, endpoint, latency_ms, health_status, request_mode, updated_at) values
			('route:provider_dashscope_primary:default', 'qwen-flash', 'Qwen', 'provider_dashscope_primary', '/v1/chat/completions', 218, 'healthy', '聊天', now()),
			('route:provider_mimo_primary:default', 'mimo-v2.5-pro', 'MIMO', 'provider_mimo_primary', '/v1/chat/completions', 286, 'warning', '聊天', now());

		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			cached_tokens,
			input_price_microyuan_per_million,
			output_price_microyuan_per_million,
			cached_price_microyuan_per_million,
			input_cost_microyuan,
			output_cost_microyuan,
			cached_cost_microyuan,
			total_cost_microyuan,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_usage_qwen_only',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_dashscope_primary',
			'route:provider_dashscope_primary:default',
			'/v1/chat/completions',
			'qwen-flash',
			'qwen-flash',
			'upstream',
			'success',
			200,
			182,
			12,
			6,
			18,
			0,
			2000000,
			20000000,
			500000,
			24,
			120,
			0,
			144,
			'',
			'',
			now() - interval '20 minutes',
			now() - interval '20 minutes' + interval '182 milliseconds',
			now() - interval '20 minutes'
		);
	`); err != nil {
		t.Fatalf("seed fallback usage latency wall failed: %v", err)
	}

	payload, err := console.UsageLatencyWall(ctx, service.UsageQuery{Window: "24h"})
	if err != nil {
		t.Fatalf("UsageLatencyWall failed: %v", err)
	}

	models := make([]string, 0, len(payload.Lanes))
	var mimoLane service.UsageLatencyLane
	for _, lane := range payload.Lanes {
		models = append(models, lane.Model)
		if lane.Model == "mimo-v2.5-pro" {
			mimoLane = lane
		}
	}

	if !slices.Contains(models, "qwen-flash") {
		t.Fatalf("expected qwen-flash lane, got %v", models)
	}
	if !slices.Contains(models, "mimo-v2.5-pro") {
		t.Fatalf("expected mimo-v2.5-pro fallback lane, got %v", models)
	}
	if mimoLane.Model == "" {
		t.Fatalf("expected mimo fallback lane to be populated, got %+v", payload.Lanes)
	}
	if len(mimoLane.Cells) == 0 {
		t.Fatalf("expected mimo fallback lane to contain empty buckets, got %+v", mimoLane)
	}
	if mimoLane.Cells[0].Status != "空窗" {
		t.Fatalf("expected mimo fallback lane status 空窗, got %+v", mimoLane.Cells[0])
	}
}

func TestPostgresConsoleServiceUsageLatencyWallIncludesHealthcheckLanes(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		delete from model_healthcheck_history;
		delete from llm_request_events;
		delete from llm_usage_agg_hourly;
		delete from llm_request_logs;
		delete from route_catalog;
		delete from provider_credentials;

		insert into provider_credentials (id, provider, display_name, supported_models, base_url, encrypted_secret, status) values
			('provider_dashscope_primary', 'dashscope', 'QWEN', '{"qwen-flash","qwen2.5-1.5b-instruct"}', 'https://dashscope.aliyuncs.com/compatible-mode/v1', '', 'active'),
			('provider_mimo_primary', 'mimo', 'MIMO', '{"mimo-v2.5-pro"}', 'https://api.xiaomimimo.com/v1', '', 'active');

		insert into route_catalog (id, requested_model, resolved_provider, provider_credential_id, endpoint, latency_ms, health_status, request_mode, updated_at, healthcheck_enabled) values
			('route:provider_dashscope_primary:default', 'qwen-flash', 'QWEN', 'provider_dashscope_primary', '/v1/chat/completions', 218, 'healthy', '聊天', now(), true),
			('route:provider_mimo_primary:default', 'mimo-v2.5-pro', 'MIMO', 'provider_mimo_primary', '/v1/chat/completions', 286, 'degraded', '推理', now(), true),
			('route:provider_dashscope_primary:qwen2_5_1_5b', 'qwen2.5-1.5b-instruct', 'QWEN', 'provider_dashscope_primary', '/v1/chat/completions', 305, 'degraded', '聊天', now(), true);

		insert into model_healthcheck_history (
			id,
			route_id,
			requested_model,
			provider_credential_id,
			route_label,
			health_status,
			last_health_error,
			request_mode,
			latency_ms,
			first_token_latency_ms,
			checked_at
		) values
			('mhh_qwen_ok', 'route:provider_dashscope_primary:default', 'qwen-flash', 'provider_dashscope_primary', 'QWEN', 'healthy', '', '聊天', 201, 88, now() - interval '30 minutes'),
			('mhh_mimo_bad', 'route:provider_mimo_primary:default', 'mimo-v2.5-pro', 'provider_mimo_primary', 'MIMO', 'degraded', 'no non-empty content token received', '推理', 4647, 0, now() - interval '20 minutes'),
			('mhh_qwen_small_bad', 'route:provider_dashscope_primary:qwen2_5_1_5b', 'qwen2.5-1.5b-instruct', 'provider_dashscope_primary', 'QWEN', 'degraded', 'access_denied', '聊天', 311, 0, now() - interval '10 minutes');
	`); err != nil {
		t.Fatalf("seed usage latency wall healthchecks failed: %v", err)
	}

	payload, err := console.UsageLatencyWall(ctx, service.UsageQuery{Window: "24h"})
	if err != nil {
		t.Fatalf("UsageLatencyWall failed: %v", err)
	}

	var mimoLane service.UsageLatencyLane
	var qwenSmallLane service.UsageLatencyLane
	for _, lane := range payload.Lanes {
		if lane.Model == "mimo-v2.5-pro" && lane.Source == "健康检查" {
			mimoLane = lane
		}
		if lane.Model == "qwen2.5-1.5b-instruct" && lane.Source == "健康检查" {
			qwenSmallLane = lane
		}
	}

	if mimoLane.Model == "" {
		t.Fatalf("expected healthcheck lane for mimo-v2.5-pro, got %+v", payload.Lanes)
	}
	if mimoLane.Provider != "MIMO" {
		t.Fatalf("expected mimo provider MIMO, got %+v", mimoLane)
	}
	if mimoLane.Cells[0].Status != "空窗" && !containsUsageLatencyCellStatus(mimoLane.Cells, "降级") {
		t.Fatalf("expected mimo lane to include degraded bucket, got %+v", mimoLane.Cells)
	}

	if qwenSmallLane.Model == "" {
		t.Fatalf("expected healthcheck lane for qwen2.5-1.5b-instruct, got %+v", payload.Lanes)
	}
	if qwenSmallLane.Provider != "QWEN" {
		t.Fatalf("expected qwen2.5 provider QWEN, got %+v", qwenSmallLane)
	}
	if !containsUsageLatencyCellStatus(qwenSmallLane.Cells, "降级") {
		t.Fatalf("expected qwen2.5 lane to include degraded bucket, got %+v", qwenSmallLane.Cells)
	}
}

func TestPostgresConsoleServiceUsageLatencyWallDeduplicatesModelLanes(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		delete from model_healthcheck_history;
		delete from llm_request_events;
		delete from llm_usage_agg_hourly;
		delete from llm_request_logs;
		delete from route_catalog;
		delete from provider_credentials;

		insert into provider_credentials (id, provider, display_name, supported_models, base_url, encrypted_secret, status) values
			('provider_dashscope_primary', 'dashscope', 'QWEN', '{"qwen-flash"}', 'https://dashscope.aliyuncs.com/compatible-mode/v1', '', 'active');

		insert into route_catalog (id, requested_model, resolved_provider, provider_credential_id, endpoint, latency_ms, health_status, request_mode, updated_at, healthcheck_enabled) values
			('route:provider_dashscope_primary:default', 'qwen-flash', 'QWEN', 'provider_dashscope_primary', '/v1/chat/completions', 218, 'healthy', '聊天', now(), true);

		insert into llm_request_logs (
			id, tenant_id, platform_api_key_id, platform_api_key_name, provider_credential_id, route_id,
			request_path, request_model, upstream_model, usage_source, usage_status, status_code, latency_ms,
			prompt_tokens, completion_tokens, total_tokens, cached_tokens,
			input_price_microyuan_per_million, output_price_microyuan_per_million, cached_price_microyuan_per_million,
			input_cost_microyuan, output_cost_microyuan, cached_cost_microyuan, total_cost_microyuan,
			error_code, error_message, request_started_at, request_completed_at, created_at
		) values (
			'llmreq_usage_qwen_dedupe',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_dashscope_primary',
			'route:provider_dashscope_primary:default',
			'/v1/chat/completions',
			'qwen-flash',
			'qwen-flash',
			'upstream',
			'success',
			200,
			182,
			12,
			6,
			18,
			0,
			2000000,
			20000000,
			500000,
			24,
			120,
			0,
			144,
			'',
			'',
			now() - interval '20 minutes',
			now() - interval '20 minutes' + interval '182 milliseconds',
			now() - interval '20 minutes'
		);

		insert into model_healthcheck_history (
			id, route_id, requested_model, provider_credential_id, route_label, health_status, last_health_error,
			request_mode, latency_ms, first_token_latency_ms, checked_at
		) values (
			'mhh_qwen_dedupe',
			'route:provider_dashscope_primary:default',
			'qwen-flash',
			'provider_dashscope_primary',
			'QWEN',
			'healthy',
			'',
			'聊天',
			201,
			88,
			now() - interval '10 minutes'
		);
	`); err != nil {
		t.Fatalf("seed dedupe usage latency wall failed: %v", err)
	}

	payload, err := console.UsageLatencyWall(ctx, service.UsageQuery{Window: "24h"})
	if err != nil {
		t.Fatalf("UsageLatencyWall failed: %v", err)
	}

	count := 0
	var lane service.UsageLatencyLane
	for _, item := range payload.Lanes {
		if item.Model == "qwen-flash" {
			count++
			lane = item
		}
	}
	if count != 1 {
		t.Fatalf("expected qwen-flash to appear once, got %d lanes: %+v", count, payload.Lanes)
	}
	if lane.Source != "真实调用" {
		t.Fatalf("expected deduped lane to prefer real usage source, got %+v", lane)
	}
	if !containsUsageLatencyCellLatency(lane.Cells, "182 ms") {
		t.Fatalf("expected deduped lane to keep real usage latency bucket, got %+v", lane.Cells)
	}
}

func TestPostgresConsoleServiceUsageOverviewFiltersByErrorCategory(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	payload, err := console.UsageOverview(ctx, service.UsageQuery{
		From:          mustParseUsageTime(t, "2026-04-24T09:00:00Z"),
		To:            mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
		ErrorCategory: "rate_limit",
	})
	if err != nil {
		t.Fatalf("UsageOverview failed: %v", err)
	}

	if payload.TotalRequests != 1 {
		t.Fatalf("expected total_requests 1, got %d", payload.TotalRequests)
	}
	if payload.SuccessRate != "0.00%" {
		t.Fatalf("expected success_rate 0.00%%, got %q", payload.SuccessRate)
	}
	if payload.TotalTokens != "16" {
		t.Fatalf("expected total_tokens 16, got %q", payload.TotalTokens)
	}
	if payload.AverageLatency != "95 ms" {
		t.Fatalf("expected average_latency 95 ms, got %q", payload.AverageLatency)
	}
	if payload.EstimatedShare != "100.00%" {
		t.Fatalf("expected estimated_share 100.00%%, got %q", payload.EstimatedShare)
	}
}

func containsUsageLatencyCellStatus(cells []service.UsageLatencyCell, target string) bool {
	for _, cell := range cells {
		if cell.Status == target {
			return true
		}
	}
	return false
}

func containsUsageLatencyCellLatency(cells []service.UsageLatencyCell, target string) bool {
	for _, cell := range cells {
		if cell.Latency == target {
			return true
		}
	}
	return false
}

func TestPostgresConsoleServiceUsageOverviewHonorsPartialHourWindow(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	payload, err := console.UsageOverview(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T10:05:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T10:30:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageOverview failed: %v", err)
	}

	if payload.TotalRequests != 1 {
		t.Fatalf("expected total_requests 1, got %d", payload.TotalRequests)
	}
	if payload.TotalTokens != "16" {
		t.Fatalf("expected total_tokens 16, got %q", payload.TotalTokens)
	}
	if payload.AverageLatency != "95 ms" {
		t.Fatalf("expected average_latency 95 ms, got %q", payload.AverageLatency)
	}
}

func TestPostgresConsoleServiceUsageOverviewUsesRequestStartedAtWindow(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_delayed',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'success',
			200,
			120,
			18,
			6,
			24,
			'',
			'',
			timestamptz '2026-04-24T10:15:00Z',
			timestamptz '2026-04-24T10:15:00.120Z',
			timestamptz '2026-04-24T12:15:00Z'
		)
	`); err != nil {
		t.Fatalf("insert delayed llm_request_logs failed: %v", err)
	}

	payload, err := console.UsageOverview(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T10:10:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T10:20:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageOverview failed: %v", err)
	}

	if payload.TotalRequests != 1 {
		t.Fatalf("expected total_requests 1, got %d", payload.TotalRequests)
	}
	if payload.TotalTokens != "24" {
		t.Fatalf("expected total_tokens 24, got %q", payload.TotalTokens)
	}
}

func TestPostgresConsoleServiceUsageTrends(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		update llm_request_logs
		set total_cost_microyuan = case
			when id = 'llmreq_demo_001' then 4900000
			when id = 'llmreq_demo_002' then 700000
			else total_cost_microyuan
		end
		where id in ('llmreq_demo_001', 'llmreq_demo_002');

		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			cached_tokens,
			input_price_microyuan_per_million,
			output_price_microyuan_per_million,
			cached_price_microyuan_per_million,
			input_cost_microyuan,
			output_cost_microyuan,
			cached_cost_microyuan,
			total_cost_microyuan,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_011',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'success',
			200,
			205,
			90,
			30,
			120,
			20,
			2500000,
			5000000,
			500000,
			600000,
			500000,
			100000,
			1200000,
			'',
			'',
			timestamptz '2026-04-24T11:00:00Z',
			timestamptz '2026-04-24T11:00:00.205Z',
			timestamptz '2026-04-24T11:00:01Z'
		)
	`); err != nil {
		t.Fatalf("insert llm_request_logs failed: %v", err)
	}

	payload, err := console.UsageTrends(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T09:00:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T12:00:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageTrends failed: %v", err)
	}

	if len(payload.Requests) != 2 {
		t.Fatalf("expected 2 request trend points, got %d", len(payload.Requests))
	}
	if payload.Requests[0].Value != "2" {
		t.Fatalf("expected first request trend value 2, got %q", payload.Requests[0].Value)
	}
	if payload.Tokens[0].Value != "188" {
		t.Fatalf("expected first token trend value 188, got %q", payload.Tokens[0].Value)
	}
	if payload.Success[0].Value != "50.00%" {
		t.Fatalf("expected first success trend value 50.00%%, got %q", payload.Success[0].Value)
	}
	if payload.Costs[0].Value != "5.60 ￥" {
		t.Fatalf("expected first cost trend value 5.60 ￥, got %q", payload.Costs[0].Value)
	}
	if payload.Requests[1].Value != "1" {
		t.Fatalf("expected second request trend value 1, got %q", payload.Requests[1].Value)
	}
	if payload.Tokens[1].Value != "120" {
		t.Fatalf("expected second token trend value 120, got %q", payload.Tokens[1].Value)
	}
	if payload.Success[1].Value != "100.00%" {
		t.Fatalf("expected second success trend value 100.00%%, got %q", payload.Success[1].Value)
	}
	if payload.Costs[1].Value != "1.20 ￥" {
		t.Fatalf("expected second cost trend value 1.20 ￥, got %q", payload.Costs[1].Value)
	}
}

func TestPostgresConsoleServiceUsageTrendsFiltersByErrorCategory(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	payload, err := console.UsageTrends(ctx, service.UsageQuery{
		From:          mustParseUsageTime(t, "2026-04-24T09:00:00Z"),
		To:            mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
		ErrorCategory: "rate_limit",
	})
	if err != nil {
		t.Fatalf("UsageTrends failed: %v", err)
	}

	if len(payload.Requests) != 1 {
		t.Fatalf("expected 1 request trend point, got %d", len(payload.Requests))
	}
	if payload.Requests[0].Value != "1" {
		t.Fatalf("expected request trend value 1, got %q", payload.Requests[0].Value)
	}
	if payload.Tokens[0].Value != "16" {
		t.Fatalf("expected token trend value 16, got %q", payload.Tokens[0].Value)
	}
	if payload.Success[0].Value != "0.00%" {
		t.Fatalf("expected success trend value 0.00%%, got %q", payload.Success[0].Value)
	}
}

func TestPostgresConsoleServiceUsageTrendsHonorsPartialHourWindow(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	payload, err := console.UsageTrends(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T10:05:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T10:30:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageTrends failed: %v", err)
	}

	if len(payload.Requests) != 1 {
		t.Fatalf("expected 1 request trend point, got %d", len(payload.Requests))
	}
	if payload.Requests[0].Value != "1" {
		t.Fatalf("expected request trend value 1, got %q", payload.Requests[0].Value)
	}
	if payload.Tokens[0].Value != "16" {
		t.Fatalf("expected token trend value 16, got %q", payload.Tokens[0].Value)
	}
	if payload.Success[0].Value != "0.00%" {
		t.Fatalf("expected success trend value 0.00%%, got %q", payload.Success[0].Value)
	}
}

func TestPostgresConsoleServiceUsageFailures(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_003',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'upstream_error',
			502,
			410,
			32,
			8,
			40,
			'bad_gateway',
			'upstream returned 502',
			timestamptz '2026-04-24T10:30:00Z',
			timestamptz '2026-04-24T10:30:00.410Z',
			timestamptz '2026-04-24T10:30:01Z'
		)
	`); err != nil {
		t.Fatalf("insert llm_request_logs failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into llm_request_events (
			id,
			request_log_id,
			tenant_id,
			event_type,
			usage_source,
			usage_status,
			status_code,
			detail,
			created_at
		) values (
			'llmevt_demo_003',
			'llmreq_demo_003',
			'tenant_demo',
			'request_failed',
			'upstream',
			'upstream_error',
			502,
			'demo upstream 502',
			timestamptz '2026-04-24T10:30:00.410Z'
		)
	`); err != nil {
		t.Fatalf("insert llm_request_events failed: %v", err)
	}

	payload, err := console.UsageFailures(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T09:00:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageFailures failed: %v", err)
	}

	if len(payload.Breakdown) < 2 {
		t.Fatalf("expected at least 2 failure buckets, got %d", len(payload.Breakdown))
	}
	if !containsFailureBucket(payload.Breakdown, "上游服务异常", "1 次") {
		t.Fatalf("expected breakdown to contain 上游服务异常=1 次, got %#v", payload.Breakdown)
	}
	if len(payload.RecentEvents) == 0 {
		t.Fatal("expected recent_events to be returned")
	}
	if payload.RecentEvents[0] == "" {
		t.Fatal("expected recent event summary to be non-empty")
	}
	if !strings.Contains(payload.RecentEvents[0], "上游服务异常") {
		t.Fatalf("expected recent event summary to use failure-category label, got %q", payload.RecentEvents[0])
	}
	if len(payload.RecentEventItems) == 0 {
		t.Fatal("expected recent_event_items to be returned")
	}
	if payload.RecentEventItems[0].TenantID != "tenant_demo" {
		t.Fatalf("expected recent_event_items tenant_id tenant_demo, got %q", payload.RecentEventItems[0].TenantID)
	}
	if payload.RecentEventItems[0].TenantName == "" {
		t.Fatal("expected recent_event_items tenant_name to be populated")
	}
	if payload.RecentEventItems[0].ResolvedModel != "gpt-4o-mini" {
		t.Fatalf("expected recent_event_items resolved_model gpt-4o-mini, got %q", payload.RecentEventItems[0].ResolvedModel)
	}
	if payload.RecentEventItems[0].Category != "上游服务异常" {
		t.Fatalf("expected recent_event_items category 上游服务异常, got %q", payload.RecentEventItems[0].Category)
	}
	if payload.RecentEventItems[0].Reason == "" {
		t.Fatal("expected recent_event_items reason to be populated")
	}
}

func TestPostgresConsoleServiceUsageFailuresIncludesUsagePublishFailedEvents(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_publish_fail',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'success',
			200,
			188,
			60,
			20,
			80,
			'',
			'',
			timestamptz '2026-04-24T10:20:00Z',
			timestamptz '2026-04-24T10:20:00.188Z',
			timestamptz '2026-04-24T10:20:01Z'
		)
	`); err != nil {
		t.Fatalf("insert publish-fail llm_request_logs failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into llm_request_events (
			id,
			request_log_id,
			tenant_id,
			event_type,
			usage_source,
			usage_status,
			status_code,
			detail,
			created_at
		) values (
			'llmevt_demo_publish_fail',
			'llmreq_demo_publish_fail',
			'tenant_demo',
			'usage_publish_failed',
			'upstream',
			'success',
			200,
			'demo publish retry exhausted',
			timestamptz '2026-04-24T10:20:00.188Z'
		)
	`); err != nil {
		t.Fatalf("insert publish-fail llm_request_events failed: %v", err)
	}

	payload, err := console.UsageFailures(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T10:00:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageFailures failed: %v", err)
	}

	if !containsRecentEvent(payload.RecentEvents, "计量事件投递失败") {
		t.Fatalf("expected recent_events to include usage publish failure, got %#v", payload.RecentEvents)
	}
	if !containsRecentEvent(payload.RecentEvents, "网关内部错误 · 用户调用 gpt-4o-mini 已完成，但计量事件投递失败") {
		t.Fatalf("expected recent_events to categorize usage publish failure with meaningful detail, got %#v", payload.RecentEvents)
	}
}

func TestPostgresConsoleServiceUsageFailuresIncludesSecurityGuardEvents(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id, tenant_id, platform_api_key_id, platform_api_key_name, provider_credential_id, route_id,
			request_path, request_model, upstream_model, usage_source, usage_status, status_code,
			latency_ms, first_token_latency_ms, prompt_tokens, completion_tokens, total_tokens, cached_tokens,
			input_price_microyuan_per_million, output_price_microyuan_per_million, cached_price_microyuan_per_million,
			input_cost_microyuan, output_cost_microyuan, cached_cost_microyuan, total_cost_microyuan,
			error_code, error_message, request_started_at, request_completed_at, task_class, routing_reason,
			target_model_tier, resolved_model, prompt_excerpt, response_excerpt
		) values (
			'req_guard_block', 'tenant_demo', 'pak_demo', 'demo key', 'provider_openai_demo', 'route:provider_openai_demo:default',
			'/v1/chat/completions', 'qwen-flash', 'qwen-flash', 'estimated', 'failed', 400,
			21, 0, 15, 0, 15, 0,
			0, 0, 0,
			0, 0, 0, 0,
			'', '请求被安全策略拦截：包含明显 SQL 注入攻击意图', now() - interval '10 minutes', now() - interval '10 minutes', '', '',
			'', 'qwen-flash', 'SELECT * FROM users WHERE name = '' OR 1=1 --', '请求被安全策略拦截：包含明显 SQL 注入攻击意图'
		), (
			'req_guard_fallback', 'tenant_demo', 'pak_demo', 'demo key', 'provider_openai_demo', 'route:provider_openai_demo:default',
			'/v1/chat/completions', 'qwen-flash', 'qwen-flash', 'estimated', 'success', 200,
			98, 0, 20, 10, 30, 0,
			0, 0, 0,
			0, 0, 0, 0,
			'', '', now() - interval '5 minutes', now() - interval '5 minutes', '', '',
			'', 'qwen-flash', '我的手机号是138XXXX5678', 'ok'
		);

		insert into llm_request_failures (
			id, request_log_id, tenant_id, user_id, platform_api_key_id, failure_stage, error_category,
			status_code, retryable, user_message, internal_message_digest, created_at
		) values (
			'failure_guard_block', 'req_guard_block', 'tenant_demo', null, 'pak_demo', 'request', 'failed',
			400, false, '请求被安全策略拦截', 'digest_guard_block', now() - interval '10 minutes'
		);

		insert into llm_request_events (
			id, request_log_id, tenant_id, event_type, usage_source, usage_status, status_code, detail, created_at
		) values (
			'evt_guard_block', 'req_guard_block', 'tenant_demo', 'security_guard_blocked', 'estimated', 'failed', 400, '包含明显 SQL 注入攻击意图', now() - interval '10 minutes'
		), (
			'evt_guard_fallback', 'req_guard_fallback', 'tenant_demo', 'security_guard_fallback', 'estimated', 'success', 200, 'content moderation unavailable, fallback_regex applied', now() - interval '5 minutes'
		);
	`); err != nil {
		t.Fatalf("seed security guard events failed: %v", err)
	}

	payload, err := console.UsageFailures(ctx, service.UsageQuery{
		Window: "7d",
	})
	if err != nil {
		t.Fatalf("UsageFailures failed: %v", err)
	}

	if len(payload.RecentEvents) < 2 {
		t.Fatalf("expected at least 2 recent events, got %d", len(payload.RecentEvents))
	}

	joined := strings.Join(payload.RecentEvents, "\n")
	if !strings.Contains(joined, "安全拦截") {
		t.Fatalf("expected recent events to mention 安全拦截, got %q", joined)
	}
	if !strings.Contains(joined, "降级") {
		t.Fatalf("expected recent events to mention 降级, got %q", joined)
	}
}

func TestPostgresConsoleServiceUsageFailuresUsesRequestStartedAtWindow(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_delayed_fail',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'upstream_error',
			502,
			320,
			20,
			4,
			24,
			'bad_gateway',
			'delayed persistence',
			timestamptz '2026-04-24T10:12:00Z',
			timestamptz '2026-04-24T10:12:00.320Z',
			timestamptz '2026-04-24T12:12:00Z'
		)
	`); err != nil {
		t.Fatalf("insert delayed failure log failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into llm_request_events (
			id,
			request_log_id,
			tenant_id,
			event_type,
			usage_source,
			usage_status,
			status_code,
			detail,
			created_at
		) values (
			'llmevt_demo_delayed_fail',
			'llmreq_demo_delayed_fail',
			'tenant_demo',
			'request_failed',
			'upstream',
			'upstream_error',
			502,
			'delayed event persistence',
			timestamptz '2026-04-24T12:12:00.320Z'
		)
	`); err != nil {
		t.Fatalf("insert delayed failure event failed: %v", err)
	}

	payload, err := console.UsageFailures(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T10:10:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T10:20:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageFailures failed: %v", err)
	}

	if !containsRecentEvent(payload.RecentEvents, "delayed event persistence") {
		t.Fatalf("expected recent_events to use request_started_at window, got %#v", payload.RecentEvents)
	}
}

func TestPostgresConsoleServiceUsageFailuresWithEmptyWindowQueriesFullHistory(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_full_history_fail',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'legacy-history-only-model',
			'legacy-history-only-model',
			'upstream',
			'upstream_error',
			503,
			420,
			24,
			6,
			30,
			'service_unavailable',
			'legacy full history failure',
			timestamptz '2000-01-02T03:04:05Z',
			timestamptz '2000-01-02T03:04:05.420Z',
			timestamptz '2000-01-02T03:04:06Z'
		)
	`); err != nil {
		t.Fatalf("insert full-history failure log failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into llm_request_events (
			id,
			request_log_id,
			tenant_id,
			event_type,
			usage_source,
			usage_status,
			status_code,
			detail,
			created_at
		) values (
			'llmevt_demo_full_history_fail',
			'llmreq_demo_full_history_fail',
			'tenant_demo',
			'request_failed',
			'upstream',
			'upstream_error',
			503,
			'legacy full history failure detail',
			timestamptz '2000-01-02T03:04:05.420Z'
		)
	`); err != nil {
		t.Fatalf("insert full-history failure event failed: %v", err)
	}

	payload, err := console.UsageFailures(ctx, service.UsageQuery{
		Model: "legacy-history-only-model",
	})
	if err != nil {
		t.Fatalf("UsageFailures failed: %v", err)
	}

	if !containsFailureBucket(payload.Breakdown, "service_unavailable", "1 次") {
		t.Fatalf("expected full-history breakdown to contain legacy failure, got %#v", payload.Breakdown)
	}
	if !containsRecentEvent(payload.RecentEvents, "legacy full history failure detail") {
		t.Fatalf("expected full-history recent_events to include legacy failure, got %#v", payload.RecentEvents)
	}
}

func TestPostgresConsoleServiceUsageFailuresWithEmptyWindowAndNoFilters(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_full_history_fail_unfiltered',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'legacy-history-no-filter-model',
			'legacy-history-no-filter-model',
			'upstream',
			'upstream_error',
			503,
			420,
			24,
			6,
			30,
			'service_unavailable',
			'legacy full history no filter failure',
			timestamptz '2000-01-03T03:04:05Z',
			timestamptz '2000-01-03T03:04:05.420Z',
			timestamptz '2000-01-03T03:04:06Z'
		)
	`); err != nil {
		t.Fatalf("insert unfiltered full-history failure log failed: %v", err)
	}

	payload, err := console.UsageFailures(ctx, service.UsageQuery{})
	if err != nil {
		t.Fatalf("UsageFailures failed: %v", err)
	}

	if len(payload.Breakdown) == 0 {
		t.Fatalf("expected non-empty breakdown for unfiltered full-history query, got %#v", payload)
	}
}

func TestPostgresConsoleServiceUsageFailuresMergesEquivalentLabels(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_rate_limited',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'rate_limited',
			429,
			140,
			10,
			0,
			10,
			'rate_limited',
			'provider throttled',
			timestamptz '2026-04-24T10:25:00Z',
			timestamptz '2026-04-24T10:25:00.140Z',
			timestamptz '2026-04-24T10:25:01Z'
		)
	`); err != nil {
		t.Fatalf("insert rate-limited failure log failed: %v", err)
	}

	payload, err := console.UsageFailures(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T10:00:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageFailures failed: %v", err)
	}

	if !containsFailureBucket(payload.Breakdown, "限流", "2 次") {
		t.Fatalf("expected equivalent rate-limit labels to merge, got %#v", payload.Breakdown)
	}
}

func TestPostgresConsoleServiceUsageOverviewFiltersByMergedFailureCategory(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_rate_limit_alias',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'rate_limited',
			429,
			140,
			10,
			0,
			10,
			'rate_limited',
			'provider throttled',
			timestamptz '2026-04-24T10:25:00Z',
			timestamptz '2026-04-24T10:25:00.140Z',
			timestamptz '2026-04-24T10:25:01Z'
		)
	`); err != nil {
		t.Fatalf("insert aliased failure log failed: %v", err)
	}

	payload, err := console.UsageOverview(ctx, service.UsageQuery{
		From:          mustParseUsageTime(t, "2026-04-24T10:00:00Z"),
		To:            mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
		ErrorCategory: "rate_limit",
	})
	if err != nil {
		t.Fatalf("UsageOverview failed: %v", err)
	}

	if payload.TotalRequests != 2 {
		t.Fatalf("expected merged failure category to include 2 requests, got %d", payload.TotalRequests)
	}
	if payload.TotalTokens != "26" {
		t.Fatalf("expected merged failure category total_tokens 26, got %q", payload.TotalTokens)
	}
}

func TestPostgresConsoleServiceUsageFailuresFiltersByMergedFailureCategory(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_rate_limit_filter_alias',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'rate_limited',
			429,
			140,
			10,
			0,
			10,
			'rate_limited',
			'provider throttled',
			timestamptz '2026-04-24T10:25:00Z',
			timestamptz '2026-04-24T10:25:00.140Z',
			timestamptz '2026-04-24T10:25:01Z'
		)
	`); err != nil {
		t.Fatalf("insert aliased failure log failed: %v", err)
	}

	payload, err := console.UsageFailures(ctx, service.UsageQuery{
		From:          mustParseUsageTime(t, "2026-04-24T10:00:00Z"),
		To:            mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
		ErrorCategory: "rate_limit",
	})
	if err != nil {
		t.Fatalf("UsageFailures failed: %v", err)
	}

	if !containsFailureBucket(payload.Breakdown, "限流", "2 次") {
		t.Fatalf("expected merged failure filter to keep breakdown consistent, got %#v", payload.Breakdown)
	}
}

func TestPostgresConsoleServiceUsageOverviewFiltersByDisplayFailureCategory(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values
			(
				'llmreq_demo_timeout_display',
				'tenant_demo',
				'pak_demo',
				'demo key',
				'provider_openai_demo',
				'route:provider_openai_demo:default',
				'/v1/chat/completions',
				'gpt-4o-mini',
				'gpt-4o-mini',
				'upstream',
				'timeout',
				504,
				900,
				10,
				0,
				10,
				'',
				'timed out',
				timestamptz '2026-04-24T10:35:00Z',
				timestamptz '2026-04-24T10:35:00.900Z',
				timestamptz '2026-04-24T10:35:01Z'
			),
			(
				'llmreq_demo_upstream_rate_limited_display',
				'tenant_demo',
				'pak_demo',
				'demo key',
				'provider_openai_demo',
				'route:provider_openai_demo:default',
				'/v1/chat/completions',
				'gpt-4o-mini',
				'gpt-4o-mini',
				'upstream',
				'failed',
				429,
				180,
				12,
				0,
				12,
				'upstream_rate_limited',
				'provider throttled',
				timestamptz '2026-04-24T10:36:00Z',
				timestamptz '2026-04-24T10:36:00.180Z',
				timestamptz '2026-04-24T10:36:01Z'
			)
	`); err != nil {
		t.Fatalf("insert display-category logs failed: %v", err)
	}

	timeoutPayload, err := console.UsageOverview(ctx, service.UsageQuery{
		From:          mustParseUsageTime(t, "2026-04-24T10:30:00Z"),
		To:            mustParseUsageTime(t, "2026-04-24T10:40:00Z"),
		ErrorCategory: "上游超时",
	})
	if err != nil {
		t.Fatalf("UsageOverview timeout filter failed: %v", err)
	}
	if timeoutPayload.TotalRequests != 1 {
		t.Fatalf("expected 上游超时 filter to include timeout status rows, got %d", timeoutPayload.TotalRequests)
	}

	rateLimitedPayload, err := console.UsageOverview(ctx, service.UsageQuery{
		From:          mustParseUsageTime(t, "2026-04-24T10:30:00Z"),
		To:            mustParseUsageTime(t, "2026-04-24T10:40:00Z"),
		ErrorCategory: "上游限流",
	})
	if err != nil {
		t.Fatalf("UsageOverview upstream rate limit filter failed: %v", err)
	}
	if rateLimitedPayload.TotalRequests != 1 {
		t.Fatalf("expected 上游限流 filter to include upstream_rate_limited rows, got %d", rateLimitedPayload.TotalRequests)
	}
}

func TestPostgresConsoleServiceUsageOverviewDoesNotLeakUpstreamErrorIntoServiceExceptionFilter(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_upstream_error_display',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'upstream_error',
			502,
			220,
			10,
			0,
			10,
			'',
			'generic upstream error',
			timestamptz '2026-04-24T10:37:00Z',
			timestamptz '2026-04-24T10:37:00.220Z',
			timestamptz '2026-04-24T10:37:01Z'
		)
	`); err != nil {
		t.Fatalf("insert upstream_error log failed: %v", err)
	}

	payload, err := console.UsageOverview(ctx, service.UsageQuery{
		From:          mustParseUsageTime(t, "2026-04-24T10:30:00Z"),
		To:            mustParseUsageTime(t, "2026-04-24T10:40:00Z"),
		ErrorCategory: "上游服务异常",
	})
	if err != nil {
		t.Fatalf("UsageOverview failed: %v", err)
	}
	if payload.TotalRequests != 0 {
		t.Fatalf("expected 上游服务异常 filter not to include generic upstream_error rows, got %d", payload.TotalRequests)
	}
}

func TestPostgresConsoleServiceUsageRequests(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		update llm_request_logs
		set
			first_token_latency_ms = 40,
			task_class = 'embedding_simple',
			routing_reason = 'model:direct',
			target_model_tier = 'text-embedding-3-small',
			resolved_model = 'text-embedding-3-small',
			cached_tokens = 5,
			input_price_microyuan_per_million = 2500000,
			output_price_microyuan_per_million = 0,
			cached_price_microyuan_per_million = 750000,
			input_cost_microyuan = 1750000,
			output_cost_microyuan = 0,
			cached_cost_microyuan = 250000,
			total_cost_microyuan = 2000000
		where id = 'llmreq_demo_002';
	`); err != nil {
		t.Fatalf("seed usage request first_token_latency_ms failed: %v", err)
	}

	payload, err := console.UsageRequests(ctx, service.UsageQuery{
		From:   mustParseUsageTime(t, "2026-04-24T09:00:00Z"),
		To:     mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
		Status: "rate_limited",
	})
	if err != nil {
		t.Fatalf("UsageRequests failed: %v", err)
	}

	if len(payload.Items) != 1 {
		t.Fatalf("expected 1 request item, got %d", len(payload.Items))
	}
	item := payload.Items[0]
	if item.RequestID != "llmreq_demo_002" {
		t.Fatalf("expected request_id llmreq_demo_002, got %q", item.RequestID)
	}
	if item.Tenant != "tenant_demo" {
		t.Fatalf("expected tenant tenant_demo, got %q", item.Tenant)
	}
	if item.TenantID != "tenant_demo" {
		t.Fatalf("expected tenant_id tenant_demo, got %q", item.TenantID)
	}
	if item.TenantName != "Demo Tenant" {
		t.Fatalf("expected tenant_name Demo Tenant, got %q", item.TenantName)
	}
	if item.Endpoint != "/v1/embeddings" {
		t.Fatalf("expected endpoint /v1/embeddings, got %q", item.Endpoint)
	}
	if item.Model != "text-embedding-3-small" {
		t.Fatalf("expected model text-embedding-3-small, got %q", item.Model)
	}
	if item.Status != "限流" {
		t.Fatalf("expected status 限流, got %q", item.Status)
	}
	if item.TotalTokens != "16" {
		t.Fatalf("expected total_tokens 16, got %q", item.TotalTokens)
	}
	if item.InputTokens != "16" {
		t.Fatalf("expected input_tokens 16, got %q", item.InputTokens)
	}
	if item.OutputTokens != "0" {
		t.Fatalf("expected output_tokens 0, got %q", item.OutputTokens)
	}
	if item.CachedTokens != "5" {
		t.Fatalf("expected cached_tokens 5, got %q", item.CachedTokens)
	}
	if item.Latency != "95 ms" {
		t.Fatalf("expected latency 95 ms, got %q", item.Latency)
	}
	if item.InputCost != "1.75 ￥" {
		t.Fatalf("expected input_cost 1.75 ￥, got %q", item.InputCost)
	}
	if item.OutputCost != "0.00 ￥" {
		t.Fatalf("expected output_cost 0.00 ￥, got %q", item.OutputCost)
	}
	if item.CachedCost != "0.25 ￥" {
		t.Fatalf("expected cached_cost 0.25 ￥, got %q", item.CachedCost)
	}
	if item.TotalCost != "2.00 ￥" {
		t.Fatalf("expected total_cost 2.00 ￥, got %q", item.TotalCost)
	}
	if item.InputPrice != "2.50 ￥/M" {
		t.Fatalf("expected input_price 2.50 ￥/M, got %q", item.InputPrice)
	}
	if item.OutputPrice != "0.00 ￥/M" {
		t.Fatalf("expected output_price 0.00 ￥/M, got %q", item.OutputPrice)
	}
	if item.CachedPrice != "0.75 ￥/M" {
		t.Fatalf("expected cached_price 0.75 ￥/M, got %q", item.CachedPrice)
	}
	if item.UsageSource != "估算" {
		t.Fatalf("expected usage_source 估算, got %q", item.UsageSource)
	}
	if item.TaskClass != "embedding_simple" {
		t.Fatalf("expected task_class %q, got %q", "embedding_simple", item.TaskClass)
	}
	if item.RoutingReason != "model:direct" {
		t.Fatalf("expected routing_reason %q, got %q", "model:direct", item.RoutingReason)
	}
	if item.TargetModelTier != "text-embedding-3-small" {
		t.Fatalf("expected target_model_tier %q, got %q", "text-embedding-3-small", item.TargetModelTier)
	}
	if item.ResolvedModel != "text-embedding-3-small" {
		t.Fatalf("expected resolved_model %q, got %q", "text-embedding-3-small", item.ResolvedModel)
	}
	if item.FirstTokenLatencyMS != 40 {
		t.Fatalf("expected first_token_latency_ms 40, got %#v", item.FirstTokenLatencyMS)
	}

	filteredPayload, err := console.UsageRequests(ctx, service.UsageQuery{
		From:          mustParseUsageTime(t, "2026-04-24T09:00:00Z"),
		To:            mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
		Status:        "rate_limited",
		ResolvedModel: "qwen-plus",
	})
	if err != nil {
		t.Fatalf("UsageRequests with resolved_model filter failed: %v", err)
	}
	if len(filteredPayload.Items) != 0 {
		t.Fatalf("expected 0 request items after resolved_model filter mismatch, got %d", len(filteredPayload.Items))
	}

	if _, err := conn.Exec(ctx, `
		insert into provider_credentials (id, provider, display_name, supported_models, base_url, encrypted_secret, status)
		values ('provider_mimo_primary', 'mimo', 'MIMO', '{"mimo-v2.5-pro"}', 'https://api.xiaomimimo.com/v1', '', 'active')
		on conflict (id) do nothing;

		insert into route_catalog (
			id, requested_model, resolved_provider, provider_credential_id, endpoint, latency_ms, health_status, request_mode, updated_at
		) values (
			'route:provider_mimo_primary:default',
			'mimo-v2.5-pro',
			'MIMO',
			'provider_mimo_primary',
			'/v1/chat/completions',
			286,
			'healthy',
			'推理',
			now()
		) on conflict (id) do nothing;

		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			cached_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at,
			resolved_model
		) values (
			'llmreq_demo_mimo_filter_option',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_mimo_primary',
			'route:provider_mimo_primary:default',
			'/v1/chat/completions',
			'mimo',
			'mimo-v2.5-pro',
			'upstream',
			'success',
			200,
			320,
			255,
			8,
			263,
			0,
			'',
			'',
			timestamptz '2026-04-24T10:08:00Z',
			timestamptz '2026-04-24T10:08:00.320Z',
			timestamptz '2026-04-24T10:08:01Z',
			'mimo-v2.5-pro'
		);
	`); err != nil {
		t.Fatalf("seed mimo filter option failed: %v", err)
	}

	optionsPayload, err := console.UsageRequests(ctx, service.UsageQuery{
		From:   mustParseUsageTime(t, "2026-04-24T09:00:00Z"),
		To:     mustParseUsageTime(t, "2026-04-24T11:00:00Z"),
		Limit:  1,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("UsageRequests with filter options failed: %v", err)
	}
	if len(optionsPayload.Items) != 1 {
		t.Fatalf("expected paged payload to contain 1 item, got %d", len(optionsPayload.Items))
	}
	if !slices.Contains(optionsPayload.ResolvedModelOptions, "mimo-v2.5-pro") {
		t.Fatalf("expected resolved model options to contain mimo-v2.5-pro, got %#v", optionsPayload.ResolvedModelOptions)
	}
	if !slices.Contains(optionsPayload.ResolvedModelOptions, "text-embedding-3-small") {
		t.Fatalf("expected resolved model options to contain text-embedding-3-small, got %#v", optionsPayload.ResolvedModelOptions)
	}
}

func TestPostgresConsoleServiceUsageRequestDetail(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		update llm_request_logs
		set
			task_class = 'embedding_simple',
			routing_reason = 'model:direct',
			target_model_tier = 'text-embedding-3-small',
			resolved_model = 'text-embedding-3-small',
			prompt_excerpt = '手机号 138XXXX0000',
			response_excerpt = '处理完成，邮箱 u***r@example.com',
			first_token_latency_ms = 12
		where id = 'llmreq_demo_002';
	`); err != nil {
		t.Fatalf("seed usage detail failed: %v", err)
	}

	payload, err := console.UsageRequestDetail(ctx, "llmreq_demo_002")
	if err != nil {
		t.Fatalf("UsageRequestDetail failed: %v", err)
	}

	if payload.RequestID != "llmreq_demo_002" {
		t.Fatalf("expected request_id llmreq_demo_002, got %q", payload.RequestID)
	}
	if payload.TenantID != "tenant_demo" {
		t.Fatalf("expected tenant_id tenant_demo, got %q", payload.TenantID)
	}
	if payload.TenantName != "Demo Tenant" {
		t.Fatalf("expected tenant_name Demo Tenant, got %q", payload.TenantName)
	}
	if payload.ResolvedModel != "text-embedding-3-small" {
		t.Fatalf("expected resolved_model text-embedding-3-small, got %q", payload.ResolvedModel)
	}
	if payload.PromptExcerpt != "手机号 138XXXX0000" {
		t.Fatalf("expected prompt_excerpt, got %q", payload.PromptExcerpt)
	}
	if payload.ResponseExcerpt != "处理完成，邮箱 u***r@example.com" {
		t.Fatalf("expected response_excerpt, got %q", payload.ResponseExcerpt)
	}
	if len(payload.FailureEvents) == 0 {
		t.Fatal("expected failure_events to be populated")
	}
}

func TestPostgresConsoleServiceUsageRequestDetailIncludesSecurityGuardEvents(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id, tenant_id, platform_api_key_id, platform_api_key_name, provider_credential_id, route_id,
			request_path, request_model, upstream_model, usage_source, usage_status, status_code,
			latency_ms, prompt_tokens, completion_tokens, total_tokens, error_code, error_message,
			request_started_at, request_completed_at, created_at
		) values (
			'llmreq_guard_detail',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'qwen-flash',
			'qwen-flash',
			'estimated',
			'failed',
			400,
			26,
			12,
			0,
			12,
			'',
			'请求被安全策略拦截：包含明显 SQL 注入攻击意图',
			now() - interval '3 minutes',
			now() - interval '3 minutes',
			now() - interval '3 minutes'
		);

		insert into llm_request_events (
			id, request_log_id, tenant_id, event_type, usage_source, usage_status, status_code, detail, created_at
		) values (
			'llmevt_guard_detail_block',
			'llmreq_guard_detail',
			'tenant_demo',
			'security_guard_blocked',
			'estimated',
			'failed',
			400,
			'包含明显 SQL 注入攻击意图',
			now() - interval '3 minutes'
		), (
			'llmevt_guard_detail_fallback',
			'llmreq_guard_detail',
			'tenant_demo',
			'security_guard_fallback',
			'estimated',
			'success',
			200,
			'content moderation unavailable, fallback_regex applied',
			now() - interval '2 minutes'
		);
	`); err != nil {
		t.Fatalf("seed usage detail security events failed: %v", err)
	}

	payload, err := console.UsageRequestDetail(ctx, "llmreq_guard_detail")
	if err != nil {
		t.Fatalf("UsageRequestDetail failed: %v", err)
	}

	if len(payload.FailureEvents) < 2 {
		t.Fatalf("expected at least 2 failure events, got %d", len(payload.FailureEvents))
	}

	var sawBlocked bool
	var sawFallback bool
	for _, item := range payload.FailureEvents {
		if item.Category == "安全拦截" && strings.Contains(item.Reason, "已被安全策略拦截") {
			sawBlocked = true
		}
		if item.Category == "安全审核降级" && strings.Contains(item.Reason, "安全审核已降级") {
			sawFallback = true
		}
	}
	if !sawBlocked {
		t.Fatalf("expected usage detail to include blocked security event, got %+v", payload.FailureEvents)
	}
	if !sawFallback {
		t.Fatalf("expected usage detail to include fallback security event, got %+v", payload.FailureEvents)
	}
}

func TestPostgresConsoleServiceUsageRequestsUsesRequestStartedAtWindow(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_delayed_request',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'success',
			200,
			90,
			14,
			3,
			17,
			'',
			'',
			timestamptz '2026-04-24T10:18:00Z',
			timestamptz '2026-04-24T10:18:00.090Z',
			timestamptz '2026-04-24T12:18:00Z'
		)
	`); err != nil {
		t.Fatalf("insert delayed request log failed: %v", err)
	}

	payload, err := console.UsageRequests(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T10:15:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T10:20:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageRequests failed: %v", err)
	}

	if len(payload.Items) != 1 {
		t.Fatalf("expected 1 request item, got %d", len(payload.Items))
	}
	if payload.Items[0].RequestID != "llmreq_demo_delayed_request" {
		t.Fatalf("expected delayed request to be selected by request_started_at, got %q", payload.Items[0].RequestID)
	}
}

func TestPostgresConsoleServiceUsageRequestsWithEmptyWindowQueriesFullHistory(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_full_history_request',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'legacy-history-request-model',
			'legacy-history-request-model',
			'upstream',
			'success',
			200,
			88,
			18,
			4,
			22,
			'',
			'',
			timestamptz '2000-01-02T03:14:05Z',
			timestamptz '2000-01-02T03:14:05.088Z',
			timestamptz '2000-01-02T03:14:06Z'
		)
	`); err != nil {
		t.Fatalf("insert full-history request log failed: %v", err)
	}

	payload, err := console.UsageRequests(ctx, service.UsageQuery{
		Model: "legacy-history-request-model",
	})
	if err != nil {
		t.Fatalf("UsageRequests failed: %v", err)
	}

	if len(payload.Items) != 1 {
		t.Fatalf("expected 1 full-history request item, got %d", len(payload.Items))
	}
	if payload.Items[0].RequestID != "llmreq_demo_full_history_request" {
		t.Fatalf("expected legacy request to be returned for empty window, got %q", payload.Items[0].RequestID)
	}
}

func TestPostgresConsoleServiceUsageRequestsWithEmptyWindowAndNoFilters(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_full_history_request_unfiltered',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'legacy-history-request-no-filter-model',
			'legacy-history-request-no-filter-model',
			'upstream',
			'success',
			200,
			88,
			18,
			4,
			22,
			'',
			'',
			timestamptz '2000-01-03T03:14:05Z',
			timestamptz '2000-01-03T03:14:05.088Z',
			timestamptz '2000-01-03T03:14:06Z'
		)
	`); err != nil {
		t.Fatalf("insert unfiltered full-history request log failed: %v", err)
	}

	payload, err := console.UsageRequests(ctx, service.UsageQuery{})
	if err != nil {
		t.Fatalf("UsageRequests failed: %v", err)
	}

	if len(payload.Items) == 0 {
		t.Fatalf("expected non-empty request list for unfiltered full-history query, got %#v", payload)
	}
}

func TestPostgresConsoleServiceUsageRequestsDefaultsFirstTokenLatencyToZero(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, conn := newUsageConsoleService(t, ctx)

	if _, err := conn.Exec(ctx, `
		insert into llm_request_logs (
			id,
			tenant_id,
			platform_api_key_id,
			platform_api_key_name,
			provider_credential_id,
			route_id,
			request_path,
			request_model,
			upstream_model,
			usage_source,
			usage_status,
			status_code,
			latency_ms,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			error_code,
			error_message,
			request_started_at,
			request_completed_at,
			created_at
		) values (
			'llmreq_demo_zero_first_token',
			'tenant_demo',
			'pak_demo',
			'demo key',
			'provider_openai_demo',
			'route:provider_openai_demo:default',
			'/v1/chat/completions',
			'gpt-4o-mini',
			'gpt-4o-mini',
			'upstream',
			'success',
			200,
			88,
			12,
			4,
			16,
			'',
			'',
			timestamptz '2026-04-24T10:16:00Z',
			timestamptz '2026-04-24T10:16:00.088Z',
			timestamptz '2026-04-24T10:16:01Z'
		)
	`); err != nil {
		t.Fatalf("insert zero first-token request log failed: %v", err)
	}

	var firstTokenLatencyMS int64
	if err := conn.QueryRow(ctx, `
		select first_token_latency_ms
		from llm_request_logs
		where id = 'llmreq_demo_zero_first_token'
	`).Scan(&firstTokenLatencyMS); err != nil {
		t.Fatalf("QueryRow llm_request_logs first_token_latency_ms failed: %v", err)
	}
	if firstTokenLatencyMS != 0 {
		t.Fatalf("expected DB default first_token_latency_ms 0, got %d", firstTokenLatencyMS)
	}

	payload, err := console.UsageRequests(ctx, service.UsageQuery{
		From: mustParseUsageTime(t, "2026-04-24T10:15:00Z"),
		To:   mustParseUsageTime(t, "2026-04-24T10:17:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageRequests failed: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("expected 1 request item, got %d", len(payload.Items))
	}

	item := payload.Items[0]
	if item.FirstTokenLatencyMS != 0 {
		t.Fatalf("expected query chain to return DB default first_token_latency_ms 0, got %d", item.FirstTokenLatencyMS)
	}
}

func newUsageConsoleService(t *testing.T, ctx context.Context) (service.ConsoleService, *pgx.Conn) {
	t.Helper()

	container, dsn := startUsagePostgresContainer(ctx, t)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	pool, err := gatewaydb.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("OpenPostgres failed: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := gatewaydb.ApplyMigrations(ctx, pool); err != nil {
		t.Fatalf("ApplyMigrations failed: %v", err)
	}
	for _, statement := range gatewaydb.RuntimeSeedStatements() {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("conn.Exec seed failed: %v", err)
		}
	}

	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("secret.NewCodec failed: %v", err)
	}

	pricingResolver, err := service.NewModelPricingResolver(map[string]service.ModelTokenPrice{
		"default": {
			InputMicroyuanPerMillion:  2_000_000,
			OutputMicroyuanPerMillion: 20_000_000,
			CachedMicroyuanPerMillion: 500_000,
		},
		"qwen-flash": {
			InputMicroyuanPerMillion:  2_000_000,
			OutputMicroyuanPerMillion: 20_000_000,
			CachedMicroyuanPerMillion: 500_000,
		},
		"mimo-v2.5-pro": {
			InputMicroyuanPerMillion:  2_000_000,
			OutputMicroyuanPerMillion: 20_000_000,
			CachedMicroyuanPerMillion: 500_000,
		},
	})
	if err != nil {
		t.Fatalf("NewModelPricingResolver failed: %v", err)
	}

	return service.NewPostgresConsoleServiceWithPricing(conn, nil, nil, nil, "", pricingResolver, 0, codec), conn
}

func mustParseUsageTime(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("time.Parse failed: %v", err)
	}
	return parsed
}

func testHashCaptchaValue(value string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(value))))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func startUsagePostgresContainer(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
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

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("container.MappedPort failed: %v", err)
	}

	dsn := "postgres://postgres:postgres@" + host + ":" + port.Port() + "/gateway_test?sslmode=disable"
	return container, dsn
}

func containsFailureBucket(items []service.UsageFailureBucket, label string, value string) bool {
	for _, item := range items {
		if item.Label == label && item.Value == value {
			return true
		}
	}
	return false
}

func containsModelHealthWallStatus(wall service.ModelHealthWall, status string) bool {
	for _, lane := range wall.Lanes {
		for _, cell := range lane.Cells {
			if cell.Status == status {
				return true
			}
		}
	}
	return false
}

func containsRecentEvent(items []string, fragment string) bool {
	for _, item := range items {
		if strings.Contains(item, fragment) {
			return true
		}
	}
	return false
}

func containsString(items []string, expected string) bool {
	for _, item := range items {
		if item == expected {
			return true
		}
	}
	return false
}

func containsPricingModel(items []service.PricingModelItem, model string, inputPrice string, outputPrice string, cachedPrice string) bool {
	for _, item := range items {
		if item.Model == model &&
			item.InputPrice == inputPrice &&
			item.OutputPrice == outputPrice &&
			item.CachedPrice == cachedPrice {
			return true
		}
	}
	return false
}

func containsMetric(items []service.KeyMetric, label string) bool {
	for _, item := range items {
		if item.Label == label {
			return true
		}
	}
	return false
}

func metricValue(items []service.KeyMetric, label string) string {
	for _, item := range items {
		if item.Label == label {
			return item.Value
		}
	}
	return ""
}

func containsTableRowValue(items []service.TableRow, expected string) bool {
	for _, item := range items {
		for _, column := range item.Columns {
			if column == expected {
				return true
			}
		}
	}
	return false
}

func findTableRowByFirstColumn(items []service.TableRow, firstColumn string) *service.TableRow {
	for index := range items {
		if len(items[index].Columns) > 0 && items[index].Columns[0] == firstColumn {
			return &items[index]
		}
	}
	return nil
}

type stubConsoleResolveAuthService struct {
	requestContext domain.RequestContext
}

func (s stubConsoleResolveAuthService) Resolve(context.Context, string, string) (domain.RequestContext, error) {
	return s.requestContext, nil
}

type stubConsoleChatProxy struct {
	completeCalls int
	streamCalls   int
	streamResult  service.ChatCompletionStream
	streamErr     error
}

func (s *stubConsoleChatProxy) Complete(context.Context, service.ChatRequest, any) (service.ChatResponse, error) {
	s.completeCalls++
	return service.ChatResponse{}, nil
}

func (s *stubConsoleChatProxy) Stream(context.Context, service.ChatRequest, any) (service.ChatCompletionStream, error) {
	s.streamCalls++
	return s.streamResult, s.streamErr
}

func (s *stubConsoleChatProxy) RecordFailure(context.Context, any, int) {}

type stubConsoleUpstreamChatClient struct {
	completeResp service.ChatResponse
	completeErr  error
	stream       service.ChatCompletionStream
	streamErr    error
}

func (s *stubConsoleUpstreamChatClient) Complete(context.Context, domain.ProviderTarget, service.ChatRequest) (service.ChatResponse, int, error) {
	if s.completeErr != nil {
		return service.ChatResponse{}, http.StatusBadGateway, s.completeErr
	}
	return s.completeResp, http.StatusOK, nil
}

func (s *stubConsoleUpstreamChatClient) StreamComplete(context.Context, domain.ProviderTarget, service.ChatRequest) (service.ChatCompletionStream, int, error) {
	if s.streamErr != nil {
		return service.ChatCompletionStream{}, http.StatusBadGateway, s.streamErr
	}
	return s.stream, http.StatusOK, nil
}
