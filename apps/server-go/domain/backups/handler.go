package backups

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/emergent/emergent-core/internal/storage"
	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
	"github.com/labstack/echo/v4"
)

type Handler struct {
	service *Service
	storage *storage.Service
	log     *slog.Logger
}

func NewHandler(service *Service, storage *storage.Service, log *slog.Logger) *Handler {
	return &Handler{
		service: service,
		storage: storage,
		log:     log.With(slog.String("component", "backups.handler")),
	}
}

type CreateBackupRequestDTO struct {
	IncludeDeleted bool `json:"includeDeleted"`
	IncludeChat    bool `json:"includeChat"`
	RetentionDays  int  `json:"retentionDays"`
}

// ListBackups lists all backups for an organization
// @Summary      List organization backups
// @Description  Returns paginated list of backups for an organization with optional project filtering and cursor-based pagination
// @Tags         backups
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        project_id query string false "Filter by project ID"
// @Param        limit query int false "Max results to return (1-100)" minimum(1) maximum(100)
// @Param        cursor query string false "Pagination cursor from previous response"
// @Success      200 {object} ListResult "Paginated backup list with next cursor"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/organizations/{orgId}/backups [get]
// @Security     bearerAuth
func (h *Handler) ListBackups(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	orgID := c.Param("orgId")

	limit := 20
	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	var projectID *string
	if pid := c.QueryParam("project_id"); pid != "" {
		projectID = &pid
	}

	var cursor *Cursor
	if cursorStr := c.QueryParam("cursor"); cursorStr != "" {
		cursor = &Cursor{}
	}

	result, err := h.service.ListBackups(c.Request().Context(), ListParams{
		OrganizationID: orgID,
		ProjectID:      projectID,
		Limit:          limit,
		Cursor:         cursor,
	})
	if err != nil {
		h.log.Error("failed to list backups",
			slog.String("org_id", orgID),
			slog.Any("error", err),
		)
		return apperror.NewInternal("failed to list backups", err)
	}

	return c.JSON(http.StatusOK, result)
}

// CreateBackup creates a new project backup
// @Summary      Create project backup
// @Description  Initiates async backup creation for a project (includes documents, chunks, graph data, optionally deleted items and chat). Returns backup entity with status 'creating'. Poll GET /organizations/{orgId}/backups/{backupId} to track progress.
// @Tags         backups
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        request body CreateBackupRequestDTO true "Backup configuration (includeDeleted, includeChat, retentionDays: 1-365)"
// @Success      202 {object} Backup "Backup creation initiated (status: creating)"
// @Failure      400 {object} apperror.Error "Invalid request or retention days out of range"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/projects/{projectId}/backups [post]
// @Security     bearerAuth
func (h *Handler) CreateBackup(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")

	var req CreateBackupRequestDTO
	if err := c.Bind(&req); err != nil {
		return err
	}

	if req.RetentionDays == 0 {
		req.RetentionDays = 30
	}

	if req.RetentionDays < 1 || req.RetentionDays > 365 {
		return echo.NewHTTPError(http.StatusBadRequest, "retention_days must be between 1 and 365")
	}

	var orgID string
	err := h.service.repo.db.NewSelect().
		Table("kb.projects").
		Column("organization_id").
		Where("id = ?", projectID).
		Scan(c.Request().Context(), &orgID)
	if err != nil {
		h.log.Error("failed to get project org",
			slog.String("project_id", projectID),
			slog.Any("error", err),
		)
		return apperror.NewInternal("failed to get project", err)
	}

	backup, err := h.service.CreateBackup(c.Request().Context(), CreateBackupRequest{
		ProjectID:      projectID,
		OrganizationID: orgID,
		CreatedBy:      user.ID,
		IncludeDeleted: req.IncludeDeleted,
		IncludeChat:    req.IncludeChat,
		RetentionDays:  req.RetentionDays,
	})
	if err != nil {
		h.log.Error("failed to create backup",
			slog.String("project_id", projectID),
			slog.Any("error", err),
		)
		return apperror.NewInternal("failed to create backup", err)
	}

	return c.JSON(http.StatusAccepted, backup)
}

