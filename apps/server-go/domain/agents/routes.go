package agents

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

// RegisterRoutes registers agent routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// All agent routes require authentication and admin:read or admin:write scope
	admin := e.Group("/api/admin/agents")
	admin.Use(authMiddleware.RequireAuth())

	// Read operations - require admin:read
	readGroup := admin.Group("")
	readGroup.Use(authMiddleware.RequireScopes("admin:read"))
	readGroup.GET("", h.ListAgents)
	readGroup.GET("/:id", h.GetAgent)
	readGroup.GET("/:id/runs", h.GetAgentRuns)
	readGroup.GET("/:id/pending-events", h.GetPendingEvents)

	// Write operations - require admin:write
	writeGroup := admin.Group("")
	writeGroup.Use(authMiddleware.RequireScopes("admin:write"))
	writeGroup.POST("", h.CreateAgent)
	writeGroup.PATCH("/:id", h.UpdateAgent)
	writeGroup.DELETE("/:id", h.DeleteAgent)
	writeGroup.POST("/:id/trigger", h.TriggerAgent)
	writeGroup.POST("/:id/batch-trigger", h.BatchTrigger)
}
