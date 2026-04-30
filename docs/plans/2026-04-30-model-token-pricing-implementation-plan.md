# 按模型计价的 Token 分类统计与费用展示 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为现有 API Gateway 补齐按模型的输入 / 输出 / 缓存 Token 分类统计、费用快照落库、聚合查询和前端展示能力，同时保持现有租户额度仍按 `total_tokens` 口径运行。

**Architecture:** 在现有 `llm_request_logs -> llm_usage_agg_hourly -> tenant_usage_ledger -> console service -> web console` 这条 usage 真值链路上直接扩字段，不新建独立 billing 子系统。单价从后端配置加载，usage recording 在请求结束时计算价格快照与费用，聚合层和控制台 API 统一消费这些真实字段，前端只做格式化展示。

**Tech Stack:** Go, PostgreSQL, pgx, Fiber, React, TypeScript, Vitest, existing usage/audit console pipeline

---

## 文件边界

### 数据库与运行时

- Modify: `gateway/db/migrations/0008_add_usage_failure_and_tenant_ledger.sql`
- Create: `gateway/db/migrations/0013_add_model_token_pricing.sql`
- Modify: `gateway/db/runtime.go`
- Modify: `gateway/db/runtime_test.go`

### 后端配置与数据结构

- Modify: `gateway/internal/config/config.go`
- Modify: `gateway/internal/config/config_test.go`
- Create: `gateway/internal/service/token_pricing.go`
- Create: `gateway/internal/service/token_pricing_test.go`
- Modify: `gateway/internal/store/models.go`
- Modify: `gateway/internal/queue/usage_publisher.go`

### Usage 写入与聚合

- Modify: `gateway/internal/service/usage_recording.go`
- Modify: `gateway/internal/service/usage_recording_test.go`
- Modify: `gateway/internal/service/usage_aggregator.go`
- Modify: `gateway/internal/service/usage_aggregator_test.go`

### 控制台查询与接口

- Modify: `gateway/internal/service/console_service.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/service/postgres_console_service_test.go`
- Modify: `gateway/internal/service/postgres_member_console_service.go`

### 前端展示

- Modify: `web/src/lib/console-api.ts`
- Modify: `web/src/pages/usage.tsx`
- Modify: `web/src/pages/member-usage.tsx`
- Modify: `web/src/pages/admin-tenants.tsx`
- Modify: `web/src/pages/audit.tsx`
- Modify: `web/src/test/router.test.tsx`

### 文档

- Modify: `README.md`
- Modify: `docs/specs/2026-04-30-model-token-pricing-design.md`

## Task 1: 扩展数据库字段与运行时种子

**Files:**
- Create: `gateway/db/migrations/0013_add_model_token_pricing.sql`
- Modify: `gateway/internal/store/models.go`
- Modify: `gateway/db/runtime_test.go`
- Modify: `gateway/db/runtime.go`

- [ ] **Step 1: 先写 migration 红灯测试**

```go
func TestApplyMigrationsAddsTokenPricingColumns(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()

	if err := gatewaydb.ApplyMigrations(ctx, pool); err != nil {
		t.Fatalf("ApplyMigrations failed: %v", err)
	}

	assertColumnExists(t, ctx, pool, "llm_request_logs", "cached_tokens")
	assertColumnExists(t, ctx, pool, "llm_request_logs", "input_price_microyuan_per_million")
	assertColumnExists(t, ctx, pool, "llm_request_logs", "output_price_microyuan_per_million")
	assertColumnExists(t, ctx, pool, "llm_request_logs", "cached_price_microyuan_per_million")
	assertColumnExists(t, ctx, pool, "llm_request_logs", "input_cost_microyuan")
	assertColumnExists(t, ctx, pool, "llm_request_logs", "output_cost_microyuan")
	assertColumnExists(t, ctx, pool, "llm_request_logs", "cached_cost_microyuan")
	assertColumnExists(t, ctx, pool, "llm_request_logs", "total_cost_microyuan")

	assertColumnExists(t, ctx, pool, "llm_usage_agg_hourly", "cached_tokens")
	assertColumnExists(t, ctx, pool, "llm_usage_agg_hourly", "input_cost_microyuan")
	assertColumnExists(t, ctx, pool, "llm_usage_agg_hourly", "output_cost_microyuan")
	assertColumnExists(t, ctx, pool, "llm_usage_agg_hourly", "cached_cost_microyuan")
	assertColumnExists(t, ctx, pool, "llm_usage_agg_hourly", "total_cost_microyuan")

	assertColumnExists(t, ctx, pool, "tenant_usage_ledger", "cached_tokens")
	assertColumnExists(t, ctx, pool, "tenant_usage_ledger", "input_cost_microyuan")
	assertColumnExists(t, ctx, pool, "tenant_usage_ledger", "output_cost_microyuan")
	assertColumnExists(t, ctx, pool, "tenant_usage_ledger", "cached_cost_microyuan")
	assertColumnExists(t, ctx, pool, "tenant_usage_ledger", "total_cost_microyuan")
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gateway/db -run TestApplyMigrationsAddsTokenPricingColumns -v`

