package handlers

import (
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/gofiber/fiber/v2"
)

func ConsoleLogin(auth service.ConsoleAuthService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if auth == nil {
			return fiber.NewError(fiber.StatusUnauthorized, "console auth unavailable")
		}

		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := c.BodyParser(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}

		payload, err := auth.AuthenticateConsoleSession(c.UserContext(), req.Email, req.Password)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}
