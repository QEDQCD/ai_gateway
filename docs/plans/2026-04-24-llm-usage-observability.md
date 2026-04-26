# LLM Usage Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 AI Gateway 增加一套完整的 LLM 调用观测能力，支持 Token 计量、成功/失败分类、小时聚合、管理员/租户视角查询，以及中文控制台展示页面。

**Architecture:** 实现沿用现有 Go 网关 + PostgreSQL + RabbitMQ + React 控制台结构。主请求链路同步写入调用明细和事件流，异步链路消费 usage 事件刷新小时聚合，控制台通过 `/admin/usage/*` 接口读取聚合与明细数据，前端新增“调用观测”页面并保留现有总览页摘要。

**Tech Stack:** Go 1.22, Fiber, pgx/PostgreSQL, RabbitMQ, React 18, TypeScript, Vitest.

---

## File Structure

- `gateway/db/migrations/0006_add_llm_usage_tables.sql` - 新增调用明细、事件、小时聚合三张表及索引
- `gateway/db/runtime.go` - 演示环境补充 usage 种子数据
- `gateway/db/runtime_test.go` - 校验迁移与演示数据包含新表
- `gateway/internal/domain/routing.go` - 为请求上下文补充平台 key 名称、provider、route 快照字段
- `gateway/internal/queue/usage_publisher.go` - 扩展 usage 事件结构，包含 request_id、provider、model、tokens、状态分类等
- `gateway/internal/queue/usage_consumer.go` - RabbitMQ usage 消费器，负责触发小时聚合更新
- `gateway/internal/service/usage_types.go` - 观测领域结构体、状态分类、DTO
- `gateway/internal/service/usage_recording.go` - 写入 `llm_request_logs` 与 `llm_request_events`
- `gateway/internal/service/usage_recording_test.go` - 观测写入和状态归类测试
- `gateway/internal/service/usage_aggregator.go` - 小时聚合刷新与离线重刷逻辑
- `gateway/internal/service/usage_aggregator_test.go` - 聚合逻辑测试
- `gateway/internal/service/proxy_service.go` - chat/embeddings 调用采集与 usage 解析
- `gateway/internal/service/rag_proxy_service.go` - RAG 调用采集与失败分类
- `gateway/internal/service/console_service.go` - 新增 usage 页面的接口与响应类型
- `gateway/internal/service/postgres_console_service.go` - `/admin/usage/*` 查询实现
- `gateway/internal/service/postgres_console_service_test.go` - usage 查询服务测试
- `gateway/internal/http/handlers/admin.go` - 新增 usage 相关处理器
- `gateway/internal/http/router.go` - 注册 `/admin/usage/*` 路由
- `gateway/internal/http/router_test.go` - 覆盖 usage 路由与权限行为
- `gateway/tests/integration/proxy_test.go` - 断言真实代理调用能记录 tokens、状态、usage_source
- `gateway/cmd/server/main.go` - 组装 recorder、aggregator、usage consumer
- `web/src/lib/console-api.ts` - usage 页面 API 类型与请求函数
- `web/src/app/router.tsx` - 新增“调用观测”导航
- `web/src/pages/usage.tsx` - 中文调用观测页面
- `web/src/components/console.tsx` - 复用或补充图表/明细展示组件
- `web/src/styles.css` - 新页面样式
- `web/src/test/router.test.tsx` - 前端路由冒烟测试

## Task 1: 建立调用观测数据表与基础类型

**Files:**
- Create: `gateway/db/migrations/0006_add_llm_usage_tables.sql`
- Modify: `gateway/db/runtime.go`
- Modify: `gateway/db/runtime_test.go`
- Modify: `gateway/internal/domain/routing.go`
- Create: `gateway/internal/service/usage_types.go`
- Test: `gateway/db/runtime_test.go`

- [ ] **Step 1: 先写数据库级失败测试，锁定新表和关键列**

```go
func TestApplyMigrationsCreatesUsageObservabilityTables(t *testing.T) {
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

	for _, migration := range readMigrations(t) {
		if _, err := conn.Exec(ctx, migration); err != nil {
			t.Fatalf("conn.Exec migration failed: %v", err)
		}
	}

	var count int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from information_schema.tables
		where table_name in ('llm_request_logs', 'llm_request_events', 'llm_usage_agg_hourly');
	`).Scan(&count); err != nil {
		t.Fatalf("QueryRow failed: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 observability tables, got %d", count)
	}
}
```

- [ ] **Step 2: 运行失败测试，确认当前迁移还没有这些表**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./db -run TestApplyMigrationsCreatesUsageObservabilityTables -v`
Expected: FAIL，提示缺少 `llm_request_logs` / `llm_request_events` / `llm_usage_agg_hourly`

- [ ] **Step 3: 添加迁移、扩展请求上下文并定义 usage 领域结构**

