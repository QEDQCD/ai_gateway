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

type sqlUsageAggregator struct {
	db store.DBTX
}

func NewUsageAggregator(db store.DBTX) queue.UsageConsumer {
	if db == nil {
		return queue.NewNoopUsageConsumer()
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
	return err
}

func usageBucketStart(event queue.UsageEvent) time.Time {
	occurredAt := event.OccurredAt.UTC()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	return occurredAt.Truncate(time.Hour)
}
