package service_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
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

func TestChatProxyCompleteRedactsSensitiveContentInResponse(t *testing.T) {
	t.Parallel()

	proxy := service.NewChatProxyService(
		stubChatClient{
			response: service.ChatResponse{
				Model: "qwen-flash",
				Choices: []service.ChatChoice{
					{Message: service.ChatMessage{Role: "assistant", Content: "您的手机号是13333333333"}},
				},
			},
		},
		queue.NewNoopUsagePublisher(),
	)

	resp, err := proxy.Complete(context.Background(), service.ChatRequest{
		Model: "qwen-flash",
		Messages: []service.ChatMessage{
			{Role: "user", Content: "本人手机号是13333333333，我的手机号多少"},
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

	if got := resp.Choices[0].Message.Content; got != "您的手机号是133XXXX3333" {
		t.Fatalf("expected redacted response %q, got %q", "您的手机号是133XXXX3333", got)
	}
}

func TestChatProxyStreamRedactsSensitiveContentInFinalResponseAndForwardedChunks(t *testing.T) {
	t.Parallel()

	proxy := service.NewChatProxyService(
		stubChatClient{
			streamRun: func(emit func([]byte) error, onFirstToken func()) (service.ChatStreamResult, error) {
				if onFirstToken != nil {
					onFirstToken()
				}
				if err := emit([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"手机号13333333333\"}}]}\n\n")); err != nil {
					return service.ChatStreamResult{}, err
				}
				if err := emit([]byte("data: [DONE]\n\n")); err != nil {
					return service.ChatStreamResult{}, err
				}
				return service.ChatStreamResult{
					SawContentToken: true,
					Response: service.ChatResponse{
						Model: "qwen-flash",
						Choices: []service.ChatChoice{
							{Message: service.ChatMessage{Role: "assistant", Content: "手机号13333333333"}},
						},
					},
				}, nil
			},
		},
		queue.NewNoopUsagePublisher(),
	)

	stream, err := proxy.Stream(context.Background(), service.ChatRequest{
		Model:  "qwen-flash",
		Stream: true,
		Messages: []service.ChatMessage{
			{Role: "user", Content: "本人手机号是13333333333"},
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

	if strings.Contains(chunks.String(), "13333333333") {
		t.Fatalf("expected stream chunks to redact phone number, got %q", chunks.String())
	}
	if !strings.Contains(chunks.String(), "133XXXX3333") {
		t.Fatalf("expected stream chunks to contain redacted phone number, got %q", chunks.String())
	}
	if got := result.Response.Choices[0].Message.Content; got != "手机号133XXXX3333" {
		t.Fatalf("expected redacted final response %q, got %q", "手机号133XXXX3333", got)
	}
}

func TestChatProxyCompleteBlocksAttackBeforeUpstream(t *testing.T) {
	t.Parallel()

	upstreamCalled := false
	proxy := service.NewChatProxyServiceWithGuard(
		stubChatClient{
			completeFn: func(context.Context, domain.ProviderTarget, service.ChatRequest) (service.ChatResponse, int, error) {
				upstreamCalled = true
				return service.ChatResponse{
					Model: "qwen-flash",
					Choices: []service.ChatChoice{
						{Message: service.ChatMessage{Role: "assistant", Content: "should not happen"}},
					},
				}, 200, nil
			},
		},
		queue.NewNoopUsagePublisher(),
		service.NewContentGuardService(&stubContentModeratorProxy{
			result: service.ModerationResult{
				Decision: service.ModerationDecisionBlock,
				Reason:   "检测到疑似 SQL 注入内容",
			},
		}),
	)

	_, err := proxy.Complete(context.Background(), service.ChatRequest{
		Model: "qwen-flash",
		Messages: []service.ChatMessage{
			{Role: "user", Content: "' OR 1=1 --"},
		},
	}, validRequestContext())
	if err == nil {
		t.Fatal("expected block error")
	}
	if upstreamCalled {
		t.Fatal("expected upstream not to be called")
	}
	code, message, ok := service.StatusCodeFromError(err)
	if !ok {
		t.Fatalf("expected status error, got %v", err)
	}
	if code != 400 {
		t.Fatalf("expected block status 400, got %d", code)
	}
	if !strings.Contains(message, "请求被安全策略拦截") {
		t.Fatalf("expected chinese block message, got %q", message)
	}
}

func TestChatProxyCompleteSendsSanitizedMessagesUpstream(t *testing.T) {
	t.Parallel()

	var forwarded service.ChatRequest
	proxy := service.NewChatProxyServiceWithGuard(
		stubChatClient{
			completeFn: func(_ context.Context, _ domain.ProviderTarget, req service.ChatRequest) (service.ChatResponse, int, error) {
				forwarded = req
				return service.ChatResponse{
					Model: "qwen-flash",
					Choices: []service.ChatChoice{
						{Message: service.ChatMessage{Role: "assistant", Content: "ok"}},
					},
				}, 200, nil
			},
		},
		queue.NewNoopUsagePublisher(),
		service.NewContentGuardService(&stubContentModeratorProxy{
			result: service.ModerationResult{
				Decision: service.ModerationDecisionAllow,
				Reason:   "safe",
				Redactions: []service.ModerationRedaction{
					{Text: "13812345678", Replacement: "***"},
				},
			},
		}),
	)

	_, err := proxy.Complete(context.Background(), service.ChatRequest{
		Model: "qwen-flash",
		Messages: []service.ChatMessage{
			{Role: "user", Content: "我手机号是13812345678"},
		},
	}, validRequestContext())
	if err != nil {
		t.Fatalf("proxy.Complete returned unexpected error: %v", err)
	}
	if len(forwarded.Messages) != 1 {
		t.Fatalf("expected 1 forwarded message, got %d", len(forwarded.Messages))
	}
	if got := forwarded.Messages[0].Content; got != "我手机号是***" {
		t.Fatalf("expected upstream sanitized message %q, got %q", "我手机号是***", got)
	}
}

func TestChatProxyCompleteRecordsSecurityGuardBlockedRequest(t *testing.T) {
	t.Parallel()

	recorder := &stubUsageRecorder{}
	proxy := service.NewChatProxyServiceWithGuard(
		stubChatClient{},
		queue.NewNoopUsagePublisher(),
		service.NewContentGuardService(&stubContentModeratorProxy{
			result: service.ModerationResult{
				Decision: service.ModerationDecisionBlock,
				Reason:   "包含明显 SQL 注入攻击意图",
			},
		}),
		recorder,
	)

	_, err := proxy.Complete(context.Background(), service.ChatRequest{
		Model: "qwen-flash",
		Messages: []service.ChatMessage{
			{Role: "user", Content: "SELECT * FROM users WHERE name = '' OR 1=1 --"},
		},
	}, validRequestContext())
	if err == nil {
		t.Fatal("expected block error")
	}

	if recorder.recordCalls != 1 {
		t.Fatalf("expected blocked request to record once, got %d", recorder.recordCalls)
	}
	if recorder.lastRecord.StatusCode != 400 {
		t.Fatalf("expected blocked request status 400, got %d", recorder.lastRecord.StatusCode)
	}
	if !strings.Contains(recorder.lastRecord.ErrorMessage, "包含明显 SQL 注入攻击意图") {
		t.Fatalf("expected blocked reason to be recorded, got %q", recorder.lastRecord.ErrorMessage)
	}
	if recorder.eventCalls != 1 {
		t.Fatalf("expected blocked request to emit 1 extra event, got %d", recorder.eventCalls)
	}
	if recorder.lastEventType != "security_guard_blocked" {
		t.Fatalf("expected blocked event type security_guard_blocked, got %q", recorder.lastEventType)
	}
	if !strings.Contains(recorder.lastEventDetail, "包含明显 SQL 注入攻击意图") {
		t.Fatalf("expected blocked event detail to include reason, got %q", recorder.lastEventDetail)
	}
}

func TestChatProxyCompleteRecordsSecurityGuardFallbackEvent(t *testing.T) {
	t.Parallel()

	recorder := &stubUsageRecorder{}
	proxy := service.NewChatProxyServiceWithGuard(
		stubChatClient{
			response: service.ChatResponse{
				Model: "qwen-flash",
				Choices: []service.ChatChoice{
					{Message: service.ChatMessage{Role: "assistant", Content: "ok"}},
				},
			},
		},
		queue.NewNoopUsagePublisher(),
		service.NewContentGuardService(&stubContentModeratorProxy{
			err: errors.New("moderation upstream timeout"),
		}),
		recorder,
	)

	_, err := proxy.Complete(context.Background(), service.ChatRequest{
		Model: "qwen-flash",
		Messages: []service.ChatMessage{
			{Role: "user", Content: "我的手机号是13812345678"},
		},
	}, validRequestContext())
	if err != nil {
		t.Fatalf("proxy.Complete returned unexpected error: %v", err)
	}

	if recorder.recordCalls != 1 {
		t.Fatalf("expected fallback request to record once, got %d", recorder.recordCalls)
	}
	if recorder.eventCalls != 1 {
		t.Fatalf("expected fallback request to emit 1 extra event, got %d", recorder.eventCalls)
	}
	if recorder.lastEventType != "security_guard_fallback" {
		t.Fatalf("expected fallback event type security_guard_fallback, got %q", recorder.lastEventType)
	}
	if !strings.Contains(recorder.lastEventDetail, "fallback_regex") {
		t.Fatalf("expected fallback event detail to mention fallback_regex, got %q", recorder.lastEventDetail)
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
	response   service.ChatResponse
	err        error
	completeFn func(context.Context, domain.ProviderTarget, service.ChatRequest) (service.ChatResponse, int, error)
	streamRun  func(emit func([]byte) error, onFirstToken func()) (service.ChatStreamResult, error)
}

type stubEmbeddingClient struct {
	response service.EmbeddingsResponse
	err      error
}

func (c stubChatClient) Complete(ctx context.Context, target domain.ProviderTarget, req service.ChatRequest) (service.ChatResponse, int, error) {
	if c.completeFn != nil {
		return c.completeFn(ctx, target, req)
	}
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

type stubContentModeratorProxy struct {
	result service.ModerationResult
	err    error
}

func (s *stubContentModeratorProxy) Moderate(context.Context, string) (service.ModerationResult, error) {
	if s.err != nil {
		return service.ModerationResult{}, s.err
	}
	return s.result, nil
}

func validRequestContext() domain.RequestContext {
	return domain.RequestContext{
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
	}
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
