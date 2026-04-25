package mcprelay

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers MCP relay routes on the Echo instance.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/mcp-relay")
	g.Use(authMiddleware.RequireAuth())

	// WebSocket: remote MCP providers connect here.
	g.GET("/connect", h.Connect)

	// REST: inspect connected instances and call tools.
	g.GET("/sessions", h.ListSessions)
	g.GET("/sessions/:instanceId/tools", h.GetTools)
	g.POST("/sessions/:instanceId/call", h.CallTool)
}
