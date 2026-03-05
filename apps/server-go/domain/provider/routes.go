package provider

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/auth"
)

// RegisterRoutes registers all provider domain routes.
//
// Route groups:
//
//	PUT    /api/v1/organizations/:orgId/providers/:provider   — upsert org config
//	GET    /api/v1/organizations/:orgId/providers/:provider   — get org config metadata
//	DELETE /api/v1/organizations/:orgId/providers/:provider   — delete org config
//	GET    /api/v1/organizations/:orgId/providers             — list org configs
//	PUT    /api/v1/projects/:projectId/providers/:provider    — upsert project config
//	GET    /api/v1/projects/:projectId/providers/:provider    — get project config metadata
//	DELETE /api/v1/projects/:projectId/providers/:provider    — delete project config
//	GET    /api/v1/providers/:provider/models                 — read-only model catalog
//	POST   /api/v1/providers/:provider/test                   — live credential test
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	api := e.Group("/api/v1")
	api.Use(authMiddleware.RequireAuth())

	// Org-level provider configs
	orgs := api.Group("/organizations/:orgId/providers")
	orgs.PUT("/:provider", h.SaveOrgConfig)
	orgs.GET("/:provider", h.GetOrgConfig)
	orgs.DELETE("/:provider", h.DeleteOrgConfig)
	orgs.GET("", h.ListOrgConfigs)

	// Org-level usage summary
	api.GET("/organizations/:orgId/usage", h.GetOrgUsageSummary)

	// Project-level provider configs
	projects := api.Group("/projects/:projectId/providers")
	projects.PUT("/:provider", h.SaveProjectConfig)
	projects.GET("/:provider", h.GetProjectConfig)
	projects.DELETE("/:provider", h.DeleteProjectConfig)

	// Project-level usage summary
	api.GET("/projects/:projectId/usage", h.GetProjectUsageSummary)

	// Read-only model catalog
	api.GET("/providers/:provider/models", h.ListModels)

	// Live provider credential test
	api.POST("/providers/:provider/test", h.TestProvider)
}
