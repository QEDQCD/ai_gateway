package main

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	gatewaydb "github.com/liwenjian/ai_gateway/gateway/db"
	"github.com/liwenjian/ai_gateway/gateway/internal/config"
	apphttp "github.com/liwenjian/ai_gateway/gateway/internal/http"
	"github.com/liwenjian/ai_gateway/gateway/internal/provider"
	"github.com/liwenjian/ai_gateway/gateway/internal/queue"
	"github.com/liwenjian/ai_gateway/gateway/internal/secret"
	"github.com/liwenjian/ai_gateway/gateway/internal/service"
	"github.com/liwenjian/ai_gateway/gateway/internal/store"
	"github.com/liwenjian/ai_gateway/gateway/internal/telemetry"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()
	logger := telemetry.NewLogger()
	logger.Fatal(newServerApp(cfg).Listen(cfg.ListenAddr))
}

func newServerApp(cfg config.Config) *fiber.App {
	if strings.TrimSpace(cfg.DatabaseURL) != "" {
		return newDatabaseBackedServerApp(cfg)
	}

	chatClient, embeddingClient := newBootstrapProviderClients(cfg)
	usagePublisher := queue.NewRabbitMQUsagePublisher(
		queue.NewNoopRabbitMQMessagePublisher(),
		"gateway.usage",
		"gateway.usage.request",
	)

	return apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: cfg.ServiceAuthUsername,
		ServiceAuthPassword: cfg.ServiceAuthPassword,
		AuthService:         newBootstrapAuthService(cfg),
		ChatProxy:           service.NewChatProxyService(chatClient, usagePublisher),
		EmbeddingProxy:      service.NewEmbeddingProxyService(embeddingClient, usagePublisher),
		RAGProxy:            service.NewRAGProxyService(cfg.RAGServiceBaseURL, cfg.RAGServiceUsername, cfg.RAGServicePassword, nil),
		ConsoleService:      service.NewUnavailableConsoleService(),
	})
}

func newDatabaseBackedServerApp(cfg config.Config) *fiber.App {
	ctx := context.Background()
	providerSecretCodec := mustNewProviderSecretCodec(cfg)

	pool, err := openPostgresWithRetry(ctx, cfg.DatabaseURL)
	if err != nil {
		panic(err)
	}
	if err := gatewaydb.ApplyMigrations(ctx, pool); err != nil {
		panic(err)
	}
	if err := gatewaydb.SeedDemoData(ctx, pool, gatewaydb.SeedConfig{
		PlatformAPIKey:      cfg.SeedPlatformAPIKey,
		ProviderBaseURL:     cfg.SeedProviderBaseURL,
		ProviderAPIKey:      cfg.SeedProviderAPIKey,
		Provider:            cfg.SeedProvider,
		ProviderDisplayName: cfg.SeedProviderDisplayName,
		SecretCodec:         providerSecretCodec,
	}); err != nil {
		panic(err)
	}

	queries := store.New(pool)
	repository := store.NewAuthRepository(queries, providerSecretCodec)
	routeService := service.NewRouteService(repository)
	authService := service.NewAuthService(repository, newQuotaGuard(cfg), routeService)
	usageRecorder := service.NewUsageRecorder(pool)
	usagePublisher := queue.NewUsagePublisherWithConsumers(
		newUsagePublisher(cfg),
		service.NewUsageAggregator(pool),
	)
	chatProxy := service.NewChatProxyService(provider.NewOpenAIClient(http.DefaultClient), usagePublisher, usageRecorder)
	embeddingProxy := service.NewEmbeddingProxyService(provider.NewOpenAIClient(http.DefaultClient), usagePublisher, usageRecorder)
	ragProxy := service.NewRAGProxyService(cfg.RAGServiceBaseURL, cfg.RAGServiceUsername, cfg.RAGServicePassword, http.DefaultClient)
	consoleService := service.NewPostgresConsoleService(pool, authService, chatProxy, ragProxy, cfg.SeedPlatformAPIKey)

	return apphttp.NewRouterWithDependencies(apphttp.RouterDependencies{
		ServiceAuthUsername: cfg.ServiceAuthUsername,
		ServiceAuthPassword: cfg.ServiceAuthPassword,
		AuthService:         authService,
		ChatProxy:           chatProxy,
		EmbeddingProxy:      embeddingProxy,
		RAGProxy:            ragProxy,
		ConsoleService:      consoleService,
	})
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

type redisQuotaClient struct {
	client *redis.Client
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

func newQuotaGuard(cfg config.Config) service.QuotaGuard {
	if strings.TrimSpace(cfg.RedisURL) == "" {
		return service.NewRedisQuotaGuard(newStaticQuotaClient(cfg.BootstrapQuotaExhaustedTenantIDs))
	}

	client := redis.NewClient(&redis.Options{})
	if options, err := redis.ParseURL(cfg.RedisURL); err == nil {
		client = redis.NewClient(options)
	}
	if err := retry(30, 2*time.Second, func() error {
		return client.Ping(context.Background()).Err()
	}); err != nil {
		panic(err)
	}
	return service.NewRedisQuotaGuard(redisQuotaClient{client: client})
}

func newUsagePublisher(cfg config.Config) queue.UsagePublisher {
	if strings.TrimSpace(cfg.RabbitMQURL) == "" {
		return queue.NewNoopUsagePublisher()
	}

	var (
		publisher *queue.AMQPPublisher
		err       error
	)
	if err := retry(30, 2*time.Second, func() error {
		publisher, err = queue.NewAMQPPublisher(cfg.RabbitMQURL)
		return err
	}); err != nil {
		panic(err)
	}
	return queue.NewRabbitMQUsagePublisher(publisher, "", "gateway_usage_events")
}

func (c redisQuotaClient) Exists(ctx context.Context, key string) (bool, error) {
	count, err := c.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func openPostgresWithRetry(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	var pool *pgxpool.Pool
	err := retry(30, 2*time.Second, func() error {
		nextPool, err := gatewaydb.OpenPostgres(ctx, databaseURL)
		if err != nil {
			return err
		}
		if pingErr := nextPool.Ping(ctx); pingErr != nil {
			nextPool.Close()
			return pingErr
		}
		pool = nextPool
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pool, nil
}

func retry(attempts int, delay time.Duration, fn func() error) error {
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		time.Sleep(delay)
	}
	return err
}

func mustNewProviderSecretCodec(cfg config.Config) *secret.Codec {
	providerSecretKey := strings.TrimSpace(cfg.ProviderSecretKey)
	if providerSecretKey == "" {
		if strings.TrimSpace(cfg.DatabaseURL) != "" {
			panic("gateway: GATEWAY_PROVIDER_SECRET_KEY or GATEWAY_PROVIDER_SECRET_KEY_FILE is required in database mode")
		}
		return nil
	}

	codec, err := secret.NewCodec(providerSecretKey)
	if err != nil {
		panic(err)
	}
	return codec
}
