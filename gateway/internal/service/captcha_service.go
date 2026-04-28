package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const captchaTTL = 5 * time.Minute

func (s postgresConsoleService) IssueCaptcha(ctx context.Context, ip string, userAgent string) (CaptchaChallenge, error) {
	code, err := randomCaptchaCode(4)
	if err != nil {
		return CaptchaChallenge{}, err
	}
	imageData := renderCaptchaDataURL(code)
	expiresAt := time.Now().UTC().Add(captchaTTL)
	id := newCaptchaID()

	if _, err := s.db.Exec(ctx, `
		insert into captcha_challenges (
			id,
			answer_hash,
			status,
			issued_ip,
			issued_user_agent,
			expires_at
		) values (
			$1, $2, 'issued', $3, $4, $5
		)
	`, id, hashCaptchaValue(code), strings.TrimSpace(ip), strings.TrimSpace(userAgent), expiresAt); err != nil {
		return CaptchaChallenge{}, err
	}

	return CaptchaChallenge{
		CaptchaID: id,
		ImageData: imageData,
		ExpiresAt: expiresAt.Format(time.RFC3339),
	}, nil
}

func (s postgresConsoleService) VerifyCaptcha(ctx context.Context, req VerifyCaptchaRequest) (CaptchaPassResult, error) {
	captchaID := strings.TrimSpace(req.CaptchaID)
	captchaCode := normalizeCaptchaCode(req.CaptchaCode)
	if captchaID == "" {
		return CaptchaPassResult{}, StatusError{Code: http.StatusBadRequest, Message: "captcha_id is required"}
	}
	if captchaCode == "" {
		return CaptchaPassResult{}, StatusError{Code: http.StatusBadRequest, Message: "captcha_code is required"}
	}

	var (
		answerHash     string
		status         string
		verifyAttempts int
		maxAttempts    int
		expiresAt      time.Time
	)
	err := s.db.QueryRow(ctx, `
		select answer_hash, status, verify_attempts, max_attempts, expires_at
		from captcha_challenges
		where id = $1
	`, captchaID).Scan(&answerHash, &status, &verifyAttempts, &maxAttempts, &expiresAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return CaptchaPassResult{}, StatusError{Code: http.StatusBadRequest, Message: "captcha challenge not found"}
		}
		return CaptchaPassResult{}, err
	}

	if !expiresAt.After(time.Now()) {
		if _, err := s.db.Exec(ctx, `
			update captcha_challenges
			set status = 'expired', updated_at = now()
			where id = $1 and status in ('issued', 'verified')
		`, captchaID); err != nil {
			return CaptchaPassResult{}, err
		}
		return CaptchaPassResult{}, StatusError{Code: http.StatusBadRequest, Message: "captcha challenge expired"}
	}
	if status != "issued" {
		return CaptchaPassResult{}, StatusError{Code: http.StatusBadRequest, Message: "captcha challenge is not available"}
	}

	if hashCaptchaValue(captchaCode) != answerHash {
		nextAttempts := verifyAttempts + 1
		nextStatus := "issued"
		if nextAttempts >= maxAttempts {
			nextStatus = "failed"
		}
		if _, err := s.db.Exec(ctx, `
			update captcha_challenges
			set verify_attempts = $2, status = $3, updated_at = now()
			where id = $1
		`, captchaID, nextAttempts, nextStatus); err != nil {
			return CaptchaPassResult{}, err
		}
		return CaptchaPassResult{}, StatusError{Code: http.StatusBadRequest, Message: "captcha code is invalid"}
	}

	passToken, err := randomOpaqueToken("cp", 24)
	if err != nil {
		return CaptchaPassResult{}, err
	}
	if _, err := s.db.Exec(ctx, `
		update captcha_challenges
		set
			status = 'verified',
			pass_token_hash = $2,
			verified_at = now(),
			updated_at = now()
		where id = $1
	`, captchaID, hashCaptchaValue(passToken)); err != nil {
		return CaptchaPassResult{}, err
	}

	return CaptchaPassResult{
		CaptchaPassToken: passToken,
		ExpiresAt:        expiresAt.UTC().Format(time.RFC3339),
	}, nil
}

func renderCaptchaDataURL(code string) string {
	svg := fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="160" height="52" viewBox="0 0 160 52" role="img" aria-label="captcha"><rect width="160" height="52" rx="12" fill="#0b1220"/><rect x="2" y="2" width="156" height="48" rx="10" fill="#13213a" stroke="#4f46e5" stroke-width="1.5"/><text x="80" y="34" text-anchor="middle" font-family="monospace" font-size="28" font-weight="700" fill="#e2e8f0" letter-spacing="6">%s</text></svg>`,
		code,
	)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}

func randomCaptchaCode(length int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	return randomFromAlphabet(length, alphabet)
}

func randomOpaqueToken(prefix string, bytesLen int) (string, error) {
	blob := make([]byte, bytesLen)
	if _, err := rand.Read(blob); err != nil {
		return "", err
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(blob), nil
}

func randomFromAlphabet(length int, alphabet string) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("length must be positive")
	}
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	out := make([]byte, length)
	for i, value := range raw {
		out[i] = alphabet[int(value)%len(alphabet)]
	}
	return string(out), nil
}

func hashCaptchaValue(value string) string {
	sum := sha256.Sum256([]byte(normalizeCaptchaCode(value)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizeCaptchaCode(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func newCaptchaID() string {
	return "cap_" + strings.ReplaceAll(newApplicationID(), "app_", "")
}
