package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	gatewaydb "github.com/example/ai_gateway/gateway/db"
	"github.com/example/ai_gateway/gateway/internal/queue"
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type failingUsagePublisher struct {
	err error
}

func (p failingUsagePublisher) Publish(context.Context, queue.UsageEvent) error {
	return p.err
}

func TestUsageAggregatorUpsertsHourlyUsage(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(ctx, t)
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

	aggregator := service.NewUsageAggregator(conn)
	eventTime := time.Date(2026, time.April, 24, 11, 15, 0, 0, time.UTC)
	periodStart, periodEnd := currentShanghaiMonthWindow(t, eventTime)
	var beforeMonthlyRequests int
	var beforeMonthlyTokens int
	if err := conn.QueryRow(ctx, `
		select requests_used, tokens_used
		from tenant_quota_usage_periods
		where tenant_id = 'tenant_demo'
		  and period_start = $1
		  and period_end = $2
	`, periodStart, periodEnd).Scan(&beforeMonthlyRequests, &beforeMonthlyTokens); err != nil {
		t.Fatalf("QueryRow tenant_quota_usage_periods failed: %v", err)
	}

	for _, totalTokens := range []int{24, 6} {
		if err := aggregator.Consume(ctx, queue.UsageEvent{
			RequestID:            fmt.Sprintf("req-%d", totalTokens),
			TenantID:             "tenant_demo",
			PlatformAPIKeyID:     "pak_demo",
			ProviderCredentialID: "provider_openai_demo",
			RouteID:              "route:provider_openai_demo:default",
			Status:               "success",
			UsageSource:          "upstream",
			PromptTokens:         totalTokens - 4,
			CompletionTokens:     4,
			TotalTokens:          totalTokens,
			Endpoint:             "/v1/chat/completions",
			OccurredAt:           eventTime,
		}); err != nil {
			t.Fatalf("aggregator.Consume failed: %v", err)
		}
	}

	var requestCount int
	var promptTokens int
	var completionTokens int
	var totalTokens int
	if err := conn.QueryRow(ctx, `
		select request_count, prompt_tokens, completion_tokens, total_tokens
		from llm_usage_agg_hourly
		where bucket_start = timestamptz '2026-04-24T11:00:00Z'
		  and tenant_id = 'tenant_demo'
		  and platform_api_key_id = 'pak_demo'
		  and provider_credential_id = 'provider_openai_demo'
		  and route_id = 'route:provider_openai_demo:default'
		  and request_path = '/v1/chat/completions'
		  and usage_source = 'upstream'
		  and usage_status = 'success'
	`).Scan(&requestCount, &promptTokens, &completionTokens, &totalTokens); err != nil {
		t.Fatalf("QueryRow aggregate failed: %v", err)
	}

	if requestCount != 2 {
		t.Fatalf("expected request_count 2, got %d", requestCount)
	}
	if promptTokens != 22 {
		t.Fatalf("expected prompt_tokens 22, got %d", promptTokens)
	}
	if completionTokens != 8 {
		t.Fatalf("expected completion_tokens 8, got %d", completionTokens)
	}
	if totalTokens != 30 {
		t.Fatalf("expected total_tokens 30, got %d", totalTokens)
	}

	var monthlyRequests int
	var monthlyTokens int
	if err := conn.QueryRow(ctx, `
		select requests_used, tokens_used
		from tenant_quota_usage_periods
		where tenant_id = 'tenant_demo'
		  and period_start = $1
		  and period_end = $2
	`, periodStart, periodEnd).Scan(&monthlyRequests, &monthlyTokens); err != nil {
		t.Fatalf("QueryRow tenant_quota_usage_periods failed: %v", err)
	}
	if monthlyRequests != beforeMonthlyRequests+2 {
		t.Fatalf("expected requests_used %d, got %d", beforeMonthlyRequests+2, monthlyRequests)
	}
	if monthlyTokens != beforeMonthlyTokens+30 {
		t.Fatalf("expected tokens_used %d, got %d", beforeMonthlyTokens+30, monthlyTokens)
	}
}

