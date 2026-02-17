package chunking

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/auth"
)

func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/documents")
	g.Use(authMiddleware.RequireAuth())
	g.Use(authMiddleware.RequireProjectID())
	g.Use(authMiddleware.RequireScopes("documents:write"))

	g.POST("/:id/recreate-chunks", h.RecreateChunks)
}
