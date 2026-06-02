package extraction

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterProjectEmbeddingRoutes registers project-scoped embedding management routes.
// All routes require authentication; project admin check is enforced in each handler.
func RegisterProjectEmbeddingRoutes(e *echo.Echo, h *ProjectEmbeddingHandler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/projects/:id/embeddings")
	g.Use(authMiddleware.RequireAuth())

	g.GET("/progress", h.Progress)
	g.POST("/retrigger", h.Retrigger)
	g.DELETE("/queue", h.Cancel)
}
