package chunking

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/documents")
	g.Use(authMiddleware.RequireAuth())
	g.Use(authMiddleware.RequireProjectID())
	g.Use(authMiddleware.RequireAPITokenScopes("documents:write"))

	g.POST("/:id/recreate-chunks", h.RecreateChunks)
}
