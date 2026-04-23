package http

import (
	"github.com/gofiber/fiber/v2"
	"github.com/liwenjian/ai_gateway/gateway/internal/http/handlers"
	"github.com/liwenjian/ai_gateway/gateway/internal/http/middleware"
	"github.com/liwenjian/ai_gateway/gateway/internal/service"
)

func NewRouter() *fiber.App {
	return NewRouterWithAuth(service.NewUnauthorizedAuthService())
}

func NewRouterWithAuth(authService service.AuthService) *fiber.App {
	app := fiber.New()
	app.Get("/healthz", handlers.Health)
	if authService == nil {
		authService = service.NewUnauthorizedAuthService()
	}
	v1 := app.Group("/v1", middleware.RequirePlatformAPIKey(authService), middleware.RequireResolvedRequestContext())
	v1.Get("/auth-check", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	return app
}
