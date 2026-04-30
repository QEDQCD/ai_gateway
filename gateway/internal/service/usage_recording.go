package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/example/ai_gateway/gateway/internal/domain"
	"github.com/example/ai_gateway/gateway/internal/queue"
	"github.com/example/ai_gateway/gateway/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"cached_tokens"`
}

type UsageRecord struct {
	RequestID            string
	TenantID             string
	UserID               string
	PlatformAPIKeyID     string
	PlatformAPIKeyName   string
	ProviderCredentialID string
	Provider             string
	RouteID              string
	RequestPath          string
	RequestModel         string
	UpstreamModel        string
	Status               UsageStatus
	UsageSource          UsageSource
	StatusCode           int
	LatencyMS            int64
	FirstTokenLatencyMS  int64
	PromptTokens         int
	CompletionTokens     int
	TotalTokens          int
	CachedTokens         int
	PriceSnapshot        ModelTokenPrice
	CostSnapshot         UsageCosts
	ErrorCode            string
	ErrorMessage         string
	RequestStartedAt     time.Time
	RequestCompletedAt   time.Time
}

type UsageRecorder interface {
	Record(ctx context.Context, record UsageRecord) error
	RecordFailure(ctx context.Context, input UsageFailureInput) error
	RecordPublishFailure(ctx context.Context, record UsageRecord, publishErr error) error
	RecordEvent(ctx context.Context, record UsageRecord, eventType string, detail string) error
}

type usageEventRecordHydrator interface {
	hydrateUsageEventRecord(record *UsageRecord) error
}

type noopUsageRecorder struct{}

type sqlUsageRecorder struct {
	db              usageRecordStore
	pricingResolver ModelPricingResolver
}

