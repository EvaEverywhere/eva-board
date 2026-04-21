package httputil

import (
	"github.com/gofiber/fiber/v2"

	"github.com/EvaEverywhere/eva-board/backend/internal/apperrors"
)

const UserIDKey = "user_id"

func CurrentUserID(c *fiber.Ctx) (string, error) {
	value, ok := c.Locals(UserIDKey).(string)
	if !ok || value == "" {
		return "", apperrors.ErrUnauthorized
	}
	return value, nil
}

func SetUserID(c *fiber.Ctx, userID string) {
	c.Locals(UserIDKey, userID)
}
