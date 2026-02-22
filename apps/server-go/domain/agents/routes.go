package agents

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/auth"
)

// RegisterRoutes registers agent routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// --- Project-scoped Agent routes (runtime agents) ---
	// Moved from /api/admin/agents to /api/projects/:projectId/agents.
	// Accessible via API tokens with agents:read / agents:write scopes.
	agents := e.Group("/api/projects/:projectId/agents")
	agents.Use(authMiddleware.RequireAuth())
	agents.Use(authMiddleware.RequireProjectScope())

	// Read operations - require agents:read
	agentsRead := agents.Group("")
	agentsRead.Use(authMiddleware.RequireScopes("agents:read"))
	agentsRead.GET("", h.ListAgents)
	agentsRead.GET("/:id", h.GetAgent)
	agentsRead.GET("/:id/runs", h.GetAgentRuns)
	agentsRead.GET("/:id/pending-events", h.GetPendingEvents)
	agentsRead.GET("/:id/hooks", h.ListWebhookHooks)

	// Write operations - require agents:write
	agentsWrite := agents.Group("")
	agentsWrite.Use(authMiddleware.RequireScopes("agents:write"))
	agentsWrite.POST("", h.CreateAgent)
	agentsWrite.PATCH("/:id", h.UpdateAgent)
	agentsWrite.DELETE("/:id", h.DeleteAgent)
	agentsWrite.POST("/:id/trigger", h.TriggerAgent)
	agentsWrite.POST("/:id/batch-trigger", h.BatchTrigger)
	agentsWrite.POST("/:id/runs/:runId/cancel", h.CancelRun)
	agentsWrite.POST("/:id/hooks", h.CreateWebhookHook)
	agentsWrite.DELETE("/:id/hooks/:hookId", h.DeleteWebhookHook)

	// --- Project-scoped Agent Definition routes (configuration/manifest) ---
	// Moved from /api/admin/agent-definitions to /api/projects/:projectId/agent-definitions.
	defs := e.Group("/api/projects/:projectId/agent-definitions")
	defs.Use(authMiddleware.RequireAuth())
	defs.Use(authMiddleware.RequireProjectScope())

	defsRead := defs.Group("")
	defsRead.Use(authMiddleware.RequireScopes("agents:read"))
	defsRead.GET("", h.ListDefinitions)
	defsRead.GET("/:id", h.GetDefinition)
	defsRead.GET("/:id/workspace-config", h.GetWorkspaceConfig)

	defsWrite := defs.Group("")
	defsWrite.Use(authMiddleware.RequireScopes("agents:write"))
	defsWrite.POST("", h.CreateDefinition)
	defsWrite.PATCH("/:id", h.UpdateDefinition)
	defsWrite.DELETE("/:id", h.DeleteDefinition)
	defsWrite.PUT("/:id/workspace-config", h.UpdateWorkspaceConfig)

	// --- Project-level install-default-agents ---
	// Moved from /api/admin/projects/:projectId to /api/projects/:projectId/agents-admin.
	projectAdmin := e.Group("/api/projects/:projectId/agents-admin")
	projectAdmin.Use(authMiddleware.RequireAuth())
	projectAdmin.Use(authMiddleware.RequireProjectScope())
	projectAdmin.Use(authMiddleware.RequireScopes("agents:write"))
	projectAdmin.POST("/install-default-agents", h.InstallDefaultAgents)

	// --- Project-scoped run history routes ---
	runs := e.Group("/api/projects/:projectId/agent-runs")
	runs.Use(authMiddleware.RequireAuth())
	runs.Use(authMiddleware.RequireProjectScope())
	runs.Use(authMiddleware.RequireScopes("agents:read"))
	runs.GET("", h.ListProjectRuns)
	runs.GET("/:runId", h.GetProjectRun)
	runs.GET("/:runId/messages", h.GetRunMessages)
	runs.GET("/:runId/tool-calls", h.GetRunToolCalls)
	runs.GET("/:runId/questions", h.HandleListQuestionsByRun)

	// --- Project-scoped agent question routes ---
	questions := e.Group("/api/projects/:projectId/agent-questions")
	questions.Use(authMiddleware.RequireAuth())
	questions.Use(authMiddleware.RequireProjectScope())
	questions.GET("", h.HandleListQuestionsByProject)
	questions.POST("/:questionId/respond", h.HandleRespondToQuestion)

	// --- Agent session status routes ---
	sessions := e.Group("/api/v1/agent/sessions")
	sessions.Use(authMiddleware.RequireAuth())
	sessions.Use(authMiddleware.RequireScopes("agents:read"))
	sessions.GET("/:id", h.GetSession)

	// --- Project-scoped ADK session routes ---
	adkSessions := e.Group("/api/projects/:projectId/adk-sessions")
	adkSessions.Use(authMiddleware.RequireAuth())
	adkSessions.Use(authMiddleware.RequireProjectScope())
	adkSessions.Use(authMiddleware.RequireScopes("agents:read"))
	adkSessions.GET("", h.GetADKSessions)
	adkSessions.GET("/:sessionId", h.GetADKSessionByID)

	// --- Public Webhook Receiver routes ---
	// NOTE: Does not use RequireAuth; authentication is handled internally via Bearer token
	webhooks := e.Group("/api/webhooks/agents")
	webhooks.POST("/:hookId", h.ReceiveWebhook)
}
