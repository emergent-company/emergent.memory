package projects

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers project routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// /api/projects/current — no projects:read scope required; any valid auth token works.
	// Must be registered before the scoped group so Echo doesn't treat "current" as an :id param.
	noScope := e.Group("/api/projects")
	noScope.Use(authMiddleware.RequireAuth())
	noScope.GET("/current", h.Current)

	// All other project endpoints require authentication
	g := e.Group("/api/projects")
	g.Use(authMiddleware.RequireAuth())
	// When accessed via emt_* API token, require projects:read scope.
	// Zitadel/OAuth sessions bypass this check.
	g.Use(authMiddleware.RequireAPITokenScopes("projects:read"))

	// List projects (user must be authenticated)
	// Scope: project:read
	g.GET("", h.List)

	// Get project by ID
	// Scope: project:read
	g.GET("/:id", h.Get)

	// Create project
	// Scope: org:project:create
	g.POST("", h.Create)

	// Update project
	// Scope: project:write
	g.PATCH("/:id", h.Update)

	// Delete project (mark pending deletion with grace period)
	// Scope: org:project:delete
	// TODO: org:project:delete is not yet enforced here; the existing
	// projects:read token-scope middleware is retained for gateway service-token
	// compatibility. Add explicit scope enforcement in a follow-up change.
	g.DELETE("/:id", h.Delete)

	// Restore a project pending deletion (cancel the grace period)
	// Scope: org:project:delete
	g.POST("/:id/restore", h.Restore)

	// List project members
	// Scope: project:read
	g.GET("/:id/members", h.ListMembers)

	// Remove project member
	// Scope: project:admin
	g.DELETE("/:id/members/:userId", h.RemoveMember)
}