Expected: FAIL，提示新字段不存在。

- [ ] **Step 3: 写 migration 与 store 模型最小实现**

```sql
alter table llm_request_logs
  add column cached_tokens integer not null default 0 check (cached_tokens >= 0),
  add column input_price_microyuan_per_million bigint not null default 0 check (input_price_microyuan_per_million >= 0),
  add column output_price_microyuan_per_million bigint not null default 0 check (output_price_microyuan_per_million >= 0),
  add column cached_price_microyuan_per_million bigint not null default 0 check (cached_price_microyuan_per_million >= 0),
  add column input_cost_microyuan bigint not null default 0 check (input_cost_microyuan >= 0),
  add column output_cost_microyuan bigint not null default 0 check (output_cost_microyuan >= 0),
  add column cached_cost_microyuan bigint not null default 0 check (cached_cost_microyuan >= 0),
  add column total_cost_microyuan bigint not null default 0 check (total_cost_microyuan >= 0);

alter table llm_usage_agg_hourly
  add column cached_tokens integer not null default 0 check (cached_tokens >= 0),
  add column input_cost_microyuan bigint not null default 0 check (input_cost_microyuan >= 0),
  add column output_cost_microyuan bigint not null default 0 check (output_cost_microyuan >= 0),
  add column cached_cost_microyuan bigint not null default 0 check (cached_cost_microyuan >= 0),
  add column total_cost_microyuan bigint not null default 0 check (total_cost_microyuan >= 0);

alter table tenant_usage_ledger
  add column cached_tokens integer not null default 0 check (cached_tokens >= 0),
  add column input_cost_microyuan bigint not null default 0 check (input_cost_microyuan >= 0),
  add column output_cost_microyuan bigint not null default 0 check (output_cost_microyuan >= 0),
  add column cached_cost_microyuan bigint not null default 0 check (cached_cost_microyuan >= 0),
  add column total_cost_microyuan bigint not null default 0 check (total_cost_microyuan >= 0);
```

```go
type LlmRequestLog struct {
	CachedTokens                  int32 `json:"cached_tokens"`
	InputPriceMicroyuanPerMillion int64 `json:"input_price_microyuan_per_million"`
	OutputPriceMicroyuanPerMillion int64 `json:"output_price_microyuan_per_million"`
	CachedPriceMicroyuanPerMillion int64 `json:"cached_price_microyuan_per_million"`
	InputCostMicroyuan           int64 `json:"input_cost_microyuan"`
	OutputCostMicroyuan          int64 `json:"output_cost_microyuan"`
	CachedCostMicroyuan          int64 `json:"cached_cost_microyuan"`
	TotalCostMicroyuan           int64 `json:"total_cost_microyuan"`
}
```

- [ ] **Step 4: 更新 runtime seed 与校验**

```go
insert into llm_request_logs (
	id, tenant_id, platform_api_key_id, platform_api_key_name, provider_credential_id, route_id,
	request_path, request_model, upstream_model, usage_source, usage_status, status_code, latency_ms,
	prompt_tokens, completion_tokens, cached_tokens, total_tokens,
	input_price_microyuan_per_million, output_price_microyuan_per_million, cached_price_microyuan_per_million,
	input_cost_microyuan, output_cost_microyuan, cached_cost_microyuan, total_cost_microyuan,
	request_started_at, request_completed_at
) values (
	..., 11, 7, 0, 18,
	2000000, 20000000, 500000,
	22, 140, 0, 162,
	...
)
```

- [ ] **Step 5: 跑数据库测试到绿**

