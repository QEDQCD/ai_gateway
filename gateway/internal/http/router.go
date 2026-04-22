package http

import (
	"github.com/gofiber/fiber/v2"
	"github.com/liwenjian/ai_gateway/gateway/internal/http/handlers"
)

func NewRouter() *fiber.App {
	app := fiber.New()
	app.Get("/healthz", handlers.Health)
	return app
}
