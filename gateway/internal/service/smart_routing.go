package service

import "strings"

type SmartRoutingConfig struct {
	FastModelTier        string
	ReasoningModelTier   string
	CodingKeywords       []string
	LongPromptThreshold  int
	EnableCodeFenceRule  bool
	EnableStackTraceRule bool
}

type SmartRoutingDecision struct {
	TaskClass       string
	TargetModelTier string
	MatchedRules    []string
}

type SmartRouter interface {
	Decide(req ChatRequest) SmartRoutingDecision
}

type ruleBasedSmartRouter struct {
	cfg SmartRoutingConfig
}

func NewRuleBasedSmartRouter(cfg SmartRoutingConfig) SmartRouter {
	if strings.TrimSpace(cfg.FastModelTier) == "" {
		cfg.FastModelTier = "gateway-chat-fast"
	}
	if strings.TrimSpace(cfg.ReasoningModelTier) == "" {
		cfg.ReasoningModelTier = "gateway-chat-reasoning"
	}
	if cfg.LongPromptThreshold <= 0 {
		cfg.LongPromptThreshold = 240
	}
	return ruleBasedSmartRouter{cfg: cfg}
}

func (r ruleBasedSmartRouter) Decide(req ChatRequest) SmartRoutingDecision {
	content := aggregateUserMessages(req.Messages)
	normalized := strings.ToLower(content)
	matched := make([]string, 0, 4)
	softKeywordMatches := 0
	directKeywordMatches := 0

	for _, keyword := range r.cfg.CodingKeywords {
		keyword = strings.TrimSpace(keyword)
		if keyword != "" && strings.Contains(normalized, strings.ToLower(keyword)) {
			matched = append(matched, "keyword:"+keyword)
			if isSoftCodingKeyword(keyword) {
				softKeywordMatches++
				continue
			}
			directKeywordMatches++
		}
	}

	hasHardSignal := false
	if r.cfg.EnableCodeFenceRule && strings.Contains(content, "```") {
		hasHardSignal = true
		matched = append(matched, "pattern:code_fence")
	}

	if r.cfg.EnableStackTraceRule && containsStackTrace(content) {
		hasHardSignal = true
		matched = append(matched, "pattern:stack_trace")
	}

	hasDirectArtifactRequest := containsDirectArtifactRequest(normalized)
	if hasDirectArtifactRequest {
		matched = append(matched, "pattern:direct_artifact_request")
	}

	hasTechnicalArtifact := containsTechnicalArtifact(normalized)
	hasDirectIntent := directKeywordMatches > 0 || hasDirectArtifactRequest
	hasSoftIntentCombo := softKeywordMatches >= 2 || (softKeywordMatches > 0 && hasTechnicalArtifact)
	hasLongCodingPrompt := len(content) >= r.cfg.LongPromptThreshold && (softKeywordMatches > 0 || directKeywordMatches > 0)
	if hasLongCodingPrompt {
		matched = append(matched, "signal:long_prompt")
	}

	if hasSoftIntentCombo {
		matched = append(matched, "signal:soft_keyword_combo")
	}

	if hasHardSignal || hasDirectIntent || hasSoftIntentCombo || hasLongCodingPrompt {
		return SmartRoutingDecision{
			TaskClass:       "coding_complex",
			TargetModelTier: r.cfg.ReasoningModelTier,
			MatchedRules:    matched,
		}
	}

	return SmartRoutingDecision{
		TaskClass:       "simple_qa",
		TargetModelTier: r.cfg.FastModelTier,
	}
}

func aggregateUserMessages(messages []ChatMessage) string {
	if len(messages) == 0 {
		return ""
	}

	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		if !strings.EqualFold(strings.TrimSpace(message.Role), "user") {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if content != "" {
			parts = append(parts, content)
		}
	}

	return strings.Join(parts, "\n")
}

func containsStackTrace(content string) bool {
	return strings.Contains(content, "panic:") ||
		strings.Contains(content, "Traceback") ||
		strings.Contains(content, "Exception")
}

func isSoftCodingKeyword(keyword string) bool {
	switch strings.ToLower(strings.TrimSpace(keyword)) {
	case "debug", "报错", "异常":
		return true
	default:
		return false
	}
}

func containsDirectArtifactRequest(content string) bool {
	return containsAny(content, "写", "编写", "生成", "创建") && containsTechnicalArtifact(content)
}

func containsTechnicalArtifact(content string) bool {
	return containsAny(content, "代码", "函数", "脚本", "sql", "查询")
}

func containsAny(content string, patterns ...string) bool {
	for _, pattern := range patterns {
		if pattern != "" && strings.Contains(content, pattern) {
			return true
		}
	}
	return false
}
