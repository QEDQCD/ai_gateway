package http

import (
	"github.com/example/ai_gateway/gateway/internal/http/handlers"
	"github.com/example/ai_gateway/gateway/internal/http/middleware"
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/gofiber/fiber/v2"
)

type RouterDependencies struct {
	ServiceAuthUsername   string
	ServiceAuthPassword   string
	ConsoleSessionEnabled bool
	AuthService           service.AuthService
	SmartRouter           service.SmartRouter
	ChatProxy             service.ChatProxyService
	EmbeddingProxy        service.EmbeddingProxyService
	RAGProxy              service.RAGProxyService
	ConsoleService        service.ConsoleService
	MemberConsoleService  service.MemberConsoleService
}

func NewRouter() *fiber.App {
	return NewRouterWithDependencies(RouterDependencies{
		AuthService:          service.NewUnauthorizedAuthService(),
		ChatProxy:            service.NewUnavailableChatProxyService(),
		EmbeddingProxy:       service.NewUnavailableEmbeddingProxyService(),
		RAGProxy:             service.NewUnavailableRAGProxyService(),
		ConsoleService:       service.NewUnavailableConsoleService(),
		MemberConsoleService: service.NewUnavailableMemberConsoleService(),
	})
}

func NewRouterWithAuth(authService service.AuthService) *fiber.App {
	return NewRouterWithDependencies(RouterDependencies{
		AuthService:          authService,
		ChatProxy:            service.NewUnavailableChatProxyService(),
		EmbeddingProxy:       service.NewUnavailableEmbeddingProxyService(),
		RAGProxy:             service.NewUnavailableRAGProxyService(),
		ConsoleService:       service.NewUnavailableConsoleService(),
		MemberConsoleService: service.NewUnavailableMemberConsoleService(),
	})
}

