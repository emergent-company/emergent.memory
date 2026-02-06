package useractivity

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

// RegisterRoutes registers user activity routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/user-activity")
	g.Use(authMiddleware.RequireAuth())

	g.POST("/record", h.Record)
	g.GET("/recent", h.GetRecent)
	g.GET("/recent/:type", h.GetRecentByType)
	g.DELETE("/recent", h.DeleteAll)
	g.DELETE("/recent/:type/:resourceId", h.DeleteByResource)
}