// GetBackup retrieves a specific backup by ID
// @Summary      Get backup details
// @Description  Returns backup details including status, progress, size, statistics, and expiration info
// @Tags         backups
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        backupId path string true "Backup ID (UUID)"
// @Success      200 {object} Backup "Backup details"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Backup not found"
// @Router       /api/v1/organizations/{orgId}/backups/{backupId} [get]
// @Security     bearerAuth
func (h *Handler) GetBackup(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	orgID := c.Param("orgId")
	backupID := c.Param("backupId")

	backup, err := h.service.GetBackup(c.Request().Context(), orgID, backupID)
	if err != nil {
		h.log.Error("failed to get backup",
			slog.String("backup_id", backupID),
			slog.Any("error", err),
		)
		return apperror.NewNotFound("backup", backupID)
	}

	return c.JSON(http.StatusOK, backup)
}

// DownloadBackup generates a signed download URL and redirects to it
// @Summary      Download backup archive
// @Description  Generates a pre-signed download URL (1 hour expiration) and redirects browser to download the backup ZIP file. Backup must have status 'ready'.
// @Tags         backups
// @Produce      application/zip
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        backupId path string true "Backup ID (UUID)"
// @Success      302 "Redirect to pre-signed download URL"
// @Failure      400 {object} apperror.Error "Backup not ready for download"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Backup not found"
// @Failure      500 {object} apperror.Error "Failed to generate download URL"
// @Router       /api/v1/organizations/{orgId}/backups/{backupId}/download [get]
// @Security     bearerAuth
func (h *Handler) DownloadBackup(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	orgID := c.Param("orgId")
	backupID := c.Param("backupId")

	backup, err := h.service.GetBackup(c.Request().Context(), orgID, backupID)
	if err != nil {
		return apperror.NewNotFound("backup", backupID)
	}

	if backup.Status != BackupStatusReady {
		return echo.NewHTTPError(http.StatusBadRequest, "backup is not ready for download")
	}

	url, err := h.storage.GetSignedDownloadURL(
		c.Request().Context(),
		backup.StorageKey,
		storage.GetSignedDownloadURLOptions{
			ExpiresIn: time.Hour,
			ResponseContentDisposition: fmt.Sprintf(
				`attachment; filename="backup-%s-%s.zip"`,
				backup.ProjectName,
				backup.CreatedAt.Format("2006-01-02"),
			),
		},
	)
	if err != nil {
		h.log.Error("failed to generate presigned URL",
			slog.String("backup_id", backupID),
			slog.Any("error", err),
		)
		return apperror.NewInternal("failed to generate download URL", err)
	}

	return c.Redirect(http.StatusFound, url)
}

// DeleteBackup permanently deletes a backup
// @Summary      Delete backup
// @Description  Permanently deletes a backup and its associated storage files. Cannot be undone.
// @Tags         backups
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        backupId path string true "Backup ID (UUID)"
// @Success      204 "Backup deleted successfully"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Failed to delete backup"
// @Router       /api/v1/organizations/{orgId}/backups/{backupId} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteBackup(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	orgID := c.Param("orgId")
	backupID := c.Param("backupId")

	if err := h.service.DeleteBackup(c.Request().Context(), orgID, backupID); err != nil {
		h.log.Error("failed to delete backup",
			slog.String("backup_id", backupID),
			slog.Any("error", err),
		)
		return apperror.NewInternal("failed to delete backup", err)
	}

	return c.NoContent(http.StatusNoContent)
}

// RestoreBackup initiates a backup restore (not yet implemented)
// @Summary      Restore project from backup
// @Description  Initiates async restore of a project from backup archive (coming in next phase)
// @Tags         backups
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      501 {object} map[string]interface{} "Not implemented - coming in next phase"
// @Router       /api/v1/projects/{projectId}/restore [post]
// @Security     bearerAuth
func (h *Handler) RestoreBackup(c echo.Context) error {
	return c.JSON(http.StatusNotImplemented, map[string]any{
		"message": "restore functionality coming in next phase",
	})
}

// GetRestoreStatus retrieves restore job status (not yet implemented)
// @Summary      Get restore job status
// @Description  Returns restore job progress and status (coming in next phase)
// @Tags         backups
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        restoreId path string true "Restore job ID (UUID)"
// @Success      501 {object} map[string]interface{} "Not implemented - coming in next phase"
// @Router       /api/v1/projects/{projectId}/restores/{restoreId} [get]
// @Security     bearerAuth
func (h *Handler) GetRestoreStatus(c echo.Context) error {
	return c.JSON(http.StatusNotImplemented, map[string]any{
		"message": "restore functionality coming in next phase",
	})
}
