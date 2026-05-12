package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/example/ai_gateway/gateway/internal/domain"
)

const defaultFAQClassifierModel = "qwen-mt-flash"

type FAQClassifierResult struct {
	Matched    bool    `json:"matched"`
	FAQKey     string  `json:"faq_key"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

type FAQClassifier interface {
	Classify(ctx context.Context, question string) (FAQClassifierResult, error)
}

type noopFAQClassifier struct{}

type transportFAQClassifier struct {
	client  UpstreamChatClient
	target  domain.ProviderTarget
	model   string
	timeout time.Duration
}

func NewNoopFAQClassifier() FAQClassifier {
	return noopFAQClassifier{}
}

func NewTransportFAQClassifier(client UpstreamChatClient, target domain.ProviderTarget, model string, timeout time.Duration) FAQClassifier {
	return transportFAQClassifier{
		client:  client,
		target:  target,
		model:   strings.TrimSpace(model),
		timeout: timeout,
	}
}

func (noopFAQClassifier) Classify(context.Context, string) (FAQClassifierResult, error) {
	return FAQClassifierResult{
		Matched: false,
		Reason:  "noop",
	}, nil
}

func (c transportFAQClassifier) Classify(ctx context.Context, question string) (FAQClassifierResult, error) {
	if c.client == nil {
		return FAQClassifierResult{}, errors.New("service: faq classifier client is required")
	}
	if strings.TrimSpace(question) == "" {
		return FAQClassifierResult{Matched: false, Reason: "empty_question"}, nil
	}

	timeout := c.timeout
	if timeout <= 0 {
		timeout = 1500 * time.Millisecond
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, _, err := c.client.Complete(requestCtx, c.target, ChatRequest{
		Model: nonEmpty(strings.TrimSpace(c.model), defaultFAQClassifierModel),
		Messages: []ChatMessage{
			{
				Role:    "user",
				Content: buildFAQClassifierPrompt(question),
			},
		},
		MaxTokens: 256,
	})
	if err != nil {
		return FAQClassifierResult{}, err
	}
	if len(resp.Choices) == 0 {
		return FAQClassifierResult{}, errors.New("service: faq classifier response has no choices")
	}
	return parseFAQClassifierResult(resp.Choices[0].Message.Content)
}

func parseFAQClassifierResult(content string) (FAQClassifierResult, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return FAQClassifierResult{}, errors.New("service: faq classifier response is empty")
	}

	var result FAQClassifierResult
	if err := json.Unmarshal([]byte(trimmed), &result); err != nil {
		return FAQClassifierResult{}, fmt.Errorf("parse faq classifier result: %w", err)
	}

	result.FAQKey = strings.TrimSpace(result.FAQKey)
	result.Reason = strings.TrimSpace(result.Reason)
	if result.Confidence < 0 {
		result.Confidence = 0
	}
	if result.Confidence > 1 {
		result.Confidence = 1
	}
	if result.Matched && result.FAQKey == "" {
		return FAQClassifierResult{}, errors.New("service: faq classifier matched result requires faq_key")
	}
	return result, nil
}

func buildFAQClassifierPrompt(question string) string {
	return fmt.Sprintf(
		"你是 AI Gateway 的 FAQ 判定器。请判断下面这条用户问题是否属于固定问答白名单，只允许在以下 key 中二选一或返回不命中：faq.greeting.hello、faq.identity.who_are_you、faq.capability.what_can_you_do、faq.platform.what_is_this。你必须只返回单行 JSON，不要输出解释、代码块或额外文字。JSON 格式固定为：{\"matched\":true|false,\"faq_key\":\"...\",\"confidence\":0-1,\"reason\":\"...\"}。如果不确定，必须返回 matched=false。用户问题如下：%s",
		strings.TrimSpace(question),
	)
}
