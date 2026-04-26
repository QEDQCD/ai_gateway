package secret

import (
	"strings"
	"testing"
)

func TestCodecEncryptDecryptRoundTrip(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if err != nil {
		t.Fatalf("NewCodec returned error: %v", err)
	}

	ciphertext, err := codec.Encrypt("provider-secret-key")
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}
	if !strings.HasPrefix(ciphertext, EncryptedSecretPrefix) {
		t.Fatalf("expected ciphertext prefix %q, got %q", EncryptedSecretPrefix, ciphertext)
	}

	plaintext, err := codec.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}
	if plaintext != "provider-secret-key" {
		t.Fatalf("expected plaintext %q, got %q", "provider-secret-key", plaintext)
	}
}

func TestCodecRejectsInvalidCiphertext(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if err != nil {
		t.Fatalf("NewCodec returned error: %v", err)
	}

	if _, err := codec.Decrypt("provider-secret-key"); err == nil {
		t.Fatal("expected decrypt to fail for plaintext input")
	}
}
