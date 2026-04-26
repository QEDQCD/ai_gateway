package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/liwenjian/ai_gateway/gateway/internal/domain"
	"github.com/liwenjian/ai_gateway/gateway/internal/queue"
	"github.com/liwenjian/ai_gateway/gateway/internal/store"
)

type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type UsageRecord struct {
	RequestID            string
	TenantID             string
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
	PromptTokens         int
	CompletionTokens     int
	TotalTokens          int
	ErrorCode            string
	ErrorMessage         string
	RequestStartedAt     time.Time
	RequestCompletedAt   time.Time
}

type UsageRecorder interface {
	Record(ctx context.Context, record UsageRecord) error
	RecordPublishFailure(ctx context.Context, record UsageRecord, publishErr error) error
}

type noopUsageRecorder struct{}

type sqlUsageRecorder struct {
	db usageRecordStore
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
	prompt_tokens,
	completion_tokens,
	total_tokens,
	error_code,
	error_message,
	request_started_at,
	request_completed_at
) values (
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
	$11, $12, $13, $14, $15, $16, $17, $18, $19, $20
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

func NewNoopUsageRecorder() UsageRecorder {
	return noopUsageRecorder{}
}

func NewUsageRecorder(db usageRecordStore) UsageRecorder {
	if db == nil {
		return noopUsageRecorder{}
	}
	return sqlUsageRecorder{db: db}
}

func (noopUsageRecorder) Record(context.Context, UsageRecord) error {
	return nil
}

func (noopUsageRecorder) RecordPublishFailure(context.Context, UsageRecord, error) error {
	return nil
}

func (r sqlUsageRecorder) Record(ctx context.Context, record UsageRecord) error {
	record.ensureDefaults()
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
		record.PromptTokens,
		record.CompletionTokens,
		record.TotalTokens,
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

func newUsageRecordContext(ctx context.Context) (context.Context, context.CancelFunc) {
	baseCtx := context.Background()
	if ctx != nil {
		baseCtx = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(baseCtx, usageRecordTimeout)
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
		Endpoint:             r.RequestPath,
		StatusCode:           r.StatusCode,
		LatencyMS:            r.LatencyMS,
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
	if upstream.PromptTokens < 0 || upstream.CompletionTokens < 0 || upstream.TotalTokens < 0 {
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