func TestUsageAggregatorUpdatesTenantQuotaUsagePeriod(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(ctx, t)
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

	aggregator := service.NewUsageAggregator(conn)
	eventTime := time.Date(2026, time.April, 28, 10, 0, 0, 0, mustLoadShanghaiLocation(t))
	periodStart, periodEnd := currentShanghaiMonthWindow(t, eventTime)
	var beforeRequests int64
	var beforeTokens int64
	if err := conn.QueryRow(ctx, `
		select requests_used, tokens_used
		from tenant_quota_usage_periods
		where tenant_id = 'tenant_demo'
		  and period_start = $1
		  and period_end = $2
	`, periodStart, periodEnd).Scan(&beforeRequests, &beforeTokens); err != nil {
		t.Fatalf("QueryRow before tenant_quota_usage_periods failed: %v", err)
	}
	if err := aggregator.Consume(ctx, queue.UsageEvent{
		TenantID:             "tenant_demo",
		PlatformAPIKeyID:     "pak_demo",
		ProviderCredentialID: "provider_openai_demo",
		RouteID:              service.RouteIDForCredential("provider_openai_demo", []string{"gpt-4o-mini", "text-embedding-3-small"}, "gpt-4o-mini"),
		Endpoint:             "/v1/chat/completions",
		Status:               "success",
		UsageSource:          "upstream",
		PromptTokens:         120,
		CompletionTokens:     80,
		TotalTokens:          200,
		OccurredAt:           eventTime,
	}); err != nil {
		t.Fatalf("Consume failed: %v", err)
	}

	var requestsUsed int64
	var tokensUsed int64
	if err := conn.QueryRow(ctx, `
		select requests_used, tokens_used
		from tenant_quota_usage_periods
		where tenant_id = 'tenant_demo'
		  and period_start = $1
		  and period_end = $2
	`, periodStart, periodEnd).Scan(&requestsUsed, &tokensUsed); err != nil {
		t.Fatalf("QueryRow tenant_quota_usage_periods failed: %v", err)
	}
	if requestsUsed != beforeRequests+1 || tokensUsed != beforeTokens+200 {
		t.Fatalf("expected usage (%d, %d), got (%d, %d)", beforeRequests+1, beforeTokens+200, requestsUsed, tokensUsed)
	}
}

