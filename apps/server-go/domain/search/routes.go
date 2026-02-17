package search

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/auth"
)

// RegisterRoutes registers the search routes
func RegisterRoutes(e *echo.Echo, handler *Handler, authMiddleware *auth.Middleware) {
	// Search routes require authentication and project ID
	search := e.Group("/api/search")
	search.Use(authMiddleware.RequireAuth())
	search.Use(authMiddleware.RequireProjectID())

	// Unified search requires search:read scope
	unified := search.Group("/unified")
	unified.Use(authMiddleware.RequireScopes("search:read"))
	unified.POST("", handler.Search)
}
