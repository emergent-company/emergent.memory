package authinfo

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

// RegisterRoutes registers auth info routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/auth")
	g.Use(authMiddleware.RequireAuth())

	g.GET("/me", h.Me)
}
