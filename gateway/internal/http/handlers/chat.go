package handlers

import (
	"bufio"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/example/ai_gateway/gateway/internal/domain"
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
		if err := service.ValidateChatRequest(req); err != nil {
			proxy.RecordFailure(c.UserContext(), c.Locals("requestContext"), fiber.StatusBadRequest)
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}

		if existing, ok := c.Locals("requestContext").(domain.RequestContext); ok {
			return handleChatWithResolvedContext(c, req, proxy, router, existing)
		}

		requestedModel := strings.TrimSpace(req.Model)
		decision := chatRoutingDecision(req, router)
		resolvedModel := decision.TargetModelTier
		if requestedModel != "" {
			resolvedModel = normalizeExplicitChatModel(requestedModel)
			decision = service.SmartRoutingDecision{
				TaskClass:       "explicit_model",
				TargetModelTier: resolvedModel,
				MatchedRules:    []string{"explicit_model:" + requestedModel},
			}
		}

		resolved, err := authService.Resolve(chatScopedRequestContext(c), bearerToken(c), resolvedModel)
		if err != nil {
			proxy.RecordFailure(c.UserContext(), c.Locals("requestContext"), authStatusCode(err))
			return chatAuthError(err)
		}
		resolved.RequestedModel = requestedModel
		resolved.TaskClass = decision.TaskClass
		resolved.TargetModelTier = decision.TargetModelTier
		resolved.RoutingReason = strings.Join(decision.MatchedRules, ",")
		resolved.ResolvedModel = resolvedModel
		if resolvedModel != "" {
			req.Model = resolvedModel
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

func handleChatWithResolvedContext(
	c *fiber.Ctx,
	req service.ChatRequest,
	proxy service.ChatProxyService,
	router service.SmartRouter,
	requestContext domain.RequestContext,
) error {
	requestedModel := strings.TrimSpace(req.Model)
	decision := chatRoutingDecision(req, router)
	resolvedModel := decision.TargetModelTier
	if requestedModel != "" {
		resolvedModel = normalizeExplicitChatModel(requestedModel)
		decision = service.SmartRoutingDecision{
			TaskClass:       "explicit_model",
			TargetModelTier: resolvedModel,
			MatchedRules:    []string{"explicit_model:" + requestedModel},
		}
	}

	requestContext.RequestedModel = requestedModel
	requestContext.TaskClass = decision.TaskClass
	requestContext.TargetModelTier = decision.TargetModelTier
	requestContext.RoutingReason = strings.Join(decision.MatchedRules, ",")
	requestContext.ResolvedModel = resolvedModel
	if resolvedModel != "" {
		req.Model = resolvedModel
	}
	c.Locals("requestContext", requestContext)

	if req.Stream {
		stream, err := proxy.Stream(c.UserContext(), req, requestContext)
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

	resp, err := proxy.Complete(c.UserContext(), req, requestContext)
	if err != nil {
		return proxyError(err)
	}
	return c.JSON(resp)
}

func chatRoutingDecision(req service.ChatRequest, router service.SmartRouter) service.SmartRoutingDecision {
	if router == nil {
		return service.SmartRoutingDecision{}
	}
	return router.Decide(req)
}

func normalizeExplicitChatModel(model string) string {
	model = strings.TrimSpace(model)
	if strings.EqualFold(model, "mimo") {
		return "mimo-v2.5-pro"
	}
	return model
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
	case errors.Is(err, service.ErrModelNotAllowed):
		return fiber.StatusForbidden
	case errors.Is(err, service.ErrRouteNotFound):
		return fiber.StatusBadGateway
	default:
		return fiber.StatusInternalServerError
	}
}

func authMessage(err error) string {
	switch {
	case errors.Is(err, service.ErrUnauthorized):
		return "认证失败：API Key 无效或已过期"
	case errors.Is(err, service.ErrQuotaExceeded):
		return "租户额度不足：请求次数或 Token 配额已耗尽"
	case errors.Is(err, service.ErrModelNotAllowed):
		return "模型未授权：当前租户不可使用该模型"
	case errors.Is(err, service.ErrRouteNotFound):
		return "路由解析失败：未找到可用的模型映射"
	default:
		return "服务暂时不可用，请稍后重试"
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
		return fiber.NewError(fiber.StatusInternalServerError, "服务暂时不可用，请稍后重试")
	}
	if message == "upstream request failed" || message == "internal server error" {
		message = "服务暂时不可用，请稍后重试"
	}
	return fiber.NewError(statusCode, message)
}