type usageRecordStore interface {
	store.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

const usageRecordTimeout = 2 * time.Second

const insertUsageRecordSQL = `
insert into llm_request_logs (
	id,
	tenant_id,
	platform_api_key_id,
	platform_api_key_name,
	provider_credential_id,
	route_id,
	request_path,
	request_model,
	upstream_model,
	usage_source,
	usage_status,
	status_code,
	latency_ms,
	first_token_latency_ms,
	prompt_tokens,
	completion_tokens,
	total_tokens,
	cached_tokens,
	input_price_microyuan_per_million,
	output_price_microyuan_per_million,
	cached_price_microyuan_per_million,
	input_cost_microyuan,
	output_cost_microyuan,
	cached_cost_microyuan,
	total_cost_microyuan,
	error_code,
	error_message,
	request_started_at,
	request_completed_at
) values (
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
	$11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
	$21, $22, $23, $24, $25, $26, $27, $28, $29
)`

const insertUsagePublishFailureEventSQL = `
insert into llm_request_events (
	id,
	request_log_id,
	tenant_id,
	event_type,
	usage_source,
	usage_status,
	status_code,
	detail,
	created_at
) values (
	$1, $2, $3, $4, $5, $6, $7, $8, $9
)`

const insertUsageLifecycleEventSQL = `
insert into llm_request_events (
	id,
	request_log_id,
	tenant_id,
	event_type,
	usage_source,
	usage_status,
	status_code,
	detail,
	created_at
) values (
	$1, $2, $3, $4, $5, $6, $7, $8, $9
)`

const insertUsageFailureSQL = `
insert into llm_request_failures (
	id,
	request_log_id,
	tenant_id,
	user_id,
	platform_api_key_id,
	failure_stage,
	error_category,
	status_code,
	retryable,
	user_message,
	internal_message_digest,
	created_at
) values (
	$1, $2, $3, nullif($4, ''), $5, $6, $7, $8, $9, $10, $11, $12
)`

func NewNoopUsageRecorder() UsageRecorder {
	return noopUsageRecorder{}
}

func NewUsageRecorder(db usageRecordStore, pricingResolver ModelPricingResolver) UsageRecorder {
	if db == nil {
		return noopUsageRecorder{}
	}
	return sqlUsageRecorder{
		db:              db,
		pricingResolver: pricingResolver,
	}
}

func (noopUsageRecorder) Record(context.Context, UsageRecord) error {
	return nil
}

func (noopUsageRecorder) RecordFailure(context.Context, UsageFailureInput) error {
	return nil
}

func (noopUsageRecorder) RecordPublishFailure(context.Context, UsageRecord, error) error {
	return nil
}

func (noopUsageRecorder) RecordEvent(context.Context, UsageRecord, string, string) error {
	return nil
}

func (noopUsageRecorder) hydrateUsageEventRecord(*UsageRecord) error {
	return nil
}

func (r sqlUsageRecorder) Record(ctx context.Context, record UsageRecord) error {
	record.ensureDefaults()
	if err := hydrateUsagePricing(&record, r.pricingResolver); err != nil {
		return err
	}
	recordCtx, cancel := newUsageRecordContext(ctx)
	defer cancel()

	eventType, detail := lifecycleEventForRecord(record)
	tx, err := r.db.Begin(recordCtx)
	if err != nil {
		return err
	}
	if err := insertUsageRecord(recordCtx, tx, record); err != nil {
		_ = tx.Rollback(recordCtx)
		return err
	}
	if err := insertUsageLifecycleEvent(recordCtx, tx, record, eventType, detail); err != nil {
		_ = tx.Rollback(recordCtx)
		return err
	}
	if isFailureStatus(record.Status) {
		if err := insertUsageFailure(recordCtx, tx, usageFailureInputFromRecord(record)); err != nil {
			_ = tx.Rollback(recordCtx)
			return err
		}
	}
	return tx.Commit(recordCtx)
}

func insertUsageRecord(ctx context.Context, db store.DBTX, record UsageRecord) error {
	_, err := db.Exec(ctx, insertUsageRecordSQL,
		record.RequestID,
		record.TenantID,
		record.PlatformAPIKeyID,
		record.PlatformAPIKeyName,
		record.ProviderCredentialID,
		record.RouteID,
		record.RequestPath,
		record.RequestModel,
		record.UpstreamModel,
		string(record.UsageSource),
		string(record.Status),
		record.StatusCode,
		record.LatencyMS,
		record.FirstTokenLatencyMS,
		record.PromptTokens,
		record.CompletionTokens,
		record.TotalTokens,
		record.CachedTokens,
		record.PriceSnapshot.InputMicroyuanPerMillion,
		record.PriceSnapshot.OutputMicroyuanPerMillion,
		record.PriceSnapshot.CachedMicroyuanPerMillion,
		record.CostSnapshot.InputCostMicroyuan,
		record.CostSnapshot.OutputCostMicroyuan,
		record.CostSnapshot.CachedCostMicroyuan,
		record.CostSnapshot.TotalCostMicroyuan,
		record.ErrorCode,
		record.ErrorMessage,
		record.RequestStartedAt,
		record.RequestCompletedAt,
	)
	return err
}

func insertUsageLifecycleEvent(ctx context.Context, db store.DBTX, record UsageRecord, eventType string, detail string) error {
	_, err := db.Exec(ctx, insertUsageLifecycleEventSQL,
		uuid.NewString(),
		record.RequestID,
		record.TenantID,
		eventType,
		string(record.UsageSource),
		string(record.Status),
		record.StatusCode,
		detail,
		record.RequestCompletedAt,
	)
	return err
}

func (r sqlUsageRecorder) RecordFailure(ctx context.Context, input UsageFailureInput) error {
	input = normalizeUsageFailureInput(input)
	_, err := r.db.Exec(ctx, insertUsageFailureSQL,
		input.RequestID,
		input.RequestLogID,
		input.TenantID,
		input.UserID,
		input.PlatformAPIKeyID,
		input.FailureStage,
		input.ErrorCategory,
		input.StatusCode,
		input.Retryable,
		input.UserMessage,
		digestFailureMessage(input.InternalMessage),
		time.Now().UTC(),
	)
	return err
}

func insertUsageFailure(ctx context.Context, db store.DBTX, input UsageFailureInput) error {
	input = normalizeUsageFailureInput(input)
	_, err := db.Exec(ctx, insertUsageFailureSQL,
		input.RequestID,
		input.RequestLogID,
		input.TenantID,
		input.UserID,
		input.PlatformAPIKeyID,
		input.FailureStage,
		input.ErrorCategory,
		input.StatusCode,
		input.Retryable,
		input.UserMessage,
		digestFailureMessage(input.InternalMessage),
		time.Now().UTC(),
	)
	return err
}

func (r sqlUsageRecorder) RecordPublishFailure(ctx context.Context, record UsageRecord, publishErr error) error {
	record.ensureDefaults()
	detail := "usage publish failed"
	if publishErr != nil {
		detail += ": " + publishErr.Error()
	}
	_, err := r.db.Exec(ctx, insertUsagePublishFailureEventSQL,
		uuid.NewString(),
		record.RequestID,
		record.TenantID,
		"usage_publish_failed",
		string(record.UsageSource),
		string(record.Status),
		record.StatusCode,
		detail,
		record.RequestCompletedAt,
	)
	return err
}

func (r sqlUsageRecorder) RecordEvent(ctx context.Context, record UsageRecord, eventType string, detail string) error {
	record.ensureDefaults()
	recordCtx, cancel := newUsageRecordContext(ctx)
	defer cancel()

	_, err := r.db.Exec(recordCtx, insertUsageLifecycleEventSQL,
		uuid.NewString(),
		record.RequestID,
		record.TenantID,
		strings.TrimSpace(eventType),
		string(record.UsageSource),
		string(record.Status),
		record.StatusCode,
		strings.TrimSpace(detail),
		record.RequestCompletedAt,
	)
	return err
}

func (r sqlUsageRecorder) hydrateUsageEventRecord(record *UsageRecord) error {
	return hydrateUsagePricing(record, r.pricingResolver)
}

func newUsageRecordContext(ctx context.Context) (context.Context, context.CancelFunc) {
	baseCtx := context.Background()
	if ctx != nil {
		baseCtx = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(baseCtx, usageRecordTimeout)
}

func hydrateUsagePricing(record *UsageRecord, resolver ModelPricingResolver) error {
	price, err := resolver.Resolve(record.RequestModel)
	if err != nil {
		return err
	}

	costs, err := ComputeUsageCosts(price, TokenUsageBreakdown{
		InputTokens:  int64(record.PromptTokens),
		OutputTokens: int64(record.CompletionTokens),
		CachedTokens: int64(record.CachedTokens),
	})
	if err != nil {
		return err
	}

	record.PriceSnapshot = price
	record.CostSnapshot = costs
	return nil
}

func NewChatUsageRecord(
	requestID string,
	requestContext domain.RequestContext,
	req ChatRequest,
	resp ChatResponse,
	statusCode int,
	startedAt time.Time,
	completedAt time.Time,
	err error,
) UsageRecord {
	usage, usageSource := normalizeUsage(resp.Usage, estimateChatUsage(req, resp))
	return newUsageRecord(
		requestID,
		requestContext,
		"/v1/chat/completions",
		req.Model,
		firstNonEmpty(strings.TrimSpace(resp.Model), strings.TrimSpace(req.Model)),
		usage,
		usageSource,
		statusFromHTTPStatus(defaultStatusCode(statusCode)),
		defaultStatusCode(statusCode),
		durationMilliseconds(completedAt.Sub(startedAt)),
		startedAt,
		completedAt,
		err,
	)
}

func NewEmbeddingsUsageRecord(
	requestID string,
	requestContext domain.RequestContext,
	req EmbeddingsRequest,
	resp EmbeddingsResponse,
	statusCode int,
	startedAt time.Time,
	completedAt time.Time,
	err error,
) UsageRecord {
	usage, usageSource := normalizeUsage(resp.Usage, estimateEmbeddingsUsage(req))
	return newUsageRecord(
		requestID,
		requestContext,
		"/v1/embeddings",
		req.Model,
		firstNonEmpty(strings.TrimSpace(resp.Model), strings.TrimSpace(req.Model)),
		usage,
		usageSource,
		statusFromHTTPStatus(defaultStatusCode(statusCode)),
		defaultStatusCode(statusCode),
		durationMilliseconds(completedAt.Sub(startedAt)),
		startedAt,
		completedAt,
		err,
	)
}

func NewFailureUsageRecord(
	requestID string,
	requestContext domain.RequestContext,
	endpoint string,
	statusCode int,
	startedAt time.Time,
	completedAt time.Time,
	err error,
) UsageRecord {
	return newUsageRecord(
		requestID,
		requestContext,
		endpoint,
		"",
		"",
		TokenUsage{},
		UsageSourceEstimated,
		statusFromHTTPStatus(defaultStatusCode(statusCode)),
		defaultStatusCode(statusCode),
		durationMilliseconds(completedAt.Sub(startedAt)),
		startedAt,
		completedAt,
		err,
	)
}

func (r UsageRecord) UsageEvent() queue.UsageEvent {
	return queue.UsageEvent{
		RequestID:            r.RequestID,
		TenantID:             r.TenantID,
		PlatformAPIKeyID:     r.PlatformAPIKeyID,
		PlatformAPIKeyName:   r.PlatformAPIKeyName,
		ProviderCredentialID: r.ProviderCredentialID,
		RouteID:              r.RouteID,
		Provider:             r.Provider,
		Model:                r.RequestModel,
		Status:               string(r.Status),
		UsageSource:          string(r.UsageSource),
		PromptTokens:         r.PromptTokens,
		CompletionTokens:     r.CompletionTokens,
		TotalTokens:          r.TotalTokens,
		CachedTokens:         r.CachedTokens,
		Endpoint:             r.RequestPath,
		StatusCode:           r.StatusCode,
		LatencyMS:            r.LatencyMS,
		InputCostMicroyuan:   r.CostSnapshot.InputCostMicroyuan,
		OutputCostMicroyuan:  r.CostSnapshot.OutputCostMicroyuan,
		CachedCostMicroyuan:  r.CostSnapshot.CachedCostMicroyuan,
		TotalCostMicroyuan:   r.CostSnapshot.TotalCostMicroyuan,
		OccurredAt:           r.RequestCompletedAt,
	}
}

func newUsageRecord(
	requestID string,
	requestContext domain.RequestContext,
	endpoint string,
	requestModel string,
	upstreamModel string,
	usage TokenUsage,
	usageSource UsageSource,
	status UsageStatus,
	statusCode int,
	latencyMS int64,
	startedAt time.Time,
	completedAt time.Time,
	err error,
) UsageRecord {
	record := UsageRecord{
		RequestID:            requestID,
		TenantID:             requestContext.TenantID,
		UserID:               requestContext.UserID,
		PlatformAPIKeyID:     requestContext.PlatformAPIKeyID,
		PlatformAPIKeyName:   requestContext.PlatformAPIKeyName,
		ProviderCredentialID: requestContext.SelectedProviderID,
		Provider:             requestContext.ProviderTarget.Provider,
		RouteID:              requestContext.RouteID,
		RequestPath:          endpoint,
		RequestModel:         strings.TrimSpace(requestModel),
		UpstreamModel:        strings.TrimSpace(upstreamModel),
		Status:               status,
		UsageSource:          usageSource,
		StatusCode:           statusCode,
		LatencyMS:            latencyMS,
		PromptTokens:         usage.PromptTokens,
		CompletionTokens:     usage.CompletionTokens,
		TotalTokens:          usage.TotalTokens,
		CachedTokens:         usage.CachedTokens,
		RequestStartedAt:     startedAt,
		RequestCompletedAt:   completedAt,
	}
	if err != nil {
		record.ErrorMessage = err.Error()
	}
	record.ensureDefaults()
	return record
}

func (r *UsageRecord) ensureDefaults() {
	if strings.TrimSpace(r.RequestID) == "" {
		r.RequestID = uuid.NewString()
	}
	if strings.TrimSpace(r.UpstreamModel) == "" {
		r.UpstreamModel = r.RequestModel
	}
	if r.TotalTokens == 0 {
		r.TotalTokens = r.PromptTokens + r.CompletionTokens
	}
	if r.RequestStartedAt.IsZero() {
		r.RequestStartedAt = time.Now().UTC()
	}
	if r.RequestCompletedAt.IsZero() || r.RequestCompletedAt.Before(r.RequestStartedAt) {
		r.RequestCompletedAt = r.RequestStartedAt
	}
	if r.LatencyMS <= 0 {
		r.LatencyMS = durationMilliseconds(r.RequestCompletedAt.Sub(r.RequestStartedAt))
	}
	if r.FirstTokenLatencyMS < 0 {
		r.FirstTokenLatencyMS = 0
	}
	if r.CachedTokens < 0 {
		r.CachedTokens = 0
	}
}

func lifecycleEventForRecord(record UsageRecord) (string, string) {
	if record.Status == UsageStatusSuccess {
		if record.UsageSource == UsageSourceEstimated {
			return "usage_estimated", "usage estimated after request completed"
		}
		return "response_received", "upstream response recorded"
	}

	detail := firstNonEmpty(strings.TrimSpace(record.ErrorMessage), "request failed")
	return "request_failed", detail
}

func normalizeUsageFailureInput(input UsageFailureInput) UsageFailureInput {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.RequestLogID = firstNonEmpty(input.RequestLogID, input.RequestID)
	input.TenantID = strings.TrimSpace(input.TenantID)
	input.UserID = strings.TrimSpace(input.UserID)
	input.PlatformAPIKeyID = strings.TrimSpace(input.PlatformAPIKeyID)
	input.ErrorCategory = normalizeUsageFailureCategory(input.ErrorCategory)
	input.FailureStage = normalizeUsageFailureStage(input.FailureStage, input.ErrorCategory)
	input.UserMessage = firstNonEmpty(input.UserMessage, userFailureMessage(UsageRecord{
		Status:       usageStatusFromFailureCategory(input.ErrorCategory),
		StatusCode:   input.StatusCode,
		ErrorMessage: input.InternalMessage,
	}))
	return input
}

func usageFailureInputFromRecord(record UsageRecord) UsageFailureInput {
	return UsageFailureInput{
		RequestID:        record.RequestID,
		RequestLogID:     record.RequestID,
		TenantID:         record.TenantID,
		UserID:           record.UserID,
		PlatformAPIKeyID: record.PlatformAPIKeyID,
		FailureStage:     failureStageFromRecord(record),
		ErrorCategory:    failureCategoryFromRecord(record),
		StatusCode:       record.StatusCode,
		Retryable:        statusCodeRetryable(record.StatusCode),
		UserMessage:      userFailureMessage(record),
		InternalMessage:  record.ErrorMessage,
	}
}

func normalizeUsageFailureStage(stage string, category string) string {
	normalized := UsageFailureStage(strings.TrimSpace(stage))
	if normalized.Valid() {
		return string(normalized)
	}
	if UsageFailureCategory(category) == UsageFailureCategoryPublishFailure {
		return string(UsageFailureStagePublish)
	}
	return string(UsageFailureStageInternal)
}

func normalizeUsageFailureCategory(category string) string {
	normalized := UsageFailureCategory(strings.TrimSpace(category))
	if normalized.Valid() {
		return string(normalized)
	}
	return string(UsageFailureCategoryFailed)
}

func failureStageFromRecord(record UsageRecord) string {
	switch record.Status {
	case UsageStatusAuthFailed:
		return string(UsageFailureStageRequest)
	case UsageStatusRateLimited, UsageStatusTimeout, UsageStatusUpstreamError:
		return string(UsageFailureStageUpstream)
	case UsageStatusFailed:
		switch record.StatusCode {
		case 400, 401, 403:
			return string(UsageFailureStageRequest)
		}
		if record.StatusCode == 429 || record.StatusCode >= 500 {
			return string(UsageFailureStageUpstream)
		}
		return string(UsageFailureStageResponse)
	}
	return string(UsageFailureStageResponse)
}

func failureCategoryFromRecord(record UsageRecord) string {
	switch record.Status {
	case UsageStatusRateLimited:
		return string(UsageFailureCategoryRateLimited)
	case UsageStatusAuthFailed:
		return string(UsageFailureCategoryAuthFailed)
	case UsageStatusTimeout:
		return string(UsageFailureCategoryTimeout)
	case UsageStatusUpstreamError:
		return string(UsageFailureCategoryUpstreamError)
	default:
		return string(UsageFailureCategoryFailed)
	}
}

func usageStatusFromFailureCategory(category string) UsageStatus {
	switch UsageFailureCategory(category) {
	case UsageFailureCategoryRateLimited:
		return UsageStatusRateLimited
	case UsageFailureCategoryAuthFailed:
		return UsageStatusAuthFailed
	case UsageFailureCategoryTimeout:
		return UsageStatusTimeout
	case UsageFailureCategoryUpstreamError:
		return UsageStatusUpstreamError
	default:
		return UsageStatusFailed
	}
}

func userFailureMessage(record UsageRecord) string {
	switch defaultStatusCode(record.StatusCode) {
	case 400:
		return "invalid request body"
	case 401, 403:
		return "unauthorized"
	case 408, 504:
		return "request timed out"
	case 429:
		return "rate limit exceeded"
	default:
		if defaultStatusCode(record.StatusCode) >= 500 {
			return "upstream request failed"
		}
	}
	return "request failed"
}

func digestFailureMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(message))
	return hex.EncodeToString(sum[:])
}

