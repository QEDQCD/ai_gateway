package main

import (
	"context"

	"github.com/liwenjian/ai_gateway/gateway/internal/config"
	apphttp "github.com/liwenjian/ai_gateway/gateway/internal/http"
	"github.com/liwenjian/ai_gateway/gateway/internal/service"
	"github.com/liwenjian/ai_gateway/gateway/internal/store"
	"github.com/liwenjian/ai_gateway/gateway/internal/telemetry"
)

func main() {
	cfg := config.Load()
	logger := telemetry.NewLogger()
	repository := store.NewBootstrapAuthRepository(store.BootstrapAuthConfig{
		RawPlatformAPIKey:    cfg.BootstrapPlatformAPIKey,
		PlatformAPIKeyID:     cfg.BootstrapPlatformAPIKeyID,
		PlatformAPIKeyName:   cfg.BootstrapPlatformAPIKeyName,
		TenantID:             cfg.BootstrapTenantID,
		TenantName:           cfg.BootstrapTenantName,
		ProviderCredentialID: cfg.BootstrapProviderID,
		Provider:             cfg.BootstrapProvider,
		ProviderDisplayName:  cfg.BootstrapProviderDisplayName,
	})
	quotaGuard := service.NewRedisQuotaGuard(newStaticQuotaClient(cfg.BootstrapQuotaExhaustedTenantIDs))
	authService := service.NewAuthService(repository, quotaGuard, service.NewRouteService(repository))

	logger.Fatal(apphttp.NewRouterWithAuth(authService).Listen(cfg.ListenAddr))
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
