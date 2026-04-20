package auth

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	magiclink "github.com/teslashibe/magiclink-auth-go"

	"github.com/teslashibe/template-app/backend/internal/apperrors"
	"github.com/teslashibe/template-app/backend/internal/httputil"
)

type Middleware struct {
	magicSvc *magiclink.Service
	authSvc  *Service
}

func NewMiddleware(magicSvc *magiclink.Service, authSvc *Service) *Middleware {
	return &Middleware{
		magicSvc: magicSvc,
		authSvc:  authSvc,
	}
}

func (m *Middleware) RequireAuth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		authHeader := strings.TrimSpace(c.Get("Authorization"))
		if authHeader == "" {
			if token := strings.TrimSpace(c.Query("token")); token != "" {
				authHeader = "Bearer " + token
			}
		}

		if authHeader == "" {
			return apperrors.Handle(c, apperrors.ErrUnauthorized)
		}

		userID, claims, err := m.magicSvc.AuthenticateBearer(c.UserContext(), authHeader)
		if err != nil {
			return apperrors.Handle(c, apperrors.ErrUnauthorized)
		}

		// Keep a fast sanity check that the local user record is available.
		if _, err := m.authSvc.GetUser(c.UserContext(), userID); err != nil {
			return apperrors.Handle(c, err)
		}

		httputil.SetUserID(c, userID)
		c.Locals("auth_claims", claims)
		return c.Next()
	}
}
