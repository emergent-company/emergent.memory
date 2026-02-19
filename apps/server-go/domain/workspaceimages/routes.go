package workspaceimages

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/auth"
)

// RegisterRoutes registers workspace image admin API routes.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	admin := e.Group("/api/admin/workspace-images")
	admin.Use(authMiddleware.RequireAuth())

	// Read operations
	readGroup := admin.Group("")
	readGroup.Use(authMiddleware.RequireScopes("admin:read"))
	readGroup.GET("", h.List)
	readGroup.GET("/:id", h.Get)

	// Write operations
	writeGroup := admin.Group("")
	writeGroup.Use(authMiddleware.RequireScopes("admin:write"))
	writeGroup.POST("", h.Create)
	writeGroup.DELETE("/:id", h.Delete)
}
