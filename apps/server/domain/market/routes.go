package market

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes mounts the project-scoped marketplace install endpoints. Every
// route requires authentication; the project comes from X-Project-ID (normalized
// onto the user by RequireAuth), with apply additionally accepting a body
// project_id that it authorizes explicitly.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/market")
	g.Use(authMiddleware.RequireAuth())
	g.Use(authMiddleware.RequireProjectTokenScope())
	g.Use(authMiddleware.RequireProjectMember())

	g.POST("/installs/resolve", h.Resolve, authMiddleware.RequireProjectID())
	g.POST("/installs/apply", h.Apply)
}
