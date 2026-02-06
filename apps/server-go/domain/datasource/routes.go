package datasource

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

// RegisterRoutes registers data source integration routes with Echo and auth middleware
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// All data source integration routes require authentication
	dsi := e.Group("/api/data-source-integrations")
	dsi.Use(authMiddleware.RequireAuth())

	// Provider endpoints
	dsi.GET("/providers", h.ListProviders)
	dsi.GET("/providers/:providerType/schema", h.GetProviderSchema)
	dsi.POST("/test-config", h.TestConfig)

	// Source types (document counts)
	dsi.GET("/source-types", h.GetSourceTypes)

	// Integration CRUD
	dsi.GET("", h.List)
	dsi.POST("", h.Create)
	dsi.GET("/:id", h.Get)
	dsi.PATCH("/:id", h.Update)
	dsi.DELETE("/:id", h.Delete)

	// Integration operations
	dsi.POST("/:id/test-connection", h.TestConnection)
	dsi.POST("/:id/sync", h.TriggerSync)

	// Sync jobs
	dsi.GET("/:id/sync-jobs", h.ListSyncJobs)
	dsi.GET("/:id/sync-jobs/latest", h.GetLatestSyncJob)
	dsi.GET("/:id/sync-jobs/:jobId", h.GetSyncJob)
	dsi.POST("/:id/sync-jobs/:jobId/cancel", h.CancelSyncJob)
}
