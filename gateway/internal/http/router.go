package http

import (
	"github.com/gofiber/fiber/v2"
	"github.com/liwenjian/ai_gateway/gateway/internal/http/handlers"
	"github.com/liwenjian/ai_gateway/gateway/internal/http/middleware"
	"github.com/liwenjian/ai_gateway/gateway/internal/service"
)

func NewRouter() *fiber.App {
	return NewRouterWithServices(
		service.NewUnauthorizedAuthService(),
		service.NewUnavailableChatProxyService(),
		service.NewUnavailableEmbeddingProxyService(),
		service.NewUnavailableRAGProxyService(),
	)
}

func NewRouterWithAuth(authService service.AuthService) *fiber.App {
	return NewRouterWithServices(
		authService,
		service.NewUnavailableChatProxyService(),
		service.NewUnavailableEmbeddingProxyService(),
		service.NewUnavailableRAGProxyService(),
	)
}

func NewRouterWithServices(
	authService service.AuthService,
	chatProxy service.ChatProxyService,
	embeddingProxy service.EmbeddingProxyService,
	ragProxy service.RAGProxyService,
) *fiber.App {
	app := fiber.New()
	app.Get("/healthz", handlers.Health)
	if authService == nil {
		authService = service.NewUnauthorizedAuthService()
	}
	if chatProxy == nil {
		chatProxy = service.NewUnavailableChatProxyService()
	}
	if embeddingProxy == nil {
		embeddingProxy = service.NewUnavailableEmbeddingProxyService()
	}
	if ragProxy == nil {
		ragProxy = service.NewUnavailableRAGProxyService()
	}
	v1 := app.Group("/v1", middleware.RequirePlatformAPIKey(authService), middleware.RequireResolvedRequestContext())
	v1.Get("/auth-check", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	v1.Post("/chat/completions", handlers.ChatCompletion(chatProxy))
	v1.Post("/embeddings", handlers.Embeddings(embeddingProxy))
	v1.Post("/rag/query", handlers.RAGQuery(ragProxy))
	return app
}
