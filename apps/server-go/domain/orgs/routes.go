package orgs

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/auth"
)

// RegisterRoutes registers organization routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// All org endpoints require authentication
	g := e.Group("/api/orgs")
	g.Use(authMiddleware.RequireAuth())

	// List organizations (user must be authenticated)
	g.GET("", h.List)

	// Get organization by ID (authenticated, no specific scope required)
	g.GET("/:id", h.Get)

	// Create organization (authenticated)
	g.POST("", h.Create)

	// Delete organization (authenticated)
	g.DELETE("/:id", h.Delete)
}
