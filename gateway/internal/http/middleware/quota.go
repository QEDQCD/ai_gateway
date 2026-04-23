package middleware

import "github.com/gofiber/fiber/v2"

func unauthorizedError(err error) error {
	return fiber.NewError(fiber.StatusUnauthorized, err.Error())
}
