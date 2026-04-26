package middleware

import (
	"github.com/example/ai_gateway/gateway/internal/domain"
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/gofiber/fiber/v2"
)

func unauthorizedError(err error) error {
	return authError(err)
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
