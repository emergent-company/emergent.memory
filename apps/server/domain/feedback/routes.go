package feedback

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers the feedback routes.
//
// The route is authenticated but deliberately not project-gated: feedback is
// about the product, and a user may want to file it before choosing a project.
// A project-token caller still gets its project context attached to the issue.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/feedback")
	g.Use(authMiddleware.RequireAuth())

	g.POST("", h.Submit)
}
