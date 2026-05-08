package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

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

func TestChatProxyCompletePublishesUsageEventWithCostSnapshot(t *testing.T) {
	t.Parallel()

	db := newRecordingTxDB()
	publisher := queue.NewRecordingUsagePublisher()
	proxy := service.NewChatProxyService(
		stubChatClient{
			response: service.ChatResponse{
				Model: "gpt-4o-mini",
				Choices: []service.ChatChoice{
					{Message: service.ChatMessage{Role: "assistant", Content: "ok"}},
				},
				Usage: &service.TokenUsage{
					PromptTokens:     20,
					CompletionTokens: 10,
					TotalTokens:      30,
					CachedTokens:     5,
				},
			},
		},
		publisher,
		service.NewUsageRecorder(db, newTestUsagePricingResolver(t)),
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

	events := publisher.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 published usage event, got %d", len(events))
	}
	event := events[0]
	if event.CachedTokens != 5 {
		t.Fatalf("expected cached_tokens 5, got %d", event.CachedTokens)
	}
	if event.InputCostMicroyuan != 30 {
		t.Fatalf("expected input_cost_microyuan 30, got %d", event.InputCostMicroyuan)
	}
	if event.OutputCostMicroyuan != 40 {
		t.Fatalf("expected output_cost_microyuan 40, got %d", event.OutputCostMicroyuan)
	}
	if event.CachedCostMicroyuan != 3 {
		t.Fatalf("expected cached_cost_microyuan 3, got %d", event.CachedCostMicroyuan)
	}
	if event.TotalCostMicroyuan != 73 {
		t.Fatalf("expected total_cost_microyuan 73, got %d", event.TotalCostMicroyuan)
	}
}

