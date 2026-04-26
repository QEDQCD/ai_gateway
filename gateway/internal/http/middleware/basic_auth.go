package middleware

import (
	"encoding/base64"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func RequireServiceBasicAuth(username string, password string) fiber.Handler {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" && password == "" {
		return func(c *fiber.Ctx) error {
			return c.Next()
		}
	}

	expectedCredentials := username + ":" + password
	expectedHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(expectedCredentials))

	return func(c *fiber.Ctx) error {
		authorizationHeader := c.Get("Authorization")
		if authorizationHeader == expectedHeader {
			return c.Next()
		}

		if strings.TrimSpace(c.Get("X-Service-User")) == username && strings.TrimSpace(c.Get("X-Service-Password")) == password {
			return c.Next()
		}

		if !strings.HasPrefix(authorizationHeader, "Bearer ") {
			c.Set("WWW-Authenticate", `Basic realm="AI Gateway"`)
		}
		return fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
	}
}
