package provider

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/auth"
)

// RegisterRoutes registers all provider domain routes.
//
// Route groups:
//
//	/api/v1/organizations/:orgId/providers/...   — org-level credential & model management
//	/api/v1/projects/:projectId/providers/...    — project-level policy management
//	/api/v1/providers/:provider/models           — read-only model catalog
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// All provider routes require authentication.
	api := e.Group("/api/v1")
	api.Use(authMiddleware.RequireAuth())

	// Org-level credentials (provider-specific POST routes)
	orgs := api.Group("/organizations/:orgId/providers")
	orgs.POST("/google-ai/credentials", h.SaveGoogleAICredential)
	orgs.POST("/vertex-ai/credentials", h.SaveVertexAICredential)
	orgs.DELETE("/:provider/credentials", h.DeleteOrgCredential)
	orgs.GET("/credentials", h.ListOrgCredentials)
	orgs.PUT("/:provider/models", h.SetOrgModelSelection)

	// Org-level usage summary
	api.GET("/organizations/:orgId/usage", h.GetOrgUsageSummary)

	// Project-level policy management
	projects := api.Group("/projects/:projectId/providers")
	projects.PUT("/:provider/policy", h.SetProjectPolicy)
	projects.GET("/:provider/policy", h.GetProjectPolicy)
	projects.GET("/policies", h.ListProjectPolicies)

	// Project-level usage summary
	api.GET("/projects/:projectId/usage", h.GetProjectUsageSummary)

	// Read-only model catalog (any authenticated user)
	api.GET("/providers/:provider/models", h.ListModels)
}
