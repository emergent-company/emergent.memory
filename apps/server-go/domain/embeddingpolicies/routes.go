package embeddingpolicies

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/auth"
)

// RegisterRoutes registers embedding policy routes with the Echo router
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// Base group for embedding policies - all routes require authentication
	g := e.Group("/api/graph/embedding-policies")
	g.Use(authMiddleware.RequireAuth())

	// Read operations - require graph:read scope
	readGroup := g.Group("")
	readGroup.Use(authMiddleware.RequireScopes("graph:read"))
	readGroup.GET("", h.List)
	readGroup.GET("/:id", h.GetByID)

	// Write operations - require graph:write scope
	writeGroup := g.Group("")
	writeGroup.Use(authMiddleware.RequireScopes("graph:write"))
	writeGroup.POST("", h.Create)
	writeGroup.PATCH("/:id", h.Update)
	writeGroup.DELETE("/:id", h.Delete)
}
