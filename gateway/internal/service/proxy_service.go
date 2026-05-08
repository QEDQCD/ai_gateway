package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/example/ai_gateway/gateway/internal/domain"
	"github.com/example/ai_gateway/gateway/internal/queue"
)

var ErrProxyUnavailable = errors.New("proxy service not configured")

const publishFailureRecordTimeout = 2 * time.Second

type StatusError struct {
	Code    int
	Message string
	Err     error
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model     string        `json:"model"`
	Messages  []ChatMessage `json:"messages"`
	Stream    bool          `json:"stream,omitempty"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

type ChatChoice struct {
	Message ChatMessage `json:"message"`
}

type ChatResponse struct {
	Model   string       `json:"model,omitempty"`
	Usage   *TokenUsage  `json:"usage,omitempty"`
	Choices []ChatChoice `json:"choices"`
}

type ChatStreamResult struct {
	Response        ChatResponse
	SawContentToken bool
	ClientAborted   bool
}

type ChatCompletionStream struct {
	StatusCode  int
	ContentType string
	Run         func(emit func([]byte) error, onFirstToken func()) (ChatStreamResult, error)
}

type EmbeddingsRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type EmbeddingsDatum struct {
	Embedding []float64 `json:"embedding"`
}

type EmbeddingsResponse struct {
	Model string            `json:"model,omitempty"`
	Usage *TokenUsage       `json:"usage,omitempty"`
	Data  []EmbeddingsDatum `json:"data"`
}

type UpstreamChatClient interface {
	Complete(ctx context.Context, target domain.ProviderTarget, req ChatRequest) (ChatResponse, int, error)
	StreamComplete(ctx context.Context, target domain.ProviderTarget, req ChatRequest) (ChatCompletionStream, int, error)
}

type UpstreamEmbeddingClient interface {
	CreateEmbedding(ctx context.Context, target domain.ProviderTarget, req EmbeddingsRequest) (EmbeddingsResponse, int, error)
}

type ChatProxyService interface {
	Complete(ctx context.Context, req ChatRequest, resolved any) (ChatResponse, error)
	Stream(ctx context.Context, req ChatRequest, resolved any) (ChatCompletionStream, error)
	RecordFailure(ctx context.Context, resolved any, statusCode int)
}

type EmbeddingProxyService interface {
	Create(ctx context.Context, req EmbeddingsRequest, resolved any) (EmbeddingsResponse, error)
	RecordFailure(ctx context.Context, resolved any, statusCode int)
}

type chatProxyService struct {
	client    UpstreamChatClient
	publisher queue.UsagePublisher
	recorder  UsageRecorder
}

type embeddingProxyService struct {
	client    UpstreamEmbeddingClient
	publisher queue.UsagePublisher
	recorder  UsageRecorder
}

type unavailableChatProxyService struct{}

type unavailableEmbeddingProxyService struct{}

type usageRecordEvent struct {
	eventType string
	detail    string
}

func NewChatProxyService(client UpstreamChatClient, publisher queue.UsagePublisher, recorders ...UsageRecorder) ChatProxyService {
	if client == nil {
		return unavailableChatProxyService{}
	}
	if publisher == nil {
		publisher = queue.NewNoopUsagePublisher()
	}
	return chatProxyService{
		client:    client,
		publisher: publisher,
		recorder:  firstUsageRecorder(recorders...),
	}
}

func NewEmbeddingProxyService(client UpstreamEmbeddingClient, publisher queue.UsagePublisher, recorders ...UsageRecorder) EmbeddingProxyService {
	if client == nil {
		return unavailableEmbeddingProxyService{}
	}
	if publisher == nil {
		publisher = queue.NewNoopUsagePublisher()
	}
	return embeddingProxyService{
		client:    client,
		publisher: publisher,
		recorder:  firstUsageRecorder(recorders...),
	}
}

func NewUnavailableChatProxyService() ChatProxyService {
	return unavailableChatProxyService{}
}

func NewUnavailableEmbeddingProxyService() EmbeddingProxyService {
	return unavailableEmbeddingProxyService{}
}

func ValidateChatRequest(req ChatRequest) error {
	if len(req.Messages) == 0 {
		return fmt.Errorf("messages is required")
	}
	for _, message := range req.Messages {
		if strings.TrimSpace(message.Content) == "" {
			return fmt.Errorf("message content is required")
		}
	}
	if req.MaxTokens < 0 {
		return fmt.Errorf("max_tokens must be greater than or equal to 0")
	}
	return nil
}

func ValidateEmbeddingsRequest(req EmbeddingsRequest) error {
	if strings.TrimSpace(req.Model) == "" {
		return fmt.Errorf("model is required")
	}
	switch value := req.Input.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("input is required")
		}
	case []any:
		if len(value) == 0 {
			return fmt.Errorf("input is required")
		}
	default:
		if req.Input == nil {
			return fmt.Errorf("input is required")
		}
	}
	return nil
}

func (s chatProxyService) Complete(ctx context.Context, req ChatRequest, resolved any) (ChatResponse, error) {
	requestContext, ok := resolvedRequestContext(resolved)
	if !ok {
		return ChatResponse{}, StatusError{
			Code:    http.StatusUnauthorized,
			Message: "unauthorized",
			Err:     fmt.Errorf("%w: request context is missing", ErrUnauthorized),
		}
	}
	if err := validateProviderTarget(requestContext); err != nil {
		now := time.Now().UTC()
		s.record(ctx, NewChatUsageRecord(requestIDFromContext(ctx), requestContext, req, ChatResponse{}, http.StatusBadGateway, now, now, err))
		return ChatResponse{}, err
	}

	requestID := requestIDFromContext(ctx)
	start := time.Now().UTC()
	resp, statusCode, err := s.client.Complete(ctx, requestContext.ProviderTarget, req)
	record := NewChatUsageRecord(requestID, requestContext, req, resp, statusCode, start, time.Now().UTC(), err)
	s.record(ctx, record)
	if err != nil {
		return ChatResponse{}, StatusError{
			Code:    defaultStatusCode(statusCode),
			Message: "upstream request failed",
			Err:     err,
		}
	}
	return resp, nil
}

func (s chatProxyService) Stream(ctx context.Context, req ChatRequest, resolved any) (ChatCompletionStream, error) {
	requestContext, ok := resolvedRequestContext(resolved)
	if !ok {
		return ChatCompletionStream{}, StatusError{
			Code:    http.StatusUnauthorized,
			Message: "unauthorized",
			Err:     fmt.Errorf("%w: request context is missing", ErrUnauthorized),
		}
	}
	if err := validateProviderTarget(requestContext); err != nil {
		now := time.Now().UTC()
		s.record(ctx, NewChatUsageRecord(requestIDFromContext(ctx), requestContext, req, ChatResponse{}, http.StatusBadGateway, now, now, err))
		return ChatCompletionStream{}, err
	}

	requestID := requestIDFromContext(ctx)
	start := time.Now().UTC()
	upstreamStream, statusCode, err := s.client.StreamComplete(ctx, requestContext.ProviderTarget, req)
	if err != nil {
		record := NewChatUsageRecord(requestID, requestContext, req, ChatResponse{}, statusCode, start, time.Now().UTC(), err)
		s.record(ctx, record)
		return ChatCompletionStream{}, StatusError{
			Code:    defaultStatusCode(statusCode),
			Message: "upstream request failed",
			Err:     err,
		}
	}

	return ChatCompletionStream{
		StatusCode:  defaultStatusCode(upstreamStream.StatusCode),
		ContentType: upstreamStream.ContentType,
		Run: func(emit func([]byte) error, onFirstToken func()) (ChatStreamResult, error) {
			var firstTokenAt time.Time
			result, streamErr := upstreamStream.Run(emit, func() {
				if firstTokenAt.IsZero() {
					firstTokenAt = time.Now().UTC()
				}
				if onFirstToken != nil {
					onFirstToken()
				}
			})
			clientAbortedAfterContent := result.ClientAborted && result.SawContentToken
			finalStatusCode := defaultStatusCode(upstreamStream.StatusCode)
			if streamErr != nil && !clientAbortedAfterContent && finalStatusCode >= 200 && finalStatusCode < 300 {
				finalStatusCode = http.StatusInternalServerError
			}

			recordErr := streamErr
			if clientAbortedAfterContent {
				recordErr = nil
			}
			record := NewChatUsageRecord(requestID, requestContext, req, result.Response, finalStatusCode, start, time.Now().UTC(), recordErr)
			if !firstTokenAt.IsZero() {
				record.FirstTokenLatencyMS = durationMilliseconds(firstTokenAt.Sub(start))
			}

			events := []usageRecordEvent(nil)
			if clientAbortedAfterContent {
				events = append(events, usageRecordEvent{
					eventType: "client_aborted",
					detail:    "client disconnected after first content token",
				})
			}
			s.recordWithEvents(ctx, record, events...)
			return result, streamErr
		},
	}, nil
}

func (s embeddingProxyService) Create(ctx context.Context, req EmbeddingsRequest, resolved any) (EmbeddingsResponse, error) {
	requestContext, ok := resolvedRequestContext(resolved)
	if !ok {
		return EmbeddingsResponse{}, StatusError{
			Code:    http.StatusUnauthorized,
			Message: "unauthorized",
			Err:     fmt.Errorf("%w: request context is missing", ErrUnauthorized),
		}
	}
	if err := validateProviderTarget(requestContext); err != nil {
		now := time.Now().UTC()
		s.record(ctx, NewEmbeddingsUsageRecord(requestIDFromContext(ctx), requestContext, req, EmbeddingsResponse{}, http.StatusBadGateway, now, now, err))
		return EmbeddingsResponse{}, err
	}

	requestID := requestIDFromContext(ctx)
	start := time.Now().UTC()
	resp, statusCode, err := s.client.CreateEmbedding(ctx, requestContext.ProviderTarget, req)
	record := NewEmbeddingsUsageRecord(requestID, requestContext, req, resp, statusCode, start, time.Now().UTC(), err)
	s.record(ctx, record)
	if err != nil {
		return EmbeddingsResponse{}, StatusError{
			Code:    defaultStatusCode(statusCode),
			Message: "upstream request failed",
			Err:     err,
		}
	}
	return resp, nil
}

func (s chatProxyService) record(ctx context.Context, record UsageRecord) {
	s.recordWithEvents(ctx, record)
}

func (s chatProxyService) recordWithEvents(ctx context.Context, record UsageRecord, events ...usageRecordEvent) {
	preparedRecord, err := s.recorder.PrepareUsageEventRecord(record)
	if err != nil {
		logUsageFailure("usage_event_prepare", record, err)
		return
	}
	if err := s.recorder.Record(ctx, preparedRecord); err != nil {
		logUsageFailure("record", record, err)
		return
	}
	for _, event := range events {
		if err := s.recorder.RecordEvent(ctx, preparedRecord, event.eventType, event.detail); err != nil {
			logUsageFailure(event.eventType, preparedRecord, err)
		}
	}
	if err := s.publisher.Publish(ctx, preparedRecord.UsageEvent()); err != nil {
		publishErr := queue.PublishFailure(err)
		stage := "consume"
		if publishErr != nil {
			stage = "publish"
		}
		logUsageFailure(stage, preparedRecord, err)
		if publishErr != nil {
			if persistErr := persistPublishFailure(preparedRecord, s.recorder, publishErr); persistErr != nil {
				logUsageFailure("publish_failure_persist", preparedRecord, persistErr)
			}
		}
	}
}

func (s embeddingProxyService) record(ctx context.Context, record UsageRecord) {
	preparedRecord, err := s.recorder.PrepareUsageEventRecord(record)
	if err != nil {
		logUsageFailure("usage_event_prepare", record, err)
		return
	}
	if err := s.recorder.Record(ctx, preparedRecord); err != nil {
		logUsageFailure("record", record, err)
		return
	}
	if err := s.publisher.Publish(ctx, preparedRecord.UsageEvent()); err != nil {
		publishErr := queue.PublishFailure(err)
		stage := "consume"
		if publishErr != nil {
			stage = "publish"
		}
		logUsageFailure(stage, preparedRecord, err)
		if publishErr != nil {
			if persistErr := persistPublishFailure(preparedRecord, s.recorder, publishErr); persistErr != nil {
				logUsageFailure("publish_failure_persist", preparedRecord, persistErr)
			}
		}
	}
}

func (unavailableChatProxyService) Complete(context.Context, ChatRequest, any) (ChatResponse, error) {
	return ChatResponse{}, StatusError{
		Code:    http.StatusNotImplemented,
		Message: "chat proxy unavailable",
		Err:     fmt.Errorf("%w: chat proxy", ErrProxyUnavailable),
	}
}

func (unavailableChatProxyService) Stream(context.Context, ChatRequest, any) (ChatCompletionStream, error) {
	return ChatCompletionStream{}, StatusError{
		Code:    http.StatusNotImplemented,
		Message: "chat proxy unavailable",
		Err:     fmt.Errorf("%w: chat proxy", ErrProxyUnavailable),
	}
}

func (unavailableEmbeddingProxyService) Create(context.Context, EmbeddingsRequest, any) (EmbeddingsResponse, error) {
	return EmbeddingsResponse{}, StatusError{
		Code:    http.StatusNotImplemented,
		Message: "embedding proxy unavailable",
		Err:     fmt.Errorf("%w: embeddings proxy", ErrProxyUnavailable),
	}
}

func (s chatProxyService) RecordFailure(ctx context.Context, resolved any, statusCode int) {
	if requestContext, ok := resolvedRequestContext(resolved); ok {
		now := time.Now().UTC()
		s.record(ctx, NewFailureUsageRecord(requestIDFromContext(ctx), requestContext, "/v1/chat/completions", statusCode, now, now, nil))
	}
}

func (s embeddingProxyService) RecordFailure(ctx context.Context, resolved any, statusCode int) {
	if requestContext, ok := resolvedRequestContext(resolved); ok {
		now := time.Now().UTC()
		s.record(ctx, NewFailureUsageRecord(requestIDFromContext(ctx), requestContext, "/v1/embeddings", statusCode, now, now, nil))
	}
}

func (unavailableChatProxyService) RecordFailure(context.Context, any, int) {}

func (unavailableEmbeddingProxyService) RecordFailure(context.Context, any, int) {}

func resolvedRequestContext(resolved any) (domain.RequestContext, bool) {
	requestContext, ok := resolved.(domain.RequestContext)
	if !ok {
		return domain.RequestContext{}, false
	}
	return requestContext, true
}

func validateProviderTarget(requestContext domain.RequestContext) error {
	if requestContext.ProviderTarget.CredentialID == "" || requestContext.ProviderTarget.BaseURL == "" || requestContext.ProviderTarget.APIKey == "" {
		return StatusError{
			Code:    http.StatusBadGateway,
			Message: "provider target not configured",
			Err:     fmt.Errorf("%w: provider target is missing", ErrProxyUnavailable),
		}
	}
	return nil
}

func (e StatusError) Error() string {
	if e.Err == nil {
		return e.Message
	}
	return e.Message + ": " + e.Err.Error()
}

func (e StatusError) Unwrap() error {
	return e.Err
}

func StatusCodeFromError(err error) (int, string, bool) {
	var statusErr StatusError
	if !errors.As(err, &statusErr) {
		return 0, "", false
	}
	return statusErr.Code, statusErr.Message, true
}

func defaultStatusCode(statusCode int) int {
	if statusCode == 0 {
		return http.StatusBadGateway
	}
	return statusCode
}

func durationMilliseconds(latency time.Duration) int64 {
	if latency.Milliseconds() <= 0 {
		return 1
	}
	return latency.Milliseconds()
}

func firstUsageRecorder(recorders ...UsageRecorder) UsageRecorder {
	for _, recorder := range recorders {
		if recorder != nil {
			return recorder
		}
	}
	return NewNoopUsageRecorder()
}

func logUsageFailure(stage string, record UsageRecord, err error) {
	log.Printf("usage %s failed: request_id=%s route_id=%s err=%v", stage, record.RequestID, record.RouteID, err)
}

func persistPublishFailure(record UsageRecord, recorder UsageRecorder, publishErr error) error {
	ctx, cancel := context.WithTimeout(context.Background(), publishFailureRecordTimeout)
	defer cancel()
	return recorder.RecordPublishFailure(ctx, record, publishErr)
}
