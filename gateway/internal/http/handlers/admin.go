package handlers

import (
	"errors"
	"strconv"
	"time"

	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/gofiber/fiber/v2"
)

func ConsoleOverview(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := console.Overview(c.UserContext())
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleSystemStatus(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := console.SystemStatus(c.UserContext())
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleApplications(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := console.Applications(c.UserContext())
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleCreateApplication(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req service.CreateApplicationRequest
		if err := c.BodyParser(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}

		payload, err := console.CreateApplication(c.UserContext(), req)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleApproveApplication(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req service.ApproveApplicationRequest
		if err := c.BodyParser(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}

		payload, err := console.ApproveApplication(c.UserContext(), c.Params("id"), req)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleRejectApplication(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req service.RejectApplicationRequest
		if err := c.BodyParser(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}

		payload, err := console.RejectApplication(c.UserContext(), c.Params("id"), req)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleAPIKeys(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := console.APIKeys(c.UserContext())
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleCreateAPIKey(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req service.CreateAPIKeyRequest
		if err := c.BodyParser(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}

		payload, err := console.CreateAPIKey(c.UserContext(), req)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleRotateAPIKey(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req service.RotateAPIKeyRequest
		if len(c.Body()) > 0 {
			if err := c.BodyParser(&req); err != nil {
				return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
			}
		}

		payload, err := console.RotateAPIKey(c.UserContext(), c.Params("id"), req)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleDeactivateAPIKey(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := console.DeactivateAPIKey(c.UserContext(), c.Params("id"))
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleDeleteAPIKey(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := console.DeleteAPIKey(c.UserContext(), c.Params("id"))
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleRevealAPIKeySecret(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := console.RevealAPIKeySecret(
			service.ContextWithRequestAuditMetadata(c.UserContext(), c.IP(), c.Get(fiber.HeaderUserAgent)),
			c.Params("id"),
		)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleCopyAPIKeySecret(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := console.CopyAPIKeySecret(c.UserContext(), c.Params("id"), c.IP(), c.Get(fiber.HeaderUserAgent))
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleRoutes(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := console.Routes(c.UserContext())
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsolePlayground(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := console.Playground(c.UserContext())
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleRunPlayground(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req service.PlaygroundRunRequest
		if err := c.BodyParser(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}

		payload, err := console.RunPlayground(c.UserContext(), req)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleAudit(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := console.Audit(c.UserContext())
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleUsageOverview(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		query, err := parseUsageQuery(c)
		if err != nil {
			return err
		}
		payload, err := console.UsageOverview(c.UserContext(), query)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleUsageTrends(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		query, err := parseUsageQuery(c)
		if err != nil {
			return err
		}
		payload, err := console.UsageTrends(c.UserContext(), query)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleUsageLatencyWall(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		query, err := parseUsageQuery(c)
		if err != nil {
			return err
		}
		payload, err := console.UsageLatencyWall(c.UserContext(), query)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleUsageFailures(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		query, err := parseUsageQuery(c)
		if err != nil {
			return err
		}
		payload, err := console.UsageFailures(c.UserContext(), query)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func ConsoleUsageRequests(console service.ConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		query, err := parseUsageQuery(c)
		if err != nil {
			return err
		}
		payload, err := console.UsageRequests(c.UserContext(), query)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func consoleError(err error) error {
	var statusErr service.StatusError
	if errors.As(err, &statusErr) {
		return fiber.NewError(statusErr.Code, statusErr.Message)
	}
	if errors.Is(err, service.ErrConsoleServiceUnavailable) {
		return fiber.NewError(fiber.StatusNotImplemented, "console service unavailable")
	}
	return fiber.NewError(fiber.StatusInternalServerError, "internal server error")
}

func parseUsageQuery(c *fiber.Ctx) (service.UsageQuery, error) {
	query := service.UsageQuery{
		Window:           c.Query("window"),
		TenantID:         c.Query("tenant_id"),
		PlatformAPIKeyID: c.Query("platform_api_key_id"),
		Provider:         c.Query("provider"),
		Model:            c.Query("model"),
		RouteID:          c.Query("route_id"),
		RequestPath:      c.Query("request_path"),
		Status:           c.Query("status"),
		ErrorCategory:    c.Query("error_category"),
		UsageSource:      c.Query("usage_source"),
	}

	var err error
	if query.From, err = parseUsageTimeQuery(c, "from"); err != nil {
		return service.UsageQuery{}, err
	}
	if query.To, err = parseUsageTimeQuery(c, "to"); err != nil {
		return service.UsageQuery{}, err
	}
	if query.Limit, err = parseUsageIntQuery(c, "limit"); err != nil {
		return service.UsageQuery{}, err
	}
	if query.Offset, err = parseUsageIntQuery(c, "offset"); err != nil {
		return service.UsageQuery{}, err
	}
	if !query.From.IsZero() && !query.To.IsZero() && !query.To.After(query.From) {
		return service.UsageQuery{}, fiber.NewError(fiber.StatusBadRequest, "invalid time range")
	}
	return query, nil
}

func parseUsageTimeQuery(c *fiber.Ctx, key string) (time.Time, error) {
	value := c.Query(key)
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fiber.NewError(fiber.StatusBadRequest, "invalid "+key+" query")
	}
	return parsed, nil
}

func parseUsageIntQuery(c *fiber.Ctx, key string) (int, error) {
	value := c.Query(key)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fiber.NewError(fiber.StatusBadRequest, "invalid "+key+" query")
	}
	return parsed, nil
}
