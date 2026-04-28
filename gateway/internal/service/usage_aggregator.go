package service

import (
	"context"
	"time"

	"github.com/example/ai_gateway/gateway/internal/queue"
	"github.com/example/ai_gateway/gateway/internal/store"
)

const upsertUsageAggregateHourlySQL = `
insert into llm_usage_agg_hourly (
	bucket_start,
	tenant_id,
	platform_api_key_id,
	provider_credential_id,
	route_id,
	request_path,
	usage_source,
	usage_status,
	request_count,
	prompt_tokens,
	completion_tokens,
	total_tokens
) values (
	$1, $2, $3, $4, $5, $6, $7, $8, 1, $9, $10, $11
)
on conflict (
	bucket_start,
	tenant_id,
	platform_api_key_id,
	provider_credential_id,
	route_id,
	request_path,
	usage_source,
	usage_status
) do update set
	request_count = llm_usage_agg_hourly.request_count + 1,
	prompt_tokens = llm_usage_agg_hourly.prompt_tokens + excluded.prompt_tokens,
	completion_tokens = llm_usage_agg_hourly.completion_tokens + excluded.completion_tokens,
	total_tokens = llm_usage_agg_hourly.total_tokens + excluded.total_tokens
`

const upsertTenantQuotaUsagePeriodSQL = `
insert into tenant_quota_usage_periods (
	tenant_id,
	period_start,
	period_end,
	requests_used,
	tokens_used,
	last_aggregated_at
) values (
	$1, $2, $3, $4, $5, now()
)
on conflict (tenant_id, period_start) do update set
	requests_used = tenant_quota_usage_periods.requests_used + excluded.requests_used,
	tokens_used = tenant_quota_usage_periods.tokens_used + excluded.tokens_used,
	last_aggregated_at = now()
`

type sqlUsageAggregator struct {
	db store.DBTX
}

type UsageAggregator interface {
	queue.UsageConsumer
	AggregateHour(ctx context.Context, bucketStart time.Time) error
}

type noopUsageAggregator struct{}

const upsertTenantUsageLedgerSQL = `
insert into tenant_usage_ledger (
	bucket_start,
	tenant_id,
	input_tokens,
	output_tokens,
	total_tokens,
	request_count,
	success_count,
	failure_count,
	estimated_count
)
select
	bucket_start,
	tenant_id,
	coalesce(sum(prompt_tokens), 0) as input_tokens,
	coalesce(sum(completion_tokens), 0) as output_tokens,
	coalesce(sum(total_tokens), 0) as total_tokens,
	coalesce(sum(request_count), 0) as request_count,
	coalesce(sum(case when usage_status = 'success' then request_count else 0 end), 0) as success_count,
	coalesce(sum(case when usage_status <> 'success' then request_count else 0 end), 0) as failure_count,
	coalesce(sum(case when usage_source = 'estimated' then request_count else 0 end), 0) as estimated_count
from llm_usage_agg_hourly
where bucket_start = $1
group by bucket_start, tenant_id
on conflict (bucket_start, tenant_id) do update set
	input_tokens = excluded.input_tokens,
	output_tokens = excluded.output_tokens,
	total_tokens = excluded.total_tokens,
	request_count = excluded.request_count,
	success_count = excluded.success_count,
	failure_count = excluded.failure_count,
	estimated_count = excluded.estimated_count,
	updated_at = now()
`

func NewUsageAggregator(db store.DBTX) UsageAggregator {
	if db == nil {
		return noopUsageAggregator{}
	}
	return sqlUsageAggregator{db: db}
}

func (a sqlUsageAggregator) Consume(ctx context.Context, event queue.UsageEvent) error {
	bucketStart := usageBucketStart(event)
	_, err := a.db.Exec(ctx, upsertUsageAggregateHourlySQL,
		bucketStart,
		event.TenantID,
		event.PlatformAPIKeyID,
		event.ProviderCredentialID,
		event.RouteID,
		event.Endpoint,
		event.UsageSource,
		event.Status,
		max(0, event.PromptTokens),
		max(0, event.CompletionTokens),
		max(0, event.TotalTokens),
	)
	if err != nil {
		return err
	}
	occurredAt := event.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	periodStart, periodEnd, err := currentMonthlyPeriod(occurredAt, shanghaiLocation())
	if err != nil {
		return err
	}
	if _, err := a.db.Exec(ctx, upsertTenantQuotaUsagePeriodSQL,
		event.TenantID,
		periodStart,
		periodEnd,
		1,
		max(0, event.TotalTokens),
	); err != nil {
		return err
	}
	return a.AggregateHour(ctx, bucketStart)
}

func (a sqlUsageAggregator) AggregateHour(ctx context.Context, bucketStart time.Time) error {
	_, err := a.db.Exec(ctx, upsertTenantUsageLedgerSQL, bucketStart.UTC().Truncate(time.Hour))
	return err
}

func (noopUsageAggregator) Consume(context.Context, queue.UsageEvent) error {
	return nil
}

func (noopUsageAggregator) AggregateHour(context.Context, time.Time) error {
	return nil
}

func usageBucketStart(event queue.UsageEvent) time.Time {
	occurredAt := event.OccurredAt.UTC()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	return occurredAt.Truncate(time.Hour)
}
