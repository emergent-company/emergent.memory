package backups

import (
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/labstack/echo/v4"
)

// RegisterRoutes registers backup routes
func RegisterRoutes(e *echo.Echo, handler *Handler, authMiddleware *auth.Middleware) {
	// Organization-level backup management
	org := e.Group("/api/v1/organizations/:orgId")
	{
		org.GET("/backups", handler.ListBackups)
		org.GET("/backups/:backupId", handler.GetBackup)
		org.GET("/backups/:backupId/download", handler.DownloadBackup)
		org.DELETE("/backups/:backupId", handler.DeleteBackup)
	}

	// Project-level backup creation and restore
	projects := e.Group("/api/v1/projects/:projectId")
	{
		projects.POST("/backups", handler.CreateBackup)
		projects.POST("/restore", handler.RestoreBackup)
		projects.GET("/restores/:restoreId", handler.GetRestoreStatus)
	}

	// Superadmin: database-level backup management
	adminBackups := e.Group("/api/superadmin/database-backups")
	adminBackups.Use(authMiddleware.RequireAuth())
	adminBackups.GET("", handler.ListDatabaseBackups)
	adminBackups.GET("/:id/download", handler.DownloadDatabaseBackup)
}
