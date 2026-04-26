package handlers

import (
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/gofiber/fiber/v2"
)

func Embeddings(proxy service.EmbeddingProxyService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req service.EmbeddingsRequest
		if err := c.BodyParser(&req); err != nil {
			proxy.RecordFailure(c.UserContext(), c.Locals("requestContext"), fiber.StatusBadRequest)
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}

		resp, err := proxy.Create(c.UserContext(), req, c.Locals("requestContext"))
		if err != nil {
			return proxyError(err)
		}
		return c.JSON(resp)
	}
}
