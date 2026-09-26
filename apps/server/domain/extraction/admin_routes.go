package extraction

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterAdminRoutes registers extraction jobs admin routes with fx
// This is called by fx.Invoke to wire up the routes
//
// Authorization (issue #959): the group previously gated every route on a bare
// `admin` API-token scope, which is mintable by any project member (#948/#949)
// and carries no membership/ownership check. The routes are now split by the
// source of the addressed project:
//
//   - :projectId-path routes are project-scoped and gated by the canonical
//     RequireProjectTokenScope → RequireProjectMember pair.
//   - :jobId-path routes and the body-scoped create resolve the project in the
//     handler (from the job record or the request body) and enforce membership
//     there via auth.RequireProjectMembership, because the shared pair cannot
//     see a project sourced from those places.
func RegisterAdminRoutes(e *echo.Echo, h *AdminHandler, authMiddleware *auth.Middleware) {
	admin := e.Group("/api/admin/extraction-jobs")
	admin.Use(authMiddleware.RequireAuth())

	// Project-scoped: the addressed project is the :projectId path param.
	projectScoped := admin.Group("")
	projectScoped.Use(authMiddleware.RequireProjectTokenScope(), authMiddleware.RequireProjectMember())
	projectScoped.GET("/projects/:projectId", h.ListJobs)
	projectScoped.GET("/projects/:projectId/statistics", h.GetStatistics)
	projectScoped.POST("/projects/:projectId/bulk-cancel", h.BulkCancelJobs)
	projectScoped.DELETE("/projects/:projectId/bulk-delete", h.BulkDeleteJobs)
	projectScoped.POST("/projects/:projectId/bulk-retry", h.BulkRetryJobs)

	// Job/body-scoped: the project is resolved in the handler, which enforces
	// membership via auth.RequireProjectMembership.
	jobScoped := admin.Group("")
	jobScoped.POST("", h.CreateJob)
	jobScoped.GET("/:jobId", h.GetJob)
	jobScoped.GET("/:jobId/logs", h.GetLogs)
	jobScoped.PATCH("/:jobId", h.UpdateJob)
	jobScoped.DELETE("/:jobId", h.DeleteJob)
	jobScoped.POST("/:jobId/cancel", h.CancelJob)
	jobScoped.POST("/:jobId/retry", h.RetryJob)
}
