package useractivity

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
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

	// Member activity indicators. Org and project rankings are admin-gated in
	// the service layer; /me/activity is self-only.
	api := e.Group("/api")
	api.Use(authMiddleware.RequireAuth())
	api.GET("/orgs/:id/activity", h.OrgActivity)
	api.GET("/projects/:id/activity", h.ProjectActivity)
	api.GET("/me/activity", h.MyActivity)
}
