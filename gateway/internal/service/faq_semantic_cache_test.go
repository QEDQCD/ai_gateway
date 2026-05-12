package service

import (
	"context"
	"errors"
	"testing"

	"github.com/example/ai_gateway/gateway/internal/domain"
)

func TestExtractLastUserQuestion(t *testing.T) {
	t.Parallel()

	question, ok := extractLastUserQuestion([]ChatMessage{
		{Role: "system", Content: "你是助手"},
		{Role: "user", Content: "你好！！！  "},
		{Role: "assistant", Content: "你好"},
		{Role: "user", Content: "  你是谁？？？"},
	})
	if !ok {
		t.Fatal("expected last user question to be extracted")
	}
	if question != "你是谁？" {
		t.Fatalf("expected normalized question, got %q", question)
	}
}

func TestExtractLastUserQuestionReturnsFalseWhenNoUserMessage(t *testing.T) {
	t.Parallel()

	_, ok := extractLastUserQuestion([]ChatMessage{{Role: "assistant", Content: "hello"}})
	if ok {
		t.Fatal("expected extraction to fail without user message")
	}
}

func TestFAQSemanticCacheOrchestratorReturnsHitFromCache(t *testing.T) {
	t.Parallel()

	registry := NewBuiltinFAQRegistry()
	classifier := stubFAQClassifier{
		result: FAQClassifierResult{
			Matched:    true,
			FAQKey:     "faq.identity.who_are_you",
			Confidence: 0.99,
		},
	}
	cache := &stubFAQCacheService{
		getEntry: FAQCacheEntry{
			FAQKey:  "faq.identity.who_are_you",
			Answer:  "我是企业 AI Gateway 提供的智能助手。",
			Version: "v1",
			Source:  "builtin",
		},
		getHit: true,
	}
	orchestrator := NewFAQSemanticCacheOrchestrator(registry, classifier, cache, "qwen-mt-flash", 0.90)

	outcome, err := orchestrator.TryServe(context.Background(), domain.RequestContext{
		PlatformAPIKeyID: "pak_demo",
	}, ChatRequest{
		Model: "qwen-flash",
		Messages: []ChatMessage{
			{Role: "user", Content: "你是谁？？？"},
		},
	})
	if err != nil {
		t.Fatalf("TryServe returned error: %v", err)
	}
	if !outcome.Hit {
		t.Fatalf("expected cache hit outcome, got %+v", outcome)
	}
	if outcome.Metadata.CacheKey != "faq_cache:pak_demo:faq.identity.who_are_you:v1" {
		t.Fatalf("unexpected cache key %q", outcome.Metadata.CacheKey)
	}
	if outcome.Metadata.CacheFAQKey != "faq.identity.who_are_you" {
		t.Fatalf("unexpected faq key %q", outcome.Metadata.CacheFAQKey)
	}
	if got := outcome.Response.Choices[0].Message.Content; got != "我是企业 AI Gateway 提供的智能助手。" {
		t.Fatalf("unexpected response content %q", got)
	}
}

func TestFAQSemanticCacheOrchestratorFallsBackWhenConfidenceTooLow(t *testing.T) {
	t.Parallel()

	registry := NewBuiltinFAQRegistry()
	classifier := stubFAQClassifier{
		result: FAQClassifierResult{
			Matched:    true,
			FAQKey:     "faq.identity.who_are_you",
			Confidence: 0.50,
		},
	}
	orchestrator := NewFAQSemanticCacheOrchestrator(registry, classifier, NewNoopFAQCacheService(), "qwen-mt-flash", 0.90)

	outcome, err := orchestrator.TryServe(context.Background(), domain.RequestContext{
		PlatformAPIKeyID: "pak_demo",
	}, ChatRequest{
		Model: "qwen-flash",
		Messages: []ChatMessage{
			{Role: "user", Content: "你是谁"},
		},
	})
	if err != nil {
		t.Fatalf("TryServe returned error: %v", err)
	}
	if outcome.Hit {
		t.Fatalf("expected miss outcome, got %+v", outcome)
	}
	if outcome.Metadata.ClassifierStatus != "confidence_too_low" {
		t.Fatalf("expected confidence_too_low, got %+v", outcome.Metadata)
	}
}

type stubFAQClassifier struct {
	result FAQClassifierResult
	err    error
}

func (s stubFAQClassifier) Classify(context.Context, string) (FAQClassifierResult, error) {
	if s.err != nil {
		return FAQClassifierResult{}, s.err
	}
	return s.result, nil
}

type stubFAQCacheService struct {
	getEntry FAQCacheEntry
	getHit   bool
	getErr   error
	setEntry FAQCacheEntry
	setErr   error
}

func (s *stubFAQCacheService) Get(context.Context, string, FAQEntry) (FAQCacheEntry, bool, error) {
	return s.getEntry, s.getHit, s.getErr
}

func (s *stubFAQCacheService) Set(_ context.Context, _ string, faq FAQEntry) (FAQCacheEntry, error) {
	if s.setErr != nil {
		return FAQCacheEntry{}, s.setErr
	}
	if s.setEntry.FAQKey == "" {
		return FAQCacheEntry{
			FAQKey:  faq.Key,
			Answer:  faq.Answer,
			Version: faq.Version,
			Source:  "builtin",
		}, nil
	}
	return s.setEntry, nil
}

var _ FAQClassifier = stubFAQClassifier{}
var _ FAQCacheService = (*stubFAQCacheService)(nil)
var _ = errors.New
