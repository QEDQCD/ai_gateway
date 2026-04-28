package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsUseDashScopeBootstrapSettings(t *testing.T) {
	t.Setenv("GATEWAY_BOOTSTRAP_PROVIDER", "")
	t.Setenv("GATEWAY_BOOTSTRAP_SUPPORTED_MODELS", "")
	t.Setenv("GATEWAY_BOOTSTRAP_PROVIDER_BASE_URL", "")
	t.Setenv("GATEWAY_SEED_PROVIDER", "")
	t.Setenv("GATEWAY_SEED_PROVIDER_BASE_URL", "")
	t.Setenv("GATEWAY_BOOTSTRAP_PROVIDER_DISPLAY_NAME", "")
	t.Setenv("GATEWAY_SEED_PROVIDER_DISPLAY_NAME", "")
	t.Setenv("GATEWAY_PROVIDER_SECRET_KEY", "")
	t.Setenv("GATEWAY_PROVIDER_SECRET_KEY_FILE", "")

	cfg := Load()

	if cfg.BootstrapProvider != "dashscope" {
		t.Fatalf("expected bootstrap provider %q, got %q", "dashscope", cfg.BootstrapProvider)
	}
	if cfg.SeedProvider != "dashscope" {
		t.Fatalf("expected seed provider %q, got %q", "dashscope", cfg.SeedProvider)
	}
	if cfg.BootstrapProviderBaseURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("expected bootstrap base URL %q, got %q", "https://dashscope.aliyuncs.com/compatible-mode/v1", cfg.BootstrapProviderBaseURL)
	}
	if cfg.SeedProviderBaseURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("expected seed base URL %q, got %q", "https://dashscope.aliyuncs.com/compatible-mode/v1", cfg.SeedProviderBaseURL)
	}
	if len(cfg.BootstrapSupportedModels) != 1 || cfg.BootstrapSupportedModels[0] != "qwen-flash" {
		t.Fatalf("expected bootstrap supported models %#v, got %#v", []string{"qwen-flash"}, cfg.BootstrapSupportedModels)
	}
}

func TestLoadReadsSecretsFromFiles(t *testing.T) {
	tempDir := t.TempDir()
	providerKeyPath := filepath.Join(tempDir, "provider.key")
	platformKeyPath := filepath.Join(tempDir, "platform.key")
	secretKeyPath := filepath.Join(tempDir, "provider_secret_key")
	if err := os.WriteFile(providerKeyPath, []byte("provider-file-secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile provider key failed: %v", err)
	}
	if err := os.WriteFile(platformKeyPath, []byte("platform-file-secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile platform key failed: %v", err)
	}
	if err := os.WriteFile(secretKeyPath, []byte("0123456789abcdef0123456789abcdef\n"), 0o600); err != nil {
		t.Fatalf("WriteFile secret key failed: %v", err)
	}

	t.Setenv("GATEWAY_BOOTSTRAP_PLATFORM_API_KEY", "")
	t.Setenv("GATEWAY_BOOTSTRAP_PLATFORM_API_KEY_FILE", platformKeyPath)
	t.Setenv("GATEWAY_BOOTSTRAP_PROVIDER_API_KEY", "")
	t.Setenv("GATEWAY_BOOTSTRAP_PROVIDER_API_KEY_FILE", providerKeyPath)
	t.Setenv("GATEWAY_SEED_PROVIDER_API_KEY", "")
	t.Setenv("GATEWAY_SEED_PROVIDER_API_KEY_FILE", providerKeyPath)
	t.Setenv("GATEWAY_PROVIDER_SECRET_KEY", "")
	t.Setenv("GATEWAY_PROVIDER_SECRET_KEY_FILE", secretKeyPath)

	cfg := Load()

	if cfg.BootstrapPlatformAPIKey != "platform-file-secret" {
		t.Fatalf("expected bootstrap platform key %q, got %q", "platform-file-secret", cfg.BootstrapPlatformAPIKey)
	}
	if cfg.BootstrapProviderAPIKey != "provider-file-secret" {
		t.Fatalf("expected bootstrap provider key %q, got %q", "provider-file-secret", cfg.BootstrapProviderAPIKey)
	}
	if cfg.SeedProviderAPIKey != "provider-file-secret" {
		t.Fatalf("expected seed provider key %q, got %q", "provider-file-secret", cfg.SeedProviderAPIKey)
	}
	if cfg.ProviderSecretKey != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("expected provider secret key from file, got %q", cfg.ProviderSecretKey)
	}
}

func TestLoadReadsPlatformAPIKeySecretKeyFromFile(t *testing.T) {
	tempDir := t.TempDir()
	secretKeyPath := filepath.Join(tempDir, "platform_api_key_secret")
	const expected = "0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(secretKeyPath, []byte(expected+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile secret key failed: %v", err)
	}

	t.Setenv("GATEWAY_PLATFORM_API_KEY_SECRET_KEY", "")
	t.Setenv("GATEWAY_PLATFORM_API_KEY_SECRET_KEY_FILE", secretKeyPath)

	cfg := Load()
	if cfg.PlatformAPIKeySecretKey != expected {
		t.Fatalf("expected PlatformAPIKeySecretKey %q, got %q", expected, cfg.PlatformAPIKeySecretKey)
	}
}
