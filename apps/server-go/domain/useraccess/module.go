package useraccess

import (
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent/pkg/auth"
)

// Module provides the user access domain
var Module = fx.Module("useraccess",
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)

// RegisterRoutes registers the user access routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	user := e.Group("/api/user", authMiddleware.RequireAuth())
	user.GET("/orgs-and-projects", h.GetOrgsAndProjects)
}
