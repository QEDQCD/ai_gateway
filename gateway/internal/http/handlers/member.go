package handlers

import (
	"github.com/example/ai_gateway/gateway/internal/service"
	"github.com/gofiber/fiber/v2"
)

func MemberOverview(member service.MemberConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := member.Overview(c.UserContext())
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func MemberAPIKeys(member service.MemberConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := member.APIKeys(c.UserContext())
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func MemberCreateAPIKey(member service.MemberConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req service.CreateAPIKeyRequest
		if err := c.BodyParser(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}

		payload, err := member.CreateAPIKey(c.UserContext(), req)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func MemberRotateAPIKey(member service.MemberConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req service.RotateAPIKeyRequest
		if len(c.Body()) > 0 {
			if err := c.BodyParser(&req); err != nil {
				return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
			}
		}

		payload, err := member.RotateAPIKey(c.UserContext(), c.Params("id"), req)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func MemberDeactivateAPIKey(member service.MemberConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := member.DeactivateAPIKey(c.UserContext(), c.Params("id"))
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func MemberRevealAPIKeySecret(member service.MemberConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := member.RevealAPIKeySecret(c.UserContext(), c.Params("id"))
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func MemberUsageOverview(member service.MemberConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		query, err := parseUsageQuery(c)
		if err != nil {
			return err
		}
		payload, err := member.UsageOverview(c.UserContext(), query)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func MemberUsageRequests(member service.MemberConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		query, err := parseUsageQuery(c)
		if err != nil {
			return err
		}
		payload, err := member.UsageRequests(c.UserContext(), query)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func MemberFailures(member service.MemberConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		query, err := parseUsageQuery(c)
		if err != nil {
			return err
		}
		payload, err := member.Failures(c.UserContext(), query)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}

func MemberAuditEvents(member service.MemberConsoleService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		payload, err := member.AuditEvents(c.UserContext())
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(payload)
	}
}