func TestUsagePublisherWithConsumersSkipsUnwrappedHourlyAggregatorWhenPublishFails(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(ctx, t)
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

	publishErr := errors.New("rabbitmq publish failed")
	publisher := queue.NewUsagePublisherWithConsumers(
		failingUsagePublisher{err: publishErr},
		service.NewUsageAggregator(conn),
	)

	eventTime := time.Date(2026, time.April, 24, 11, 15, 0, 0, time.UTC)
	err = publisher.Publish(ctx, queue.UsageEvent{
		RequestID:            "req-publish-failure",
		TenantID:             "tenant_demo",
		PlatformAPIKeyID:     "pak_demo",
		ProviderCredentialID: "provider_openai_demo",
		RouteID:              "route:provider_openai_demo:default",
		Status:               "success",
		UsageSource:          "upstream",
		PromptTokens:         8,
		CompletionTokens:     4,
		TotalTokens:          12,
		Endpoint:             "/v1/chat/completions",
		OccurredAt:           eventTime,
	})
	if !errors.Is(err, publishErr) {
		t.Fatalf("expected publish error %v, got %v", publishErr, err)
	}

	var aggregateRows int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from llm_usage_agg_hourly
		where bucket_start = timestamptz '2026-04-24T11:00:00Z'
		  and tenant_id = 'tenant_demo'
		  and platform_api_key_id = 'pak_demo'
		  and provider_credential_id = 'provider_openai_demo'
		  and route_id = 'route:provider_openai_demo:default'
		  and request_path = '/v1/chat/completions'
		  and usage_source = 'upstream'
		  and usage_status = 'success'
	`).Scan(&aggregateRows); err != nil {
		t.Fatalf("QueryRow aggregate rows after publish failure failed: %v", err)
	}

	if aggregateRows != 0 {
		t.Fatalf("expected unwrapped aggregator to be skipped after publish failure, got %d aggregate rows", aggregateRows)
	}
}

func TestUsagePublisherWithConsumersUpdatesHourlyUsageWithFallbackWhenPublishFails(t *testing.T) {
	t.Parallel()

	dbCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(dbCtx, t)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	conn, err := pgx.Connect(dbCtx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	pool, err := gatewaydb.OpenPostgres(dbCtx, dsn)
	if err != nil {
		t.Fatalf("OpenPostgres failed: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := gatewaydb.ApplyMigrations(dbCtx, pool); err != nil {
		t.Fatalf("ApplyMigrations failed: %v", err)
	}
	for _, statement := range gatewaydb.RuntimeSeedStatements() {
		if _, err := conn.Exec(dbCtx, statement); err != nil {
			t.Fatalf("conn.Exec seed failed: %v", err)
		}
	}

	publishErr := errors.New("rabbitmq publish failed")
	publisher := queue.NewUsagePublisherWithConsumers(
		failingUsagePublisher{err: publishErr},
		queue.WithPublishFailureTimeout(service.NewUsageAggregator(conn), time.Second),
	)

	requestCtx, cancelRequest := context.WithTimeout(context.Background(), time.Minute)
	defer cancelRequest()

	eventTime := time.Date(2026, time.April, 24, 11, 15, 0, 0, time.UTC)
	err = publisher.Publish(requestCtx, queue.UsageEvent{
		RequestID:            "req-fallback-publish-failure",
		TenantID:             "tenant_demo",
		PlatformAPIKeyID:     "pak_demo",
		ProviderCredentialID: "provider_openai_demo",
		RouteID:              "route:provider_openai_demo:default",
		Status:               "success",
		UsageSource:          "upstream",
		PromptTokens:         7,
		CompletionTokens:     5,
		TotalTokens:          12,
		Endpoint:             "/v1/chat/completions",
		OccurredAt:           eventTime,
	})
	if !errors.Is(err, publishErr) {
		t.Fatalf("expected publish error %v, got %v", publishErr, err)
	}

	var requestCount int
	var totalTokens int
	if err := conn.QueryRow(dbCtx, `
		select request_count, total_tokens
		from llm_usage_agg_hourly
		where bucket_start = timestamptz '2026-04-24T11:00:00Z'
		  and tenant_id = 'tenant_demo'
		  and platform_api_key_id = 'pak_demo'
		  and provider_credential_id = 'provider_openai_demo'
		  and route_id = 'route:provider_openai_demo:default'
		  and request_path = '/v1/chat/completions'
		  and usage_source = 'upstream'
		  and usage_status = 'success'
	`).Scan(&requestCount, &totalTokens); err != nil {
		t.Fatalf("QueryRow aggregate after fallback publish failure failed: %v", err)
	}

	if requestCount != 1 {
		t.Fatalf("expected request_count 1 after fallback publish failure, got %d", requestCount)
	}
	if totalTokens != 12 {
		t.Fatalf("expected total_tokens 12 after fallback publish failure, got %d", totalTokens)
	}
}

func TestUsagePublisherWithConsumersUpdatesHourlyUsageWithFallbackAfterRequestCancellationOnNormalPath(t *testing.T) {
	t.Parallel()

	dbCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(dbCtx, t)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	conn, err := pgx.Connect(dbCtx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	pool, err := gatewaydb.OpenPostgres(dbCtx, dsn)
	if err != nil {
		t.Fatalf("OpenPostgres failed: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := gatewaydb.ApplyMigrations(dbCtx, pool); err != nil {
		t.Fatalf("ApplyMigrations failed: %v", err)
	}
	for _, statement := range gatewaydb.RuntimeSeedStatements() {
		if _, err := conn.Exec(dbCtx, statement); err != nil {
			t.Fatalf("conn.Exec seed failed: %v", err)
		}
	}

	publisher := queue.NewUsagePublisherWithConsumers(
		queue.NewNoopUsagePublisher(),
		queue.WithPublishFailureTimeout(service.NewUsageAggregator(conn), time.Second),
	)

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()

	eventTime := time.Date(2026, time.April, 24, 11, 15, 0, 0, time.UTC)
	if err := publisher.Publish(requestCtx, queue.UsageEvent{
		RequestID:            "req-fallback-normal-path",
		TenantID:             "tenant_demo",
		PlatformAPIKeyID:     "pak_demo",
		ProviderCredentialID: "provider_openai_demo",
		RouteID:              "route:provider_openai_demo:default",
		Status:               "success",
		UsageSource:          "upstream",
		PromptTokens:         9,
		CompletionTokens:     3,
		TotalTokens:          12,
		Endpoint:             "/v1/chat/completions",
		OccurredAt:           eventTime,
	}); err != nil {
		t.Fatalf("expected wrapped aggregator on normal path to succeed, got %v", err)
	}

	var requestCount int
	var totalTokens int
	if err := conn.QueryRow(dbCtx, `
		select request_count, total_tokens
		from llm_usage_agg_hourly
		where bucket_start = timestamptz '2026-04-24T11:00:00Z'
		  and tenant_id = 'tenant_demo'
		  and platform_api_key_id = 'pak_demo'
		  and provider_credential_id = 'provider_openai_demo'
		  and route_id = 'route:provider_openai_demo:default'
		  and request_path = '/v1/chat/completions'
		  and usage_source = 'upstream'
		  and usage_status = 'success'
	`).Scan(&requestCount, &totalTokens); err != nil {
		t.Fatalf("QueryRow aggregate after normal-path cancellation failed: %v", err)
	}

	if requestCount != 1 {
		t.Fatalf("expected request_count 1 after normal-path cancellation, got %d", requestCount)
	}
	if totalTokens != 12 {
		t.Fatalf("expected total_tokens 12 after normal-path cancellation, got %d", totalTokens)
	}
}

func TestUsageAggregatorRollsUpTenantUsageLedger(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, dsn := startPostgresContainer(ctx, t)
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

	aggregator := service.NewUsageAggregator(conn)
	bucketStart := time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC)
	events := []queue.UsageEvent{
		{
			RequestID:            "req-ledger-success",
			TenantID:             "tenant_demo",
			PlatformAPIKeyID:     "pak_demo",
			ProviderCredentialID: "provider_openai_demo",
			RouteID:              "route:provider_openai_demo:default",
			Status:               "success",
			UsageSource:          "upstream",
			PromptTokens:         10,
			CompletionTokens:     4,
			TotalTokens:          14,
			Endpoint:             "/v1/chat/completions",
			OccurredAt:           bucketStart.Add(5 * time.Minute),
		},
		{
			RequestID:            "req-ledger-failure",
			TenantID:             "tenant_demo",
			PlatformAPIKeyID:     "pak_demo",
			ProviderCredentialID: "provider_openai_demo",
			RouteID:              "route:provider_openai_demo:default",
			Status:               "failed",
			UsageSource:          "estimated",
			PromptTokens:         6,
			CompletionTokens:     0,
			TotalTokens:          6,
			Endpoint:             "/v1/chat/completions",
			OccurredAt:           bucketStart.Add(20 * time.Minute),
		},
	}
	for _, event := range events {
		if err := aggregator.Consume(ctx, event); err != nil {
			t.Fatalf("aggregator.Consume failed: %v", err)
		}
	}

	if err := aggregator.AggregateHour(ctx, bucketStart); err != nil {
		t.Fatalf("aggregator.AggregateHour failed: %v", err)
	}

	var inputTokens int
	var outputTokens int
	var totalTokens int
	var requestCount int
	var successCount int
	var failureCount int
	var estimatedCount int
	if err := conn.QueryRow(ctx, `
		select input_tokens,
		       output_tokens,
		       total_tokens,
		       request_count,
		       success_count,
		       failure_count,
		       estimated_count
		from tenant_usage_ledger
		where bucket_start = $1
		  and tenant_id = 'tenant_demo'
	`, bucketStart).Scan(
		&inputTokens,
		&outputTokens,
		&totalTokens,
		&requestCount,
		&successCount,
		&failureCount,
		&estimatedCount,
	); err != nil {
		t.Fatalf("QueryRow tenant_usage_ledger failed: %v", err)
	}

	if inputTokens != 16 {
		t.Fatalf("expected input_tokens 16, got %d", inputTokens)
	}
	if outputTokens != 4 {
		t.Fatalf("expected output_tokens 4, got %d", outputTokens)
	}
	if totalTokens != 20 {
		t.Fatalf("expected total_tokens 20, got %d", totalTokens)
	}
	if requestCount != 2 {
		t.Fatalf("expected request_count 2, got %d", requestCount)
	}
	if successCount != 1 {
		t.Fatalf("expected success_count 1, got %d", successCount)
	}
	if failureCount != 1 {
		t.Fatalf("expected failure_count 1, got %d", failureCount)
	}
	if estimatedCount != 1 {
		t.Fatalf("expected estimated_count 1, got %d", estimatedCount)
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

func mustLoadShanghaiLocation(t *testing.T) *time.Location {
	t.Helper()

	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("time.LoadLocation failed: %v", err)
	}
	return location
}
