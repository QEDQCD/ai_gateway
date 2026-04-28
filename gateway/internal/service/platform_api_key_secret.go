package service

import (
	"strings"
	"time"

	"github.com/example/ai_gateway/gateway/internal/secret"
)

const (
	apiKeyExpiryWarningWindow  = 72 * time.Hour
	platformAPIKeyKEKVersionV1 = "v1"
)

type platformAPIKeySecretService struct {
	codec *secret.Codec
}

func newPlatformAPIKeySecretService(codec *secret.Codec) platformAPIKeySecretService {
	return platformAPIKeySecretService{codec: codec}
}

func (s platformAPIKeySecretService) Encrypt(rawKey string) (string, bool, error) {
	if strings.TrimSpace(rawKey) == "" {
		return "", false, nil
	}
	if s.codec == nil {
		return "", false, nil
	}

	ciphertext, err := s.codec.Encrypt(rawKey)
	if err != nil {
		return "", false, err
	}
	return ciphertext, true, nil
}

func (s platformAPIKeySecretService) Reveal(ciphertext string, recoverable bool) (string, error) {
	if !recoverable || strings.TrimSpace(ciphertext) == "" || s.codec == nil {
		return "", nil
	}
	return s.codec.Decrypt(ciphertext)
}

func buildAPIKeySecretView(apiKeyID string, fullKey string, recoverable bool, expiresAt time.Time, includeFullKey bool) APIKeySecretView {
	expiresAtText := ""
	if !expiresAt.IsZero() {
		expiresAtText = expiresAt.In(shanghaiLocation()).Format(time.RFC3339)
	}

	fullKeyText := ""
	if includeFullKey {
		fullKeyText = fullKey
	}

	return APIKeySecretView{
		APIKeyID:            apiKeyID,
		MaskedKey:           maskManagedAPIKey(fullKey),
		FullKey:             fullKeyText,
		Revealable:          recoverable && strings.TrimSpace(fullKey) != "",
		LegacyUnrecoverable: !recoverable,
		ExpiresAt:           expiresAtText,
	}
}

func buildAPIKeySecretSummaryView(apiKeyID string, fullKey string, recoverable bool, expiresAt time.Time) APIKeySecretView {
	return buildAPIKeySecretView(apiKeyID, fullKey, recoverable, expiresAt, false)
}

func buildAPIKeySecretCopyView(apiKeyID string, fullKey string, recoverable bool, expiresAt time.Time) APIKeySecretView {
	return buildAPIKeySecretView(apiKeyID, fullKey, recoverable, expiresAt, true)
}

func maskManagedAPIKey(rawKey string) string {
	if strings.TrimSpace(rawKey) == "" {
		return "••••••••"
	}
	if len(rawKey) <= 8 {
		return "••••••••"
	}
	return rawKey[:4] + "••••••••" + rawKey[len(rawKey)-4:]
}
