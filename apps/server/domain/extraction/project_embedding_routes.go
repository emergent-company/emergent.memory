package extraction

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterProjectEmbeddingRoutes registers project-scoped embedding management routes.
// RequireAuth authenticates the caller; RequireProjectTokenScope binds an emt_* API
// token to its project; RequireProjectMember authorizes session callers against the
// project's owning organization. Mutating handlers additionally require project admin.
func RegisterProjectEmbeddingRoutes(e *echo.Echo, h *ProjectEmbeddingHandler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/projects/:projectId/embeddings")
	g.Use(authMiddleware.RequireAuth())
	g.Use(authMiddleware.RequireProjectTokenScope())
	g.Use(authMiddleware.RequireProjectMember())

	g.GET("/progress", h.Progress)
	g.POST("/retrigger", h.Retrigger)
	g.DELETE("/queue", h.Cancel)
}
