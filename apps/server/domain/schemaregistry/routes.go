package schemaregistry

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers schema registry routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// All schema-registry endpoints require authentication
	g := e.Group("/api/schema-registry")
	g.Use(authMiddleware.RequireAuth())

	// Get all object types for a project
	g.GET("/projects/:projectId", h.GetProjectTypes)

	// Get a specific object type definition
	g.GET("/projects/:projectId/types/:typeName", h.GetObjectType)

	// Get stats for project's schema registry
	g.GET("/projects/:projectId/stats", h.GetTypeStats)

	// Register a custom type for a project
	g.POST("/projects/:projectId/types", h.CreateType)

	// Update a registered type
	g.PUT("/projects/:projectId/types/:typeName", h.UpdateType)

	// Delete a registered type
	g.DELETE("/projects/:projectId/types/:typeName", h.DeleteType)
}
