package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	gatewaydb "github.com/example/ai_gateway/gateway/db"
	"github.com/example/ai_gateway/gateway/internal/queue"
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/jackc/pgx/v5"
)

func TestUsageAggregatorUpsertsUserUsageLedger(t *testing.T) {
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
	eventTime := time.Date(2026, time.June, 16, 8, 15, 0, 0, time.UTC)
	if err := aggregator.Consume(ctx, queue.UsageEvent{
		RequestID:            "req-user-points",
		TenantID:             "tenant_demo",
		UserID:               "user_member_a",
		PlatformAPIKeyID:     "pak_demo",
		ProviderCredentialID: "provider_openai_demo",
		RouteID:              "route:provider_openai_demo:default",
		Status:               "success",
		UsageSource:          "upstream",
		PromptTokens:         100,
		CompletionTokens:     20,
		TotalTokens:          120,
		CachedTokens:         10,
		InputCostMicroyuan:   50_000,
		OutputCostMicroyuan:  30_000,
		CachedCostMicroyuan:  5_000,
		TotalCostMicroyuan:   85_000,
		Endpoint:             "/v1/chat/completions",
		OccurredAt:           eventTime,
	}); err != nil {
		t.Fatalf("Consume failed: %v", err)
	}

	var requestCount int
	var totalTokens int
	var totalCostMicroyuan int64
	if err := conn.QueryRow(ctx, `
		select request_count, total_tokens, total_cost_microyuan
		from user_usage_ledger
		where tenant_id = 'tenant_demo'
		  and user_id = 'user_member_a'
		  and bucket_start = $1
	`, eventTime.UTC().Truncate(time.Hour)).Scan(&requestCount, &totalTokens, &totalCostMicroyuan); err != nil {
		t.Fatalf("QueryRow user_usage_ledger failed: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected request_count 1, got %d", requestCount)
	}
	if totalTokens != 120 {
		t.Fatalf("expected total_tokens 120, got %d", totalTokens)
	}
	if totalCostMicroyuan != 85_000 {
		t.Fatalf("expected total_cost_microyuan 85000, got %d", totalCostMicroyuan)
	}

	points := service.PointsFromMicroyuan(totalCostMicroyuan, service.DefaultPointsDivisor)
	if fmt.Sprintf("%.2f", points) != "8.50" {
		t.Fatalf("expected 8.50 points, got %v", points)
	}
}
