package config

import (
	"os"
	"strings"
)

type Config struct {
	ListenAddr        string
	RAGServiceBaseURL string

	BootstrapPlatformAPIKey          string
	BootstrapPlatformAPIKeyID        string
	BootstrapPlatformAPIKeyName      string
	BootstrapTenantID                string
	BootstrapTenantName              string
	BootstrapProviderID              string
	BootstrapProvider                string
	BootstrapProviderDisplayName     string
	BootstrapProviderBaseURL         string
	BootstrapProviderAPIKey          string
	BootstrapSupportedModels         []string
	BootstrapQuotaExhaustedTenantIDs []string
}

func Load() Config {
	listenAddr := os.Getenv("GATEWAY_LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}

	bootstrapProvider := defaultString(os.Getenv("GATEWAY_BOOTSTRAP_PROVIDER"), "openai")
	bootstrapSupportedModels := splitCommaSeparatedEnv(os.Getenv("GATEWAY_BOOTSTRAP_SUPPORTED_MODELS"))
	if len(bootstrapSupportedModels) == 0 {
		bootstrapSupportedModels = defaultBootstrapSupportedModels(bootstrapProvider)
	}

	return Config{
		ListenAddr:                       listenAddr,
		RAGServiceBaseURL:                os.Getenv("GATEWAY_RAG_SERVICE_BASE_URL"),
		BootstrapPlatformAPIKey:          os.Getenv("GATEWAY_BOOTSTRAP_PLATFORM_API_KEY"),
		BootstrapPlatformAPIKeyID:        defaultString(os.Getenv("GATEWAY_BOOTSTRAP_PLATFORM_API_KEY_ID"), "pak_bootstrap"),
		BootstrapPlatformAPIKeyName:      defaultString(os.Getenv("GATEWAY_BOOTSTRAP_PLATFORM_API_KEY_NAME"), "bootstrap platform key"),
		BootstrapTenantID:                defaultString(os.Getenv("GATEWAY_BOOTSTRAP_TENANT_ID"), "tenant_bootstrap"),
		BootstrapTenantName:              defaultString(os.Getenv("GATEWAY_BOOTSTRAP_TENANT_NAME"), "Bootstrap Tenant"),
		BootstrapProviderID:              defaultString(os.Getenv("GATEWAY_BOOTSTRAP_PROVIDER_ID"), "pc_bootstrap"),
		BootstrapProvider:                bootstrapProvider,
		BootstrapProviderDisplayName:     defaultString(os.Getenv("GATEWAY_BOOTSTRAP_PROVIDER_DISPLAY_NAME"), "OpenAI Primary"),
		BootstrapProviderBaseURL:         os.Getenv("GATEWAY_BOOTSTRAP_PROVIDER_BASE_URL"),
		BootstrapProviderAPIKey:          os.Getenv("GATEWAY_BOOTSTRAP_PROVIDER_API_KEY"),
		BootstrapSupportedModels:         bootstrapSupportedModels,
		BootstrapQuotaExhaustedTenantIDs: splitCommaSeparatedEnv(os.Getenv("GATEWAY_QUOTA_EXHAUSTED_TENANTS")),
	}
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func splitCommaSeparatedEnv(value string) []string {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}

func defaultBootstrapSupportedModels(provider string) []string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai":
		return []string{"gpt-4o-mini"}
	default:
		return nil
	}
}