Run: `go test ./gateway/db -run 'TestApplyMigrationsAddsTokenPricingColumns|TestSeedDemoData' -v`

Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add gateway/db/migrations/0013_add_model_token_pricing.sql gateway/internal/store/models.go gateway/db/runtime.go gateway/db/runtime_test.go
git commit -m "feat: add token pricing usage schema"
```

## Task 2: 增加按模型定价配置与费用计算器

**Files:**
- Modify: `gateway/internal/config/config.go`
- Modify: `gateway/internal/config/config_test.go`
- Create: `gateway/internal/service/token_pricing.go`
- Create: `gateway/internal/service/token_pricing_test.go`

- [ ] **Step 1: 先写价格解析与计算红灯测试**

```go
func TestResolveModelPricingFallsBackToDefault(t *testing.T) {
	cfg := config.Config{
		ModelTokenPricing: map[string]config.ModelTokenPrice{
			"default": {
				InputMicroyuanPerMillion:  2_000_000,
				OutputMicroyuanPerMillion: 20_000_000,
				CachedMicroyuanPerMillion: 500_000,
			},
			"qwen-flash": {
				InputMicroyuanPerMillion:  3_000_000,
				OutputMicroyuanPerMillion: 30_000_000,
				CachedMicroyuanPerMillion: 700_000,
			},
		},
	}

	resolver, err := service.NewModelPricingResolver(cfg.ModelTokenPricing)
	if err != nil {
		t.Fatalf("NewModelPricingResolver failed: %v", err)
	}

	got, err := resolver.Resolve("unknown-model")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if got.InputMicroyuanPerMillion != 2_000_000 {
		t.Fatalf("expected default input price, got %d", got.InputMicroyuanPerMillion)
	}
}

