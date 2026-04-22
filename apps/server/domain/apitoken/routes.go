package apitoken

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers API token routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// Project-scoped token routes
	g := e.Group("/api/projects/:projectId/tokens")
	g.Use(authMiddleware.RequireAuth())
	g.Use(authMiddleware.RequireAPITokenScopes("project:read"))

	// Creating a token only requires auth + project membership (checked in service layer).
	// The project:read scope guard is intentionally omitted here — a project_admin
	// authenticated via a token without project:read should still be able to bootstrap
	// new tokens for their project.
	e.POST("/api/projects/:projectId/tokens", h.Create, authMiddleware.RequireAuth())

	g.GET("", h.List)
	g.GET("/:tokenId", h.Get)
	g.DELETE("/:tokenId", h.Revoke)

	// Account-level token routes (not bound to a project)
	ag := e.Group("/api/tokens")
	ag.Use(authMiddleware.RequireAuth())

	ag.POST("", h.CreateAccountToken)
	ag.GET("", h.ListAccountTokens)
	ag.GET("/:tokenId", h.GetAccountToken)
	ag.DELETE("/:tokenId", h.RevokeAccountToken)
}
