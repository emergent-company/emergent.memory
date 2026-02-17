package projects

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/auth"
)

// RegisterRoutes registers project routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// All project endpoints require authentication
	g := e.Group("/api/projects")
	g.Use(authMiddleware.RequireAuth())

	// List projects (user must be authenticated)
	// Scope: project:read
	g.GET("", h.List)

	// Get project by ID
	// Scope: project:read
	g.GET("/:id", h.Get)

	// Create project
	// Scope: org:project:create
	g.POST("", h.Create)

	// Update project
	// Scope: project:write
	g.PATCH("/:id", h.Update)

	// Delete project
	// Scope: org:project:delete
	g.DELETE("/:id", h.Delete)

	// List project members
	// Scope: project:read
	g.GET("/:id/members", h.ListMembers)

	// Remove project member
	// Scope: project:admin
	g.DELETE("/:id/members/:userId", h.RemoveMember)
}
