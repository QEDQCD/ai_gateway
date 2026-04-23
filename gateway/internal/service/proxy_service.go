package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/liwenjian/ai_gateway/gateway/internal/domain"
	"github.com/liwenjian/ai_gateway/gateway/internal/queue"
)

var ErrProxyUnavailable = errors.New("proxy service not configured")

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
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
}

type ChatChoice struct {
	Message ChatMessage `json:"message"`
}

type ChatResponse struct {
	Choices []ChatChoice `json:"choices"`
}

type EmbeddingsRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type EmbeddingsDatum struct {
	Embedding []float64 `json:"embedding"`
}

type EmbeddingsResponse struct {
	Data []EmbeddingsDatum `json:"data"`
}

type UpstreamChatClient interface {
	Complete(ctx context.Context, target domain.ProviderTarget, req ChatRequest) (ChatResponse, int, error)
}

type UpstreamEmbeddingClient interface {
	CreateEmbedding(ctx context.Context, target domain.ProviderTarget, req EmbeddingsRequest) (EmbeddingsResponse, int, error)
}

type ChatProxyService interface {
	Complete(ctx context.Context, req ChatRequest, resolved any) (ChatResponse, error)
	RecordFailure(ctx context.Context, resolved any, statusCode int)
}

type EmbeddingProxyService interface {
	Create(ctx context.Context, req EmbeddingsRequest, resolved any) (EmbeddingsResponse, error)
	RecordFailure(ctx context.Context, resolved any, statusCode int)
}

type chatProxyService struct {
	client    UpstreamChatClient
	publisher queue.UsagePublisher
}

type embeddingProxyService struct {
	client    UpstreamEmbeddingClient
	publisher queue.UsagePublisher
}

type unavailableChatProxyService struct{}

type unavailableEmbeddingProxyService struct{}

func NewChatProxyService(client UpstreamChatClient, publisher queue.UsagePublisher) ChatProxyService {
	if client == nil {
		return unavailableChatProxyService{}
	}
	if publisher == nil {
		publisher = queue.NewNoopUsagePublisher()
	}
	return chatProxyService{client: client, publisher: publisher}
}

func NewEmbeddingProxyService(client UpstreamEmbeddingClient, publisher queue.UsagePublisher) EmbeddingProxyService {
	if client == nil {
		return unavailableEmbeddingProxyService{}
	}
	if publisher == nil {
		publisher = queue.NewNoopUsagePublisher()
	}
	return embeddingProxyService{client: client, publisher: publisher}
}

func NewUnavailableChatProxyService() ChatProxyService {
	return unavailableChatProxyService{}
}

func NewUnavailableEmbeddingProxyService() EmbeddingProxyService {
	return unavailableEmbeddingProxyService{}
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
		s.publish(ctx, requestContext, "/v1/chat/completions", http.StatusBadGateway, 0)
		return ChatResponse{}, err
	}

	start := time.Now()
	resp, statusCode, err := s.client.Complete(ctx, requestContext.ProviderTarget, req)
	s.publish(ctx, requestContext, "/v1/chat/completions", statusCode, time.Since(start))
	if err != nil {
		return ChatResponse{}, StatusError{
			Code:    defaultStatusCode(statusCode),
			Message: "upstream request failed",
			Err:     err,
		}
	}
	return resp, nil
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
		s.publish(ctx, requestContext, "/v1/embeddings", http.StatusBadGateway, 0)
		return EmbeddingsResponse{}, err
	}

	start := time.Now()
	resp, statusCode, err := s.client.CreateEmbedding(ctx, requestContext.ProviderTarget, req)
	s.publish(ctx, requestContext, "/v1/embeddings", statusCode, time.Since(start))
	if err != nil {
		return EmbeddingsResponse{}, StatusError{
			Code:    defaultStatusCode(statusCode),
			Message: "upstream request failed",
			Err:     err,
		}
	}
	return resp, nil
}

func (s chatProxyService) publish(ctx context.Context, requestContext domain.RequestContext, endpoint string, statusCode int, latency time.Duration) {
	_ = s.publisher.Publish(ctx, queue.UsageEvent{
		TenantID:             requestContext.TenantID,
		PlatformAPIKeyID:     requestContext.PlatformAPIKeyID,
		ProviderCredentialID: requestContext.SelectedProviderID,
		Endpoint:             endpoint,
		StatusCode:           defaultStatusCode(statusCode),
		LatencyMS:            durationMilliseconds(latency),
	})
}

func (s embeddingProxyService) publish(ctx context.Context, requestContext domain.RequestContext, endpoint string, statusCode int, latency time.Duration) {
	_ = s.publisher.Publish(ctx, queue.UsageEvent{
		TenantID:             requestContext.TenantID,
		PlatformAPIKeyID:     requestContext.PlatformAPIKeyID,
		ProviderCredentialID: requestContext.SelectedProviderID,
		Endpoint:             endpoint,
		StatusCode:           defaultStatusCode(statusCode),
		LatencyMS:            durationMilliseconds(latency),
	})
}

func (unavailableChatProxyService) Complete(context.Context, ChatRequest, any) (ChatResponse, error) {
	return ChatResponse{}, StatusError{
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
		s.publish(ctx, requestContext, "/v1/chat/completions", statusCode, 0)
	}
}

func (s embeddingProxyService) RecordFailure(ctx context.Context, resolved any, statusCode int) {
	if requestContext, ok := resolvedRequestContext(resolved); ok {
		s.publish(ctx, requestContext, "/v1/embeddings", statusCode, 0)
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
