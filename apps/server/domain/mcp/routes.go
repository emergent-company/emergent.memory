package mcp

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func RegisterRoutes(e *echo.Echo, h *Handler, sseHandler *SSEHandler, streamableHandler *StreamableHTTPHandler, authMiddleware *auth.Middleware) {
	// OAuth 2.0 Protected Resource Metadata (RFC 9728 / MCP 2025-11-25 auth spec).
	// Tells MCP clients (mcp-remote etc.) that this server uses bearer token auth
	// and does not require an OAuth authorization flow.
	e.GET("/.well-known/oauth-protected-resource", h.HandleOAuthProtectedResource)

	// Public redirect: /api/mcp/install?url=<mcpUrl>&name=<name>
	// Redirects to claude://install-mcp deep link. Must be registered before the
	// auth-gated group so it doesn't require authentication.
	e.GET("/api/mcp/install", h.HandleInstallRedirect)
	e.GET("/api/mcp/bundle", h.HandleDownloadMCPBundle)

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

	// Project-scoped MCP share endpoint
	pg := e.Group("/api/projects/:projectId/mcp")
	pg.Use(authMiddleware.RequireAuth())
	pg.POST("/share", h.HandleShareMCPAccess)
	pg.GET("/bundle", h.HandleGenerateMCPBundle)

	// Named MCP share instances (add-mcp-share-instances).
	//
	// These endpoints are project-admin operations, but handlers resolve the
	// role of the API-token OWNER (always a project admin), so without an
	// explicit scope guard a narrowly-scoped MCP share token could mint, rotate,
	// or delete sibling shares — escalating read access to write and stealing
	// other instances' rotated tokens. Share tokens derive their scopes solely
	// from their tool allowlist plus projects:read and never carry "admin", so
	// RequireAPITokenScopes("admin") blocks that token-chaining path while
	// leaving Zitadel/OAuth sessions unaffected.
	adminGroup := pg.Group("")
	adminGroup.Use(authMiddleware.RequireAPITokenScopes("admin"))
	adminGroup.POST("/shares", h.HandleCreateShareInstance)
	adminGroup.GET("/shares", h.HandleListShareInstances)
	adminGroup.GET("/shares/:id", h.HandleGetShareInstance)
	adminGroup.PATCH("/shares/:id", h.HandleUpdateShareInstance)
	adminGroup.DELETE("/shares/:id", h.HandleRevokeShareInstance)
	adminGroup.POST("/shares/:id/rotate", h.HandleRotateShareInstance)

	// Includable tool catalog for the allowlist picker
	adminGroup.GET("/tools", h.HandleListToolCatalog)
}
