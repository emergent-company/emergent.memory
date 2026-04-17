package provider

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers all provider domain routes.
//
// Route groups:
//
//	PUT    /api/v1/organizations/:orgId/providers/:provider   — upsert org config
//	GET    /api/v1/organizations/:orgId/providers/:provider   — get org config metadata
//	DELETE /api/v1/organizations/:orgId/providers/:provider   — delete org config
//	GET    /api/v1/organizations/:orgId/providers             — list org configs
//	GET    /api/v1/organizations/:orgId/project-providers    — list all project-level overrides for org
//	PUT    /api/v1/projects/:projectId/providers/:provider    — upsert project config
//	GET    /api/v1/projects/:projectId/providers/:provider    — get project config metadata
//	DELETE /api/v1/projects/:projectId/providers/:provider    — delete project config
//	GET    /api/v1/providers/:provider/models                 — read-only model catalog (per provider)
//	GET    /api/v1/models                                     — list all models across providers (agents:read)
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

	// Org-scoped project-level overrides
	api.GET("/organizations/:orgId/project-providers", h.ListProjectConfigs)

	// Org-level usage summary and breakdowns
	api.GET("/organizations/:orgId/usage", h.GetOrgUsageSummary)
	api.GET("/organizations/:orgId/usage/timeseries", h.GetOrgUsageTimeSeries)
	api.GET("/organizations/:orgId/usage/by-project", h.GetOrgUsageByProject)

	// Project-level provider configs
	projects := api.Group("/projects/:projectId/providers")
	projects.PUT("/:provider", h.SaveProjectConfig)
	projects.GET("/:provider", h.GetProjectConfig)
	projects.DELETE("/:provider", h.DeleteProjectConfig)

	// Project-level usage summary and timeseries
	api.GET("/projects/:projectId/usage", h.GetProjectUsageSummary)
	api.GET("/projects/:projectId/usage/timeseries", h.GetProjectUsageTimeSeries)

	// Read-only model catalog (per provider)
	api.GET("/providers/:provider/models", h.ListModels)

	// Global model catalog — accessible with agents:read project token
	globalModels := e.Group("/api/v1/models")
	globalModels.Use(authMiddleware.RequireAuth())
	globalModels.Use(authMiddleware.RequireAPITokenScopes("agents:read"))
	globalModels.GET("", h.ListAllModels)

	// Live provider credential test
	api.POST("/providers/:provider/test", h.TestProvider)
}
