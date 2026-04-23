package main

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/liwenjian/ai_gateway/gateway/internal/config"
	apphttp "github.com/liwenjian/ai_gateway/gateway/internal/http"
	"github.com/liwenjian/ai_gateway/gateway/internal/provider"
	"github.com/liwenjian/ai_gateway/gateway/internal/queue"
	"github.com/liwenjian/ai_gateway/gateway/internal/service"
	"github.com/liwenjian/ai_gateway/gateway/internal/store"
	"github.com/liwenjian/ai_gateway/gateway/internal/telemetry"
)

func main() {
	cfg := config.Load()
	logger := telemetry.NewLogger()
	logger.Fatal(newServerApp(cfg).Listen(cfg.ListenAddr))
}

func newServerApp(cfg config.Config) *fiber.App {
	chatClient, embeddingClient := newBootstrapProviderClients(cfg)
	usagePublisher := queue.NewRabbitMQUsagePublisher(
		queue.NewNoopRabbitMQMessagePublisher(),
		"gateway.usage",
		"gateway.usage.request",
	)

	return apphttp.NewRouterWithServices(
		newBootstrapAuthService(cfg),
		service.NewChatProxyService(chatClient, usagePublisher),
		service.NewEmbeddingProxyService(embeddingClient, usagePublisher),
	)
}

func newBootstrapAuthService(cfg config.Config) service.AuthService {
	repository := store.NewBootstrapAuthRepository(store.BootstrapAuthConfig{
		RawPlatformAPIKey:    cfg.BootstrapPlatformAPIKey,
		PlatformAPIKeyID:     cfg.BootstrapPlatformAPIKeyID,
		PlatformAPIKeyName:   cfg.BootstrapPlatformAPIKeyName,
		TenantID:             cfg.BootstrapTenantID,
		TenantName:           cfg.BootstrapTenantName,
		ProviderCredentialID: cfg.BootstrapProviderID,
		Provider:             cfg.BootstrapProvider,
		ProviderDisplayName:  cfg.BootstrapProviderDisplayName,
		ProviderBaseURL:      cfg.BootstrapProviderBaseURL,
		ProviderAPIKey:       cfg.BootstrapProviderAPIKey,
		SupportedModels:      cfg.BootstrapSupportedModels,
	})
	quotaGuard := service.NewRedisQuotaGuard(newStaticQuotaClient(cfg.BootstrapQuotaExhaustedTenantIDs))
	return service.NewAuthService(repository, quotaGuard, service.NewRouteService(repository))
}

func newBootstrapProviderClients(cfg config.Config) (service.UpstreamChatClient, service.UpstreamEmbeddingClient) {
	switch cfg.BootstrapProvider {
	case "dashscope":
		client := provider.NewDashScopeClient(nil)
		return client, client
	default:
		client := provider.NewOpenAIClient(nil)
		return client, client
	}
}

type staticQuotaClient struct {
	exhaustedKeys map[string]struct{}
}

func newStaticQuotaClient(exhaustedTenantIDs []string) staticQuotaClient {
	exhaustedKeys := make(map[string]struct{}, len(exhaustedTenantIDs))
	for _, tenantID := range exhaustedTenantIDs {
		exhaustedKeys["tenant_quota_exhausted:"+tenantID] = struct{}{}
	}
	return staticQuotaClient{exhaustedKeys: exhaustedKeys}
}

func (c staticQuotaClient) Exists(ctx context.Context, key string) (bool, error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	_, exhausted := c.exhaustedKeys[key]
	return exhausted, nil
}
