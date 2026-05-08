package service

import (
	"context"
	"errors"
	"time"

	"github.com/example/ai_gateway/gateway/internal/store"
	"github.com/jackc/pgx/v5"
)

type DatabaseQuotaGuard struct {
	db       store.DBTX
	location *time.Location
}

type compositeQuotaGuard struct {
	guards []QuotaGuard
}

type TenantQuotaSummary struct {
	Configured          bool   `json:"configured"`
	RequestLimit        int64  `json:"request_limit"`
	RequestsUsed        int64  `json:"requests_used"`
	RequestsRemaining   int64  `json:"requests_remaining"`
	TokenLimit          int64  `json:"token_limit"`
	TokensUsed          int64  `json:"tokens_used"`
	TokensRemaining     int64  `json:"tokens_remaining"`
	CostLimitMicroyuan  int64  `json:"cost_limit_microyuan,omitempty"`
	TotalCostMicroyuan  int64  `json:"total_cost_microyuan,omitempty"`
	PeriodStart         string `json:"period_start,omitempty"`
	PeriodEnd           string `json:"period_end,omitempty"`
	ResetsAt            string `json:"resets_at"`
}

func NewCompositeQuotaGuard(guards ...QuotaGuard) QuotaGuard {
	return compositeQuotaGuard{guards: guards}
}

func (g compositeQuotaGuard) CheckTenantQuota(ctx context.Context, tenantID string) error {
	for _, guard := range g.guards {
		if guard == nil {
			continue
		}
		if err := guard.CheckTenantQuota(ctx, tenantID); err != nil {
			return err
		}
	}
	return nil
}

func NewDatabaseQuotaGuard(db store.DBTX) DatabaseQuotaGuard {
	return DatabaseQuotaGuard{
		db:       db,
		location: shanghaiLocation(),
	}
}

func (g DatabaseQuotaGuard) CheckTenantQuota(ctx context.Context, tenantID string) error {
	if g.db == nil || tenantID == "" {
		return nil
	}

	now := time.Now()
	periodStart, periodEnd, err := currentMonthlyPeriod(now, g.location)
	if err != nil {
		return err
	}

	var requestLimit int64
	var tokenLimit int64
	var costLimitMicroyuan int64
	var requestsUsed int64
	var tokensUsed int64
	var totalCostMicroyuan int64
	err = g.db.QueryRow(ctx, `
		select
			p.request_limit,
			p.token_limit,
			p.cost_limit_microyuan,
			coalesce(u.requests_used, 0),
			coalesce(u.tokens_used, 0),
			coalesce((
				select sum(l.total_cost_microyuan)
				from tenant_usage_ledger l
				where l.tenant_id = p.tenant_id
				  and l.bucket_start >= $2
				  and l.bucket_start < $3
			), 0)
		from tenant_quota_policies p
		left join tenant_quota_usage_periods u
		  on u.tenant_id = p.tenant_id
		 and u.period_start = $2
		 and u.period_end = $3
		where p.tenant_id = $1
		  and p.period_type = 'monthly'
		  and p.effective_from <= $4
		order by p.effective_from desc
		limit 1;
	`, tenantID, periodStart, periodEnd, now).Scan(&requestLimit, &tokenLimit, &costLimitMicroyuan, &requestsUsed, &tokensUsed, &totalCostMicroyuan)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if requestLimit > 0 && requestsUsed >= requestLimit {
		return ErrQuotaExceeded
	}
	if tokenLimit > 0 && tokensUsed >= tokenLimit {
		return ErrQuotaExceeded
	}
	if costLimitMicroyuan > 0 && totalCostMicroyuan >= costLimitMicroyuan {
		return ErrQuotaExceeded
	}
	return nil
}

