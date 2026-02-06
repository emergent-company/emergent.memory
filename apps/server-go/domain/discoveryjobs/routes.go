package discoveryjobs

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

// RegisterRoutes registers the discovery jobs routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/discovery-jobs")
	g.Use(authMiddleware.RequireAuth())

	// Read operations - require discovery:read scope
	readGroup := g.Group("")
	readGroup.Use(authMiddleware.RequireScopes("discovery:read"))
	readGroup.GET("/:jobId", h.GetJobStatus)
	readGroup.GET("/projects/:projectId", h.ListJobs)

	// Write operations - require discovery:write scope
	writeGroup := g.Group("")
	writeGroup.Use(authMiddleware.RequireScopes("discovery:write"))
	writeGroup.POST("/projects/:projectId/start", h.StartDiscovery)
	writeGroup.DELETE("/:jobId", h.CancelJob)
	writeGroup.POST("/:jobId/finalize", h.FinalizeDiscovery)
}
