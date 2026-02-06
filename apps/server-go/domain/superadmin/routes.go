package superadmin

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

// RegisterRoutes registers superadmin routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// All superadmin endpoints require authentication
	g := e.Group("/api/superadmin")
	g.Use(authMiddleware.RequireAuth())

	// Get current user's superadmin status (accessible to all authenticated users)
	g.GET("/me", h.GetMe)

	// Users management
	g.GET("/users", h.ListUsers)
	g.DELETE("/users/:id", h.DeleteUser)

	// Organizations management
	g.GET("/organizations", h.ListOrganizations)
	g.DELETE("/organizations/:id", h.DeleteOrganization)

	// Projects management
	g.GET("/projects", h.ListProjects)
	g.DELETE("/projects/:id", h.DeleteProject)

	// Email jobs management
	g.GET("/email-jobs", h.ListEmailJobs)
	g.GET("/email-jobs/:id/preview-json", h.GetEmailJobPreview)

	// Embedding jobs management
	g.GET("/embedding-jobs", h.ListEmbeddingJobs)
	g.POST("/embedding-jobs/delete", h.DeleteEmbeddingJobs)
	g.POST("/embedding-jobs/cleanup-orphans", h.CleanupOrphanEmbeddingJobs)

	// Extraction jobs management
	g.GET("/extraction-jobs", h.ListExtractionJobs)
	g.POST("/extraction-jobs/delete", h.DeleteExtractionJobs)
	g.POST("/extraction-jobs/cancel", h.CancelExtractionJobs)

	// Document parsing jobs management
	g.GET("/document-parsing-jobs", h.ListDocumentParsingJobs)
	g.POST("/document-parsing-jobs/delete", h.DeleteDocumentParsingJobs)
	g.POST("/document-parsing-jobs/retry", h.RetryDocumentParsingJobs)

	// Sync jobs management
	g.GET("/sync-jobs", h.ListSyncJobs)
	g.GET("/sync-jobs/:id/logs", h.GetSyncJobLogs)
	g.POST("/sync-jobs/delete", h.DeleteSyncJobs)
	g.POST("/sync-jobs/cancel", h.CancelSyncJobs)
}