func loadTenantQuotaSummary(ctx context.Context, db store.DBTX, tenantID string, now time.Time) (TenantQuotaSummary, error) {
	periodStart, periodEnd, err := currentMonthlyPeriod(now, shanghaiLocation())
	if err != nil {
		return TenantQuotaSummary{}, err
	}

	summary := TenantQuotaSummary{
		PeriodStart: periodStart.In(shanghaiLocation()).Format(time.RFC3339),
		PeriodEnd:   periodEnd.In(shanghaiLocation()).Format(time.RFC3339),
		ResetsAt:    periodEnd.In(shanghaiLocation()).Format(time.RFC3339),
	}
	if db == nil || tenantID == "" {
		return summary, nil
	}

	err = db.QueryRow(ctx, `
		select
			p.request_limit,
			p.token_limit,
			p.cost_limit_microyuan,
			coalesce(u.requests_used, 0),
			coalesce(u.tokens_used, 0),
			coalesce((
				select sum(l.total_cost_microyuan)
				from tenant_usage_ledger l
				where l.tenant_id = p.tenant_id
				  and l.bucket_start >= $2
				  and l.bucket_start < $3
			), 0)
		from tenant_quota_policies p
		left join tenant_quota_usage_periods u
		  on u.tenant_id = p.tenant_id
		 and u.period_start = $2
		 and u.period_end = $3
		where p.tenant_id = $1
		  and p.period_type = 'monthly'
		  and p.effective_from <= $4
		order by p.effective_from desc
		limit 1;
	`, tenantID, periodStart, periodEnd, now).Scan(
		&summary.RequestLimit,
		&summary.TokenLimit,
		&summary.CostLimitMicroyuan,
		&summary.RequestsUsed,
		&summary.TokensUsed,
		&summary.TotalCostMicroyuan,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return summary, nil
		}
		return TenantQuotaSummary{}, err
	}

	summary.Configured = true
	summary.RequestsRemaining = maxInt64(0, summary.RequestLimit-summary.RequestsUsed)
	summary.TokensRemaining = maxInt64(0, summary.TokenLimit-summary.TokensUsed)
	return summary, nil
}

func loadAggregateTenantQuotaSummary(ctx context.Context, db store.DBTX, now time.Time) (TenantQuotaSummary, error) {
	periodStart, periodEnd, err := currentMonthlyPeriod(now, shanghaiLocation())
	if err != nil {
		return TenantQuotaSummary{}, err
	}

	summary := TenantQuotaSummary{
		PeriodStart: periodStart.In(shanghaiLocation()).Format(time.RFC3339),
		PeriodEnd:   periodEnd.In(shanghaiLocation()).Format(time.RFC3339),
		ResetsAt:    periodEnd.In(shanghaiLocation()).Format(time.RFC3339),
	}
	if db == nil {
		return summary, nil
	}

	var configuredCount int64
	err = db.QueryRow(ctx, `
		with latest_policies as (
			select distinct on (tenant_id)
				tenant_id,
				request_limit,
				token_limit,
				cost_limit_microyuan
			from tenant_quota_policies
			where period_type = 'monthly'
			  and effective_from <= $1
			order by tenant_id, effective_from desc
		)
		select
			count(*),
			coalesce(sum(p.request_limit), 0),
			coalesce(sum(p.token_limit), 0),
			coalesce(sum(p.cost_limit_microyuan), 0),
			coalesce(sum(u.requests_used), 0),
			coalesce(sum(u.tokens_used), 0),
			coalesce((
				select sum(l.total_cost_microyuan)
				from tenant_usage_ledger l
				where l.bucket_start >= $2
				  and l.bucket_start < $3
			), 0)
		from latest_policies p
		left join tenant_quota_usage_periods u
		  on u.tenant_id = p.tenant_id
		 and u.period_start = $2
		 and u.period_end = $3;
	`, now, periodStart, periodEnd).Scan(
		&configuredCount,
		&summary.RequestLimit,
		&summary.TokenLimit,
		&summary.CostLimitMicroyuan,
		&summary.RequestsUsed,
		&summary.TokensUsed,
		&summary.TotalCostMicroyuan,
	)
	if err != nil {
		return TenantQuotaSummary{}, err
	}

	summary.Configured = configuredCount > 0
	summary.RequestsRemaining = maxInt64(0, summary.RequestLimit-summary.RequestsUsed)
	summary.TokensRemaining = maxInt64(0, summary.TokenLimit-summary.TokensUsed)
	return summary, nil
}

func currentMonthlyPeriod(now time.Time, loc *time.Location) (time.Time, time.Time, error) {
	if loc == nil {
		loc = shanghaiLocation()
	}
	localNow := now.In(loc)
	start := time.Date(localNow.Year(), localNow.Month(), 1, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 1, 0)
	return start.UTC(), end.UTC(), nil
}

func maxInt64(left int64, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
