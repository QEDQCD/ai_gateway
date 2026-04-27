package http

import (
	"github.com/example/ai_gateway/gateway/internal/http/handlers"
	"github.com/example/ai_gateway/gateway/internal/http/middleware"
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/gofiber/fiber/v2"
)

type RouterDependencies struct {
	ServiceAuthUsername string
	ServiceAuthPassword string
	AuthService         service.AuthService
	ChatProxy           service.ChatProxyService
	EmbeddingProxy      service.EmbeddingProxyService
	RAGProxy            service.RAGProxyService
	ConsoleService      service.ConsoleService
}

func NewRouter() *fiber.App {
	return NewRouterWithDependencies(RouterDependencies{
		AuthService:    service.NewUnauthorizedAuthService(),
		ChatProxy:      service.NewUnavailableChatProxyService(),
		EmbeddingProxy: service.NewUnavailableEmbeddingProxyService(),
		RAGProxy:       service.NewUnavailableRAGProxyService(),
		ConsoleService: service.NewUnavailableConsoleService(),
	})
}

func NewRouterWithAuth(authService service.AuthService) *fiber.App {
	return NewRouterWithDependencies(RouterDependencies{
		AuthService:    authService,
		ChatProxy:      service.NewUnavailableChatProxyService(),
		EmbeddingProxy: service.NewUnavailableEmbeddingProxyService(),
		RAGProxy:       service.NewUnavailableRAGProxyService(),
		ConsoleService: service.NewUnavailableConsoleService(),
	})
}

func NewRouterWithServices(
	authService service.AuthService,
	chatProxy service.ChatProxyService,
	embeddingProxy service.EmbeddingProxyService,
	ragProxy service.RAGProxyService,
) *fiber.App {
	return NewRouterWithDependencies(RouterDependencies{
		AuthService:    authService,
		ChatProxy:      chatProxy,
		EmbeddingProxy: embeddingProxy,
		RAGProxy:       ragProxy,
		ConsoleService: service.NewUnavailableConsoleService(),
	})
}

func NewRouterWithDependencies(deps RouterDependencies) *fiber.App {
	app := fiber.New()
	app.Get("/", handlers.Health)
	app.Get("/healthz", handlers.Health)
	if deps.AuthService == nil {
		deps.AuthService = service.NewUnauthorizedAuthService()
	}
	if deps.ChatProxy == nil {
		deps.ChatProxy = service.NewUnavailableChatProxyService()
	}
	if deps.EmbeddingProxy == nil {
		deps.EmbeddingProxy = service.NewUnavailableEmbeddingProxyService()
	}
	if deps.RAGProxy == nil {
		deps.RAGProxy = service.NewUnavailableRAGProxyService()
	}
	if deps.ConsoleService == nil {
		deps.ConsoleService = service.NewUnavailableConsoleService()
	}

	admin := app.Group("/admin", middleware.RequireServiceBasicAuth(deps.ServiceAuthUsername, deps.ServiceAuthPassword))
	admin.Get("/overview", handlers.ConsoleOverview(deps.ConsoleService))
	admin.Get("/system/status", handlers.ConsoleSystemStatus(deps.ConsoleService))
	admin.Get("/applications", handlers.ConsoleApplications(deps.ConsoleService))
	admin.Post("/applications/:id/approve", handlers.ConsoleApproveApplication(deps.ConsoleService))
	admin.Get("/api-keys", handlers.ConsoleAPIKeys(deps.ConsoleService))
	admin.Post("/api-keys", handlers.ConsoleCreateAPIKey(deps.ConsoleService))
	admin.Post("/api-keys/:id/rotate", handlers.ConsoleRotateAPIKey(deps.ConsoleService))
	admin.Post("/api-keys/:id/deactivate", handlers.ConsoleDeactivateAPIKey(deps.ConsoleService))
	admin.Delete("/api-keys/:id", handlers.ConsoleDeleteAPIKey(deps.ConsoleService))
	admin.Get("/routes", handlers.ConsoleRoutes(deps.ConsoleService))
	admin.Get("/playground", handlers.ConsolePlayground(deps.ConsoleService))
	admin.Post("/playground/chat", handlers.ConsoleRunPlayground(deps.ConsoleService))
	admin.Get("/knowledge-bases", handlers.ConsoleKnowledgeBases(deps.ConsoleService))
	admin.Get("/audit", handlers.ConsoleAudit(deps.ConsoleService))
	admin.Get("/usage/overview", handlers.ConsoleUsageOverview(deps.ConsoleService))
	admin.Get("/usage/trends", handlers.ConsoleUsageTrends(deps.ConsoleService))
	admin.Get("/usage/latency-wall", handlers.ConsoleUsageLatencyWall(deps.ConsoleService))
	admin.Get("/usage/failures", handlers.ConsoleUsageFailures(deps.ConsoleService))
	admin.Get("/usage/requests", handlers.ConsoleUsageRequests(deps.ConsoleService))

	v1 := app.Group("/v1", middleware.RequirePlatformAPIKey(deps.AuthService), middleware.RequireResolvedRequestContext())
	v1.Get("/auth-check", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	v1.Post("/chat/completions", handlers.ChatCompletion(deps.ChatProxy))
	v1.Post("/embeddings", handlers.Embeddings(deps.EmbeddingProxy))
	v1.Post("/rag/query", handlers.RAGQuery(deps.RAGProxy))
	return app
}
