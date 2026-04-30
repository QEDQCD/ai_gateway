package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestLoadModelTokenPricingDefaultsAndOverrides(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		t.Setenv("GATEWAY_MODEL_TOKEN_PRICING_DEFAULT_INPUT_MICROYUAN_PER_MILLION", "")
		t.Setenv("GATEWAY_MODEL_TOKEN_PRICING_DEFAULT_OUTPUT_MICROYUAN_PER_MILLION", "")
		t.Setenv("GATEWAY_MODEL_TOKEN_PRICING_DEFAULT_CACHED_MICROYUAN_PER_MILLION", "")
		t.Setenv("GATEWAY_MODEL_TOKEN_PRICING_QWEN_FLASH_INPUT_MICROYUAN_PER_MILLION", "")
		t.Setenv("GATEWAY_MODEL_TOKEN_PRICING_QWEN_FLASH_OUTPUT_MICROYUAN_PER_MILLION", "")
		t.Setenv("GATEWAY_MODEL_TOKEN_PRICING_QWEN_FLASH_CACHED_MICROYUAN_PER_MILLION", "")

		cfg := Load()

		defaultPrice, ok := cfg.ModelTokenPricing["default"]
		if !ok {
			t.Fatal("expected default model pricing entry")
		}
		if defaultPrice.InputMicroyuanPerMillion != 2_000_000 {
			t.Fatalf("expected default input price %d, got %d", int64(2_000_000), defaultPrice.InputMicroyuanPerMillion)
		}
		if defaultPrice.OutputMicroyuanPerMillion != 20_000_000 {
			t.Fatalf("expected default output price %d, got %d", int64(20_000_000), defaultPrice.OutputMicroyuanPerMillion)
		}
		if defaultPrice.CachedMicroyuanPerMillion != 500_000 {
			t.Fatalf("expected default cached price %d, got %d", int64(500_000), defaultPrice.CachedMicroyuanPerMillion)
		}

		qwenFlashPrice, ok := cfg.ModelTokenPricing["qwen-flash"]
		if !ok {
			t.Fatal("expected qwen-flash model pricing entry")
		}
		if qwenFlashPrice != defaultPrice {
			t.Fatalf("expected qwen-flash pricing to default to %#v, got %#v", defaultPrice, qwenFlashPrice)
		}
	})

	t.Run("overrides", func(t *testing.T) {
		t.Setenv("GATEWAY_MODEL_TOKEN_PRICING_DEFAULT_INPUT_MICROYUAN_PER_MILLION", "2100000")
		t.Setenv("GATEWAY_MODEL_TOKEN_PRICING_DEFAULT_OUTPUT_MICROYUAN_PER_MILLION", "22000000")
		t.Setenv("GATEWAY_MODEL_TOKEN_PRICING_DEFAULT_CACHED_MICROYUAN_PER_MILLION", "2300000")
		t.Setenv("GATEWAY_MODEL_TOKEN_PRICING_QWEN_FLASH_INPUT_MICROYUAN_PER_MILLION", "3100000")
		t.Setenv("GATEWAY_MODEL_TOKEN_PRICING_QWEN_FLASH_OUTPUT_MICROYUAN_PER_MILLION", "32000000")
		t.Setenv("GATEWAY_MODEL_TOKEN_PRICING_QWEN_FLASH_CACHED_MICROYUAN_PER_MILLION", "3300000")

		cfg := Load()

		if got := cfg.ModelTokenPricing["default"]; got != (ModelTokenPrice{
			InputMicroyuanPerMillion:  2_100_000,
			OutputMicroyuanPerMillion: 22_000_000,
			CachedMicroyuanPerMillion: 2_300_000,
		}) {
			t.Fatalf("expected overridden default model pricing, got %#v", got)
		}
		if got := cfg.ModelTokenPricing["qwen-flash"]; got != (ModelTokenPrice{
			InputMicroyuanPerMillion:  3_100_000,
			OutputMicroyuanPerMillion: 32_000_000,
			CachedMicroyuanPerMillion: 3_300_000,
		}) {
			t.Fatalf("expected overridden qwen-flash model pricing, got %#v", got)
		}
	})
}

func TestLoadModelTokenPricingPanicsOnInvalidOverride(t *testing.T) {
	t.Run("non-numeric", func(t *testing.T) {
		t.Setenv("GATEWAY_MODEL_TOKEN_PRICING_DEFAULT_INPUT_MICROYUAN_PER_MILLION", "not-a-number")

		assertPanicContains(t, "GATEWAY_MODEL_TOKEN_PRICING_DEFAULT_INPUT_MICROYUAN_PER_MILLION", func() {
			Load()
		})
	})

	t.Run("negative", func(t *testing.T) {
		t.Setenv("GATEWAY_MODEL_TOKEN_PRICING_QWEN_FLASH_CACHED_MICROYUAN_PER_MILLION", "-1")

		assertPanicContains(t, "GATEWAY_MODEL_TOKEN_PRICING_QWEN_FLASH_CACHED_MICROYUAN_PER_MILLION", func() {
			Load()
		})
	})
}

func assertPanicContains(t *testing.T, want string, fn func()) {
	t.Helper()

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("expected panic")
		}
		if !strings.Contains(recovered.(string), want) {
			t.Fatalf("expected panic containing %q, got %q", want, recovered)
		}
	}()

	fn()
}
