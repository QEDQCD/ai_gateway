package middleware

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/liwenjian/ai_gateway/gateway/internal/service"
)

func RequirePlatformAPIKey(authService service.AuthService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		raw := strings.TrimSpace(strings.TrimPrefix(c.Get("Authorization"), "Bearer "))
		requestCtx := scopedRequestContext(c)
		ctx, err := authService.Resolve(requestCtx, raw, c.Query("model"))
		if err != nil {
			return authError(err)
		}
		c.Locals("requestContext", ctx)
		return c.Next()
	}
}

func authError(err error) error {
	switch {
	case errors.Is(err, service.ErrUnauthorized):
		return fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
	case errors.Is(err, service.ErrQuotaExceeded):
		return fiber.NewError(fiber.StatusTooManyRequests, "quota exceeded")
	case errors.Is(err, service.ErrRouteNotFound):
		return fiber.NewError(fiber.StatusBadGateway, "route resolution failed")
	default:
		return fiber.NewError(fiber.StatusInternalServerError, "internal server error")
	}
}

func scopedRequestContext(c *fiber.Ctx) context.Context {
	return requestScopedContext{
		requestCtx: c.Context(),
		valueCtx:   c.UserContext(),
	}
}

type requestScopedContext struct {
	requestCtx context.Context
	valueCtx   context.Context
}

func (c requestScopedContext) Deadline() (time.Time, bool) {
	return c.requestCtx.Deadline()
}

func (c requestScopedContext) Done() <-chan struct{} {
	return c.requestCtx.Done()
}

func (c requestScopedContext) Err() error {
	return c.requestCtx.Err()
}

func (c requestScopedContext) Value(key any) any {
	if c.valueCtx != nil {
		if value := c.valueCtx.Value(key); value != nil {
			return value
		}
	}
	return c.requestCtx.Value(key)
}
