package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/example/ai_gateway/gateway/internal/secret"
)

type Config struct {
	ListenAddr        string
	RAGServiceBaseURL string
	DatabaseURL       string
	RedisURL          string
	RabbitMQURL       string

	ServiceAuthUsername  string
	ServiceAuthPassword  string
	ConsoleSessionSecret string

	RAGServiceUsername string
	RAGServicePassword string

	SeedPlatformAPIKey      string
	SeedProviderBaseURL     string
	SeedProviderAPIKey      string
	SeedProvider            string
	SeedProviderDisplayName string
	ProviderSecretKey       string
	PlatformAPIKeySecretKey string
	SeedAdminPassword       string
	SeedMemberPassword      string

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

	bootstrapProvider := defaultString(lookupEnv("GATEWAY_BOOTSTRAP_PROVIDER"), "dashscope")
	bootstrapSupportedModels := splitCommaSeparatedEnv(os.Getenv("GATEWAY_BOOTSTRAP_SUPPORTED_MODELS"))
	if len(bootstrapSupportedModels) == 0 {
		bootstrapSupportedModels = defaultBootstrapSupportedModels(bootstrapProvider)
	}
	bootstrapProviderDisplayName := defaultString(lookupEnv("GATEWAY_BOOTSTRAP_PROVIDER_DISPLAY_NAME"), defaultProviderDisplayName(bootstrapProvider))
	bootstrapProviderBaseURL := defaultString(lookupEnv("GATEWAY_BOOTSTRAP_PROVIDER_BASE_URL"), defaultProviderBaseURL(bootstrapProvider))
	seedProviderBaseURL := defaultString(lookupEnv("GATEWAY_SEED_PROVIDER_BASE_URL"), bootstrapProviderBaseURL)

	return Config{
		ListenAddr:                       listenAddr,
		RAGServiceBaseURL:                lookupEnv("GATEWAY_RAG_SERVICE_BASE_URL"),
		DatabaseURL:                      lookupEnv("GATEWAY_DATABASE_URL"),
		RedisURL:                         lookupEnv("GATEWAY_REDIS_URL"),
		RabbitMQURL:                      lookupEnv("GATEWAY_RABBITMQ_URL"),
		ServiceAuthUsername:              lookupEnv("GATEWAY_SERVICE_AUTH_USERNAME"),
		ServiceAuthPassword:              lookupEnv("GATEWAY_SERVICE_AUTH_PASSWORD"),
		ConsoleSessionSecret:             lookupEnv("GATEWAY_CONSOLE_SESSION_SECRET"),
		RAGServiceUsername:               lookupEnv("GATEWAY_RAG_SERVICE_USERNAME"),
		RAGServicePassword:               lookupEnv("GATEWAY_RAG_SERVICE_PASSWORD"),
		SeedPlatformAPIKey:               defaultString(lookupEnv("GATEWAY_SEED_PLATFORM_API_KEY"), lookupEnv("GATEWAY_BOOTSTRAP_PLATFORM_API_KEY")),
		SeedProviderBaseURL:              seedProviderBaseURL,
		SeedProviderAPIKey:               defaultString(lookupEnv("GATEWAY_SEED_PROVIDER_API_KEY"), lookupEnv("GATEWAY_BOOTSTRAP_PROVIDER_API_KEY")),
		SeedProvider:                     defaultString(lookupEnv("GATEWAY_SEED_PROVIDER"), bootstrapProvider),
		SeedProviderDisplayName:          defaultString(lookupEnv("GATEWAY_SEED_PROVIDER_DISPLAY_NAME"), bootstrapProviderDisplayName),
		ProviderSecretKey:                lookupEnv("GATEWAY_PROVIDER_SECRET_KEY"),
		PlatformAPIKeySecretKey:          lookupEnv("GATEWAY_PLATFORM_API_KEY_SECRET_KEY"),
		SeedAdminPassword:                lookupEnv("GATEWAY_CONSOLE_ADMIN_PASSWORD"),
		SeedMemberPassword:               lookupEnv("GATEWAY_CONSOLE_MEMBER_PASSWORD"),
		BootstrapPlatformAPIKey:          lookupEnv("GATEWAY_BOOTSTRAP_PLATFORM_API_KEY"),
		BootstrapPlatformAPIKeyID:        defaultString(os.Getenv("GATEWAY_BOOTSTRAP_PLATFORM_API_KEY_ID"), "pak_bootstrap"),
		BootstrapPlatformAPIKeyName:      defaultString(os.Getenv("GATEWAY_BOOTSTRAP_PLATFORM_API_KEY_NAME"), "bootstrap platform key"),
		BootstrapTenantID:                defaultString(os.Getenv("GATEWAY_BOOTSTRAP_TENANT_ID"), "tenant_bootstrap"),
		BootstrapTenantName:              defaultString(os.Getenv("GATEWAY_BOOTSTRAP_TENANT_NAME"), "Bootstrap Tenant"),
		BootstrapProviderID:              defaultString(os.Getenv("GATEWAY_BOOTSTRAP_PROVIDER_ID"), "pc_bootstrap"),
		BootstrapProvider:                bootstrapProvider,
		BootstrapProviderDisplayName:     bootstrapProviderDisplayName,
		BootstrapProviderBaseURL:         bootstrapProviderBaseURL,
		BootstrapProviderAPIKey:          lookupEnv("GATEWAY_BOOTSTRAP_PROVIDER_API_KEY"),
		BootstrapSupportedModels:         bootstrapSupportedModels,
		BootstrapQuotaExhaustedTenantIDs: splitCommaSeparatedEnv(os.Getenv("GATEWAY_QUOTA_EXHAUSTED_TENANTS")),
	}
}

func lookupEnv(name string) string {
	value, err := secret.ResolveEnvOrFile(name)
	if err != nil {
		panic(fmt.Sprintf("config: read %s: %v", name+"_FILE", err))
	}
	return value
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
	case "dashscope":
		return []string{"qwen-flash"}
	case "openai":
		return []string{"gpt-4o-mini"}
	default:
		return nil
	}
}

func defaultProviderBaseURL(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "dashscope":
		return "https://dashscope.aliyuncs.com/compatible-mode/v1"
	case "openai":
		return "https://api.openai.com/v1"
	default:
		return ""
	}
}

func defaultProviderDisplayName(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "dashscope":
		return "DashScope Primary"
	case "openai":
		return "OpenAI Primary"
	default:
		return "Primary Provider"
	}
}
