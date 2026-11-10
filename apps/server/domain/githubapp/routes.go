package githubapp

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes wires the session-less webhook ingress and the org-scoped
// admin surface. Org-admin authority is enforced inside the service.
func RegisterRoutes(e *echo.Echo, h *Handler, mw *auth.Middleware) {
	e.POST("/api/webhooks/github/app", h.Webhook)

	g := e.Group("/api/orgs/:orgId/github-app")
	g.Use(mw.RequireAuth())
	g.GET("", h.GetOrgApp)
	g.POST("", h.RegisterApp)
	g.DELETE("/:appPK", h.DeleteApp)
	g.POST("/installations", h.CreateInstallation)
	g.PATCH("/installations/:id", h.UpdateInstallation)
	g.DELETE("/installations/:id", h.DeleteInstallation)
}
