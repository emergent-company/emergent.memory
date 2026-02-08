package mcp

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

func RegisterRoutes(e *echo.Echo, h *Handler, sseHandler *SSEHandler, streamableHandler *StreamableHTTPHandler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/mcp")
	g.Use(authMiddleware.RequireAuth())

	// Unified MCP endpoint (Spec 2025-11-25)
	// POST: Send JSON-RPC requests/notifications
	// GET: Open SSE stream for server-initiated messages
	// DELETE: Terminate session
	g.Match([]string{"GET", "POST", "DELETE"}, "", streamableHandler.HandleUnifiedEndpoint)

	// Project-scoped SSE endpoints (convenience API)
	// These provide direct project access via URL path instead of session management
	g.GET("/sse/:projectId", sseHandler.HandleSSEConnect)
	g.POST("/sse/:projectId/message", sseHandler.HandleSSEMessage)

	// Legacy JSON-RPC endpoint
	g.POST("/rpc", h.HandleRPC)
}
