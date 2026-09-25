package sandbox

import (
	"log/slog"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers sandbox HTTP routes.
//
// Authorization (issue #959, sibling): agent sandboxes are deployment-wide
// infrastructure (host containers keyed by a workspace UUID in
// kb.agent_sandboxes with no project/org linkage), so every route that reads or
// mutates a workspace — the list, the :id reads, the write/stop/resume/attach/
// snapshot routes, and the :id tool routes — is platform-scoped and admits only
// an active superadmin_full principal. A scope gate is deliberately NOT used: a
// bare `admin` scope is mintable by any project member (#948/#949) and cannot
// authorize deployment-wide container control.
//
// GET /providers is the one exception: it is a read-only catalogue of provider
// types, capabilities, and live health (no workspace or project data), and it
// is consumed by the gateway's agent sandbox settings page with a plain session
// token. It is gated on authenticated read (RequireAuth) only.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware, log *slog.Logger) {
	// Agent sandbox routes
	g := e.Group("/api/v1/agent/sandboxes")
	g.Use(authMiddleware.RequireAuth())

	// Provider catalogue: authenticated read (see the group doc above).
	g.GET("/providers", h.ListProviders)

	// Read operations (deployment-wide container state) — superadmin_full.
	readGroup := g.Group("")
	readGroup.Use(authMiddleware.RequireSuperadminFull())
	readGroup.GET("", h.ListWorkspaces)
	readGroup.GET("/:id", h.GetWorkspace)

	// Write operations
	writeGroup := g.Group("")
	writeGroup.Use(authMiddleware.RequireSuperadminFull())
	writeGroup.POST("", h.CreateWorkspace)
	writeGroup.POST("/from-snapshot", h.CreateFromSnapshot)
	writeGroup.DELETE("/:id", h.DeleteWorkspace)
	writeGroup.POST("/:id/stop", h.StopWorkspace)
	writeGroup.POST("/:id/resume", h.ResumeWorkspace)
	writeGroup.POST("/:id/attach", h.AttachSession)
	writeGroup.POST("/:id/detach", h.DetachSession)
	writeGroup.POST("/:id/snapshot", h.CreateSnapshot)

	// Tool operations (require superadmin_full + audit logging)
	toolGroup := g.Group("/:id")
	toolGroup.Use(authMiddleware.RequireSuperadminFull())
	toolGroup.Use(ToolAuditMiddleware(log))
	toolGroup.POST("/bash", h.BashTool)
	toolGroup.POST("/read", h.ReadTool)
	toolGroup.POST("/write", h.WriteTool)
	toolGroup.POST("/edit", h.EditTool)
	toolGroup.POST("/glob", h.GlobTool)
	toolGroup.POST("/grep", h.GrepTool)
	toolGroup.POST("/git", h.GitTool)
}
