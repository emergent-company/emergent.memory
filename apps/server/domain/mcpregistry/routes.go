package mcpregistry

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers MCP registry routes.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	admin := e.Group("/api/admin/mcp-servers")
	admin.Use(authMiddleware.RequireAuth())
	// These admin routes scope by user.ProjectID (X-Project-ID header): enforce
	// token binding and session org-membership (issue #868, the #864 class).
	admin.Use(authMiddleware.RequireProjectTokenScope())
	admin.Use(authMiddleware.RequireProjectMember())

	// Read operations - require admin
	readGroup := admin.Group("")
	readGroup.Use(authMiddleware.RequireAPITokenScopes("admin"))
	readGroup.GET("", h.ListServers)
	readGroup.GET("/:id", h.GetServer)
	readGroup.GET("/:id/tools", h.ListServerTools)
	readGroup.POST("/:id/inspect", h.InspectServer)

	// Write operations - require admin
	writeGroup := admin.Group("")
	writeGroup.Use(authMiddleware.RequireAPITokenScopes("admin"))
	writeGroup.POST("", h.CreateServer)
	writeGroup.PATCH("/:id", h.UpdateServer)
	writeGroup.DELETE("/:id", h.DeleteServer)
	writeGroup.PATCH("/:id/tools/:toolId", h.ToggleTool)
	writeGroup.POST("/:id/sync", h.SyncTools)

	// Built-in tools — separate endpoint family that never exposes the internal
	// "builtin" MCPServer record, only the flat tool list with inheritance info.
	builtins := e.Group("/api/admin/builtin-tools")
	builtins.Use(authMiddleware.RequireAuth())
	builtins.Use(authMiddleware.RequireProjectTokenScope())
	builtins.Use(authMiddleware.RequireProjectMember())

	builtinRead := builtins.Group("")
	builtinRead.Use(authMiddleware.RequireAPITokenScopes("admin"))
	builtinRead.GET("", h.ListBuiltinTools)

	builtinWrite := builtins.Group("")
	builtinWrite.Use(authMiddleware.RequireAPITokenScopes("admin"))
	builtinWrite.PATCH("/:toolId", h.UpdateBuiltinTool)

	// Official MCP Registry browse/install routes
	registry := e.Group("/api/admin/mcp-registry")
	registry.Use(authMiddleware.RequireAuth())
	registry.Use(authMiddleware.RequireProjectTokenScope())
	registry.Use(authMiddleware.RequireProjectMember())

	// Read operations - search/get from public registry
	registryRead := registry.Group("")
	registryRead.Use(authMiddleware.RequireAPITokenScopes("admin"))
	registryRead.GET("/search", h.SearchRegistry)
	registryRead.GET("/servers/:name", h.GetRegistryServer)

	// Write operations - install from registry into project
	registryWrite := registry.Group("")
	registryWrite.Use(authMiddleware.RequireAPITokenScopes("admin"))
	registryWrite.POST("/install", h.InstallFromRegistry)
}
