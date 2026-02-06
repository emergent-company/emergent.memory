package integrations

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

// RegisterRoutes registers integrations routes with Echo and auth middleware
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// All integrations routes require authentication
	integrations := e.Group("/api/integrations")
	integrations.Use(authMiddleware.RequireAuth())

	// Available integrations (registry-based, no project context needed)
	integrations.GET("/available", h.ListAvailable)

	// Project-scoped integration operations
	integrations.GET("", h.List)
	integrations.GET("/:name", h.Get)
	integrations.GET("/:name/public", h.GetPublic)
	integrations.POST("", h.Create)
	integrations.PUT("/:name", h.Update)
	integrations.DELETE("/:name", h.Delete)
	integrations.POST("/:name/test", h.TestConnection)
	integrations.POST("/:name/sync", h.TriggerSync)
}
