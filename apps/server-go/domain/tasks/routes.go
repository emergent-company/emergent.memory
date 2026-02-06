package tasks

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

// RegisterRoutes registers task routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// All task endpoints require authentication
	g := e.Group("/api/tasks")
	g.Use(authMiddleware.RequireAuth())

	// List tasks for a project
	g.GET("", h.List)

	// Get task counts by project
	g.GET("/counts", h.GetCounts)

	// List tasks across all accessible projects
	g.GET("/all", h.ListAll)

	// Get task counts across all accessible projects
	g.GET("/all/counts", h.GetAllCounts)

	// Get a specific task
	g.GET("/:id", h.GetByID)

	// Resolve a task (accept or reject)
	g.POST("/:id/resolve", h.Resolve)

	// Cancel a task
	g.POST("/:id/cancel", h.Cancel)
}