func TestEmbeddingProxyCreatePublishesUsageEventWithCostSnapshot(t *testing.T) {
	t.Parallel()

	db := newRecordingTxDB()
	publisher := queue.NewRecordingUsagePublisher()
	proxy := service.NewEmbeddingProxyService(
		stubEmbeddingClient{
			response: service.EmbeddingsResponse{
				Model: "text-embedding-3-small",
				Usage: &service.TokenUsage{
					PromptTokens: 12,
					TotalTokens:  12,
					CachedTokens: 2,
				},
				Data: []service.EmbeddingsDatum{
					{Embedding: []float64{0.1, 0.2}},
				},
			},
		},
		publisher,
		service.NewUsageRecorder(db, newTestUsagePricingResolver(t)),
	)

	_, err := proxy.Create(context.Background(), service.EmbeddingsRequest{
		Model: "text-embedding-3-small",
		Input: "hello",
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
		t.Fatalf("proxy.Create returned unexpected error: %v", err)
	}

	events := publisher.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 published usage event, got %d", len(events))
	}
	event := events[0]
	if event.CachedTokens != 2 {
		t.Fatalf("expected cached_tokens 2, got %d", event.CachedTokens)
	}
	if event.InputCostMicroyuan != 10 {
		t.Fatalf("expected input_cost_microyuan 10, got %d", event.InputCostMicroyuan)
	}
	if event.OutputCostMicroyuan != 0 {
		t.Fatalf("expected output_cost_microyuan 0, got %d", event.OutputCostMicroyuan)
	}
	if event.CachedCostMicroyuan != 1 {
		t.Fatalf("expected cached_cost_microyuan 1, got %d", event.CachedCostMicroyuan)
	}
	if event.TotalCostMicroyuan != 11 {
		t.Fatalf("expected total_cost_microyuan 11, got %d", event.TotalCostMicroyuan)
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
	result, err := stream.Run(func(chunk []byte) error {
		_, writeErr := chunks.Write(chunk)
		return writeErr
	}, nil)
	if err != nil {
		t.Fatalf("stream.Run returned unexpected error: %v", err)
	}
	resp := result.Response
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
	if recorder.lastRecord.FirstTokenLatencyMS != 0 {
		t.Fatalf("expected zero first token latency without callback, got %d", recorder.lastRecord.FirstTokenLatencyMS)
	}
	if recorder.eventCalls != 0 {
		t.Fatalf("expected no extra lifecycle events, got %d", recorder.eventCalls)
	}
}

func TestValidateChatRequest(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		req     service.ChatRequest
		wantErr string
	}{
		{
			name:    "messages required",
			req:     service.ChatRequest{},
			wantErr: "messages is required",
		},
		{
			name: "message content required",
			req: service.ChatRequest{
				Messages: []service.ChatMessage{{Role: "user", Content: "   "}},
			},
			wantErr: "message content is required",
		},
		{
			name: "max tokens non negative",
			req: service.ChatRequest{
				Messages:  []service.ChatMessage{{Role: "user", Content: "hello"}},
				MaxTokens: -1,
			},
			wantErr: "max_tokens must be greater than or equal to 0",
		},
		{
			name: "model optional",
			req: service.ChatRequest{
				Messages: []service.ChatMessage{{Role: "user", Content: "hello"}},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := service.ValidateChatRequest(tc.req)
			if tc.wantErr == "" && err != nil {
				t.Fatalf("expected nil, got %v", err)
			}
			if tc.wantErr != "" && (err == nil || err.Error() != tc.wantErr) {
				t.Fatalf("expected error %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateEmbeddingsRequest(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		req     service.EmbeddingsRequest
		wantErr string
	}{
		{
			name:    "model required",
			req:     service.EmbeddingsRequest{Input: "hello"},
			wantErr: "model is required",
		},
		{
			name:    "input required",
			req:     service.EmbeddingsRequest{Model: "text-embedding-3-small", Input: "   "},
			wantErr: "input is required",
		},
		{
			name: "valid",
			req:  service.EmbeddingsRequest{Model: "text-embedding-3-small", Input: "hello"},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := service.ValidateEmbeddingsRequest(tc.req)
			if tc.wantErr == "" && err != nil {
				t.Fatalf("expected nil, got %v", err)
			}
			if tc.wantErr != "" && (err == nil || err.Error() != tc.wantErr) {
				t.Fatalf("expected error %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestChatProxyStreamRecordsFirstTokenLatencyAndClientAbortEvent(t *testing.T) {
	t.Parallel()

	recorder := &stubUsageRecorder{}
	clientAbortErr := errors.New("client disconnected")
	proxy := service.NewChatProxyService(
		stubChatClient{
			streamRun: func(emit func([]byte) error, onFirstToken func()) (service.ChatStreamResult, error) {
				time.Sleep(2 * time.Millisecond)
				if onFirstToken != nil {
					onFirstToken()
				}
				resp := service.ChatResponse{
					Model: "gpt-4o-mini",
					Choices: []service.ChatChoice{
						{Message: service.ChatMessage{Role: "assistant", Content: "streamed answer"}},
					},
				}
				if err := emit([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"streamed answer\"}}]}\n\n")); err != nil {
					return service.ChatStreamResult{
						Response:        resp,
						SawContentToken: true,
						ClientAborted:   true,
					}, err
				}
				return service.ChatStreamResult{
					Response:        resp,
					SawContentToken: true,
				}, nil
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

	_, err = stream.Run(func([]byte) error {
		return clientAbortErr
	}, nil)
	if !errors.Is(err, clientAbortErr) {
		t.Fatalf("expected client abort error %v, got %v", clientAbortErr, err)
	}
	if recorder.recordCalls != 1 {
		t.Fatalf("expected 1 usage record write, got %d", recorder.recordCalls)
	}
	if recorder.lastRecord.Status != service.UsageStatusSuccess {
		t.Fatalf("expected success status despite client abort, got %q", recorder.lastRecord.Status)
	}
	if recorder.lastRecord.FirstTokenLatencyMS <= 0 {
		t.Fatalf("expected first token latency to be recorded, got %d", recorder.lastRecord.FirstTokenLatencyMS)
	}
	if recorder.eventCalls != 1 {
		t.Fatalf("expected 1 extra lifecycle event, got %d", recorder.eventCalls)
	}
	if recorder.lastEventType != "client_aborted" {
		t.Fatalf("expected client_aborted event, got %q", recorder.lastEventType)
	}
	if recorder.lastEventRecord.RequestID != recorder.lastRecord.RequestID {
		t.Fatalf("expected event request id %q, got %q", recorder.lastRecord.RequestID, recorder.lastEventRecord.RequestID)
	}
}

func TestChatProxyStreamDoesNotTreatPreTokenAbortAsSuccess(t *testing.T) {
	t.Parallel()

	recorder := &stubUsageRecorder{}
	clientAbortErr := errors.New("client disconnected before content")
	proxy := service.NewChatProxyService(
		stubChatClient{
			streamRun: func(func([]byte) error, func()) (service.ChatStreamResult, error) {
				return service.ChatStreamResult{
					ClientAborted: true,
				}, clientAbortErr
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

	_, err = stream.Run(func([]byte) error {
		return clientAbortErr
	}, nil)
	if !errors.Is(err, clientAbortErr) {
		t.Fatalf("expected client abort error %v, got %v", clientAbortErr, err)
	}
	if recorder.recordCalls != 1 {
		t.Fatalf("expected 1 usage record write, got %d", recorder.recordCalls)
	}
	if recorder.lastRecord.Status == service.UsageStatusSuccess {
		t.Fatalf("expected pre-token abort not to be recorded as success, got %q", recorder.lastRecord.Status)
	}
	if recorder.lastRecord.FirstTokenLatencyMS != 0 {
		t.Fatalf("expected zero first token latency before content token, got %d", recorder.lastRecord.FirstTokenLatencyMS)
	}
	if recorder.eventCalls != 0 {
		t.Fatalf("expected no client_aborted event before content token, got %d", recorder.eventCalls)
	}
}

type stubChatClient struct {
	response  service.ChatResponse
	err       error
	streamRun func(emit func([]byte) error, onFirstToken func()) (service.ChatStreamResult, error)
}

type stubEmbeddingClient struct {
	response service.EmbeddingsResponse
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
		Run: func(emit func([]byte) error, onFirstToken func()) (service.ChatStreamResult, error) {
			if c.streamRun != nil {
				return c.streamRun(emit, onFirstToken)
			}
			if emit != nil {
				if err := emit([]byte("data: [DONE]\n\n")); err != nil {
					return service.ChatStreamResult{}, err
				}
			}
			return service.ChatStreamResult{Response: c.response}, nil
		},
	}, 200, nil
}

func (c stubEmbeddingClient) CreateEmbedding(context.Context, domain.ProviderTarget, service.EmbeddingsRequest) (service.EmbeddingsResponse, int, error) {
	if c.err != nil {
		return service.EmbeddingsResponse{}, 502, c.err
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
	lastRecord          service.UsageRecord
	eventCalls          int
	lastEventRecord     service.UsageRecord
	lastEventType       string
	lastEventDetail     string
}

func (r *stubUsageRecorder) PrepareUsageEventRecord(record service.UsageRecord) (service.UsageRecord, error) {
	return record, nil
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

func (r *stubUsageRecorder) RecordEvent(_ context.Context, record service.UsageRecord, eventType string, detail string) error {
	r.eventCalls++
	r.lastEventRecord = record
	r.lastEventType = eventType
	r.lastEventDetail = detail
	return nil
}
