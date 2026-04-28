package service

import (
	"testing"
	"time"

	"github.com/example/ai_gateway/gateway/internal/secret"
)

func TestPlatformAPIKeySecretServiceEncryptAndReveal(t *testing.T) {
	t.Parallel()

	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("secret.NewCodec failed: %v", err)
	}

	service := newPlatformAPIKeySecretService(codec)
	ciphertext, recoverable, err := service.Encrypt("agw_live_secret")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	if !recoverable {
		t.Fatal("expected recoverable secret")
	}
	if ciphertext == "" {
		t.Fatal("expected ciphertext")
	}

	plaintext, err := service.Reveal(ciphertext, recoverable)
	if err != nil {
		t.Fatalf("Reveal failed: %v", err)
	}
	if plaintext != "agw_live_secret" {
		t.Fatalf("expected plaintext %q, got %q", "agw_live_secret", plaintext)
	}
}

func TestBuildAPIKeySecretSummaryViewMarksLegacyUnrecoverable(t *testing.T) {
	t.Parallel()

	view := buildAPIKeySecretSummaryView("pak_legacy", "", false, time.Time{})
	if view.Revealable {
		t.Fatal("expected unrecoverable view to not be revealable")
	}
	if !view.LegacyUnrecoverable {
		t.Fatal("expected legacy key to be marked unrecoverable")
	}
	if view.MaskedKey != "••••••••" {
		t.Fatalf("expected masked fallback, got %q", view.MaskedKey)
	}
	if view.FullKey != "" {
		t.Fatalf("expected summary view to omit full key, got %q", view.FullKey)
	}
}

func TestBuildAPIKeySecretCopyViewIncludesFullKey(t *testing.T) {
	t.Parallel()

	view := buildAPIKeySecretCopyView("pak_live", "agw_live_secret", true, time.Time{})
	if !view.Revealable {
		t.Fatal("expected copy view to be revealable")
	}
	if view.FullKey != "agw_live_secret" {
		t.Fatalf("expected full key in copy view, got %q", view.FullKey)
	}
}
