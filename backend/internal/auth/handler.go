package auth

import (
	"github.com/gofiber/fiber/v2"

	"github.com/EvaEverywhere/eva-board/backend/internal/apperrors"
	"github.com/EvaEverywhere/eva-board/backend/internal/httputil"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) GetMe(c *fiber.Ctx) error {
	userID, err := httputil.CurrentUserID(c)
	if err != nil {
		return apperrors.Handle(c, err)
	}

	user, err := h.svc.GetUser(c.UserContext(), userID)
	if err != nil {
		return apperrors.Handle(c, err)
	}

	return c.JSON(user)
}
