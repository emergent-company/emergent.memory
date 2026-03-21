package journal

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers all journal routes.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/graph/journal")
	g.Use(authMiddleware.RequireAuth())

	g.GET("", h.ListJournal)
	g.POST("/notes", h.AddNote)
}
