package handlers

import (
	"bufio"

	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/gofiber/fiber/v2"
)

func ChatCompletion(proxy service.ChatProxyService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req service.ChatRequest
		if err := c.BodyParser(&req); err != nil {
			proxy.RecordFailure(c.UserContext(), c.Locals("requestContext"), fiber.StatusBadRequest)
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}

		if req.Stream {
			stream, err := proxy.Stream(c.UserContext(), req, c.Locals("requestContext"))
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
				})
			})
			return nil
		}

		resp, err := proxy.Complete(c.UserContext(), req, c.Locals("requestContext"))
		if err != nil {
			return proxyError(err)
		}
		return c.JSON(resp)
	}
}

func proxyError(err error) error {
	statusCode, message, ok := service.StatusCodeFromError(err)
	if !ok {
		return fiber.NewError(fiber.StatusInternalServerError, "internal server error")
	}
	return fiber.NewError(statusCode, message)
}
