package agents

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/auth"
)

// RegisterRoutes registers agent routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// --- Admin Agent routes (runtime agents) ---
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
	writeGroup.POST("/:id/runs/:runId/cancel", h.CancelRun)

	// --- Admin Agent Definition routes (configuration/manifest) ---
	defAdmin := e.Group("/api/admin/agent-definitions")
	defAdmin.Use(authMiddleware.RequireAuth())

	defRead := defAdmin.Group("")
	defRead.Use(authMiddleware.RequireScopes("admin:read"))
	defRead.GET("", h.ListDefinitions)
	defRead.GET("/:id", h.GetDefinition)
	defRead.GET("/:id/workspace-config", h.GetWorkspaceConfig)

	defWrite := defAdmin.Group("")
	defWrite.Use(authMiddleware.RequireScopes("admin:write"))
	defWrite.POST("", h.CreateDefinition)
	defWrite.PATCH("/:id", h.UpdateDefinition)
	defWrite.DELETE("/:id", h.DeleteDefinition)
	defWrite.PUT("/:id/workspace-config", h.UpdateWorkspaceConfig)

	// --- Project-scoped run history routes ---
	runs := e.Group("/api/projects/:projectId/agent-runs")
	runs.Use(authMiddleware.RequireAuth())
	runs.Use(authMiddleware.RequireScopes("project:read"))
	runs.GET("", h.ListProjectRuns)
	runs.GET("/:runId", h.GetProjectRun)
	runs.GET("/:runId/messages", h.GetRunMessages)
	runs.GET("/:runId/tool-calls", h.GetRunToolCalls)

	// --- Agent session status routes ---
	sessions := e.Group("/api/v1/agent/sessions")
	sessions.Use(authMiddleware.RequireAuth())
	sessions.Use(authMiddleware.RequireScopes("project:read"))
	sessions.GET("/:id", h.GetSession)
}
