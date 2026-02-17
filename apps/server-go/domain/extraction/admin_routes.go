package extraction

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/auth"
)

// RegisterAdminRoutes registers extraction jobs admin routes with fx
// This is called by fx.Invoke to wire up the routes
func RegisterAdminRoutes(e *echo.Echo, h *AdminHandler, authMiddleware *auth.Middleware) {
	// All extraction job routes require authentication and admin:read or admin:write scope
	admin := e.Group("/api/admin/extraction-jobs")
	admin.Use(authMiddleware.RequireAuth())

	// Read operations - require admin:read
	readGroup := admin.Group("")
	readGroup.Use(authMiddleware.RequireScopes("admin:read"))
	readGroup.GET("/projects/:projectId", h.ListJobs)
	readGroup.GET("/projects/:projectId/statistics", h.GetStatistics)
	readGroup.GET("/:jobId", h.GetJob)
	readGroup.GET("/:jobId/logs", h.GetLogs)

	// Write operations - require admin:write
	writeGroup := admin.Group("")
	writeGroup.Use(authMiddleware.RequireScopes("admin:write"))
	writeGroup.POST("", h.CreateJob)
	writeGroup.POST("/projects/:projectId/bulk-cancel", h.BulkCancelJobs)
	writeGroup.DELETE("/projects/:projectId/bulk-delete", h.BulkDeleteJobs)
	writeGroup.POST("/projects/:projectId/bulk-retry", h.BulkRetryJobs)
	writeGroup.PATCH("/:jobId", h.UpdateJob)
	writeGroup.DELETE("/:jobId", h.DeleteJob)
	writeGroup.POST("/:jobId/cancel", h.CancelJob)
	writeGroup.POST("/:jobId/retry", h.RetryJob)
}
