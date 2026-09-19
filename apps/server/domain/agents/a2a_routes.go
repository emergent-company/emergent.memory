package agents

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterA2ARoutes registers the A2A v1.0 HTTP+JSON route set at the server
// root (outside /api/), mirroring RegisterACPRoutes and agentcompat/routes.go.
//
// Discovery (global card) is unauthenticated; everything else requires a Bearer
// emt_* project token with the stated scope. Project addressing is
// credential-scoped (token-bound), never path-scoped.
func RegisterA2ARoutes(e *echo.Echo, h *A2AHandler, authMiddleware *auth.Middleware) {
	// --- Discovery (global card: no auth) ---
	e.GET("/.well-known/agent-card.json", h.GlobalAgentCardHandler)

	// --- Extended card (agents:read) ---
	extended := e.Group("/extendedAgentCard")
	extended.Use(authMiddleware.RequireAuth())
	extended.Use(authMiddleware.RequireAPITokenScopes("agents:read"))
	extended.GET("", h.ExtendedAgentCardHandler)

	// --- Message flow (agents:write) ---
	messageWrite := e.Group("/message")
	messageWrite.Use(authMiddleware.RequireAuth())
	messageWrite.Use(authMiddleware.RequireAPITokenScopes("agents:write"))
	messageWrite.POST(":send", h.SendMessage)
	messageWrite.POST(":stream", h.StreamMessage)

	// --- Task read (agents:read) ---
	tasksRead := e.Group("/tasks")
	tasksRead.Use(authMiddleware.RequireAuth())
	tasksRead.Use(authMiddleware.RequireAPITokenScopes("agents:read"))
	tasksRead.GET("", h.ListTasks)
	tasksRead.GET("/:id", h.GetTask)

	// --- Task write (agents:write) ---
	tasksWrite := e.Group("/tasks")
	tasksWrite.Use(authMiddleware.RequireAuth())
	tasksWrite.Use(authMiddleware.RequireAPITokenScopes("agents:write"))
	tasksWrite.POST("/:id:cancel", h.CancelTask)

	// --- Task subscribe (agents:read) ---
	tasksSubscribe := e.Group("/tasks")
	tasksSubscribe.Use(authMiddleware.RequireAuth())
	tasksSubscribe.Use(authMiddleware.RequireAPITokenScopes("agents:read"))
	tasksSubscribe.POST("/:id:subscribe", h.SubscribeTask)
}
