package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/example/ai_gateway/gateway/internal/domain"
)

func TestParseFAQClassifierResultAcceptsMatchedPayload(t *testing.T) {
	t.Parallel()

	result, err := parseFAQClassifierResult(`{
		"matched": true,
		"faq_key": "faq.identity.who_are_you",
		"confidence": 0.92,
		"reason": "明确身份问句"
	}`)
	if err != nil {
		t.Fatalf("parseFAQClassifierResult returned error: %v", err)
	}
	if !result.Matched {
		t.Fatal("expected matched result")
	}
	if result.FAQKey != "faq.identity.who_are_you" {
		t.Fatalf("expected faq key %q, got %q", "faq.identity.who_are_you", result.FAQKey)
	}
	if result.Confidence != 0.92 {
		t.Fatalf("expected confidence 0.92, got %v", result.Confidence)
	}
}

func TestParseFAQClassifierResultRejectsMatchedPayloadWithoutKey(t *testing.T) {
	t.Parallel()

	_, err := parseFAQClassifierResult(`{"matched":true,"faq_key":"   ","confidence":0.6}`)
	if err == nil {
		t.Fatal("expected parseFAQClassifierResult to reject empty faq key when matched")
	}
}

func TestNoopFAQClassifierAlwaysReturnsNoMatch(t *testing.T) {
	t.Parallel()

	classifier := NewNoopFAQClassifier()
	result, err := classifier.Classify(context.Background(), "你是谁？")
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}
	if result.Matched || result.Reason != "noop" {
		t.Fatalf("expected noop no-match result, got %+v", result)
	}
}

func TestTransportFAQClassifierBuildsPromptAndParsesResult(t *testing.T) {
	t.Parallel()

	client := &fakeFAQClassifierChatClient{
		response: ChatResponse{
			Choices: []ChatChoice{{
				Message: ChatMessage{
					Role:    "assistant",
					Content: `{"matched":true,"faq_key":"faq.capability.what_can_you_do","confidence":0.88,"reason":"能力咨询"}`,
				},
			}},
		},
		statusCode: 200,
	}

	classifier := NewTransportFAQClassifier(client, domain.ProviderTarget{
		CredentialID: "provider_faq",
		Provider:     "dashscope",
	}, "qwen-mt-flash", time.Second)

	result, err := classifier.Classify(context.Background(), "你能做什么？")
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}
	if client.calls != 1 {
		t.Fatalf("expected 1 classifier call, got %d", client.calls)
	}
	if client.lastReq.Model != "qwen-mt-flash" {
		t.Fatalf("expected classifier model qwen-mt-flash, got %q", client.lastReq.Model)
	}
	if len(client.lastReq.Messages) != 1 || client.lastReq.Messages[0].Role != "user" {
		t.Fatalf("unexpected classifier request: %+v", client.lastReq)
	}
	if !strings.Contains(client.lastReq.Messages[0].Content, "faq.capability.what_can_you_do") {
		t.Fatalf("expected classifier prompt to mention faq keys, got %q", client.lastReq.Messages[0].Content)
	}
	if !result.Matched || result.FAQKey != "faq.capability.what_can_you_do" {
		t.Fatalf("expected matched faq result, got %+v", result)
	}
}

type fakeFAQClassifierChatClient struct {
	response   ChatResponse
	statusCode int
	err        error
	calls      int
	lastTarget domain.ProviderTarget
	lastReq    ChatRequest
}

func (f *fakeFAQClassifierChatClient) Complete(_ context.Context, target domain.ProviderTarget, req ChatRequest) (ChatResponse, int, error) {
	f.calls++
	f.lastTarget = target
	f.lastReq = req
	return f.response, f.statusCode, f.err
}

func (*fakeFAQClassifierChatClient) StreamComplete(context.Context, domain.ProviderTarget, ChatRequest) (ChatCompletionStream, int, error) {
	return ChatCompletionStream{}, 0, nil
}
