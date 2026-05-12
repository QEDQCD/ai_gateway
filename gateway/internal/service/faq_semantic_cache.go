package service

import (
	"context"
	"strings"
	"time"

	"github.com/example/ai_gateway/gateway/internal/domain"
)

type FAQSemanticCacheMetadata struct {
	CacheHit            bool
	CacheType           string
	CacheKey            string
	CacheFAQKey         string
	ClassifierModel     string
	ClassifierStatus    string
	ClassifierLatencyMS int64
}

type FAQSemanticCacheOutcome struct {
	Hit      bool
	Response ChatResponse
	Metadata FAQSemanticCacheMetadata
}

type FAQSemanticCacheOrchestrator interface {
	TryServe(ctx context.Context, requestContext domain.RequestContext, req ChatRequest) (FAQSemanticCacheOutcome, error)
}

type noopFAQSemanticCacheOrchestrator struct{}

type faqSemanticCacheOrchestrator struct {
	registry            FAQRegistry
	classifier          FAQClassifier
	cache               FAQCacheService
	classifierModel     string
	confidenceThreshold float64
}

func NewNoopFAQSemanticCacheOrchestrator() FAQSemanticCacheOrchestrator {
	return noopFAQSemanticCacheOrchestrator{}
}

func NewFAQSemanticCacheOrchestrator(
	registry FAQRegistry,
	classifier FAQClassifier,
	cache FAQCacheService,
	classifierModel string,
	confidenceThreshold float64,
) FAQSemanticCacheOrchestrator {
	if registry == nil || classifier == nil || cache == nil {
		return noopFAQSemanticCacheOrchestrator{}
	}
	if confidenceThreshold <= 0 {
		confidenceThreshold = 0.90
	}
	return faqSemanticCacheOrchestrator{
		registry:            registry,
		classifier:          classifier,
		cache:               cache,
		classifierModel:     strings.TrimSpace(classifierModel),
		confidenceThreshold: confidenceThreshold,
	}
}

func (noopFAQSemanticCacheOrchestrator) TryServe(context.Context, domain.RequestContext, ChatRequest) (FAQSemanticCacheOutcome, error) {
	return FAQSemanticCacheOutcome{}, nil
}

func (o faqSemanticCacheOrchestrator) TryServe(ctx context.Context, requestContext domain.RequestContext, req ChatRequest) (FAQSemanticCacheOutcome, error) {
	if req.Stream {
		return FAQSemanticCacheOutcome{}, nil
	}

	question, ok := extractLastUserQuestion(req.Messages)
	if !ok {
		return FAQSemanticCacheOutcome{
			Metadata: FAQSemanticCacheMetadata{
				ClassifierModel:  o.classifierModel,
				ClassifierStatus: "no_user_question",
			},
		}, nil
	}

	classifierStartedAt := time.Now().UTC()
	result, err := o.classifier.Classify(ctx, question)
	latencyMS := durationMilliseconds(time.Since(classifierStartedAt))
	if err != nil {
		return FAQSemanticCacheOutcome{
			Metadata: FAQSemanticCacheMetadata{
				ClassifierModel:     o.classifierModel,
				ClassifierStatus:    "classifier_error",
				ClassifierLatencyMS: latencyMS,
			},
		}, err
	}
	if !result.Matched {
		return FAQSemanticCacheOutcome{
			Metadata: FAQSemanticCacheMetadata{
				ClassifierModel:     o.classifierModel,
				ClassifierStatus:    "miss",
				ClassifierLatencyMS: latencyMS,
			},
		}, nil
	}
	if result.Confidence < o.confidenceThreshold {
		return FAQSemanticCacheOutcome{
			Metadata: FAQSemanticCacheMetadata{
				ClassifierModel:     o.classifierModel,
				ClassifierStatus:    "confidence_too_low",
				CacheFAQKey:         result.FAQKey,
				ClassifierLatencyMS: latencyMS,
			},
		}, nil
	}

	faq, ok := o.registry.Get(result.FAQKey)
	if !ok {
		return FAQSemanticCacheOutcome{
			Metadata: FAQSemanticCacheMetadata{
				ClassifierModel:     o.classifierModel,
				ClassifierStatus:    "unknown_faq_key",
				CacheFAQKey:         result.FAQKey,
				ClassifierLatencyMS: latencyMS,
			},
		}, nil
	}

	cacheKey, err := buildFAQCacheKey(requestContext.PlatformAPIKeyID, faq.Key, faq.Version)
	if err != nil {
		return FAQSemanticCacheOutcome{
			Metadata: FAQSemanticCacheMetadata{
				ClassifierModel:     o.classifierModel,
				ClassifierStatus:    "cache_key_error",
				CacheFAQKey:         faq.Key,
				ClassifierLatencyMS: latencyMS,
			},
		}, err
	}

	entry, hit, err := o.cache.Get(ctx, requestContext.PlatformAPIKeyID, faq)
	if err != nil {
		return FAQSemanticCacheOutcome{
			Metadata: FAQSemanticCacheMetadata{
				ClassifierModel:     o.classifierModel,
				ClassifierStatus:    "cache_read_error",
				CacheFAQKey:         faq.Key,
				CacheKey:            cacheKey,
				ClassifierLatencyMS: latencyMS,
			},
		}, err
	}
	if !hit {
		entry, err = o.cache.Set(ctx, requestContext.PlatformAPIKeyID, faq)
		if err != nil {
			entry = FAQCacheEntry{
				FAQKey:  faq.Key,
				Answer:  faq.Answer,
				Version: faq.Version,
				Source:  "builtin",
			}
		}
	}

	response := buildFAQCacheChatResponse(req, entry.Answer)
	return FAQSemanticCacheOutcome{
		Hit: true,
		Response: response,
		Metadata: FAQSemanticCacheMetadata{
			CacheHit:         true,
			CacheType:        "faq_semantic",
			CacheKey:         cacheKey,
			CacheFAQKey:      faq.Key,
			ClassifierModel:  o.classifierModel,
			ClassifierStatus: "hit",
			ClassifierLatencyMS: latencyMS,
		},
	}, nil
}

func buildFAQCacheChatResponse(req ChatRequest, answer string) ChatResponse {
	response := ChatResponse{
		Model: strings.TrimSpace(req.Model),
		Choices: []ChatChoice{
			{Message: ChatMessage{Role: "assistant", Content: strings.TrimSpace(answer)}},
		},
	}
	usage := estimateChatUsage(req, response)
	response.Usage = &usage
	return response
}

func extractLastUserQuestion(messages []ChatMessage) (string, bool) {
	for index := len(messages) - 1; index >= 0; index-- {
		if strings.TrimSpace(messages[index].Role) != "user" {
			continue
		}
		content := normalizeFAQQuestion(messages[index].Content)
		if content == "" {
			return "", false
		}
		return content, true
	}
	return "", false
}

func normalizeFAQQuestion(input string) string {
	text := strings.TrimSpace(input)
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	replacer := strings.NewReplacer(
		"???", "？",
		"？？？", "？",
		"??", "？",
		"!!!", "！",
		"！！！", "！",
		"!!", "！",
	)
	text = replacer.Replace(text)
	for strings.Contains(text, "。。") {
		text = strings.ReplaceAll(text, "。。", "。")
	}
	for strings.Contains(text, "？？") {
		text = strings.ReplaceAll(text, "？？", "？")
	}
	for strings.Contains(text, "！！") {
		text = strings.ReplaceAll(text, "！！", "！")
	}
	return text
}
