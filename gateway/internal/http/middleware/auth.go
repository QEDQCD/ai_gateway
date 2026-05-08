package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/gofiber/fiber/v2"
)

const (
	requestContextLocalKey   = "requestContext"
	consolePrincipalLocalKey = "console_principal"
)

func RequirePlatformAPIKey(authService service.AuthService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		raw := strings.TrimSpace(strings.TrimPrefix(c.Get("Authorization"), "Bearer "))
		requestCtx := scopedRequestContext(c)
		ctx, err := authService.Resolve(requestCtx, raw, requestedModel(c))
		if err != nil {
			return authError(err)
		}
		c.Locals(requestContextLocalKey, ctx)
		return c.Next()
	}
}

func requestedModel(c *fiber.Ctx) string {
	if model := strings.TrimSpace(c.Query("model")); model != "" {
		return normalizeRequestedModel(model)
	}

	if len(c.Body()) == 0 {
		return ""
	}

	var payload struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(c.Body(), &payload); err != nil {
		return ""
	}
	return normalizeRequestedModel(payload.Model)
}

func normalizeRequestedModel(model string) string {
	model = strings.TrimSpace(model)
	if strings.EqualFold(model, "mimo") {
		return "mimo-v2.5-pro"
	}
	return model
}

func authError(err error) error {
	switch {
	case errors.Is(err, service.ErrUnauthorized):
		return fiber.NewError(fiber.StatusUnauthorized, "认证失败：API Key 无效或已过期")
	case errors.Is(err, service.ErrQuotaExceeded):
		return fiber.NewError(fiber.StatusTooManyRequests, "租户额度不足：请求次数或 Token 配额已耗尽")
	case errors.Is(err, service.ErrModelNotAllowed):
		return fiber.NewError(fiber.StatusForbidden, "模型未授权：当前租户不可使用该模型")
	case errors.Is(err, service.ErrRouteNotFound):
		return fiber.NewError(fiber.StatusBadGateway, "路由解析失败：未找到可用的模型映射")
	default:
		return fiber.NewError(fiber.StatusInternalServerError, "服务暂时不可用，请稍后重试")
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
