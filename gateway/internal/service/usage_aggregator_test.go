package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	gatewaydb "github.com/liwenjian/ai_gateway/gateway/db"
	"github.com/liwenjian/ai_gateway/gateway/internal/queue"
	"github.com/liwenjian/ai_gateway/gateway/internal/service"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

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
