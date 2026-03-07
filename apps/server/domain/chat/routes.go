package chat

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers chat routes with the Echo router
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// Base group for chat - all routes require authentication and project ID
	g := e.Group("/api/chat")
	g.Use(authMiddleware.RequireAuth())
	g.Use(authMiddleware.RequireProjectID())

	// All chat operations require chat:use scope
	g.Use(authMiddleware.RequireAPITokenScopes("chat:use"))

	// Streaming endpoint - POST /api/chat/stream
	g.POST("/stream", h.StreamChat)

	// Conversation CRUD
	g.GET("/conversations", h.ListConversations)
	g.POST("/conversations", h.CreateConversation)
	g.GET("/:id", h.GetConversation)

	// Admin operations - require chat:admin scope
	adminGroup := g.Group("")
	adminGroup.Use(authMiddleware.RequireAPITokenScopes("chat:admin"))
	adminGroup.PATCH("/:id", h.UpdateConversation)
	adminGroup.DELETE("/:id", h.DeleteConversation)

	// Message operations
	g.POST("/:id/messages", h.AddMessage)

	// Project-scoped query endpoint — stateless NL query against the knowledge graph.
	// Uses the internal graph-query-agent; no agent ID needed from the client.
	queryGroup := e.Group("/api/projects/:projectId/query")
	queryGroup.Use(authMiddleware.RequireAuth())
	queryGroup.Use(authMiddleware.RequireProjectScope())
	queryGroup.Use(authMiddleware.RequireAPITokenScopes("chat:use"))
	queryGroup.POST("", h.QueryStream)
}
