package userprofile

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

// RegisterRoutes registers the user profile routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/user/profile")
	g.Use(authMiddleware.RequireAuth())

	g.GET("", h.Get)
	g.PUT("", h.Update)
}
