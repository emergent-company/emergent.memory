package tracing

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers the Tempo proxy routes under /api/traces.
// All routes require authentication — Tempo is never exposed publicly.
//
// Authority model (issue #994, mechanism 1/5): trace data carries LLM prompt
// text and payloads, so access is gated on TENANT MEMBERSHIP, not a global
// scope. The previous `RequireScopes("admin:read")` gate was satisfiable by any
// admin:read/admin:all token (mintable by an org_admin) and admitted cross-tenant
// reads. The routes are header/project-scoped (no :projectId path param), so
// RequireProjectTokenScope + RequireProjectMember resolve the effective project
// from the token binding or the X-Project-ID header and validate the caller's
// owning-org membership. When no project context is present, the handler refuses
// the instance-wide aggregate unless the caller holds superadmin_full.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/traces")
	g.Use(authMiddleware.RequireAuth())
	g.Use(authMiddleware.RequireProjectTokenScope())
	g.Use(authMiddleware.RequireProjectMember())
	g.GET("", h.Search)
	g.GET("/search", h.Search) // also accept /api/traces/search for explicitness
	g.GET("/:id", h.GetTrace)
}
