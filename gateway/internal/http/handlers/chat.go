package handlers

import (
	"bufio"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/gofiber/fiber/v2"
)

func ChatCompletion(proxy service.ChatProxyService, router service.SmartRouter, authService service.AuthService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req service.ChatRequest
		if err := c.BodyParser(&req); err != nil {
			recordContext := c.Locals("requestContext")
			if resolved, resolveErr := authService.Resolve(chatScopedRequestContext(c), bearerToken(c), ""); resolveErr == nil {
				recordContext = resolved
			}
			proxy.RecordFailure(c.UserContext(), recordContext, fiber.StatusBadRequest)
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}

		decision := router.Decide(req)
		resolved, err := authService.Resolve(chatScopedRequestContext(c), bearerToken(c), decision.TargetModelTier)
		if err != nil {
			proxy.RecordFailure(c.UserContext(), c.Locals("requestContext"), authStatusCode(err))
			return chatAuthError(err)
		}
		resolved.RequestedModel = strings.TrimSpace(req.Model)
		resolved.TaskClass = decision.TaskClass
		resolved.TargetModelTier = decision.TargetModelTier
		resolved.RoutingReason = strings.Join(decision.MatchedRules, ",")
		resolved.ResolvedModel = decision.TargetModelTier
		if strings.TrimSpace(decision.TargetModelTier) != "" {
			req.Model = decision.TargetModelTier
		}
		c.Locals("requestContext", resolved)

		if req.Stream {
			stream, err := proxy.Stream(c.UserContext(), req, resolved)
			if err != nil {
				return proxyError(err)
			}

			c.Status(stream.StatusCode)
			c.Set(fiber.HeaderContentType, stream.ContentType)
			c.Set(fiber.HeaderCacheControl, "no-cache")
			c.Set(fiber.HeaderConnection, "keep-alive")
			c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
				_, _ = stream.Run(func(chunk []byte) error {
					if _, err := w.Write(chunk); err != nil {
						return err
					}
					return w.Flush()
				}, nil)
			})
			return nil
		}

		resp, err := proxy.Complete(c.UserContext(), req, resolved)
		if err != nil {
			return proxyError(err)
		}
		return c.JSON(resp)
	}
}

func bearerToken(c *fiber.Ctx) string {
	return strings.TrimSpace(strings.TrimPrefix(c.Get("Authorization"), "Bearer "))
}

func chatAuthError(err error) error {
	return fiber.NewError(authStatusCode(err), authMessage(err))
}

func authStatusCode(err error) int {
	switch {
	case errors.Is(err, service.ErrUnauthorized):
		return fiber.StatusUnauthorized
	case errors.Is(err, service.ErrQuotaExceeded):
		return fiber.StatusTooManyRequests
	case errors.Is(err, service.ErrRouteNotFound):
		return fiber.StatusBadGateway
	default:
		return fiber.StatusInternalServerError
	}
}

func authMessage(err error) string {
	switch {
	case errors.Is(err, service.ErrUnauthorized):
		return "unauthorized"
	case errors.Is(err, service.ErrQuotaExceeded):
		return "quota exceeded"
	case errors.Is(err, service.ErrRouteNotFound):
		return "route resolution failed"
	default:
		return "internal server error"
	}
}

func chatScopedRequestContext(c *fiber.Ctx) context.Context {
	return chatRequestScopedContext{
		requestCtx: c.Context(),
		valueCtx:   c.UserContext(),
	}
}

type chatRequestScopedContext struct {
	requestCtx context.Context
	valueCtx   context.Context
}

func (c chatRequestScopedContext) Deadline() (time.Time, bool) {
	return c.requestCtx.Deadline()
}

func (c chatRequestScopedContext) Done() <-chan struct{} {
	return c.requestCtx.Done()
}

func (c chatRequestScopedContext) Err() error {
	return c.requestCtx.Err()
}

func (c chatRequestScopedContext) Value(key any) any {
	if c.valueCtx != nil {
		if value := c.valueCtx.Value(key); value != nil {
			return value
		}
	}
	return c.requestCtx.Value(key)
}

func proxyError(err error) error {
	statusCode, message, ok := service.StatusCodeFromError(err)
	if !ok {
		return fiber.NewError(fiber.StatusInternalServerError, "internal server error")
	}
	return fiber.NewError(statusCode, message)
}
