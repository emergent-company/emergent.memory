package agents

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers agent routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// --- Project-scoped Agent routes (runtime agents) ---
	// Moved from /api/admin/agents to /api/projects/:projectId/agents.
	// Accessible via API tokens with agents:read / agents:write scopes.
	agents := e.Group("/api/projects/:projectId/agents")
	agents.Use(authMiddleware.RequireAuth())
	agents.Use(authMiddleware.RequireProjectTokenScope())
	agents.Use(authMiddleware.RequireProjectMember())

	// Read operations - require agents:read
	agentsRead := agents.Group("")
	agentsRead.Use(authMiddleware.RequireAPITokenScopes("agents:read"))
	agentsRead.GET("", h.ListAgents)
	agentsRead.GET("/:id", h.GetAgent)
	agentsRead.GET("/:id/runs", h.GetAgentRuns)
	agentsRead.GET("/:id/pending-events", h.GetPendingEvents)
	agentsRead.GET("/:id/hooks", h.ListWebhookHooks)

	// Write operations - require agents:write
	agentsWrite := agents.Group("")
	agentsWrite.Use(authMiddleware.RequireAPITokenScopes("agents:write"))
	agentsWrite.POST("", h.CreateAgent)
	agentsWrite.PATCH("/:id", h.UpdateAgent)
	agentsWrite.POST("/:id/enable", h.EnableAgent)
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
	defs.Use(authMiddleware.RequireProjectTokenScope())
	defs.Use(authMiddleware.RequireProjectMember())

	defsRead := defs.Group("")
	defsRead.Use(authMiddleware.RequireAPITokenScopes("agents:read"))
	defsRead.GET("", h.ListDefinitions)
	defsRead.GET("/:id", h.GetDefinition)
	defsRead.GET("/:id/sandbox-config", h.GetSandboxConfig)

	defsWrite := defs.Group("")
	defsWrite.Use(authMiddleware.RequireAPITokenScopes("agents:write"))
	defsWrite.POST("", h.CreateDefinition)
	defsWrite.PATCH("/:id", h.UpdateDefinition)
	defsWrite.DELETE("/:id", h.DeleteDefinition)
	defsWrite.PUT("/:id/sandbox-config", h.UpdateSandboxConfig)

	// --- Agent Definition Overrides (per-project config overrides) ---
	defsRead.GET("/overrides", h.ListAgentOverrides)
	defsRead.GET("/overrides/:agentName", h.GetAgentOverride)
	defsWrite.PUT("/overrides/:agentName", h.SetAgentOverride)
	defsWrite.DELETE("/overrides/:agentName", h.DeleteAgentOverride)

	// --- Generic project settings (key/value config) ---
	settings := e.Group("/api/projects/:projectId/settings")
	settings.Use(authMiddleware.RequireAuth())
	settings.Use(authMiddleware.RequireProjectTokenScope())
	settings.Use(authMiddleware.RequireProjectMember())

	settingsRead := settings.Group("")
	settingsRead.Use(authMiddleware.RequireAPITokenScopes("agents:read"))
	settingsRead.GET("/:category/:key", h.GetProjectSetting)

	settingsWrite := settings.Group("")
	settingsWrite.Use(authMiddleware.RequireAPITokenScopes("agents:write"))
	settingsWrite.PUT("/:category/:key", h.SetProjectSetting)
	settingsWrite.DELETE("/:category/:key", h.DeleteProjectSetting)

	// --- Project-scoped run history routes ---
	runs := e.Group("/api/projects/:projectId/agent-runs")
	runs.Use(authMiddleware.RequireAuth())
	runs.Use(authMiddleware.RequireProjectTokenScope())
	runs.Use(authMiddleware.RequireProjectMember())
	runs.Use(authMiddleware.RequireAPITokenScopes("agents:read"))
	runs.GET("", h.ListProjectRuns)
	runs.GET("/stats", h.GetProjectRunStats)
	runs.GET("/stats/sessions", h.GetProjectRunSessionStats)
	runs.GET("/:runId", h.GetProjectRun)
	runs.GET("/:runId/full", h.GetProjectRunFull)
	runs.GET("/:runId/messages", h.GetRunMessages)
	runs.GET("/:runId/tool-calls", h.GetRunToolCalls)
	runs.GET("/:runId/steps", h.GetRunSteps)
	runs.GET("/:runId/questions", h.HandleListQuestionsByRun)
	runs.GET("/:runId/remember-status", h.GetRunRememberStatus)

	// --- Project-scoped agent question routes ---
	questions := e.Group("/api/projects/:projectId/agent-questions")
	questions.Use(authMiddleware.RequireAuth())
	questions.Use(authMiddleware.RequireProjectTokenScope())
	questions.Use(authMiddleware.RequireProjectMember())
	questions.GET("", h.HandleListQuestionsByProject)
	questions.POST("/:questionId/respond", h.HandleRespondToQuestion)
	questions.POST("/:questionId/cancel", h.HandleCancelQuestion)

	// --- Project-scoped tool-approval audit routes ---
	approvals := e.Group("/api/projects/:projectId/agent-approvals")
	approvals.Use(authMiddleware.RequireAuth())
	approvals.Use(authMiddleware.RequireProjectTokenScope())
	approvals.Use(authMiddleware.RequireProjectMember())
	approvals.GET("", h.HandleListToolApprovals)

	// --- Agent session status routes ---
	sessions := e.Group("/api/v1/agent/sessions")
	sessions.Use(authMiddleware.RequireAuth())
	sessions.Use(authMiddleware.RequireAPITokenScopes("agents:read"))
	sessions.GET("/:id", h.GetSession)

	// --- Global run lookup (no project required — run IDs are globally unique UUIDs) ---
	globalRuns := e.Group("/api/v1/runs")
	globalRuns.Use(authMiddleware.RequireAuth())
	globalRuns.Use(authMiddleware.RequireAPITokenScopes("agents:read"))
	globalRuns.GET("/:runId", h.GetRunByID)
	globalRuns.GET("/:runId/steps", h.GetRunStepsStream)
	globalRuns.GET("/:runId/logs", h.GetRunLogs)

	// --- Project-scoped ADK session routes ---
	adkSessions := e.Group("/api/projects/:projectId/adk-sessions")
	adkSessions.Use(authMiddleware.RequireAuth())
	adkSessions.Use(authMiddleware.RequireProjectTokenScope())
	adkSessions.Use(authMiddleware.RequireProjectMember())
	adkSessions.Use(authMiddleware.RequireAPITokenScopes("agents:read"))
	adkSessions.GET("", h.GetADKSessions)
	adkSessions.GET("/:sessionId", h.GetADKSessionByID)

	// --- Legacy flat agent-definitions alias (no projectId in path) ---
	// iOS clients that pre-date the project-scoped path can use this endpoint.
	// Project ID is resolved from the API token binding or X-Project-ID header.
	legacyDefs := e.Group("/api/agent-definitions")
	legacyDefs.Use(authMiddleware.RequireAuth())
	legacyDefs.Use(authMiddleware.RequireProjectTokenScope())
	legacyDefs.Use(authMiddleware.RequireProjectMember())
	legacyDefs.Use(authMiddleware.RequireAPITokenScopes("agents:read"))
	legacyDefs.GET("", h.ListDefinitions)
	legacyDefs.GET("/:id", h.GetDefinition)

	// --- Public Webhook Receiver routes ---
	// NOTE: Does not use RequireAuth; authentication is handled internally via Bearer token
	webhooks := e.Group("/api/webhooks/agents")
	webhooks.POST("/:hookId", h.ReceiveWebhook)
}
