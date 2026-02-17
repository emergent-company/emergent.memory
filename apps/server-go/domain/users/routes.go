package users

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/auth"
)

// RegisterRoutes registers the users routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/users")
	g.Use(authMiddleware.RequireAuth())
	g.Use(authMiddleware.RequireScopes("org:read")) // Same scope as NestJS

	g.GET("/search", h.Search)
}
