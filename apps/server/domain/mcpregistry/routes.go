package mcpregistry

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers MCP registry routes.
//
// Authorization (issue #968, the #959/#964 class): every registry resource is
// project-scoped (kb.mcp_servers.project_id UUID NOT NULL — migration 00022;
// kb.mcp_server_tools joins to it), and each group's authority is the canonical
// RequireProjectTokenScope → RequireProjectMember pair (issue #868, the #864
// class). The per-subgroup `admin` API-token scope gate is deliberately removed:
// a bare `admin` scope is mintable by any project member (#948/#949) and — since
// RequireAPITokenScopes no-ops for OAuth sessions — carried no ownership signal.
// Project membership is the authority; the scope gate is a scope-only leftover.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	admin := e.Group("/api/admin/mcp-servers")
	admin.Use(authMiddleware.RequireAuth())
	// These admin routes scope by user.ProjectID (X-Project-ID header): enforce
	// token binding and session org-membership (issue #868, the #864 class).
	admin.Use(authMiddleware.RequireProjectTokenScope())
	admin.Use(authMiddleware.RequireProjectMember())

	// Read operations
	admin.GET("", h.ListServers)
	admin.GET("/:id", h.GetServer)
	admin.GET("/:id/tools", h.ListServerTools)
	admin.POST("/:id/inspect", h.InspectServer)

	// Write operations
	admin.POST("", h.CreateServer)
	admin.PATCH("/:id", h.UpdateServer)
	admin.DELETE("/:id", h.DeleteServer)
	admin.PATCH("/:id/tools/:toolId", h.ToggleTool)
	admin.POST("/:id/sync", h.SyncTools)

	// Built-in tools — separate endpoint family that never exposes the internal
	// "builtin" MCPServer record, only the flat tool list with inheritance info.
	builtins := e.Group("/api/admin/builtin-tools")
	builtins.Use(authMiddleware.RequireAuth())
	builtins.Use(authMiddleware.RequireProjectTokenScope())
	builtins.Use(authMiddleware.RequireProjectMember())

	builtins.GET("", h.ListBuiltinTools)
	builtins.PATCH("/:toolId", h.UpdateBuiltinTool)

	// Official MCP Registry browse/install routes
	registry := e.Group("/api/admin/mcp-registry")
	registry.Use(authMiddleware.RequireAuth())
	registry.Use(authMiddleware.RequireProjectTokenScope())
	registry.Use(authMiddleware.RequireProjectMember())

	// Read operations - search/get from public registry
	registry.GET("/search", h.SearchRegistry)
	registry.GET("/servers/:name", h.GetRegistryServer)

	// Write operations - install from registry into project
	registry.POST("/install", h.InstallFromRegistry)
}
