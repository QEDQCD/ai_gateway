package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/liwenjian/ai_gateway/gateway/internal/domain"
	"github.com/liwenjian/ai_gateway/gateway/internal/queue"
	"github.com/liwenjian/ai_gateway/gateway/internal/service"
)

func TestChatProxySkipsPublishFailurePersistenceOnConsumerOnlyError(t *testing.T) {
	t.Parallel()

	consumerErr := errors.New("local consumer failed")
	recorder := &stubUsageRecorder{}
	proxy := service.NewChatProxyService(
		stubChatClient{
			response: service.ChatResponse{
				Model: "gpt-4o-mini",
				Choices: []service.ChatChoice{
					{Message: service.ChatMessage{Role: "assistant", Content: "ok"}},
				},
			},
		},
		stubUsagePublisher{err: consumerErr},
		recorder,
	)

	_, err := proxy.Complete(context.Background(), service.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []service.ChatMessage{
			{Role: "user", Content: "hello"},
		},
	}, domain.RequestContext{
		TenantID:           "tenant_demo",
		PlatformAPIKeyID:   "pak_demo",
		PlatformAPIKeyName: "demo key",
		RouteID:            "route:provider_openai_demo:default",
		ProviderTarget: domain.ProviderTarget{
			CredentialID: "provider_openai_demo",
			Provider:     "openai",
			BaseURL:      "https://api.openai.example/v1",
			APIKey:       "provider-secret",
		},
	})
	if err != nil {
		t.Fatalf("proxy.Complete returned unexpected error: %v", err)
	}

	if recorder.recordCalls != 1 {
		t.Fatalf("expected 1 usage record write, got %d", recorder.recordCalls)
	}
	if recorder.publishFailureCalls != 0 {
		t.Fatalf("expected consumer-only publish error not to persist usage_publish_failed, got %d calls", recorder.publishFailureCalls)
	}
}

type stubChatClient struct {
	response service.ChatResponse
	err      error
}

func (c stubChatClient) Complete(context.Context, domain.ProviderTarget, service.ChatRequest) (service.ChatResponse, int, error) {
	if c.err != nil {
		return service.ChatResponse{}, 502, c.err
	}
	return c.response, 200, nil
}

type stubUsagePublisher struct {
	err error
}

func (p stubUsagePublisher) Publish(context.Context, queue.UsageEvent) error {
	return p.err
}

type stubUsageRecorder struct {
	recordCalls         int
	publishFailureCalls int
}

func (r *stubUsageRecorder) Record(context.Context, service.UsageRecord) error {
	r.recordCalls++
	return nil
}

func (r *stubUsageRecorder) RecordPublishFailure(context.Context, service.UsageRecord, error) error {
	r.publishFailureCalls++
	return nil
}
