package mcpregistry

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/auth"
)

// RegisterRoutes registers MCP registry routes.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	admin := e.Group("/api/admin/mcp-servers")
	admin.Use(authMiddleware.RequireAuth())

	// Read operations - require admin:read
	readGroup := admin.Group("")
	readGroup.Use(authMiddleware.RequireScopes("admin:read"))
	readGroup.GET("", h.ListServers)
	readGroup.GET("/:id", h.GetServer)
	readGroup.GET("/:id/tools", h.ListServerTools)
	readGroup.POST("/:id/inspect", h.InspectServer)

	// Write operations - require admin:write
	writeGroup := admin.Group("")
	writeGroup.Use(authMiddleware.RequireScopes("admin:write"))
	writeGroup.POST("", h.CreateServer)
	writeGroup.PATCH("/:id", h.UpdateServer)
	writeGroup.DELETE("/:id", h.DeleteServer)
	writeGroup.PATCH("/:id/tools/:toolId", h.ToggleTool)
	writeGroup.POST("/:id/sync", h.SyncTools)

	// Official MCP Registry browse/install routes
	registry := e.Group("/api/admin/mcp-registry")
	registry.Use(authMiddleware.RequireAuth())

	// Read operations - search/get from public registry
	registryRead := registry.Group("")
	registryRead.Use(authMiddleware.RequireScopes("admin:read"))
	registryRead.GET("/search", h.SearchRegistry)
	registryRead.GET("/servers/:name", h.GetRegistryServer)

	// Write operations - install from registry into project
	registryWrite := registry.Group("")
	registryWrite.Use(authMiddleware.RequireScopes("admin:write"))
	registryWrite.POST("/install", h.InstallFromRegistry)
}
