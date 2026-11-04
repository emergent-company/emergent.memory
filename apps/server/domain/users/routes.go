package users

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers the users routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/users")
	g.Use(authMiddleware.RequireAuth())
	g.Use(authMiddleware.RequireAPITokenScopes("org:read"))

	g.GET("/search", h.Search)
}
