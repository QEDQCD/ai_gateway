package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/liwenjian/ai_gateway/gateway/internal/domain"
	"github.com/liwenjian/ai_gateway/gateway/internal/service"
)

func unauthorizedError(err error) error {
	return fiber.NewError(fiber.StatusUnauthorized, err.Error())
}

func RequireResolvedRequestContext() fiber.Handler {
	return func(c *fiber.Ctx) error {
		requestContext, ok := c.Locals("requestContext").(domain.RequestContext)
		if !ok || requestContext.TenantID == "" {
			return unauthorizedError(service.ErrUnauthorized)
		}
		return c.Next()
	}
}
