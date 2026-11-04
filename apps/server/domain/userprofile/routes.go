package userprofile

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers the user profile routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/user/profile")
	g.Use(authMiddleware.RequireAuth())

	g.GET("", h.Get)
	g.PUT("", h.Update)

	av := e.Group("/api/user/avatar")
	av.Use(authMiddleware.RequireAuth())

	av.PUT("", h.Upload)
	av.GET("", h.GetAvatar)
	av.DELETE("", h.DeleteAvatar)
}
