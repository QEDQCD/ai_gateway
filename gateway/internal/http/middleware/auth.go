package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/liwenjian/ai_gateway/gateway/internal/service"
)

func RequirePlatformAPIKey(authService service.AuthService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		raw := strings.TrimSpace(strings.TrimPrefix(c.Get("Authorization"), "Bearer "))
		ctx, err := authService.Resolve(raw, c.Query("model"))
		if err != nil {
			return unauthorizedError(err)
		}
		c.Locals("requestContext", ctx)
		return c.Next()
	}
}