func TestComputeUsageCostsRoundsHalfUp(t *testing.T) {
	costs := service.ComputeUsageCosts(service.ModelTokenPrice{
		InputMicroyuanPerMillion: 500_000,
	}, service.TokenUsageBreakdown{
		InputTokens: 1,
	})

	if costs.InputCostMicroyuan != 1 {
		t.Fatalf("expected rounded input cost 1 microyuan, got %d", costs.InputCostMicroyuan)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gateway/internal/config ./gateway/internal/service -run 'TestResolveModelPricingFallsBackToDefault|TestComputeUsageCostsRoundsHalfUp' -v`

Expected: FAIL，提示 `ModelTokenPricing`、`NewModelPricingResolver`、`ComputeUsageCosts` 未定义。

- [ ] **Step 3: 实现配置解析与价格计算器**

```go
type ModelTokenPrice struct {
	InputMicroyuanPerMillion  int64
	OutputMicroyuanPerMillion int64
	CachedMicroyuanPerMillion int64
}

type ModelPricingResolver struct {
	prices map[string]ModelTokenPrice
}

func NewModelPricingResolver(prices map[string]config.ModelTokenPrice) (ModelPricingResolver, error) {
	if _, ok := prices["default"]; !ok {
		return ModelPricingResolver{}, errors.New("model pricing requires default entry")
	}
	normalized := make(map[string]ModelTokenPrice, len(prices))
	for model, price := range prices {
		normalized[strings.TrimSpace(model)] = ModelTokenPrice{
			InputMicroyuanPerMillion:  max(price.InputMicroyuanPerMillion, 0),
			OutputMicroyuanPerMillion: max(price.OutputMicroyuanPerMillion, 0),
			CachedMicroyuanPerMillion: max(price.CachedMicroyuanPerMillion, 0),
		}
	}
	return ModelPricingResolver{prices: normalized}, nil
}

func (r ModelPricingResolver) Resolve(model string) (ModelTokenPrice, error) {
	model = strings.TrimSpace(model)
	if price, ok := r.prices[model]; ok {
		return price, nil
	}
	price, ok := r.prices["default"]
	if !ok {
		return ModelTokenPrice{}, errors.New("default model pricing missing")
	}
	return price, nil
}
```

```go
func computeCostMicroyuan(tokens int, pricePerMillion int64) int64 {
	if tokens <= 0 || pricePerMillion <= 0 {
		return 0
	}
	return (int64(tokens)*pricePerMillion + 500_000) / 1_000_000
}
```

- [ ] **Step 4: 把环境变量接入配置**

```go
type Config struct {
	ModelTokenPricing map[string]ModelTokenPrice
}

func Load() Config {
	return Config{
		ModelTokenPricing: loadModelTokenPricing(),
	}
}

func loadModelTokenPricing() map[string]ModelTokenPrice {
	return map[string]ModelTokenPrice{
		"default": mustReadPriceTriple("GATEWAY_PRICING_DEFAULT", 2_000_000, 20_000_000, 0),
		"qwen-flash": mustReadOptionalPriceTriple("GATEWAY_PRICING_QWEN_FLASH"),
	}
}
```

- [ ] **Step 5: 跑配置与定价单测到绿**

Run: `go test ./gateway/internal/config ./gateway/internal/service -run 'TestResolveModelPricingFallsBackToDefault|TestComputeUsageCostsRoundsHalfUp|TestLoad' -v`

Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add gateway/internal/config/config.go gateway/internal/config/config_test.go gateway/internal/service/token_pricing.go gateway/internal/service/token_pricing_test.go
git commit -m "feat: add model token pricing config"
```

## Task 3: 扩展 usage recording，写入价格快照与费用

**Files:**
- Modify: `gateway/internal/service/usage_recording.go`
- Modify: `gateway/internal/service/usage_recording_test.go`
- Modify: `gateway/internal/queue/usage_publisher.go`
- Modify: `gateway/cmd/server/main.go`

- [ ] **Step 1: 先写 usage record 红灯测试**

```go
func TestUsageRecorderRecordPersistsTokenPricingSnapshot(t *testing.T) {
	pool := openUsageRecorderTestDB(t)
	recorder := service.NewUsageRecorder(pool, staticPricingResolver{
		price: service.ModelTokenPrice{
			InputMicroyuanPerMillion:  2_000_000,
			OutputMicroyuanPerMillion: 20_000_000,
			CachedMicroyuanPerMillion: 500_000,
		},
	})

	record := service.UsageRecord{
		RequestID:          "req_price_1",
		TenantID:           "tenant_demo",
		PlatformAPIKeyID:   "pak_demo",
		PlatformAPIKeyName: "Demo Key",
		ProviderCredentialID: "pc_demo",
		RouteID:            "route_demo",
		RequestPath:        "/v1/chat/completions",
		RequestModel:       "qwen-flash",
		UpstreamModel:      "qwen-flash",
		Status:             service.UsageStatusSuccess,
		UsageSource:        service.UsageSourceUpstream,
		StatusCode:         200,
		PromptTokens:       11,
		CompletionTokens:   7,
		CachedTokens:       3,
		TotalTokens:        21,
		RequestStartedAt:   time.Now().Add(-2 * time.Second),
		RequestCompletedAt: time.Now(),
	}

	if err := recorder.Record(context.Background(), record); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	var cachedTokens int
	var inputPrice int64
	var outputPrice int64
	var cachedPrice int64
	var totalCost int64
	err := pool.QueryRow(context.Background(), `
		select cached_tokens, input_price_microyuan_per_million, output_price_microyuan_per_million,
		       cached_price_microyuan_per_million, total_cost_microyuan
		from llm_request_logs
		where id = $1
	`, record.RequestID).Scan(&cachedTokens, &inputPrice, &outputPrice, &cachedPrice, &totalCost)
	if err != nil {
		t.Fatalf("QueryRow failed: %v", err)
	}

	if cachedTokens != 3 || inputPrice != 2_000_000 || outputPrice != 20_000_000 || cachedPrice != 500_000 {
		t.Fatalf("unexpected pricing snapshot: cached=%d input=%d output=%d cachedPrice=%d", cachedTokens, inputPrice, outputPrice, cachedPrice)
	}
	if totalCost == 0 {
		t.Fatal("expected total cost to be stored")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gateway/internal/service -run TestUsageRecorderRecordPersistsTokenPricingSnapshot -v`

Expected: FAIL，提示 `CachedTokens`、价格字段或构造函数签名不存在。

- [ ] **Step 3: 扩展 UsageRecord、SQL 和 publisher event**

```go
type UsageRecord struct {
	CachedTokens                   int
	InputPriceMicroyuanPerMillion  int64
	OutputPriceMicroyuanPerMillion int64
	CachedPriceMicroyuanPerMillion int64
	InputCostMicroyuan             int64
	OutputCostMicroyuan            int64
	CachedCostMicroyuan            int64
	TotalCostMicroyuan             int64
}
```

```go
type UsageEvent struct {
	CachedTokens       int   `json:"cached_tokens"`
	InputCostMicroyuan int64 `json:"input_cost_microyuan"`
	OutputCostMicroyuan int64 `json:"output_cost_microyuan"`
	CachedCostMicroyuan int64 `json:"cached_cost_microyuan"`
	TotalCostMicroyuan int64 `json:"total_cost_microyuan"`
}
```

- [ ] **Step 4: 在写库前计算费用**

```go
price, err := r.pricing.Resolve(record.RequestModel)
if err != nil {
	return err
}
costs := ComputeUsageCosts(price, TokenUsageBreakdown{
	InputTokens:  record.PromptTokens,
	OutputTokens: record.CompletionTokens,
	CachedTokens: record.CachedTokens,
})
record.InputPriceMicroyuanPerMillion = price.InputMicroyuanPerMillion
record.OutputPriceMicroyuanPerMillion = price.OutputMicroyuanPerMillion
record.CachedPriceMicroyuanPerMillion = price.CachedMicroyuanPerMillion
record.InputCostMicroyuan = costs.InputCostMicroyuan
record.OutputCostMicroyuan = costs.OutputCostMicroyuan
record.CachedCostMicroyuan = costs.CachedCostMicroyuan
record.TotalCostMicroyuan = costs.TotalCostMicroyuan
```

- [ ] **Step 5: 跑 usage recording 测试到绿**

Run: `go test ./gateway/internal/service -run 'TestUsageRecorderRecordPersistsTokenPricingSnapshot|TestUsageRecorder' -v`

Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add gateway/internal/service/usage_recording.go gateway/internal/service/usage_recording_test.go gateway/internal/queue/usage_publisher.go gateway/cmd/server/main.go
git commit -m "feat: persist token pricing snapshots"
```

## Task 4: 扩展 usage aggregator 与 tenant ledger 聚合

**Files:**
- Modify: `gateway/internal/service/usage_aggregator.go`
- Modify: `gateway/internal/service/usage_aggregator_test.go`

- [ ] **Step 1: 先写聚合红灯测试**

```go
func TestUsageAggregatorAggregatesCachedTokensAndCosts(t *testing.T) {
	pool := openUsageAggregatorTestDB(t)
	aggregator := service.NewUsageAggregator(pool)

	err := aggregator.Consume(context.Background(), queue.UsageEvent{
		RequestID:            "req_agg_1",
		TenantID:             "tenant_demo",
		PlatformAPIKeyID:     "pak_demo",
		ProviderCredentialID: "pc_demo",
		RouteID:              "route_demo",
		Endpoint:             "/v1/chat/completions",
		UsageSource:          "upstream",
		Status:               "success",
		PromptTokens:         20,
		CompletionTokens:     5,
		CachedTokens:         4,
		TotalTokens:          29,
		InputCostMicroyuan:   40,
		OutputCostMicroyuan:  100,
		CachedCostMicroyuan:  2,
		TotalCostMicroyuan:   142,
		OccurredAt:           time.Now(),
	})
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}

	var cachedTokens int
	var totalCost int64
	if err := pool.QueryRow(context.Background(), `
		select cached_tokens, total_cost_microyuan
		from llm_usage_agg_hourly
		limit 1
	`).Scan(&cachedTokens, &totalCost); err != nil {
		t.Fatalf("QueryRow aggregate failed: %v", err)
	}
	if cachedTokens != 4 || totalCost != 142 {
		t.Fatalf("unexpected aggregate values: cached=%d totalCost=%d", cachedTokens, totalCost)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gateway/internal/service -run TestUsageAggregatorAggregatesCachedTokensAndCosts -v`

Expected: FAIL，提示 SQL 或事件字段不匹配。

- [ ] **Step 3: 扩展 hourly aggregate 与 tenant ledger SQL**

```go
const upsertUsageAggregateHourlySQL = `
insert into llm_usage_agg_hourly (
	bucket_start, tenant_id, platform_api_key_id, provider_credential_id, route_id,
	request_path, usage_source, usage_status, request_count,
	prompt_tokens, completion_tokens, cached_tokens, total_tokens,
	input_cost_microyuan, output_cost_microyuan, cached_cost_microyuan, total_cost_microyuan
) values (
	$1, $2, $3, $4, $5, $6, $7, $8, 1, $9, $10, $11, $12, $13, $14, $15, $16
)
on conflict (...) do update set
	cached_tokens = llm_usage_agg_hourly.cached_tokens + excluded.cached_tokens,
	input_cost_microyuan = llm_usage_agg_hourly.input_cost_microyuan + excluded.input_cost_microyuan,
	output_cost_microyuan = llm_usage_agg_hourly.output_cost_microyuan + excluded.output_cost_microyuan,
	cached_cost_microyuan = llm_usage_agg_hourly.cached_cost_microyuan + excluded.cached_cost_microyuan,
	total_cost_microyuan = llm_usage_agg_hourly.total_cost_microyuan + excluded.total_cost_microyuan
`
```

```go
const upsertTenantUsageLedgerSQL = `
insert into tenant_usage_ledger (
	bucket_start, tenant_id, input_tokens, output_tokens, cached_tokens, total_tokens,
	request_count, success_count, failure_count, estimated_count,
	input_cost_microyuan, output_cost_microyuan, cached_cost_microyuan, total_cost_microyuan
)
select
	bucket_start, tenant_id,
	coalesce(sum(prompt_tokens), 0),
	coalesce(sum(completion_tokens), 0),
	coalesce(sum(cached_tokens), 0),
	coalesce(sum(total_tokens), 0),
	coalesce(sum(request_count), 0),
	coalesce(sum(case when usage_status = 'success' then request_count else 0 end), 0),
	coalesce(sum(case when usage_status <> 'success' then request_count else 0 end), 0),
	coalesce(sum(case when usage_source = 'estimated' then request_count else 0 end), 0),
	coalesce(sum(input_cost_microyuan), 0),
	coalesce(sum(output_cost_microyuan), 0),
	coalesce(sum(cached_cost_microyuan), 0),
	coalesce(sum(total_cost_microyuan), 0)
from llm_usage_agg_hourly
where bucket_start = $1
group by bucket_start, tenant_id
on conflict (bucket_start, tenant_id) do update set
	cached_tokens = excluded.cached_tokens,
	total_cost_microyuan = excluded.total_cost_microyuan,
	updated_at = now()
`
```

- [ ] **Step 4: 跑聚合测试到绿**

Run: `go test ./gateway/internal/service -run 'TestUsageAggregatorAggregatesCachedTokensAndCosts|TestUsageAggregator' -v`

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add gateway/internal/service/usage_aggregator.go gateway/internal/service/usage_aggregator_test.go
git commit -m "feat: aggregate token pricing metrics"
```

## Task 5: 扩展 console service 查询合同

**Files:**
- Modify: `gateway/internal/service/console_service.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `gateway/internal/service/postgres_console_service_test.go`
- Modify: `gateway/internal/service/postgres_member_console_service.go`

- [ ] **Step 1: 先写 UsageOverview / Trends / Requests / Audit 红灯测试**

```go
func TestPostgresConsoleServiceUsageOverviewIncludesTokenCosts(t *testing.T) {
	pool := openConsoleServiceTestDB(t)
	insertUsageLogWithCosts(t, pool, "tenant_demo", "qwen-flash", 11, 7, 3, 21, 22, 140, 2, 164)

	console := service.NewPostgresConsoleService(pool, nil, nil, nil, "", nil)
	payload, err := console.UsageOverview(context.Background(), service.UsageQuery{
		Window: "24h",
	})
	if err != nil {
		t.Fatalf("UsageOverview failed: %v", err)
	}

	if payload.InputTokens == "" || payload.TotalCost == "" {
		t.Fatalf("expected overview token and cost fields, got %#v", payload)
	}
}

func TestPostgresConsoleServiceUsageRequestsIncludesPerRequestCosts(t *testing.T) {
	pool := openConsoleServiceTestDB(t)
	insertUsageLogWithCosts(t, pool, "tenant_demo", "qwen-flash", 11, 7, 0, 18, 22, 140, 0, 162)

	console := service.NewPostgresConsoleService(pool, nil, nil, nil, "", nil)
	payload, err := console.UsageRequests(context.Background(), service.UsageQuery{Window: "24h", Limit: 20})
	if err != nil {
		t.Fatalf("UsageRequests failed: %v", err)
	}

	if len(payload.Items) != 1 || payload.Items[0].TotalCost == "" {
		t.Fatalf("expected request cost fields, got %#v", payload.Items)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gateway/internal/service -run 'TestPostgresConsoleServiceUsageOverviewIncludesTokenCosts|TestPostgresConsoleServiceUsageRequestsIncludesPerRequestCosts' -v`

Expected: FAIL，提示返回结构缺少字段或 SQL 未返回对应列。

- [ ] **Step 3: 扩展 service DTO**

```go
type PricingModelItem struct {
	Model        string `json:"model"`
	InputPrice   string `json:"input_price"`
	OutputPrice  string `json:"output_price"`
	CachedPrice  string `json:"cached_price"`
}

type UsageOverviewData struct {
	TotalRequests  int64              `json:"total_requests"`
	SuccessRate    string             `json:"success_rate"`
	TotalTokens    string             `json:"total_tokens"`
	InputTokens    string             `json:"input_tokens"`
	OutputTokens   string             `json:"output_tokens"`
	CachedTokens   string             `json:"cached_tokens"`
	InputCost      string             `json:"input_cost"`
	OutputCost     string             `json:"output_cost"`
	CachedCost     string             `json:"cached_cost"`
	TotalCost      string             `json:"total_cost"`
	AverageLatency string             `json:"average_latency"`
	EstimatedShare string             `json:"estimated_share"`
	PricingModels  []PricingModelItem `json:"pricing_models"`
}
```

- [ ] **Step 4: 改查询 SQL 与格式化函数**

```go
select
	count(*),
	coalesce(sum(l.total_tokens), 0),
	coalesce(sum(l.prompt_tokens), 0),
	coalesce(sum(l.completion_tokens), 0),
	coalesce(sum(l.cached_tokens), 0),
	coalesce(sum(l.input_cost_microyuan), 0),
	coalesce(sum(l.output_cost_microyuan), 0),
	coalesce(sum(l.cached_cost_microyuan), 0),
	coalesce(sum(l.total_cost_microyuan), 0),
	coalesce(sum(case when l.usage_status = 'success' then 1 else 0 end), 0),
	coalesce(sum(case when l.usage_source = 'estimated' then 1 else 0 end), 0),
	coalesce(avg(l.latency_ms), 0)
from llm_request_logs l
...
```

```go
func formatMicroyuanCurrency(value int64) string {
	return fmt.Sprintf("%.2f ￥", float64(value)/1_000_000)
}

func formatPricePerMillion(value int64) string {
	return fmt.Sprintf("%.2f ￥/M", float64(value)/1_000_000)
}
```

- [ ] **Step 5: 把 trends / requests / audit 都扩到新口径**

```go
data.Costs = append(data.Costs, UsageTrendPoint{
	Label: label,
	Value: formatMicroyuanCurrency(totalCostMicroyuan),
})
```

```go
item.InputTokens = formatLargeNumber(inputTokens)
item.OutputTokens = formatLargeNumber(outputTokens)
item.CachedTokens = formatLargeNumber(cachedTokens)
item.TotalCost = formatMicroyuanCurrency(totalCostMicroyuan)
item.InputPrice = formatPricePerMillion(inputPriceMicroyuanPerMillion)
item.OutputPrice = formatPricePerMillion(outputPriceMicroyuanPerMillion)
item.CachedPrice = formatPricePerMillion(cachedPriceMicroyuanPerMillion)
```

- [ ] **Step 6: 跑 console service 测试到绿**

Run: `go test ./gateway/internal/service -run 'TestPostgresConsoleServiceUsage|TestPostgresConsoleServiceAudit' -v`

Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add gateway/internal/service/console_service.go gateway/internal/service/postgres_console_service.go gateway/internal/service/postgres_console_service_test.go gateway/internal/service/postgres_member_console_service.go
git commit -m "feat: expose token pricing in console usage views"
```

## Task 6: 联动前端类型与页面展示

**Files:**
- Modify: `web/src/lib/console-api.ts`
- Modify: `web/src/pages/usage.tsx`
- Modify: `web/src/pages/member-usage.tsx`
- Modify: `web/src/pages/admin-tenants.tsx`
- Modify: `web/src/pages/audit.tsx`
- Modify: `web/src/test/router.test.tsx`

- [ ] **Step 1: 先写前端红灯测试**

```tsx
test("调用观测页展示输入输出缓存 Token 与费用", async () => {
  mockFetchRoutes({
    "/api/admin/usage/overview": {
      total_requests: 12,
      success_rate: "91.67%",
      total_tokens: "24,560",
      input_tokens: "18,000",
      output_tokens: "6,000",
      cached_tokens: "560",
      input_cost: "0.04 ￥",
      output_cost: "0.12 ￥",
      cached_cost: "0.00 ￥",
      total_cost: "0.16 ￥",
      average_latency: "310 ms",
      estimated_share: "0.00%",
      pricing_models: [
        {
          model: "qwen-flash",
          input_price: "2.00 ￥/M",
          output_price: "20.00 ￥/M",
          cached_price: "0.50 ￥/M",
        },
      ],
    },
    "/api/admin/usage/trends": {
      requests: [],
      tokens: [],
      success: [],
      costs: [{ label: "04-30 10:00", value: "0.16 ￥" }],
    },
    "/api/admin/usage/latency-wall?window=24h": { window_label: "最近 24 小时", buckets: [], lanes: [] },
    "/api/admin/usage/failures": { breakdown: [], recent_events: [] },
    "/api/admin/usage/requests?limit=20&offset=0": {
      total: 1,
      limit: 20,
      offset: 0,
      items: [{
        request_id: "req_1",
        tenant: "tenant_demo",
        endpoint: "/v1/chat/completions",
        model: "qwen-flash",
        status: "成功",
        input_tokens: "11",
        output_tokens: "7",
        cached_tokens: "0",
        total_tokens: "18",
        input_cost: "0.00 ￥",
        output_cost: "0.14 ￥",
        cached_cost: "0.00 ￥",
        total_cost: "0.14 ￥",
        input_price: "2.00 ￥/M",
        output_price: "20.00 ￥/M",
        cached_price: "0.50 ￥/M",
        latency: "320 ms",
        usage_source: "上游返回",
      }],
    },
  })

  renderRoute("/usage")

  expect(await screen.findByText("输入 Token")).toBeInTheDocument()
  expect(screen.getByText("总费用")).toBeInTheDocument()
  expect(screen.getByText("2.00 ￥/M")).toBeInTheDocument()
  expect(screen.getByText("0.16 ￥")).toBeInTheDocument()
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `npm --prefix web test -- --runInBand router.test.tsx -t "调用观测页展示输入输出缓存 Token 与费用"`

Expected: FAIL，提示类型缺失或页面未渲染新字段。

- [ ] **Step 3: 扩展前端 API 类型**

```ts
export type PricingModelItem = {
  model: string;
  input_price: string;
  output_price: string;
  cached_price: string;
};

export type UsageOverviewData = {
  total_requests: number;
  success_rate: string;
  total_tokens: string;
  input_tokens: string;
  output_tokens: string;
  cached_tokens: string;
  input_cost: string;
  output_cost: string;
  cached_cost: string;
  total_cost: string;
  average_latency: string;
  estimated_share: string;
  pricing_models: PricingModelItem[];
};
```

- [ ] **Step 4: 改 admin / member / audit 展示**

```tsx
<div className="stats-grid stats-grid--five">
  <StatCard label="总调用数" value={String(overview.data.total_requests)} />
  <StatCard label="输入 Token" value={`${overview.data.input_tokens} / ${overview.data.input_cost}`} />
  <StatCard label="输出 Token" value={`${overview.data.output_tokens} / ${overview.data.output_cost}`} />
  <StatCard label="缓存 Token" value={`${overview.data.cached_tokens} / ${overview.data.cached_cost}`} />
  <StatCard label="总费用" value={overview.data.total_cost} />
</div>
```

```tsx
<MetricSeriesSection title="费用趋势" points={trends.data.costs} />
```

```tsx
columns={["请求 ID", "租户", "模型", "输入", "输出", "缓存", "总 Token", "总费用", "延迟", "计量来源"]}
```

- [ ] **Step 5: 跑前端测试到绿**

Run: `npm --prefix web test -- --runInBand router.test.tsx`

Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add web/src/lib/console-api.ts web/src/pages/usage.tsx web/src/pages/member-usage.tsx web/src/pages/admin-tenants.tsx web/src/pages/audit.tsx web/src/test/router.test.tsx
git commit -m "feat: show token pricing in console UI"
```

## Task 7: 端到端验证与文档收尾

**Files:**
- Modify: `README.md`
- Modify: `docs/specs/2026-04-30-model-token-pricing-design.md`

- [ ] **Step 1: 更新 README 部署配置说明**

````md
## 模型计价配置

后端支持通过环境变量配置按模型的 Token 单价，单位为微元 / 百万 Token。

- `GATEWAY_PRICING_DEFAULT`
- `GATEWAY_PRICING_QWEN_FLASH`

示例：

```env
GATEWAY_PRICING_DEFAULT=2000000,20000000,0
GATEWAY_PRICING_QWEN_FLASH=2000000,20000000,500000
```
````

- [ ] **Step 2: 跑后端全量相关测试**

Run: `go test ./gateway/internal/... ./gateway/db/... ./gateway/cmd/server/...`

Expected: PASS

- [ ] **Step 3: 跑前端全量测试**

Run: `npm --prefix web test -- --runInBand`

Expected: PASS

- [ ] **Step 4: 启动本地服务并验证真实 usage 页面**

Run: `docker compose up -d postgres redis rabbitmq && make dev`

Expected:
- 管理端 `/usage` 能显示输入 / 输出 / 缓存 Token 与总费用
- member `/usage` 能显示当前租户费用
- `/audit` 能显示请求成本字段

- [ ] **Step 5: 用真实请求验证费用落库**

Run:

```bash
curl -sS http://127.0.0.1:30080/v1/chat/completions \
  -H "Authorization: Bearer <member-platform-key>" \
  -H "Content-Type: application/json" \
  --data '{"model":"qwen-flash","messages":[{"role":"user","content":"hello"}]}'
```

```bash
psql "$GATEWAY_DATABASE_URL" -c "
select request_model, prompt_tokens, completion_tokens, cached_tokens, total_tokens, total_cost_microyuan
from llm_request_logs
order by request_started_at desc
limit 5;"
```

Expected:
- 新请求有 `cached_tokens`
- `total_cost_microyuan > 0`
- 页面与数据库金额一致

- [ ] **Step 6: 提交**

```bash
git add README.md docs/specs/2026-04-30-model-token-pricing-design.md
git commit -m "docs: document token pricing rollout"
```

## 自检结论

- spec coverage: 已覆盖 schema、配置、usage recording、aggregator、console API、admin/member/audit UI、验证与文档。
- placeholder scan: 无 `TODO`、`TBD`、`implement later` 等占位项。
- type consistency: 统一使用 `cached_tokens`、`input_cost_microyuan`、`pricing_models`、`total_cost` 这组字段名，前后端口径一致。