```sql
create table llm_request_logs (
	id bigserial primary key,
	request_id text not null unique,
	tenant_id text not null,
	platform_api_key_id text not null,
	platform_api_key_name text not null,
	route_id text,
	endpoint text not null,
	provider text not null,
	model text not null,
	upstream_base_url text,
	request_at timestamptz not null,
	finish_at timestamptz not null,
	latency_ms integer not null default 0,
	status text not null,
	http_status_code integer not null,
	error_code text,
	error_category text,
	error_message_digest text,
	input_tokens integer not null default 0,
	output_tokens integer not null default 0,
	total_tokens integer not null default 0,
	usage_source text not null,
	cost_amount numeric(18,6),
	cost_currency text,
	byok_provider_credential_id text,
	is_byok boolean not null default false,
	client_ip_hash text,
	user_agent_digest text,
	trace_id text,
	created_at timestamptz not null default now()
);

create table llm_request_events (
	id bigserial primary key,
	request_id text not null,
	tenant_id text not null,
	event_type text not null,
	event_time timestamptz not null,
	stage text not null,
	status text not null,
	message text not null,
	payload_json jsonb not null default '{}'::jsonb,
	created_at timestamptz not null default now()
);

create table llm_usage_agg_hourly (
	bucket_time timestamptz not null,
	tenant_id text not null,
	platform_api_key_id text not null,
	route_id text not null default '',
	provider text not null,
	model text not null,
	request_count integer not null default 0,
	success_count integer not null default 0,
	failed_count integer not null default 0,
	timeout_count integer not null default 0,
	rate_limited_count integer not null default 0,
	auth_failed_count integer not null default 0,
	input_tokens integer not null default 0,
	output_tokens integer not null default 0,
	total_tokens integer not null default 0,
	estimated_usage_count integer not null default 0,
	estimated_cost numeric(18,6) not null default 0,
	avg_latency_ms integer not null default 0,
	p50_latency_ms integer not null default 0,
	p95_latency_ms integer not null default 0,
	primary key (bucket_time, tenant_id, platform_api_key_id, route_id, provider, model)
);

create index idx_llm_request_logs_tenant_request_at on llm_request_logs (tenant_id, request_at desc);
create index idx_llm_request_logs_status_request_at on llm_request_logs (status, request_at desc);
create index idx_llm_request_logs_provider_model_request_at on llm_request_logs (provider, model, request_at desc);
```

```go
type RequestContext struct {
	TenantID             string
	PlatformAPIKeyID     string
	PlatformAPIKeyName   string
	SelectedProviderID   string
	SelectedProviderName string
	ProviderTarget       ProviderTarget
	RouteID              string
}
```

```go
type UsageStatus string

const (
	UsageStatusSuccess       UsageStatus = "success"
	UsageStatusFailed        UsageStatus = "failed"
	UsageStatusTimeout       UsageStatus = "timeout"
	UsageStatusRateLimited   UsageStatus = "rate_limited"
	UsageStatusAuthFailed    UsageStatus = "auth_failed"
	UsageStatusUpstreamError UsageStatus = "upstream_error"
)

type UsageSource string

const (
	UsageSourceUpstream  UsageSource = "upstream"
	UsageSourceEstimated UsageSource = "estimated"
)
```

- [ ] **Step 4: 让测试通过，并补演示数据种子**

```go
fmt.Sprintf(`insert into llm_request_logs
	(request_id, tenant_id, platform_api_key_id, platform_api_key_name, route_id, endpoint, provider, model, upstream_base_url, request_at, finish_at, latency_ms, status, http_status_code, input_tokens, output_tokens, total_tokens, usage_source)
select * from (values
	('req_seed_chat_001', 'tenant_alpha', 'pak_live_console', 'prod-gateway', 'route_chat_primary', '/v1/chat/completions', '%s', '%s', '%s', now() - interval '70 minutes', now() - interval '70 minutes' + interval '218 milliseconds', 218, 'success', 200, 120, 34, 154, 'upstream'),
	('req_seed_embed_001', 'tenant_beta', 'pak_batch_worker', 'batch-worker', 'route_embedding_primary', '/v1/embeddings', '%s', '%s', '%s', now() - interval '40 minutes', now() - interval '40 minutes' + interval '64 milliseconds', 64, 'rate_limited', 429, 18, 0, 18, 'estimated')
) as seed(request_id, tenant_id, platform_api_key_id, platform_api_key_name, route_id, endpoint, provider, model, upstream_base_url, request_at, finish_at, latency_ms, status, http_status_code, input_tokens, output_tokens, total_tokens, usage_source)
where not exists (select 1 from llm_request_logs);`, cfg.Provider, chatModel, cfg.ProviderBaseURL, cfg.Provider, embeddingModel, cfg.ProviderBaseURL)
```

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./db -run 'TestApplyMigrationsCreatesUsageObservabilityTables|TestSeedDemoDataEncryptsProviderSecrets' -v`
Expected: PASS，包含新表检查和种子加密检查

- [ ] **Step 5: 提交这一层**

```bash
cd /root/liwenjian/ai_gateway/gateway
git add db/migrations/0006_add_llm_usage_tables.sql db/runtime.go db/runtime_test.go internal/domain/routing.go internal/service/usage_types.go
git commit -m "feat: add usage observability schema"
```

## Task 2: 实现调用写入器、状态分类和 Token 兜底估算

**Files:**
- Create: `gateway/internal/service/usage_recording.go`
- Create: `gateway/internal/service/usage_recording_test.go`
- Modify: `gateway/internal/queue/usage_publisher.go`
- Test: `gateway/internal/service/usage_recording_test.go`

- [ ] **Step 1: 先写失败测试，锁定状态归类、usage 来源和数据库写入**

```go
func TestUsageRecorderStoresEstimatedUsageWhenUpstreamUsageMissing(t *testing.T) {
	recorder := newTestUsageRecorder(t)

	record := UsageRecord{
		RequestID:           "req_test_001",
		TenantID:            "tenant_alpha",
		PlatformAPIKeyID:    "pak_live_console",
		PlatformAPIKeyName:  "prod-gateway",
		RouteID:             "route_chat_primary",
		Endpoint:            "/v1/chat/completions",
		Provider:            "dashscope",
		Model:               "qwen-flash",
		Status:              UsageStatusSuccess,
		HTTPStatusCode:      200,
		RequestText:         `{"messages":[{"role":"user","content":"你好"}]}`,
		ResponseText:        `{"choices":[{"message":{"content":"你好，我可以帮你查看调用统计。"}}]}`,
		StartedAt:           time.Now().Add(-200 * time.Millisecond),
		FinishedAt:          time.Now(),
	}

	if err := recorder.Record(context.Background(), record); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	got := fetchUsageLog(t, recorder.db, "req_test_001")
	if got.UsageSource != string(UsageSourceEstimated) {
		t.Fatalf("expected usage_source=estimated, got %q", got.UsageSource)
	}
	if got.TotalTokens == 0 {
		t.Fatal("expected estimated total tokens > 0")
	}
}

