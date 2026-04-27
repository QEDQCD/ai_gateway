package middleware

import (
	"strings"

	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/gofiber/fiber/v2"
)

func RequireConsoleRole(allowed ...string) fiber.Handler {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, role := range allowed {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		allowedSet[role] = struct{}{}
	}

	return func(c *fiber.Ctx) error {
		principal, ok := c.Locals(consolePrincipalLocalKey).(service.ConsolePrincipal)
		if !ok {
			return fiber.NewError(fiber.StatusUnauthorized, "console principal missing")
		}
		if _, ok := allowedSet[principal.Role]; !ok {
			return fiber.NewError(fiber.StatusForbidden, "forbidden")
		}
		return c.Next()
	}
}
