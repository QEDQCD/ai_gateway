package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/example/ai_gateway/gateway/internal/domain"
)

type ModerationDecision string

const (
	ModerationDecisionAllow ModerationDecision = "allow"
	ModerationDecisionBlock ModerationDecision = "block"
)

type ModerationRedaction struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Replacement string `json:"replacement"`
}

type ModerationResult struct {
	Decision   ModerationDecision    `json:"decision"`
	Reason     string                `json:"reason"`
	AttackType string                `json:"attack_type"`
	Redactions []ModerationRedaction `json:"redactions"`
}

const defaultContentGuardModel = "qwen-mt-flash"

const moderationPromptTemplate = "你是 AI Gateway 的内容审核器。你只需要判断下面这段用户文本是否包含明显网络攻击意图，并识别其中需要脱敏的敏感信息。攻击类型只考虑：sql_injection、xss、command_injection、ssrf、path_traversal、deserialization_exploit、privilege_bypass、malicious_script_generation、other_attack。你必须只返回单行 JSON，不要输出解释、代码块或额外文字。JSON 格式固定为：{\"decision\":\"allow|block\",\"reason\":\"...\",\"attack_type\":\"...\",\"redactions\":[{\"type\":\"phone\",\"text\":\"...\",\"replacement\":\"***\"}]}。\n判定规则：\n1. 只有在文本本身包含明显攻击 payload、利用语句、破坏指令、越权绕过、恶意脚本、路径穿越或 SSRF 探测意图时，才允许判定为 block。\n2. 普通业务咨询、手机号/身份证/邮箱等个人信息、要求复述或处理敏感信息、普通技术问答、普通 SQL 解释、普通代码片段，一律优先 allow，不得仅因出现技术词汇或敏感信息而 block。\n3. 如果不确定，必须默认 allow，并只在 redactions 中返回需要脱敏的敏感片段。\n4. 对于 `SELECT * FROM users WHERE name = '' OR 1=1 --`、`<script>alert(1)</script>`、`../../etc/passwd`、`curl 169.254.169.254/latest/meta-data` 这类明显攻击 payload，应当 block。\n待审核用户文本如下：\n%s"

type ModerationClient struct {
	client  UpstreamChatClient
	target  domain.ProviderTarget
	model   string
	timeout time.Duration
}

func NewModerationClient() ModerationClient {
	return ModerationClient{}
}

func NewTransportModerationClient(client UpstreamChatClient, target domain.ProviderTarget, model string, timeout time.Duration) ModerationClient {
	return ModerationClient{
		client:  client,
		target:  target,
		model:   strings.TrimSpace(model),
		timeout: timeout,
	}
}

func (c ModerationClient) Moderate(ctx context.Context, input string) (ModerationResult, error) {
	if c.client == nil {
		return ModerationResult{}, fmt.Errorf("moderation client transport is required")
	}
	if strings.TrimSpace(input) == "" {
		return ModerationResult{
			Decision: ModerationDecisionAllow,
			Reason:   "empty user input",
		}, nil
	}

	timeout := c.timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, _, err := c.client.Complete(requestCtx, c.target, ChatRequest{
		Model: nonEmpty(c.model, defaultContentGuardModel),
		Messages: []ChatMessage{
			{Role: "user", Content: fmt.Sprintf(moderationPromptTemplate, input)},
		},
		MaxTokens: 256,
	})
	if err != nil {
		return ModerationResult{}, err
	}
	if len(resp.Choices) == 0 {
		return ModerationResult{}, fmt.Errorf("moderation response contains no choices")
	}
	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	if content == "" {
		return ModerationResult{}, fmt.Errorf("moderation response content is empty")
	}
	return c.ParseResult([]byte(content))
}

func (ModerationClient) ParseResult(payload []byte) (ModerationResult, error) {
	var result ModerationResult
	if err := json.Unmarshal(payload, &result); err != nil {
		return ModerationResult{}, fmt.Errorf("parse moderation result: %w", err)
	}
	result.Reason = strings.TrimSpace(result.Reason)
	result.AttackType = strings.TrimSpace(result.AttackType)
	for index := range result.Redactions {
		result.Redactions[index].Type = strings.TrimSpace(result.Redactions[index].Type)
		result.Redactions[index].Text = strings.TrimSpace(result.Redactions[index].Text)
		result.Redactions[index].Replacement = strings.TrimSpace(result.Redactions[index].Replacement)
		if result.Redactions[index].Text == "" || result.Redactions[index].Replacement == "" {
			return ModerationResult{}, fmt.Errorf("invalid moderation redaction at index %d", index)
		}
	}

	switch result.Decision {
	case ModerationDecisionAllow, ModerationDecisionBlock:
		if result.Decision == ModerationDecisionBlock && result.Reason == "" {
			return ModerationResult{}, fmt.Errorf("blocked moderation result requires reason")
		}
		return result, nil
	default:
		return ModerationResult{}, fmt.Errorf("invalid moderation decision %q", result.Decision)
	}
}

func nonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
