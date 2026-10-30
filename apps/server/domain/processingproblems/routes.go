package processingproblems

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers the project-scoped processing-problem endpoint.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/projects/:projectId/processing-problems")
	g.Use(authMiddleware.RequireAuth())
	g.GET("", h.List)
}