func newTestUsageRecorder(t *testing.T) postgresUsageRecorder {
	t.Helper()

	db := openTestDB(t)
	return postgresUsageRecorder{db: db}
}

func openTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	container, dsn := startPostgresContainer(ctx, t)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatalf("ApplyMigrations failed: %v", err)
	}
	return pool
}

type usageLogRow struct {
	UsageSource string
	TotalTokens int
}

func fetchUsageLog(t *testing.T, db consoleDB, requestID string) usageLogRow {
	t.Helper()

	var row usageLogRow
	if err := db.QueryRow(context.Background(), `
		select usage_source, total_tokens
		from llm_request_logs
		where request_id = $1
	`, requestID).Scan(&row.UsageSource, &row.TotalTokens); err != nil {
		t.Fatalf("QueryRow failed: %v", err)
	}
	return row
}
```

```go
func TestClassifyUsageErrorMapsTimeoutAndRateLimit(t *testing.T) {
	if got := classifyUsageError(context.DeadlineExceeded, 504); got != "upstream_timeout" {
		t.Fatalf("expected upstream_timeout, got %q", got)
	}
	if got := classifyUsageError(nil, 429); got != "rate_limited" {
		t.Fatalf("expected rate_limited, got %q", got)
	}
}
```

- [ ] **Step 2: 运行测试，确认 recorder 还不存在**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/service -run 'TestUsageRecorderStoresEstimatedUsageWhenUpstreamUsageMissing|TestClassifyUsageErrorMapsTimeoutAndRateLimit' -v`
Expected: FAIL，提示 `UsageRecord` / `Record` / `classifyUsageError` 未定义

- [ ] **Step 3: 实现 usage 记录器、轻量估算器和扩展 usage 事件结构**

```go
type UsageRecord struct {
	RequestID          string
	TenantID           string
	PlatformAPIKeyID   string
	PlatformAPIKeyName string
	RouteID            string
	Endpoint           string
	Provider           string
	Model              string
	UpstreamBaseURL    string
	Status             UsageStatus
	HTTPStatusCode     int
	ErrorCode          string
	ErrorCategory      string
	ErrorMessageDigest string
	InputTokens        int
	OutputTokens       int
	TotalTokens        int
	UsageSource        UsageSource
	RequestText        string
	ResponseText       string
	StartedAt          time.Time
	FinishedAt         time.Time
}

type UsageRecorder interface {
	Record(ctx context.Context, record UsageRecord) error
}

func (r postgresUsageRecorder) Record(ctx context.Context, record UsageRecord) error {
	input, output, total, source := normalizeUsage(record)
	_, err := r.db.Exec(ctx, `
		insert into llm_request_logs
			(request_id, tenant_id, platform_api_key_id, platform_api_key_name, route_id, endpoint, provider, model, upstream_base_url, request_at, finish_at, latency_ms, status, http_status_code, error_code, error_category, error_message_digest, input_tokens, output_tokens, total_tokens, usage_source)
		values
			($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
	`, record.RequestID, record.TenantID, record.PlatformAPIKeyID, record.PlatformAPIKeyName, record.RouteID, record.Endpoint, record.Provider, record.Model, record.UpstreamBaseURL, record.StartedAt, record.FinishedAt, durationMilliseconds(record.FinishedAt.Sub(record.StartedAt)), string(record.Status), record.HTTPStatusCode, record.ErrorCode, record.ErrorCategory, record.ErrorMessageDigest, input, output, total, string(source))
	return err
}

func normalizeUsage(record UsageRecord) (int, int, int, UsageSource) {
	if record.TotalTokens > 0 || record.InputTokens > 0 || record.OutputTokens > 0 {
		total := record.TotalTokens
		if total == 0 {
			total = record.InputTokens + record.OutputTokens
		}
		return record.InputTokens, record.OutputTokens, total, UsageSourceUpstream
	}
	input := estimateTokens(record.RequestText)
	output := estimateTokens(record.ResponseText)
	return input, output, input + output, UsageSourceEstimated
}

func estimateTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	estimated := len([]rune(text)) / 4
	if estimated < 1 {
		return 1
	}
	return estimated
}
```

```go
type UsageEvent struct {
	RequestID            string `json:"request_id"`
	TenantID             string `json:"tenant_id"`
	PlatformAPIKeyID     string `json:"platform_api_key_id"`
	PlatformAPIKeyName   string `json:"platform_api_key_name"`
	RouteID              string `json:"route_id"`
	ProviderCredentialID string `json:"provider_credential_id"`
	Provider             string `json:"provider"`
	Model                string `json:"model"`
	Endpoint             string `json:"endpoint"`
	StatusCode           int    `json:"status_code"`
	Status               string `json:"status"`
	ErrorCategory        string `json:"error_category"`
	InputTokens          int    `json:"input_tokens"`
	OutputTokens         int    `json:"output_tokens"`
	TotalTokens          int    `json:"total_tokens"`
	UsageSource          string `json:"usage_source"`
	LatencyMS            int64  `json:"latency_ms"`
	OccurredAtUnix       int64  `json:"occurred_at_unix"`
}
```

- [ ] **Step 4: 让 recorder 测试通过**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/service -run 'TestUsageRecorderStoresEstimatedUsageWhenUpstreamUsageMissing|TestClassifyUsageErrorMapsTimeoutAndRateLimit' -v`
Expected: PASS，断言 `usage_source`、`total_tokens`、`error_category` 正确

- [ ] **Step 5: 提交这一层**

```bash
cd /root/liwenjian/ai_gateway/gateway
git add internal/service/usage_recording.go internal/service/usage_recording_test.go internal/queue/usage_publisher.go
git commit -m "feat: add usage recorder and normalization"
```

## Task 3: 给 chat、embeddings、RAG 代理链路接入观测采集

**Files:**
- Modify: `gateway/internal/service/proxy_service.go`
- Modify: `gateway/internal/service/rag_proxy_service.go`
- Modify: `gateway/internal/http/handlers/chat.go`
- Modify: `gateway/internal/http/handlers/embeddings.go`
- Modify: `gateway/internal/http/handlers/rag.go`
- Modify: `gateway/tests/integration/proxy_test.go`
- Test: `gateway/tests/integration/proxy_test.go`

- [ ] **Step 1: 先扩展集成测试，要求真实代理调用写出 tokens、状态和 usage_source**

```go
func TestChatCompletionProxyPublishesUsageWithUpstreamTokens(t *testing.T) {
	t.Parallel()

	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"stub-answer"}}],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`)
	}))
	t.Cleanup(providerServer.Close)

	app, usagePublisher := newGatewayApp(t, providerServer.URL+"/v1", providerServer.URL)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Authorization", "Bearer platform-live-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	events := usagePublisher.Events()
	if events[0].TotalTokens != 18 {
		t.Fatalf("expected total tokens 18, got %d", events[0].TotalTokens)
	}
	if events[0].UsageSource != "upstream" {
		t.Fatalf("expected usage_source upstream, got %q", events[0].UsageSource)
	}
}
```

