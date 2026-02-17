package workspace

import (
	"log/slog"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

// RegisterRoutes registers workspace HTTP routes.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware, log *slog.Logger) {
	// Agent workspace routes
	g := e.Group("/api/v1/agent/workspaces")
	g.Use(authMiddleware.RequireAuth())

	// Read operations
	readGroup := g.Group("")
	readGroup.Use(authMiddleware.RequireScopes("admin:read"))
	readGroup.GET("", h.ListWorkspaces)
	readGroup.GET("/providers", h.ListProviders)
	readGroup.GET("/:id", h.GetWorkspace)

	// Write operations
	writeGroup := g.Group("")
	writeGroup.Use(authMiddleware.RequireScopes("admin:write"))
	writeGroup.POST("", h.CreateWorkspace)
	writeGroup.DELETE("/:id", h.DeleteWorkspace)
	writeGroup.POST("/:id/stop", h.StopWorkspace)
	writeGroup.POST("/:id/resume", h.ResumeWorkspace)

	// Tool operations (require write scope + audit logging)
	toolGroup := g.Group("/:id")
	toolGroup.Use(authMiddleware.RequireScopes("admin:write"))
	toolGroup.Use(ToolAuditMiddleware(log))
	toolGroup.POST("/bash", h.BashTool)
	toolGroup.POST("/read", h.ReadTool)
	toolGroup.POST("/write", h.WriteTool)
	toolGroup.POST("/edit", h.EditTool)
	toolGroup.POST("/glob", h.GlobTool)
	toolGroup.POST("/grep", h.GrepTool)
	toolGroup.POST("/git", h.GitTool)
}