func statusCodeRetryable(statusCode int) bool {
	switch defaultStatusCode(statusCode) {
	case 408, 409, 425, 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

func isFailureStatus(status UsageStatus) bool {
	return status != UsageStatusSuccess
}

func normalizeUsage(upstream *TokenUsage, estimated TokenUsage) (TokenUsage, UsageSource) {
	estimated = sanitizeUsage(estimated)
	if normalized, ok := trustworthyUpstreamUsage(upstream); ok {
		return normalized, UsageSourceUpstream
	}
	return estimated, UsageSourceEstimated
}

func trustworthyUpstreamUsage(upstream *TokenUsage) (TokenUsage, bool) {
	if upstream == nil {
		return TokenUsage{}, false
	}
	if upstream.PromptTokens < 0 || upstream.CompletionTokens < 0 || upstream.TotalTokens < 0 || upstream.CachedTokens < 0 {
		return TokenUsage{}, false
	}
	if upstream.TotalTokens <= 0 {
		return TokenUsage{}, false
	}
	if upstream.TotalTokens != upstream.PromptTokens+upstream.CompletionTokens {
		return TokenUsage{}, false
	}
	return sanitizeUsage(*upstream), true
}

func sanitizeUsage(usage TokenUsage) TokenUsage {
	usage.PromptTokens = max(0, usage.PromptTokens)
	usage.CompletionTokens = max(0, usage.CompletionTokens)
	usage.TotalTokens = max(0, usage.TotalTokens)
	usage.CachedTokens = max(0, usage.CachedTokens)
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return usage
}

func estimateChatUsage(req ChatRequest, resp ChatResponse) TokenUsage {
	promptTokens := estimateTextTokens(req.Model)
	for _, message := range req.Messages {
		promptTokens += estimateTextTokens(message.Role)
		promptTokens += estimateTextTokens(message.Content)
	}

	completionTokens := 0
	for _, choice := range resp.Choices {
		completionTokens += estimateTextTokens(choice.Message.Role)
		completionTokens += estimateTextTokens(choice.Message.Content)
	}

	return TokenUsage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}
}

func estimateEmbeddingsUsage(req EmbeddingsRequest) TokenUsage {
	promptTokens := estimateTextTokens(req.Model)
	promptTokens += estimateAnyTokens(req.Input)
	return TokenUsage{
		PromptTokens: promptTokens,
		TotalTokens:  promptTokens,
	}
}

func estimateAnyTokens(input any) int {
	switch value := input.(type) {
	case string:
		return estimateTextTokens(value)
	case []string:
		total := 0
		for _, item := range value {
			total += estimateTextTokens(item)
		}
		return total
	case []any:
		total := 0
		for _, item := range value {
			total += estimateAnyTokens(item)
		}
		return total
	default:
		body, err := json.Marshal(value)
		if err != nil {
			return 0
		}
		return estimateTextTokens(string(body))
	}
}

func estimateTextTokens(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	return max(1, (len([]rune(value))+3)/4)
}

func statusFromHTTPStatus(statusCode int) UsageStatus {
	switch statusCode {
	case 401, 403:
		return UsageStatusAuthFailed
	case 408, 504:
		return UsageStatusTimeout
	case 429:
		return UsageStatusRateLimited
	}
	if statusCode >= 200 && statusCode < 300 {
		return UsageStatusSuccess
	}
	if statusCode >= 500 {
		return UsageStatusUpstreamError
	}
	return UsageStatusFailed
}

func requestIDFromContext(ctx context.Context) string {
	if ctx != nil {
		if requestID, ok := ctx.Value(requestIDContextKey{}).(string); ok && strings.TrimSpace(requestID) != "" {
			return requestID
		}
	}
	return uuid.NewString()
}

type requestIDContextKey struct{}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
