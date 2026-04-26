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
