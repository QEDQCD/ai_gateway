package middleware

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/liwenjian/ai_gateway/gateway/internal/service"
)

func authResolutionError(err error) error {
	if errors.Is(err, service.ErrQuotaExceeded) {
		return fiber.NewError(fiber.StatusTooManyRequests, err.Error())
	}

	return fiber.NewError(fiber.StatusUnauthorized, err.Error())
}
