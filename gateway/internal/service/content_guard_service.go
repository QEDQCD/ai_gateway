package service

import (
	"context"
	"strings"

	"github.com/example/ai_gateway/gateway/internal/security"
)

type ContentModerator interface {
	Moderate(ctx context.Context, input string) (ModerationResult, error)
}

type ContentGuardResult struct {
	Decision   ModerationDecision
	Reason     string
	AttackType string
	Redactions []ModerationRedaction
	Messages   []ChatMessage
}

type ContentGuardService struct {
	moderator ContentModerator
}

func NewContentGuardService(moderator ContentModerator) ContentGuardService {
	return ContentGuardService{moderator: moderator}
}

func (s ContentGuardService) Guard(ctx context.Context, messages []ChatMessage) ContentGuardResult {
	userInput := collectUserMessages(messages)

	if s.moderator == nil {
		return ContentGuardResult{
			Decision: ModerationDecisionAllow,
			Reason:   "fallback_regex",
			Messages: sanitizeMessages(messages),
		}
	}

	result, err := s.moderator.Moderate(ctx, userInput)
	if err != nil {
		return ContentGuardResult{
			Decision: ModerationDecisionAllow,
			Reason:   "fallback_regex",
			Messages: sanitizeMessages(messages),
		}
	}

	if result.Decision == ModerationDecisionBlock {
		return ContentGuardResult{
			Decision:   result.Decision,
			Reason:     result.Reason,
			AttackType: result.AttackType,
			Redactions: result.Redactions,
			Messages:   cloneMessages(messages),
		}
	}

	sanitized := applyResultRedactions(messages, result.Redactions)
	sanitized = sanitizeMessages(sanitized)

	return ContentGuardResult{
		Decision:   ModerationDecisionAllow,
		Reason:     result.Reason,
		AttackType: result.AttackType,
		Redactions: result.Redactions,
		Messages:   sanitized,
	}
}

func collectUserMessages(messages []ChatMessage) string {
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		if strings.TrimSpace(message.Role) != "user" {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		parts = append(parts, content)
	}
	return strings.Join(parts, "\n")
}

func applyResultRedactions(messages []ChatMessage, redactions []ModerationRedaction) []ChatMessage {
	sanitized := cloneMessages(messages)
	for messageIndex := range sanitized {
		for _, redaction := range redactions {
			if redaction.Text == "" {
				continue
			}
			sanitized[messageIndex].Content = strings.ReplaceAll(
				sanitized[messageIndex].Content,
				redaction.Text,
				redaction.Replacement,
			)
		}
	}
	return sanitized
}

func sanitizeMessages(messages []ChatMessage) []ChatMessage {
	sanitized := cloneMessages(messages)
	for index := range sanitized {
		sanitized[index].Content = security.SanitizeTextForUpstream(sanitized[index].Content)
	}
	return sanitized
}

func cloneMessages(messages []ChatMessage) []ChatMessage {
	cloned := make([]ChatMessage, len(messages))
	copy(cloned, messages)
	return cloned
}