```go
func TestEmbeddingsProxyMarksRateLimitedFailures(t *testing.T) {
	t.Parallel()

	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"too many requests"}`, http.StatusTooManyRequests)
	}))
	t.Cleanup(providerServer.Close)

	app, usagePublisher := newGatewayApp(t, providerServer.URL+"/v1", providerServer.URL)

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewBufferString(`{"model":"text-embedding-3-small","input":"hello"}`))
	req.Header.Set("Authorization", "Bearer platform-live-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}

	if got := usagePublisher.Events()[0].ErrorCategory; got != "upstream_rate_limited" {
		t.Fatalf("expected upstream_rate_limited, got %q", got)
	}
}
```

- [ ] **Step 2: 运行代理集成测试，确认会因为新字段未发布而失败**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./tests/integration -run 'TestChatCompletionProxyPublishesUsageWithUpstreamTokens|TestEmbeddingsProxyMarksRateLimitedFailures' -v`
Expected: FAIL，提示 `TotalTokens` / `UsageSource` / `ErrorCategory` 不存在或值不对

- [ ] **Step 3: 在 chat、embeddings、RAG 代理里接入 request_id、usage 解析和失败分类**

```go
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type chatProxyService struct {
	client    UpstreamChatClient
	publisher queue.UsagePublisher
	recorder  UsageRecorder
}

func (s chatProxyService) Complete(ctx context.Context, req ChatRequest, resolved any) (ChatResponse, error) {
	requestContext, ok := resolvedRequestContext(resolved)
	if !ok {
		return ChatResponse{}, unauthorizedStatusError("request context is missing")
	}

	start := time.Now()
	resp, statusCode, err := s.client.Complete(ctx, requestContext.ProviderTarget, req)
	record := buildChatUsageRecord(requestContext, req, resp, statusCode, err, start, time.Now())
	s.recordAndPublish(ctx, record, requestContext)
	if err != nil {
		return ChatResponse{}, mapProxyError(statusCode, err)
	}
	return resp, nil
}

func newRequestID() string {
	return uuid.NewString()
}

func unauthorizedStatusError(detail string) StatusError {
	return StatusError{
		Code:    http.StatusUnauthorized,
		Message: "unauthorized",
		Err:     fmt.Errorf("%w: %s", ErrUnauthorized, detail),
	}
}

func mapProxyError(statusCode int, err error) StatusError {
	return StatusError{
		Code:    defaultStatusCode(statusCode),
		Message: "upstream request failed",
		Err:     err,
	}
}

func buildChatUsageRecord(requestContext domain.RequestContext, req ChatRequest, resp ChatResponse, statusCode int, err error, startedAt, finishedAt time.Time) UsageRecord {
	requestBody, _ := json.Marshal(req)
	responseBody, _ := json.Marshal(resp)
	return UsageRecord{
		RequestID:          newRequestID(),
		TenantID:           requestContext.TenantID,
		PlatformAPIKeyID:   requestContext.PlatformAPIKeyID,
		PlatformAPIKeyName: requestContext.PlatformAPIKeyName,
		RouteID:            requestContext.RouteID,
		Endpoint:           "/v1/chat/completions",
		Provider:           requestContext.ProviderTarget.Provider,
		Model:              req.Model,
		UpstreamBaseURL:    requestContext.ProviderTarget.BaseURL,
		Status:             classifyUsageStatus(err, statusCode),
		HTTPStatusCode:     defaultStatusCode(statusCode),
		ErrorCategory:      classifyUsageError(err, statusCode),
		RequestText:        string(requestBody),
		ResponseText:       string(responseBody),
		InputTokens:        resp.Usage.PromptTokens,
		OutputTokens:       resp.Usage.CompletionTokens,
		TotalTokens:        resp.Usage.TotalTokens,
		StartedAt:          startedAt,
		FinishedAt:         finishedAt,
	}
}

func (s chatProxyService) recordAndPublish(ctx context.Context, record UsageRecord, requestContext domain.RequestContext) {
	if s.recorder != nil {
		_ = s.recorder.Record(ctx, record)
	}
	_ = s.publisher.Publish(ctx, queue.UsageEvent{
		RequestID:            record.RequestID,
		TenantID:             record.TenantID,
		PlatformAPIKeyID:     record.PlatformAPIKeyID,
		PlatformAPIKeyName:   record.PlatformAPIKeyName,
		RouteID:              record.RouteID,
		ProviderCredentialID: requestContext.SelectedProviderID,
		Provider:             record.Provider,
		Model:                record.Model,
		Endpoint:             record.Endpoint,
		StatusCode:           record.HTTPStatusCode,
		Status:               string(record.Status),
		ErrorCategory:        record.ErrorCategory,
		InputTokens:          record.InputTokens,
		OutputTokens:         record.OutputTokens,
		TotalTokens:          record.TotalTokens,
		UsageSource:          string(record.UsageSource),
		LatencyMS:            durationMilliseconds(record.FinishedAt.Sub(record.StartedAt)),
		OccurredAtUnix:       record.FinishedAt.Unix(),
	})
}

type ChatResponse struct {
	Choices []ChatChoice `json:"choices"`
	Usage   TokenUsage   `json:"usage"`
}

type EmbeddingsResponse struct {
	Data  []EmbeddingsDatum `json:"data"`
	Usage TokenUsage        `json:"usage"`
}
```

