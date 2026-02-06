package apitoken

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

// RegisterRoutes registers API token routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// All routes require authentication and project:read scope
	g := e.Group("/api/projects/:projectId/tokens")
	g.Use(authMiddleware.RequireAuth())
	g.Use(authMiddleware.RequireScopes("project:read"))

	g.POST("", h.Create)
	g.GET("", h.List)
	g.GET("/:tokenId", h.Get)
	g.DELETE("/:tokenId", h.Revoke)
}
