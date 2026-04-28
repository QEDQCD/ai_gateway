package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	gatewaydb "github.com/example/ai_gateway/gateway/db"
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/jackc/pgx/v5"
)

func TestDatabaseQuotaGuardRejectsExhaustedTenant(t *testing.T) {
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
	if _, err := conn.Exec(ctx, `
		insert into tenants (id, name, status)
		values ('tenant_quota_guard', 'Quota Guard Tenant', 'active');
		insert into tenant_quota_policies (tenant_id, period_type, request_limit, token_limit)
		values ('tenant_quota_guard', 'monthly', 1, 100);
	`); err != nil {
		t.Fatalf("seed quota policy failed: %v", err)
	}

	periodStart, periodEnd := currentShanghaiMonthWindow(t, time.Now())
	if _, err := conn.Exec(ctx, `
		insert into tenant_quota_usage_periods (tenant_id, period_start, period_end, requests_used, tokens_used)
		values ($1, $2, $3, $4, $5);
	`, "tenant_quota_guard", periodStart, periodEnd, 1, 100); err != nil {
		t.Fatalf("seed tenant_quota_usage_periods failed: %v", err)
	}

	guard := service.NewDatabaseQuotaGuard(conn)
	if err := guard.CheckTenantQuota(ctx, "tenant_quota_guard"); !errors.Is(err, service.ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded, got %v", err)
	}
}

func currentShanghaiMonthWindow(t *testing.T, now time.Time) (time.Time, time.Time) {
	t.Helper()

	location := mustLoadShanghaiLocation(t)
	localNow := now.In(location)
	start := time.Date(localNow.Year(), localNow.Month(), 1, 0, 0, 0, 0, location)
	return start.UTC(), start.AddDate(0, 1, 0).UTC()
}
