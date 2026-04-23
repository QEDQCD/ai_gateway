package handlers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/liwenjian/ai_gateway/gateway/internal/service"
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
