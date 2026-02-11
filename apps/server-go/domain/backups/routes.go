package backups

import (
	"github.com/labstack/echo/v4"
)

// RegisterRoutes registers backup routes
func RegisterRoutes(e *echo.Echo, handler *Handler) {
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
}
