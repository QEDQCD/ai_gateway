package service_test

import (
	"testing"

	"github.com/example/ai_gateway/gateway/internal/service"
)

func TestRuleBasedSmartRouterClassifiesComplexCodingPrompt(t *testing.T) {
	router := service.NewRuleBasedSmartRouter(service.SmartRoutingConfig{
		FastModelTier:      "gateway-chat-fast",
		ReasoningModelTier: "gateway-chat-reasoning",
		CodingKeywords: []string{
			"写代码",
			"debug",
			"报错",
			"重构",
		},
		LongPromptThreshold:  240,
		EnableCodeFenceRule:  true,
		EnableStackTraceRule: true,
	})

	result := router.Decide(service.ChatRequest{
		Model: "qwen-flash",
		Messages: []service.ChatMessage{
			{
				Role:    "user",
				Content: "请帮我 debug 下面 Go 代码为什么 panic:\n```go\nfunc main(){ panic(\"x\") }\n```",
			},
		},
	})

	if result.TaskClass != "coding_complex" {
		t.Fatalf("expected task class %q, got %q", "coding_complex", result.TaskClass)
	}
	if result.TargetModelTier != "gateway-chat-reasoning" {
		t.Fatalf("expected target tier %q, got %q", "gateway-chat-reasoning", result.TargetModelTier)
	}
	if len(result.MatchedRules) == 0 {
		t.Fatal("expected matched rules to be recorded")
	}
}

func TestRuleBasedSmartRouterFallsBackToFastModelForSimpleQuestion(t *testing.T) {
	router := service.NewRuleBasedSmartRouter(service.SmartRoutingConfig{
		FastModelTier:      "gateway-chat-fast",
		ReasoningModelTier: "gateway-chat-reasoning",
		CodingKeywords:     []string{"写代码", "debug", "报错"},
	})

	result := router.Decide(service.ChatRequest{
		Model: "qwen-flash",
		Messages: []service.ChatMessage{
			{
				Role:    "user",
				Content: "请用一句话解释什么是 API Gateway。",
			},
		},
	})

	if result.TaskClass != "simple_qa" {
		t.Fatalf("expected task class %q, got %q", "simple_qa", result.TaskClass)
	}
	if result.TargetModelTier != "gateway-chat-fast" {
		t.Fatalf("expected target tier %q, got %q", "gateway-chat-fast", result.TargetModelTier)
	}
}
