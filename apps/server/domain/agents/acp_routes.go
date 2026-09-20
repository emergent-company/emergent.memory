package agents

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Deprecated: ACP v1 (/acp/v1/ and /agent-chat/v1/) is superseded by the A2A
// v1.0 surface (see RegisterA2ARoutes). These routes are slated for removal in
// a later release (design Decision 6); they currently keep working unchanged
// behind Deprecation/Sunset headers.

const (
	// acpDeprecationHeader marks the response as deprecated.
	acpDeprecationHeader = "Deprecation"
	// acpSunsetHeader announces when the deprecated surface will be removed.
	acpSunsetHeader = "Sunset"
	// acpDeprecationDate is the date the ACP surface was deprecated.
	acpDeprecationDate = "Sat, 01 Nov 2025 00:00:00 GMT"
	// acpSunsetDate is the planned removal date for the ACP surface.
	acpSunsetDate = "Wed, 01 Sep 2027 00:00:00 GMT"
)

// RegisterACPRoutes registers Agent Communication Protocol (ACP) v1 routes.
// All routes are mounted at /acp/v1/ and /agent-chat/v1/ (top-level, not under /api/).
func RegisterACPRoutes(e *echo.Echo, h *ACPHandler, authMiddleware *auth.Middleware) {
	// Deprecated: use /agent-chat/v1 instead. Kept for backward compatibility.
	registerAgentChatRoutes(e, "/acp/v1", h, authMiddleware)

	// Agent Chat API v1 — preferred prefix. Replaces /acp/v1.
	registerAgentChatRoutes(e, "/agent-chat/v1", h, authMiddleware)
}

// acpDeprecationMiddleware attaches Deprecation and Sunset headers to every
// ACP/agent-chat response without altering status or body.
func acpDeprecationMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Response().Header().Set(acpDeprecationHeader, acpDeprecationDate)
		c.Response().Header().Set(acpSunsetHeader, acpSunsetDate)
		return next(c)
	}
}

// registerAgentChatRoutes registers the Agent Chat API v1 route set under the given prefix.
// Handlers and middleware are identical for every prefix.
func registerAgentChatRoutes(e *echo.Echo, prefix string, h *ACPHandler, authMiddleware *auth.Middleware) {
	root := e.Group(prefix, acpDeprecationMiddleware)

	// --- Ping (no auth) ---
	root.GET("/ping", h.Ping)

	// --- Agent discovery (agents:read) ---
	agentsRead := root.Group("/agents")
	agentsRead.Use(authMiddleware.RequireAuth())
	agentsRead.Use(authMiddleware.RequireAPITokenScopes("agents:read"))
	agentsRead.GET("", h.ListAgents)
	agentsRead.GET("/:name", h.GetAgent)

	// --- Run lifecycle (mixed read/write) ---
	// Read operations on runs
	runsRead := root.Group("/agents/:name/runs")
	runsRead.Use(authMiddleware.RequireAuth())
	runsRead.Use(authMiddleware.RequireAPITokenScopes("agents:read"))
	runsRead.GET("/:runId", h.GetRun)
	runsRead.GET("/:runId/events", h.GetRunEvents)

	// Write operations on runs
	runsWrite := root.Group("/agents/:name/runs")
	runsWrite.Use(authMiddleware.RequireAuth())
	runsWrite.Use(authMiddleware.RequireAPITokenScopes("agents:write"))
	runsWrite.POST("", h.CreateRun)
	runsWrite.DELETE("/:runId", h.CancelRun)
	runsWrite.POST("/:runId/resume", h.ResumeRun)

	// --- Sessions ---
	sessionsRead := root.Group("/sessions")
	sessionsRead.Use(authMiddleware.RequireAuth())
	sessionsRead.Use(authMiddleware.RequireAPITokenScopes("agents:read"))
	sessionsRead.GET("", h.ListSessions)
	sessionsRead.GET("/:sessionId", h.GetSession)

	sessionsWrite := root.Group("/sessions")
	sessionsWrite.Use(authMiddleware.RequireAuth())
	sessionsWrite.Use(authMiddleware.RequireAPITokenScopes("agents:write"))
	sessionsWrite.POST("", h.CreateSession)
	sessionsWrite.PATCH("/:sessionId/archive", h.ArchiveSession)
	sessionsWrite.PATCH("/:sessionId/unarchive", h.UnarchiveSession)
}
