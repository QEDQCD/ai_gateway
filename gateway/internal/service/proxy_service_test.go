package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/example/ai_gateway/gateway/internal/domain"
	"github.com/example/ai_gateway/gateway/internal/queue"
	"github.com/example/ai_gateway/gateway/internal/service"
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

func TestChatProxyStreamRecordsUsageAfterSuccessfulStream(t *testing.T) {
	t.Parallel()

	recorder := &stubUsageRecorder{}
	proxy := service.NewChatProxyService(
		stubChatClient{
			response: service.ChatResponse{
				Model: "gpt-4o-mini",
				Choices: []service.ChatChoice{
					{Message: service.ChatMessage{Role: "assistant", Content: "streamed answer"}},
				},
				Usage: &service.TokenUsage{
					PromptTokens:     11,
					CompletionTokens: 7,
					TotalTokens:      18,
				},
			},
		},
		queue.NewNoopUsagePublisher(),
		recorder,
	)

	stream, err := proxy.Stream(context.Background(), service.ChatRequest{
		Model:  "gpt-4o-mini",
		Stream: true,
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
		t.Fatalf("proxy.Stream returned unexpected error: %v", err)
	}

	var chunks bytes.Buffer
	resp, err := stream.Run(func(chunk []byte) error {
		_, writeErr := chunks.Write(chunk)
		return writeErr
	})
	if err != nil {
		t.Fatalf("stream.Run returned unexpected error: %v", err)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 18 {
		t.Fatalf("expected upstream usage 18, got %#v", resp.Usage)
	}
	if got := chunks.String(); got != "data: [DONE]\n\n" {
		t.Fatalf("expected done-only stream output, got %q", got)
	}
	if recorder.recordCalls != 1 {
		t.Fatalf("expected 1 usage record write, got %d", recorder.recordCalls)
	}
	if recorder.lastRecord.Status != service.UsageStatusSuccess {
		t.Fatalf("expected success status, got %q", recorder.lastRecord.Status)
	}
	if recorder.lastRecord.TotalTokens != 18 {
		t.Fatalf("expected recorded total tokens 18, got %d", recorder.lastRecord.TotalTokens)
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

func (c stubChatClient) StreamComplete(context.Context, domain.ProviderTarget, service.ChatRequest) (service.ChatCompletionStream, int, error) {
	if c.err != nil {
		return service.ChatCompletionStream{}, 502, c.err
	}
	return service.ChatCompletionStream{
		StatusCode:  200,
		ContentType: "text/event-stream; charset=utf-8",
		Run: func(emit func([]byte) error) (service.ChatResponse, error) {
			if emit != nil {
				if err := emit([]byte("data: [DONE]\n\n")); err != nil {
					return service.ChatResponse{}, err
				}
			}
			return c.response, nil
		},
	}, 200, nil
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
	lastRecord          service.UsageRecord
}

func (r *stubUsageRecorder) Record(_ context.Context, record service.UsageRecord) error {
	r.recordCalls++
	r.lastRecord = record
	return nil
}

func (r *stubUsageRecorder) RecordFailure(context.Context, service.UsageFailureInput) error {
	return nil
}

func (r *stubUsageRecorder) RecordPublishFailure(context.Context, service.UsageRecord, error) error {
	r.publishFailureCalls++
	return nil
}
