package extraction

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterEmbeddingControlRoutes registers the embedding control endpoints.
//
// The group mixes three authorization postures (issue #940):
//
//   - Read surface (status, progress): project-scoped. Both are consumed by the
//     gateway embeddings page on behalf of a project member, so they are gated
//     by project membership (the shared token-scope + membership pair), never
//     platform admin. progress additionally scopes its queue counts to the
//     caller's own project in the handler.
//   - Operator write surface (pause, resume, config, queue, reset-schedule):
//     deployment-wide controls that affect the whole embedding fleet, not a
//     single project, so they require platform admin authority (admin:write).
//   - Diagnostic read surface (diagnose): unprojected global queue counts, so it
//     requires platform admin read authority (admin:read).
func RegisterEmbeddingControlRoutes(e *echo.Echo, h *EmbeddingControlHandler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/embeddings")
	g.Use(authMiddleware.RequireAuth())

	read := g.Group("")
	read.Use(authMiddleware.RequireProjectTokenScope(), authMiddleware.RequireProjectMember())
	read.GET("/status", h.Status)
	read.GET("/progress", h.Progress)

	write := g.Group("")
	write.Use(authMiddleware.RequireScopes("admin:write"))
	write.POST("/pause", h.Pause)
	write.POST("/resume", h.Resume)
	write.PATCH("/config", h.Config)
	write.DELETE("/queue", h.ClearQueue)
	write.POST("/reset-schedule", h.ResetSchedule)

	diag := g.Group("")
	diag.Use(authMiddleware.RequireScopes("admin:read"))
	diag.GET("/diagnose", h.DiagnoseQueue)
}
