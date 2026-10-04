package chunking

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/documents")
	g.Use(authMiddleware.RequireAuth())
	g.Use(authMiddleware.RequireProjectID())
	g.Use(authMiddleware.RequireProjectTokenScope())
	g.Use(authMiddleware.RequireProjectMember())
	g.Use(authMiddleware.RequireAPITokenScopes("documents:write"))

	g.POST("/:id/recreate-chunks", h.RecreateChunks)

	// Project chunking configuration (read/write).
	cfg := e.Group("/api/chunking")
	cfg.Use(authMiddleware.RequireAuth())
	cfg.Use(authMiddleware.RequireProjectID())
	cfg.Use(authMiddleware.RequireProjectTokenScope())
	cfg.Use(authMiddleware.RequireProjectMember())

	cfgRead := cfg.Group("")
	cfgRead.Use(authMiddleware.RequireAPITokenScopes("documents:read"))
	cfgRead.GET("/config", h.GetChunkingConfig)

	cfgWrite := cfg.Group("")
	cfgWrite.Use(authMiddleware.RequireAPITokenScopes("documents:write"))
	cfgWrite.PUT("/config", h.UpdateChunkingConfig)
}
