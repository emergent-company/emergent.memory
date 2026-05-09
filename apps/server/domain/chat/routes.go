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

	// Project-scoped ask endpoint — stateless CLI assistant.
	// Uses the internal cli-assistant-agent; accepts OAuth, emt_* tokens, and API keys.
	// The agent is context-aware: it adapts responses based on auth state and project availability.
	askProjectGroup := e.Group("/api/projects/:projectId/ask")
	askProjectGroup.Use(authMiddleware.RequireAuth())
	askProjectGroup.Use(authMiddleware.RequireProjectScope())
	askProjectGroup.Use(authMiddleware.RequireAPITokenScopes("chat:use"))
	askProjectGroup.POST("", h.AskStream)

	// User-level ask endpoint — no project context required.
	// Useful for documentation questions and account-level tasks (e.g. "how do I configure a provider?").
	// Still requires authentication so the agent can personalise responses and access account info.
	askGroup := e.Group("/api/ask")
	askGroup.Use(authMiddleware.RequireAuth())
	askGroup.Use(authMiddleware.RequireAPITokenScopes("chat:use"))
	askGroup.POST("", h.AskStream)
	// Project-scoped remember endpoint — stateless NL insertion into the knowledge graph.
	// Uses the internal graph-insert-agent; understands NL, deduplicates, branches, and merges.
	rememberGroup := e.Group("/api/projects/:projectId/remember")
	rememberGroup.Use(authMiddleware.RequireAuth())
	rememberGroup.Use(authMiddleware.RequireProjectScope())
	rememberGroup.Use(authMiddleware.RequireAPITokenScopes("chat:use"))
	rememberGroup.POST("", h.RememberStream)
}
