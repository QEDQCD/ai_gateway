package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/example/ai_gateway/gateway/internal/service"
)

func TestPostgresCaptchaServiceIssueAndVerifyChallenge(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	console, _ := newUsageConsoleService(t, ctx)

	challenge, err := console.IssueCaptcha(ctx, "127.0.0.1", "vitest")
	if err != nil {
		t.Fatalf("IssueCaptcha failed: %v", err)
	}
	if challenge.CaptchaID == "" || challenge.ImageData == "" || challenge.ExpiresAt == "" {
		t.Fatalf("expected populated challenge, got %+v", challenge)
	}

	_, err = console.VerifyCaptcha(ctx, service.VerifyCaptchaRequest{
		CaptchaID:   challenge.CaptchaID,
		CaptchaCode: "WRONG",
	})
	if err == nil {
		t.Fatal("expected VerifyCaptcha to reject wrong code")
	}
}
