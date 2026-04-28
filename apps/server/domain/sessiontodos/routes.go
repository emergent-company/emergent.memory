package sessiontodos

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers session todo routes under the existing session namespace.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/v1/agent/sessions/:sessionId/todos")
	g.Use(authMiddleware.RequireAuth())

	g.GET("", h.List)
	g.POST("", h.Create)
	g.PATCH("/:todoId", h.Update)
	g.DELETE("/:todoId", h.Delete)
}
