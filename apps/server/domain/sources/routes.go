package sources

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers the data-source-integrations routes.
//
// The addressed project is header-scoped (X-Project-ID), matching the SDK
// client which sends X-Project-ID and no :projectId path param. The auth stack
// mirrors discoveryjobs: RequireAuth → RequireProjectTokenScope →
// RequireProjectMember. There is no `data_sources:read`/`data_sources:write`
// scope in the auth middleware, so the API-token scope gates mirror the
// closest existing project-scoped data resource (documents:read / write /
// delete), which are granted to every canonical project role via the
// data:read/data:write umbrellas.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/data-source-integrations")
	g.Use(authMiddleware.RequireAuth())
	g.Use(authMiddleware.RequireProjectTokenScope())
	g.Use(authMiddleware.RequireProjectMember())

	// Provider catalog / schema / source-types (read).
	readGroup := g.Group("")
	readGroup.Use(authMiddleware.RequireAPITokenScopes("documents:read"))
	readGroup.GET("/providers", h.ListProviders)
	readGroup.GET("/providers/:providerType/schema", h.GetProviderSchema)
	readGroup.GET("/credentials", h.ListCredentials)
	readGroup.GET("/credentials/saved", h.ListSavedCredentials)
	readGroup.GET("/source-types", h.ListSourceTypes)
	readGroup.GET("", h.List)
	readGroup.GET("/:id", h.Get)
	readGroup.GET("/:id/resources", h.ListResources)
	readGroup.GET("/:id/sync-jobs", h.ListSyncJobs)
	readGroup.GET("/:id/sync-jobs/latest", h.GetLatestSyncJob)
	readGroup.GET("/:id/sync-jobs/:jobId", h.GetSyncJob)

	// Write operations (create/update/test + sync trigger/cancel).
	writeGroup := g.Group("")
	writeGroup.Use(authMiddleware.RequireAPITokenScopes("documents:write"))
	writeGroup.POST("", h.Create)
	writeGroup.PATCH("/:id", h.Update)
	writeGroup.POST("/test-config", h.TestConfig)
	writeGroup.POST("/resources", h.ListProviderResources)
	writeGroup.POST("/refs", h.ListProviderRefs)
	writeGroup.POST("/credentials/saved", h.CreateSavedCredential)
	writeGroup.PATCH("/credentials/saved/:id", h.UpdateSavedCredential)
	writeGroup.POST("/:id/test-connection", h.TestConnection)
	// The picker session mints a Google access token from the source's refresh
	// token, so it is grouped with the write scope even though it is a GET: a
	// read-scoped API token must not be able to obtain a token that reaches
	// further than the token's own scope.
	writeGroup.GET("/:id/picker-session", h.PickerSession)
	writeGroup.PUT("/:id/sync-targets", h.SaveSyncTargets)
	writeGroup.POST("/:id/sync", h.TriggerSync)
	writeGroup.POST("/:id/sync-jobs/:jobId/cancel", h.CancelSyncJob)
	writeGroup.GET("/providers/:providerType/oauth/authorize", h.StartOAuth)

	// Delete operation.
	deleteGroup := g.Group("")
	deleteGroup.Use(authMiddleware.RequireAPITokenScopes("documents:delete"))
	deleteGroup.DELETE("/credentials/saved/:id", h.DeleteSavedCredential)
	deleteGroup.DELETE("/:id", h.Delete)

	// OAuth callback: intentionally unauthenticated (the browser arrives
	// cross-site from the provider). The single-use, short-lived, unguessable
	// state is the trust anchor; project scope is carried in the state.
	e.GET("/api/data-source-integrations/oauth/:providerType/callback", h.CompleteOAuth)

	// Org-scoped OAuth client configs (§3.3). Registered under /api/orgs so the
	// addressed org is explicit; only RequireAuth is applied at the route layer —
	// org-admin authority is enforced inside the handler via the shared
	// org-administration check (pkg/auth.CanAdministerOrg).
	orgClients := e.Group("/api/orgs/:orgId/data-source-integrations/oauth-clients")
	orgClients.Use(authMiddleware.RequireAuth())
	orgClients.GET("", h.ListOrgOAuthClients)
	orgClients.GET("/:providerType", h.GetOrgOAuthClient)
	orgClients.PUT("/:providerType", h.UpsertOrgOAuthClient)
	orgClients.DELETE("/:providerType", h.DeleteOrgOAuthClient)

	// Project-scoped OAuth client configs. Registered under /api/projects so the
	// addressed project is explicit; only RequireAuth is applied at the route
	// layer — project-admin authority is enforced inside the handler via the
	// shared project-administration check (pkg/auth.CanAdministerProject).
	projectClients := e.Group("/api/projects/:projectId/data-source-integrations/oauth-clients")
	projectClients.Use(authMiddleware.RequireAuth())
	projectClients.GET("", h.ListProjectOAuthClients)
	projectClients.GET("/:providerType", h.GetProjectOAuthClient)
	projectClients.PUT("/:providerType", h.UpsertProjectOAuthClient)
	projectClients.DELETE("/:providerType", h.DeleteProjectOAuthClient)
}
