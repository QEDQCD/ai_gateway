package handlers

import (
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/gofiber/fiber/v2"
)

func RAGQuery(proxy service.RAGProxyService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req service.RAGQueryRequest
		if err := c.BodyParser(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}

		resp, err := proxy.Query(c.UserContext(), req, c.Locals("requestContext"))
		if err != nil {
			return proxyError(err)
		}
		return c.JSON(resp)
	}
}
