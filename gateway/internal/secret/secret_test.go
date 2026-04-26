package secret

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveEnvOrFileReadsSecretFile(t *testing.T) {
	tempDir := t.TempDir()
	secretPath := filepath.Join(tempDir, "provider.key")
	if err := os.WriteFile(secretPath, []byte("file-secret-value\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile failed: %v", err)
	}

	t.Setenv("TEST_PROVIDER_SECRET", "")
	t.Setenv("TEST_PROVIDER_SECRET_FILE", secretPath)

	value, err := ResolveEnvOrFile("TEST_PROVIDER_SECRET")
	if err != nil {
		t.Fatalf("ResolveEnvOrFile returned error: %v", err)
	}
	if value != "file-secret-value" {
		t.Fatalf("expected file secret %q, got %q", "file-secret-value", value)
	}
}

func TestAESCipherEncryptDecryptRoundTrips(t *testing.T) {
	codec, err := NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("NewCodec returned error: %v", err)
	}

	encrypted, err := codec.Encrypt("dashscope-provider-key")
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}
	if encrypted == "dashscope-provider-key" {
		t.Fatal("expected encrypted payload to differ from plaintext")
	}
	if !strings.HasPrefix(encrypted, EncryptedSecretPrefix) {
		t.Fatalf("expected encrypted payload prefix %q, got %q", EncryptedSecretPrefix, encrypted)
	}

	decrypted, err := codec.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}
	if decrypted != "dashscope-provider-key" {
		t.Fatalf("expected decrypted payload %q, got %q", "dashscope-provider-key", decrypted)
	}
}
