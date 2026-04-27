package middleware

import (
	"strings"

	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/gofiber/fiber/v2"
)

func ResolveConsolePrincipal(authService service.AuthService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		subject := strings.TrimSpace(c.Get("X-Console-Subject"))
		if subject == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "console subject is required")
		}

		consoleAuthService, ok := authService.(service.ConsoleAuthService)
		if !ok {
			return fiber.NewError(fiber.StatusUnauthorized, "console principal resolution unavailable")
		}

		principal, err := consoleAuthService.ResolveConsolePrincipal(scopedRequestContext(c), subject)
		if err != nil {
			return authError(err)
		}

		c.Locals(consolePrincipalLocalKey, principal)
		c.SetUserContext(service.ContextWithConsolePrincipal(c.UserContext(), principal))
		return c.Next()
	}
}
