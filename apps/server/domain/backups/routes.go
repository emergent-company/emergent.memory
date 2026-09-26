package backups

import (
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/labstack/echo/v4"
)

// RegisterRoutes registers backup routes.
//
// Authorization model (tier-correct, server-derived — never from a
// client-supplied org/project id):
//
//   - /api/v1/organizations/:orgId/backups[...] and .../restore (clone): the
//     caller must be org_admin of the addressed org (requireOrgAdmin).
//   - /api/v1/projects/:projectId/backups and .../restore (overwrite): the
//     caller must be project_admin of the addressed project OR org_admin of its
//     owning org (requireProjectBackupAuthority).
//   - /api/v1/restores/:restoreId: the caller must be org_admin of the restore's
//     owning org; a foreign/missing id is a 404 (requireRestoreOwnership).
//   - /api/superadmin/database-backups[...]: the caller must hold an active
//     superadmin_full grant (RequireSuperadminFull) — a database-backup download
//     is the highest-sensitivity surface in this set.
func RegisterRoutes(e *echo.Echo, handler *Handler, authMiddleware *auth.Middleware) {
	// Organization-level backup management
	org := e.Group("/api/v1/organizations/:orgId")
	org.Use(authMiddleware.RequireAuth())
	{
		org.GET("/backups", handler.ListBackups)
		org.GET("/backups/:backupId", handler.GetBackup)
		org.GET("/backups/:backupId/download", handler.DownloadBackup)
		org.DELETE("/backups/:backupId", handler.DeleteBackup)
		// Import an archive produced by another deployment.
		org.POST("/backups/import", handler.ImportBackup)
		// Clone restore: creates a new project in this org from a backup.
		org.POST("/restore", handler.RestoreBackup)
	}

	// Project-level backup creation and restore
	projects := e.Group("/api/v1/projects/:projectId")
	projects.Use(authMiddleware.RequireAuth())
	{
		projects.POST("/backups", handler.CreateBackup)
		// Overwrite restore: replaces this project with the backup snapshot.
		projects.POST("/restore", handler.RestoreBackup)
	}

	// Top-level restore job status (clone may cross orgs)
	restores := e.Group("/api/v1/restores")
	restores.Use(authMiddleware.RequireAuth())
	{
		restores.GET("/:restoreId", handler.GetRestoreStatus)
	}

	// Superadmin: database-level backup management
	adminBackups := e.Group("/api/superadmin/database-backups")
	adminBackups.Use(authMiddleware.RequireAuth())
	adminBackups.Use(authMiddleware.RequireSuperadminFull())
	adminBackups.GET("", handler.ListDatabaseBackups)
	adminBackups.GET("/:id/download", handler.DownloadDatabaseBackup)
}
