package typeregistry

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

// RegisterRoutes registers type registry routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// All type-registry endpoints require authentication
	g := e.Group("/api/type-registry")
	g.Use(authMiddleware.RequireAuth())

	// Get all object types for a project
	g.GET("/projects/:projectId", h.GetProjectTypes)

	// Get a specific object type definition
	g.GET("/projects/:projectId/types/:typeName", h.GetObjectType)

	// Get stats for project's type registry
	g.GET("/projects/:projectId/stats", h.GetTypeStats)
}