```go
func (s ragProxyService) Query(ctx context.Context, req RAGQueryRequest, resolved any) (RAGQueryResponse, error) {
	requestContext, ok := resolvedRequestContext(resolved)
	if !ok {
		return RAGQueryResponse{}, unauthorizedStatusError("request context is missing")
	}

	start := time.Now()
	response, statusCode, err := s.doRAGRequest(ctx, requestContext, req)
	record := buildRAGUsageRecord(requestContext, req, response, statusCode, err, start, time.Now())
	s.recordAndPublish(ctx, record, requestContext)
	if err != nil {
		return RAGQueryResponse{}, mapProxyError(statusCode, err)
	}
	return response, nil
}
```

- [ ] **Step 4: 跑完整代理测试，确认旧行为不回退且新字段生效**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./tests/integration -v`
Expected: PASS，原有代理测试仍通过，新增 usage 字段断言也通过

- [ ] **Step 5: 提交这一层**

```bash
cd /root/liwenjian/ai_gateway/gateway
git add internal/service/proxy_service.go internal/service/rag_proxy_service.go internal/http/handlers/chat.go internal/http/handlers/embeddings.go internal/http/handlers/rag.go tests/integration/proxy_test.go
git commit -m "feat: instrument proxy usage collection"
```

## Task 4: 实现 RabbitMQ 聚合消费器与小时聚合刷新

**Files:**
- Create: `gateway/internal/queue/usage_consumer.go`
- Create: `gateway/internal/service/usage_aggregator.go`
- Create: `gateway/internal/service/usage_aggregator_test.go`
- Modify: `gateway/cmd/server/main.go`
- Test: `gateway/internal/service/usage_aggregator_test.go`

- [ ] **Step 1: 先写聚合失败测试，锁定小时聚合和重刷行为**

```go
func TestUsageAggregatorUpsertsHourlyBucket(t *testing.T) {
	aggregator, db := newTestUsageAggregator(t)

	event := queue.UsageEvent{
		RequestID:          "req_agg_001",
		TenantID:           "tenant_alpha",
		PlatformAPIKeyID:   "pak_live_console",
		RouteID:            "route_chat_primary",
		Provider:           "dashscope",
		Model:              "qwen-flash",
		StatusCode:         200,
		Status:             "success",
		InputTokens:        10,
		OutputTokens:       6,
		TotalTokens:        16,
		UsageSource:        "upstream",
		LatencyMS:          218,
		OccurredAtUnix:     time.Now().Unix(),
	}

	if err := aggregator.Aggregate(context.Background(), event); err != nil {
		t.Fatalf("Aggregate failed: %v", err)
	}

	var requests, successes, tokens int
	if err := db.QueryRow(context.Background(), `
		select request_count, success_count, total_tokens
		from llm_usage_agg_hourly
		where tenant_id = 'tenant_alpha' and platform_api_key_id = 'pak_live_console' and model = 'qwen-flash'
	`).Scan(&requests, &successes, &tokens); err != nil {
		t.Fatalf("QueryRow failed: %v", err)
	}
	if requests != 1 || successes != 1 || tokens != 16 {
		t.Fatalf("unexpected aggregate row: requests=%d successes=%d tokens=%d", requests, successes, tokens)
	}
}

func newTestUsageAggregator(t *testing.T) (postgresUsageAggregator, consoleDB) {
	t.Helper()

	db := openTestDB(t)
	return postgresUsageAggregator{db: db}, db
}
```

- [ ] **Step 2: 运行测试，确认 aggregator 尚未实现**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/service -run TestUsageAggregatorUpsertsHourlyBucket -v`
Expected: FAIL，提示 `Aggregate` 或 `newTestUsageAggregator` 未定义

- [ ] **Step 3: 实现聚合服务、RabbitMQ consumer，并在主程序里启动**

```go
type UsageAggregator interface {
	Aggregate(ctx context.Context, event queue.UsageEvent) error
	RebuildHourly(ctx context.Context, from time.Time, to time.Time) error
}

func (a postgresUsageAggregator) Aggregate(ctx context.Context, event queue.UsageEvent) error {
	bucket := time.Unix(event.OccurredAtUnix, 0).UTC().Truncate(time.Hour)
	_, err := a.db.Exec(ctx, `
		insert into llm_usage_agg_hourly
			(bucket_time, tenant_id, platform_api_key_id, route_id, provider, model, request_count, success_count, failed_count, timeout_count, rate_limited_count, auth_failed_count, input_tokens, output_tokens, total_tokens, estimated_usage_count, avg_latency_ms, p50_latency_ms, p95_latency_ms)
		values
			($1,$2,$3,$4,$5,$6,1,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$16,$16)
		on conflict (bucket_time, tenant_id, platform_api_key_id, route_id, provider, model)
		do update set
			request_count = llm_usage_agg_hourly.request_count + 1,
			success_count = llm_usage_agg_hourly.success_count + excluded.success_count,
			failed_count = llm_usage_agg_hourly.failed_count + excluded.failed_count,
			timeout_count = llm_usage_agg_hourly.timeout_count + excluded.timeout_count,
			rate_limited_count = llm_usage_agg_hourly.rate_limited_count + excluded.rate_limited_count,
			auth_failed_count = llm_usage_agg_hourly.auth_failed_count + excluded.auth_failed_count,
			input_tokens = llm_usage_agg_hourly.input_tokens + excluded.input_tokens,
			output_tokens = llm_usage_agg_hourly.output_tokens + excluded.output_tokens,
			total_tokens = llm_usage_agg_hourly.total_tokens + excluded.total_tokens,
			estimated_usage_count = llm_usage_agg_hourly.estimated_usage_count + excluded.estimated_usage_count;
	`, bucket, event.TenantID, event.PlatformAPIKeyID, event.RouteID, event.Provider, event.Model, boolToInt(event.Status == "success"), boolToInt(event.Status != "success"), boolToInt(event.Status == "timeout"), boolToInt(event.Status == "rate_limited"), boolToInt(event.Status == "auth_failed"), event.InputTokens, event.OutputTokens, event.TotalTokens, boolToInt(event.UsageSource == "estimated"), int(event.LatencyMS))
	return err
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
```