func NewRouterWithServices(
	authService service.AuthService,
	chatProxy service.ChatProxyService,
	embeddingProxy service.EmbeddingProxyService,
	ragProxy service.RAGProxyService,
) *fiber.App {
	return NewRouterWithDependencies(RouterDependencies{
		AuthService:          authService,
		ChatProxy:            chatProxy,
		EmbeddingProxy:       embeddingProxy,
		RAGProxy:             ragProxy,
		ConsoleService:       service.NewUnavailableConsoleService(),
		MemberConsoleService: service.NewUnavailableMemberConsoleService(),
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
	if deps.SmartRouter == nil {
		deps.SmartRouter = service.NewRuleBasedSmartRouter(service.SmartRoutingConfig{})
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
	if deps.MemberConsoleService == nil {
		deps.MemberConsoleService = service.NewUnavailableMemberConsoleService()
	}
	if consoleAuthService, ok := deps.AuthService.(service.ConsoleAuthService); ok {
		app.Post("/console/session/login", handlers.ConsoleLogin(consoleAuthService))
	}
	app.Get("/console/captcha", handlers.ConsoleIssueCaptcha(deps.ConsoleService))
	app.Post("/console/captcha/verify", handlers.ConsoleVerifyCaptcha(deps.ConsoleService))
	app.Post("/console/applications", handlers.ConsoleCreateApplication(deps.ConsoleService))

	admin := app.Group("/admin", middleware.RequireServiceBasicAuth(deps.ServiceAuthUsername, deps.ServiceAuthPassword))
	if deps.ConsoleSessionEnabled {
		admin.Use(middleware.ResolveConsolePrincipal(deps.AuthService, true))
		admin.Use(middleware.RequireConsoleRole("admin"))
	}
	admin.Get("/overview", handlers.ConsoleOverview(deps.ConsoleService))
	admin.Get("/tenants", handlers.ConsoleTenants(deps.ConsoleService))
	admin.Get("/system/status", handlers.ConsoleSystemStatus(deps.ConsoleService))
	admin.Get("/applications", handlers.ConsoleApplications(deps.ConsoleService))
	admin.Post("/applications/:id/approve", handlers.ConsoleApproveApplication(deps.ConsoleService))
	admin.Post("/applications/:id/reject", handlers.ConsoleRejectApplication(deps.ConsoleService))
	admin.Get("/account-deletion-applications", handlers.ConsoleAccountDeletionApplications(deps.ConsoleService))
	admin.Post("/account-deletion-applications/:id/approve", handlers.ConsoleApproveAccountDeletionApplication(deps.ConsoleService))
	admin.Post("/account-deletion-applications/:id/reject", handlers.ConsoleRejectAccountDeletionApplication(deps.ConsoleService))
	admin.Get("/api-keys", handlers.ConsoleAPIKeys(deps.ConsoleService))
	admin.Post("/api-keys", handlers.ConsoleCreateAPIKey(deps.ConsoleService))
	admin.Post("/api-keys/:id/rotate", handlers.ConsoleRotateAPIKey(deps.ConsoleService))
	admin.Post("/api-keys/:id/deactivate", handlers.ConsoleDeactivateAPIKey(deps.ConsoleService))
	admin.Delete("/api-keys/:id", handlers.ConsoleDeleteAPIKey(deps.ConsoleService))
	admin.Get("/api-keys/:id/secret", handlers.ConsoleRevealAPIKeySecret(deps.ConsoleService))
	admin.Post("/api-keys/:id/secret/copy", handlers.ConsoleCopyAPIKeySecret(deps.ConsoleService))
	admin.Get("/provider-models", handlers.ConsoleProviderModels(deps.ConsoleService))
	admin.Post("/providers", handlers.ConsoleCreateProvider(deps.ConsoleService))
	admin.Post("/provider-models", handlers.ConsoleCreateProviderModel(deps.ConsoleService))
	admin.Delete("/provider-models/:id", handlers.ConsoleDeleteProviderModel(deps.ConsoleService))
	admin.Get("/model-health", handlers.ConsoleModelHealth(deps.ConsoleService))
	admin.Post("/provider-models/:id/health-check", handlers.ConsoleRunProviderModelHealthcheck(deps.ConsoleService))
	admin.Get("/billing/tenant", handlers.ConsoleTenantBilling(deps.ConsoleService))
	admin.Get("/routes", handlers.ConsoleRoutes(deps.ConsoleService))
	admin.Get("/playground", handlers.ConsolePlayground(deps.ConsoleService))
	admin.Post("/playground/chat", handlers.ConsoleRunPlayground(deps.ConsoleService))
	admin.Post("/playground/chat/stream", handlers.ConsoleStreamPlayground(deps.ConsoleService))
	admin.Get("/audit", handlers.ConsoleAudit(deps.ConsoleService))
	admin.Get("/usage/overview", handlers.ConsoleUsageOverview(deps.ConsoleService))
	admin.Get("/usage/trends", handlers.ConsoleUsageTrends(deps.ConsoleService))
	admin.Get("/usage/latency-wall", handlers.ConsoleUsageLatencyWall(deps.ConsoleService))
	admin.Get("/usage/failures", handlers.ConsoleUsageFailures(deps.ConsoleService))
	admin.Get("/usage/requests", handlers.ConsoleUsageRequests(deps.ConsoleService))
	admin.Get("/usage/requests/:id", handlers.ConsoleUsageRequestDetail(deps.ConsoleService))

	member := app.Group(
		"/me",
		middleware.RequireServiceBasicAuth(deps.ServiceAuthUsername, deps.ServiceAuthPassword),
	)
	if deps.ConsoleSessionEnabled {
		member.Use(middleware.ResolveConsolePrincipal(deps.AuthService, true))
		member.Use(middleware.RequireConsoleRole("member"))
	} else {
		member.Use(middleware.ResolveConsolePrincipal(deps.AuthService, false))
		member.Use(middleware.RequireConsoleRole("member"))
	}
	member.Get("/overview", handlers.MemberOverview(deps.MemberConsoleService))
	member.Get("/api-keys", handlers.MemberAPIKeys(deps.MemberConsoleService))
	member.Post("/api-keys", handlers.MemberCreateAPIKey(deps.MemberConsoleService))
	member.Post("/api-keys/:id/rotate", handlers.MemberRotateAPIKey(deps.MemberConsoleService))
	member.Post("/api-keys/:id/deactivate", handlers.MemberDeactivateAPIKey(deps.MemberConsoleService))
	member.Get("/api-keys/:id/secret", handlers.MemberRevealAPIKeySecret(deps.MemberConsoleService))
	member.Post("/api-keys/:id/secret/copy", handlers.MemberCopyAPIKeySecret(deps.MemberConsoleService))
	member.Post("/account-deletion-applications", handlers.MemberCreateAccountDeletionApplication(deps.MemberConsoleService))
	member.Get("/usage/overview", handlers.MemberUsageOverview(deps.MemberConsoleService))
	member.Get("/usage/requests", handlers.MemberUsageRequests(deps.MemberConsoleService))
	member.Get("/failures", handlers.MemberFailures(deps.MemberConsoleService))
	member.Get("/audit-events", handlers.MemberAuditEvents(deps.MemberConsoleService))

	v1 := app.Group("/v1")

	v1Protected := v1.Group("/", middleware.RequirePlatformAPIKey(deps.AuthService), middleware.RequireResolvedRequestContext())
	v1Protected.Get("/auth-check", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	v1Protected.Post("/chat/completions", handlers.ChatCompletion(deps.ChatProxy, deps.SmartRouter, deps.AuthService))
	v1Protected.Post("/embeddings", handlers.Embeddings(deps.EmbeddingProxy))
	v1Protected.Post("/internal-search", handlers.RAGQuery(deps.RAGProxy))
	return app
}
