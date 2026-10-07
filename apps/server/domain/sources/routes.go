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
	readGroup.GET("/source-types", h.ListSourceTypes)
	readGroup.GET("", h.List)
	readGroup.GET("/:id", h.Get)
	readGroup.GET("/:id/sync-jobs", h.ListSyncJobs)
	readGroup.GET("/:id/sync-jobs/latest", h.GetLatestSyncJob)
	readGroup.GET("/:id/sync-jobs/:jobId", h.GetSyncJob)

	// Write operations (create/update/test + sync trigger/cancel).
	writeGroup := g.Group("")
	writeGroup.Use(authMiddleware.RequireAPITokenScopes("documents:write"))
	writeGroup.POST("", h.Create)
	writeGroup.PATCH("/:id", h.Update)
	writeGroup.POST("/test-config", h.TestConfig)
	writeGroup.POST("/:id/test-connection", h.TestConnection)
	writeGroup.POST("/:id/sync", h.TriggerSync)
	writeGroup.POST("/:id/sync-jobs/:jobId/cancel", h.CancelSyncJob)

	// Delete operation.
	deleteGroup := g.Group("")
	deleteGroup.Use(authMiddleware.RequireAPITokenScopes("documents:delete"))
	deleteGroup.DELETE("/:id", h.Delete)
}