```go
type UsageConsumer struct {
	aggregator service.UsageAggregator
}

func (c UsageConsumer) Handle(ctx context.Context, body []byte) error {
	var event UsageEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return err
	}
	return c.aggregator.Aggregate(ctx, event)
}
```

```go
usageAggregator := service.NewPostgresUsageAggregator(pool)
if strings.TrimSpace(cfg.RabbitMQURL) != "" {
	go queue.MustStartUsageConsumer(context.Background(), cfg.RabbitMQURL, "gateway_usage_events", usageAggregator)
}
```

- [ ] **Step 4: 让聚合测试通过**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/service -run TestUsageAggregatorUpsertsHourlyBucket -v`
Expected: PASS，断言小时 bucket 已 upsert

- [ ] **Step 5: 提交这一层**

```bash
cd /root/liwenjian/ai_gateway/gateway
git add internal/queue/usage_consumer.go internal/service/usage_aggregator.go internal/service/usage_aggregator_test.go cmd/server/main.go
git commit -m "feat: aggregate usage events hourly"
```

## Task 5: 实现 `/admin/usage/*` 后端查询接口

**Files:**
- Modify: `gateway/internal/service/console_service.go`
- Modify: `gateway/internal/service/postgres_console_service.go`
- Create: `gateway/internal/service/postgres_console_service_test.go`
- Modify: `gateway/internal/http/handlers/admin.go`
- Modify: `gateway/internal/http/router.go`
- Modify: `gateway/internal/http/router_test.go`
- Test: `gateway/internal/service/postgres_console_service_test.go`
- Test: `gateway/internal/http/router_test.go`

- [ ] **Step 1: 先写服务层和路由层失败测试，锁定 overview、trends、failures、requests 四类接口**

```go
func TestPostgresConsoleServiceUsageOverview(t *testing.T) {
	console := newUsageConsoleService(t)

	payload, err := console.UsageOverview(context.Background(), UsageQuery{
		From: time.Now().Add(-24 * time.Hour),
		To:   time.Now(),
	})
	if err != nil {
		t.Fatalf("UsageOverview failed: %v", err)
	}
	if payload.TotalRequests == 0 {
		t.Fatal("expected total requests > 0")
	}
	if payload.TotalTokens == 0 {
		t.Fatal("expected total tokens > 0")
	}
}

func newUsageConsoleService(t *testing.T) postgresConsoleService {
	t.Helper()

	db := openTestDB(t)
	return postgresConsoleService{db: db}
}
```

```go
type stubConsoleService struct {
	// 保留文件中已有字段，额外增加 usageOverview
	usageOverview service.UsageOverviewData
}

func (s stubConsoleService) UsageOverview(context.Context, service.UsageQuery) (service.UsageOverviewData, error) {
	return s.usageOverview, nil
}

func TestAdminUsageOverviewRouteReturnsConsoleData(t *testing.T) {
	app := apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: "example-console-user",
		ServiceAuthPassword: "change-me-console-password",
		ConsoleService: stubConsoleService{
			usageOverview: service.UsageOverviewData{
				TotalRequests:   128,
				SuccessRate:     "98.4%",
				TotalTokens:     "24,560",
				AverageLatency:  "182 ms",
				EstimatedShare:  "12.0%",
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/overview", nil)
	req.SetBasicAuth("example-console-user", "change-me-console-password")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: 运行失败测试，确认 ConsoleService 还没有 usage 接口**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/service ./internal/http -run 'TestPostgresConsoleServiceUsageOverview|TestAdminUsageOverviewRouteReturnsConsoleData' -v`
Expected: FAIL，提示 `UsageOverview` / `usageOverview` / 路由未定义

- [ ] **Step 3: 扩展 ConsoleService 类型和 Postgres 查询实现**

```go
type UsageQuery struct {
	From             time.Time
	To               time.Time
	TenantID         string
	PlatformAPIKeyID string
	Provider         string
	Model            string
	RouteID          string
	Status           string
	ErrorCategory    string
	UsageSource      string
	Metric           string
	Dimension        string
}

type UsageOverviewData struct {
	TotalRequests  int64  `json:"total_requests"`
	SuccessRate    string `json:"success_rate"`
	TotalTokens    string `json:"total_tokens"`
	AverageLatency string `json:"average_latency"`
	EstimatedShare string `json:"estimated_share"`
}

type UsageTrendPoint struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type UsageTrendData struct {
	Requests []UsageTrendPoint `json:"requests"`
	Tokens   []UsageTrendPoint `json:"tokens"`
	Success  []UsageTrendPoint `json:"success"`
}

type UsageFailureBucket struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type UsageFailureData struct {
	Breakdown    []UsageFailureBucket `json:"breakdown"`
	RecentEvents []string             `json:"recent_events"`
}

type UsageRequestItem struct {
	RequestID   string `json:"request_id"`
	Tenant      string `json:"tenant"`
	Endpoint    string `json:"endpoint"`
	Model       string `json:"model"`
	Status      string `json:"status"`
	TotalTokens string `json:"total_tokens"`
	Latency     string `json:"latency"`
	UsageSource string `json:"usage_source"`
}

type UsageRequestsPageData struct {
	Items []UsageRequestItem `json:"items"`
}

type ConsoleService interface {
	Overview(ctx context.Context) (OverviewPageData, error)
	APIKeys(ctx context.Context) (APIKeysPageData, error)
	Routes(ctx context.Context) (RoutesPageData, error)
	Playground(ctx context.Context) (PlaygroundPageData, error)
	RunPlayground(ctx context.Context, req PlaygroundRunRequest) (PlaygroundRunResponse, error)
	KnowledgeBases(ctx context.Context) (KnowledgeBasesPageData, error)
	Audit(ctx context.Context) (AuditPageData, error)
	UsageOverview(ctx context.Context, query UsageQuery) (UsageOverviewData, error)
	UsageTrends(ctx context.Context, query UsageQuery) (UsageTrendData, error)
	UsageFailures(ctx context.Context, query UsageQuery) (UsageFailureData, error)
	UsageRequests(ctx context.Context, query UsageQuery) (UsageRequestsPageData, error)
}
```

```go
func (s postgresConsoleService) UsageOverview(ctx context.Context, query UsageQuery) (UsageOverviewData, error) {
	var totalRequests, totalTokens, estimatedCount int64
	var successRate, avgLatency float64

	err := s.db.QueryRow(ctx, `
		select
			coalesce(sum(request_count), 0),
			coalesce(sum(total_tokens), 0),
			coalesce(avg(case when request_count > 0 then success_count * 100.0 / request_count else 0 end), 0),
			coalesce(avg(avg_latency_ms), 0),
			coalesce(sum(estimated_usage_count), 0)
		from llm_usage_agg_hourly
		where bucket_time >= $1 and bucket_time < $2
	`, query.From, query.To).Scan(&totalRequests, &totalTokens, &successRate, &avgLatency, &estimatedCount)
	if err != nil {
		return UsageOverviewData{}, err
	}

	estimatedShare := 0.0
	if totalRequests > 0 {
		estimatedShare = float64(estimatedCount) * 100 / float64(totalRequests)
	}

	return UsageOverviewData{
		TotalRequests:  totalRequests,
		SuccessRate:    formatPercentage(successRate),
		TotalTokens:    formatLargeNumber(int(totalTokens)),
		AverageLatency: fmt.Sprintf("%d ms", int(math.Round(avgLatency))),
		EstimatedShare: formatPercentage(estimatedShare),
	}, nil
}
```

- [ ] **Step 4: 注册路由和处理器，让测试通过**

```go
func ConsoleUsageOverview(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := console.UsageOverview(c.UserContext(), parseUsageQuery(c))
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func parseUsageQuery(c *fiber.Ctx) service.UsageQuery {
	return service.UsageQuery{
		TenantID:         c.Query("tenant_id"),
		PlatformAPIKeyID: c.Query("platform_api_key_id"),
		Provider:         c.Query("provider"),
		Model:            c.Query("model"),
		RouteID:          c.Query("route_id"),
		Status:           c.Query("status"),
		ErrorCategory:    c.Query("error_category"),
		UsageSource:      c.Query("usage_source"),
	}
}
```

```go
admin.Get("/usage/overview", handlers.ConsoleUsageOverview(deps.ConsoleService))
admin.Get("/usage/trends", handlers.ConsoleUsageTrends(deps.ConsoleService))
admin.Get("/usage/failures", handlers.ConsoleUsageFailures(deps.ConsoleService))
admin.Get("/usage/requests", handlers.ConsoleUsageRequests(deps.ConsoleService))
```

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./internal/service ./internal/http -v`
Expected: PASS，usage 查询与路由测试都通过

- [ ] **Step 5: 提交这一层**

```bash
cd /root/liwenjian/ai_gateway/gateway
git add internal/service/console_service.go internal/service/postgres_console_service.go internal/service/postgres_console_service_test.go internal/http/handlers/admin.go internal/http/router.go internal/http/router_test.go
git commit -m "feat: add admin usage query endpoints"
```

## Task 6: 新增前端“调用观测”页面并接入后端接口

**Files:**
- Modify: `web/src/lib/console-api.ts`
- Modify: `web/src/app/router.tsx`
- Create: `web/src/pages/usage.tsx`
- Modify: `web/src/components/console.tsx`
- Modify: `web/src/styles.css`
- Modify: `web/src/test/router.test.tsx`
- Test: `web/src/test/router.test.tsx`

- [ ] **Step 1: 先写前端路由失败测试，要求能看到“调用观测”导航和页面标题**

```tsx
test("renders usage observability route", async () => {
  render(<RouterProvider router={createTestRouter(["/usage"])} />);
  expect(await screen.findByText("调用观测")).toBeInTheDocument();
  expect(screen.getByText("查看 Token、成功率、失败分类与调用明细。")).toBeInTheDocument();
});
```

- [ ] **Step 2: 运行测试，确认 `/usage` 路由还不存在**

Run: `cd /root/liwenjian/ai_gateway/web && npm test -- --runInBand router.test.tsx`
Expected: FAIL，提示找不到 “调用观测” 文本或 `/usage` 路由未注册

- [ ] **Step 3: 扩展 console API 类型、路由配置和 usage 页面**

```ts
export type UsageOverviewData = {
  total_requests: number;
  success_rate: string;
  total_tokens: string;
  average_latency: string;
  estimated_share: string;
};

export type UsageFailureBucket = {
  label: string;
  value: string;
};

export type UsageFailureData = {
  breakdown: UsageFailureBucket[];
  recent_events: string[];
};

export type UsageRequestsPageData = {
  items: Array<{
    request_id: string;
    tenant: string;
    endpoint: string;
    model: string;
    status: string;
    total_tokens: string;
    latency: string;
    usage_source: string;
  }>;
};

export function getUsageOverview() {
  return requestJson<UsageOverviewData>("/api/admin/usage/overview");
}

export function getUsageFailures() {
  return requestJson<UsageFailureData>("/api/admin/usage/failures");
}

export function getUsageRequests() {
  return requestJson<UsageRequestsPageData>("/api/admin/usage/requests");
}
```

```tsx
{
  path: "/usage",
  label: "调用观测",
  title: "调用观测",
  description: "查看 Token、成功率、失败分类与调用明细。",
  element: <UsagePage />,
}
```

```tsx
export function UsagePage() {
  const overview = useRemoteData(getUsageOverview);
  const failures = useRemoteData(getUsageFailures);
  const requests = useRemoteData(getUsageRequests);

  if (overview.loading || failures.loading || requests.loading) {
    return <LoadingSection text="正在加载调用观测数据..." />;
  }
  if (overview.error || failures.error || requests.error || !overview.data || !failures.data || !requests.data) {
    return <ErrorSection message="调用观测数据加载失败。" />;
  }

  return (
    <div className="page-grid">
      <div className="stats-grid">
        <StatCard label="总调用数" value={String(overview.data.total_requests)} />
        <StatCard label="成功率" value={overview.data.success_rate} />
        <StatCard label="总 Token" value={overview.data.total_tokens} />
        <StatCard label="平均延迟" value={overview.data.average_latency} />
        <StatCard label="估算占比" value={overview.data.estimated_share} />
      </div>
      <div className="two-column-grid">
        <SummarySection title="失败分类" items={failures.data.breakdown.map((item) => `${item.label}：${item.value}`)} />
        <SummarySection title="最近事件" items={failures.data.recent_events} />
      </div>
      <section className="section-card">
        <h2>调用明细</h2>
        <DataTable
          columns={["请求 ID", "租户", "端点", "模型", "状态", "总 Token", "延迟", "计量来源"]}
          rows={requests.data.items.map((item) => [
            item.request_id,
            item.tenant,
            item.endpoint,
            item.model,
            item.status,
            item.total_tokens,
            item.latency,
            item.usage_source,
          ])}
        />
      </section>
    </div>
  );
}
```

- [ ] **Step 4: 让前端测试通过，并做一次构建校验**

Run: `cd /root/liwenjian/ai_gateway/web && npm test -- --runInBand router.test.tsx && npm run build`
Expected: PASS，路由测试通过且 Vite build 成功

- [ ] **Step 5: 提交这一层**

```bash
cd /root/liwenjian/ai_gateway/web
git add src/lib/console-api.ts src/app/router.tsx src/pages/usage.tsx src/components/console.tsx src/styles.css src/test/router.test.tsx
git commit -m "feat: add usage observability page"
```

## Task 7: 做一轮端到端验证并补总览页摘要

**Files:**
- Modify: `gateway/internal/service/postgres_console_service.go`
- Modify: `web/src/pages/dashboard.tsx`
- Modify: `gateway/tests/integration/proxy_test.go`
- Test: `gateway/tests/integration/proxy_test.go`
- Test: `web/src/test/router.test.tsx`

- [ ] **Step 1: 先写最终行为断言，要求总览页复用 usage 核心摘要且后端可查到请求明细**

```go
func TestChatCompletionProxyPersistsUsageLog(t *testing.T) {
	t.Parallel()

	app, usagePublisher, pool := newDatabaseBackedGatewayApp(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"qwen-flash","messages":[{"role":"user","content":"统计一下今天的 token"}]}`))
	req.Header.Set("Authorization", "Bearer platform-live-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var count int
	if err := pool.QueryRow(context.Background(), `select count(*) from llm_request_logs`).Scan(&count); err != nil {
		t.Fatalf("QueryRow failed: %v", err)
	}
	if count == 0 {
		t.Fatal("expected usage logs to be persisted")
	}
	if len(usagePublisher.Events()) == 0 {
		t.Fatal("expected usage events to be published")
	}
}
```

```tsx
expect(await screen.findByText("总 Token")).toBeInTheDocument();
expect(screen.getByText("成功率")).toBeInTheDocument();
```

- [ ] **Step 2: 运行最终验证测试，确认还需要补 dashboard 摘要和数据库写入链路**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./tests/integration -run TestChatCompletionProxyPersistsUsageLog -v`
Expected: 如果链路没打通会 FAIL；修完后应 PASS

- [ ] **Step 3: 把 usage 摘要回填到总览页，并收口接口文案**

```go
routeHealthRows, err := s.collectTableRows(ctx, `...`)
usageOverview, err := s.UsageOverview(ctx, UsageQuery{
	From: time.Now().Add(-24 * time.Hour),
	To:   time.Now(),
})

return OverviewPageData{
	Stats: []KeyMetric{
		{Label: "24 小时请求量", Value: formatLargeNumber(int(usageOverview.TotalRequests))},
		{Label: "成功率", Value: usageOverview.SuccessRate},
		{Label: "总 Token", Value: usageOverview.TotalTokens},
		{Label: "平均延迟", Value: usageOverview.AverageLatency},
	},
	...
}
```

```tsx
<StatCard key={item.label} label={item.label} value={item.value} />
```

- [ ] **Step 4: 执行完整验证**

Run: `cd /root/liwenjian/ai_gateway/gateway && go test ./...`
Expected: PASS

Run: `cd /root/liwenjian/ai_gateway/web && npm test -- --runInBand && npm run build`
Expected: PASS

- [ ] **Step 5: 提交收尾**

```bash
cd /root/liwenjian/ai_gateway
git add gateway/internal/service/postgres_console_service.go gateway/tests/integration/proxy_test.go web/src/pages/dashboard.tsx web/src/test/router.test.tsx
git commit -m "feat: finalize usage observability rollout"
```
