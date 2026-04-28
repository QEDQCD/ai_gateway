package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const consoleSessionLifetime = 24 * time.Hour

type consoleSessionCodec struct {
	secret []byte
}

type consoleSessionClaims struct {
	Subject   string `json:"sub"`
	ExpiresAt int64  `json:"exp"`
}

func newConsoleSessionCodec(secret string) *consoleSessionCodec {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil
	}
	return &consoleSessionCodec{secret: []byte(secret)}
}

func (c *consoleSessionCodec) Enabled() bool {
	return c != nil && len(c.secret) > 0
}

func (c *consoleSessionCodec) Sign(subject string, now time.Time) (string, time.Time, error) {
	if !c.Enabled() {
		return "", time.Time{}, errors.New("console session codec unavailable")
	}

	claims := consoleSessionClaims{
		Subject:   strings.TrimSpace(subject),
		ExpiresAt: now.Add(consoleSessionLifetime).UTC().Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, err
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := c.sign(encodedPayload)
	return encodedPayload + "." + signature, time.Unix(claims.ExpiresAt, 0).UTC(), nil
}

func (c *consoleSessionCodec) Verify(token string, now time.Time) (string, error) {
	if !c.Enabled() {
		return "", ErrUnauthorized
	}

	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 2 {
		return "", ErrUnauthorized
	}
	if !hmac.Equal([]byte(parts[1]), []byte(c.sign(parts[0]))) {
		return "", ErrUnauthorized
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", ErrUnauthorized
	}

	var claims consoleSessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", ErrUnauthorized
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return "", ErrUnauthorized
	}
	if now.UTC().Unix() > claims.ExpiresAt {
		return "", ErrUnauthorized
	}
	return claims.Subject, nil
}

func (c *consoleSessionCodec) sign(payload string) string {
	mac := hmac.New(sha256.New, c.secret)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
