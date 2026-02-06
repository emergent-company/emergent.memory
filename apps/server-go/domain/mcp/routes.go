package mcp

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

// RegisterRoutes registers all MCP routes
func RegisterRoutes(e *echo.Echo, h *Handler, sseHandler *SSEHandler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/mcp")
	g.Use(authMiddleware.RequireAuth())

	g.POST("/rpc", h.HandleRPC)

	g.GET("/sse/:projectId", sseHandler.HandleSSEConnect)
	g.POST("/sse/:projectId/message", sseHandler.HandleSSEMessage)
}
