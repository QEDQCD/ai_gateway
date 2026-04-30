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

func TestRuleBasedSmartRouterIgnoresAssistantMessagesWhenClassifying(t *testing.T) {
	router := service.NewRuleBasedSmartRouter(service.SmartRoutingConfig{
		FastModelTier:       "gateway-chat-fast",
		ReasoningModelTier:  "gateway-chat-reasoning",
		CodingKeywords:      []string{"debug", "报错"},
		EnableCodeFenceRule: true,
	})

	result := router.Decide(service.ChatRequest{
		Model: "qwen-flash",
		Messages: []service.ChatMessage{
			{
				Role:    "assistant",
				Content: "你可以参考这个例子：\n```go\nfunc main() { panic(\"x\") }\n```",
			},
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

func TestRuleBasedSmartRouterDoesNotUpgradeTerminologyQuestionOnSingleSoftKeyword(t *testing.T) {
	router := service.NewRuleBasedSmartRouter(service.SmartRoutingConfig{
		FastModelTier:      "gateway-chat-fast",
		ReasoningModelTier: "gateway-chat-reasoning",
		CodingKeywords:     []string{"debug", "报错"},
	})

	result := router.Decide(service.ChatRequest{
		Model: "qwen-flash",
		Messages: []service.ChatMessage{
			{
				Role:    "user",
				Content: "请解释一下 debug 是什么。",
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

func TestRuleBasedSmartRouterUpgradesShortDirectDebugRequest(t *testing.T) {
	router := service.NewRuleBasedSmartRouter(service.SmartRoutingConfig{
		FastModelTier:      "gateway-chat-fast",
		ReasoningModelTier: "gateway-chat-reasoning",
		CodingKeywords:     []string{"写代码", "实现", "重构", "debug", "报错", "异常", "单元测试", "架构设计"},
	})

	result := router.Decide(service.ChatRequest{
		Model: "qwen-flash",
		Messages: []service.ChatMessage{
			{
				Role:    "user",
				Content: "帮我 debug 这个报错",
			},
		},
	})

	if result.TaskClass != "coding_complex" {
		t.Fatalf("expected task class %q, got %q", "coding_complex", result.TaskClass)
	}
	if result.TargetModelTier != "gateway-chat-reasoning" {
		t.Fatalf("expected target tier %q, got %q", "gateway-chat-reasoning", result.TargetModelTier)
	}
}

func TestRuleBasedSmartRouterUpgradesShortRefactorRequest(t *testing.T) {
	router := service.NewRuleBasedSmartRouter(service.SmartRoutingConfig{
		FastModelTier:      "gateway-chat-fast",
		ReasoningModelTier: "gateway-chat-reasoning",
		CodingKeywords:     []string{"写代码", "实现", "重构", "debug", "报错", "异常", "单元测试", "架构设计"},
	})

	result := router.Decide(service.ChatRequest{
		Model: "qwen-flash",
		Messages: []service.ChatMessage{
			{
				Role:    "user",
				Content: "请帮我重构这段代码",
			},
		},
	})

	if result.TaskClass != "coding_complex" {
		t.Fatalf("expected task class %q, got %q", "coding_complex", result.TaskClass)
	}
	if result.TargetModelTier != "gateway-chat-reasoning" {
		t.Fatalf("expected target tier %q, got %q", "gateway-chat-reasoning", result.TargetModelTier)
	}
}

func TestRuleBasedSmartRouterUpgradesFunctionWritingRequest(t *testing.T) {
	router := service.NewRuleBasedSmartRouter(service.SmartRoutingConfig{
		FastModelTier:      "gateway-chat-fast",
		ReasoningModelTier: "gateway-chat-reasoning",
		CodingKeywords:     []string{"写代码", "实现", "重构", "debug", "报错", "异常", "单元测试", "架构设计"},
	})

	result := router.Decide(service.ChatRequest{
		Model: "qwen-flash",
		Messages: []service.ChatMessage{
			{
				Role:    "user",
				Content: "写一个 Go 函数去重",
			},
		},
	})

	if result.TaskClass != "coding_complex" {
		t.Fatalf("expected task class %q, got %q", "coding_complex", result.TaskClass)
	}
	if result.TargetModelTier != "gateway-chat-reasoning" {
		t.Fatalf("expected target tier %q, got %q", "gateway-chat-reasoning", result.TargetModelTier)
	}
}

func TestRuleBasedSmartRouterDefaultsMatchInternalTiers(t *testing.T) {
	router := service.NewRuleBasedSmartRouter(service.SmartRoutingConfig{})

	simple := router.Decide(service.ChatRequest{
		Messages: []service.ChatMessage{
			{Role: "user", Content: "什么是 API Gateway？"},
		},
	})
	if simple.TargetModelTier != "gateway-chat-fast" {
		t.Fatalf("expected default fast tier %q, got %q", "gateway-chat-fast", simple.TargetModelTier)
	}

	complexRouter := service.NewRuleBasedSmartRouter(service.SmartRoutingConfig{
		EnableCodeFenceRule: true,
	})
	complex := complexRouter.Decide(service.ChatRequest{
		Messages: []service.ChatMessage{
			{
				Role:    "user",
				Content: "请帮我处理下面的报错：\n```go\npanic(\"x\")\n```",
			},
		},
	})
	if complex.TargetModelTier != "gateway-chat-reasoning" {
		t.Fatalf("expected default reasoning tier %q, got %q", "gateway-chat-reasoning", complex.TargetModelTier)
	}
}

func TestRuleBasedSmartRouterDefaultsIncludeCodingKeywords(t *testing.T) {
	router := service.NewRuleBasedSmartRouter(service.SmartRoutingConfig{})

	result := router.Decide(service.ChatRequest{
		Messages: []service.ChatMessage{
			{Role: "user", Content: "写一个 Go 函数去重"},
		},
	})

	if result.TaskClass != "coding_complex" {
		t.Fatalf("expected task class %q, got %q", "coding_complex", result.TaskClass)
	}
	if result.TargetModelTier != "gateway-chat-reasoning" {
		t.Fatalf("expected target tier %q, got %q", "gateway-chat-reasoning", result.TargetModelTier)
	}
}

func TestRuleBasedSmartRouterUsesRuneCountForLongPromptThreshold(t *testing.T) {
	router := service.NewRuleBasedSmartRouter(service.SmartRoutingConfig{
		FastModelTier:       "gateway-chat-fast",
		ReasoningModelTier:  "gateway-chat-reasoning",
		CodingKeywords:      []string{"debug", "报错"},
		LongPromptThreshold: 15,
	})

	result := router.Decide(service.ChatRequest{
		Model: "qwen-flash",
		Messages: []service.ChatMessage{
			{
				Role:    "user",
				Content: "debug 是什么含义",
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
