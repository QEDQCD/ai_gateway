package handlers

import (
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/gofiber/fiber/v2"
)

func ConsoleIssueCaptcha(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := console.IssueCaptcha(c.UserContext(), c.IP(), c.Get(fiber.HeaderUserAgent))
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleVerifyCaptcha(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req service.VerifyCaptchaRequest
		if err := c.BodyParser(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}

		payload, err := console.VerifyCaptcha(c.UserContext(), req)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}
