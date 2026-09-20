package backups

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/labstack/echo/v4"
)

// MaxImportArchiveSize is the maximum accepted size (1 GiB) of an imported
// backup archive uploaded via the import endpoint.
const MaxImportArchiveSize int64 = 1 << 30

// isZipArchive reports whether data starts with a ZIP local-file, empty-archive,
// or spanned-archive signature (PK\x03\x04 / PK\x05\x06 / PK\x07\x08).
func isZipArchive(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	if data[0] != 'P' || data[1] != 'K' {
		return false
	}
	switch data[2] {
	case 0x03, 0x05, 0x07:
		return data[3] == data[2]+1
	default:
		return false
	}
}

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

// ParseCursor decodes a base64url-encoded cursor (JSON {createdAt, id}) into a
// Cursor. It is the inverse of the SDK's Cursor.Encode, so the value printed by
// the CLI can be passed back verbatim as the `cursor` query parameter.
func ParseCursor(encoded string) (*Cursor, error) {
	if encoded == "" {
		return nil, nil
	}
	data, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor encoding: %w", err)
	}
	var cursor Cursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, fmt.Errorf("invalid cursor format: %w", err)
	}
	return &cursor, nil
}

// CreateBackupRequestDTO is the request body for creating a backup.
type CreateBackupRequestDTO struct {
	IncludeDeleted bool `json:"includeDeleted"`
	IncludeChat    bool `json:"includeChat"`
	IncludeJournal bool `json:"includeJournal"`
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
		parsed, err := ParseCursor(cursorStr)
		if err != nil {
			return apperror.NewBadRequest("invalid cursor")
		}
		cursor = parsed
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
// @Param        request body CreateBackupRequestDTO true "Backup configuration (includeDeleted, includeChat, includeJournal, retentionDays: 1-365)"
// @Success      202 {object} Backup "Backup creation initiated (status: creating)"
// @Failure      400 {object} apperror.Error "Invalid request or retention days out of range"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/projects/{projectId}/backups [post]
// @Security     bearerAuth
func (h *Handler) CreateBackup(c echo.Context) error {
	user := auth.MustGetUser(c)

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
		IncludeJournal: req.IncludeJournal,
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

// parseRetentionDays reads the optional retentionDays form value, defaulting to
// 30 when absent and validating the 1..365 range.
func parseRetentionDays(c echo.Context) (int, *apperror.Error) {
	retentionDays := 30
	if rd := c.FormValue("retentionDays"); rd != "" {
		parsed, err := strconv.Atoi(rd)
		if err != nil {
			return 0, apperror.NewBadRequest("retentionDays must be an integer")
		}
		retentionDays = parsed
	}
	if retentionDays < 1 || retentionDays > 365 {
		return 0, apperror.NewBadRequest("retentionDays must be between 1 and 365")
	}
	return retentionDays, nil
}

// readArchiveUpload parses the multipart `file` field, enforcing the size cap
// and ZIP signature, and returns the raw archive bytes. The caller must have
// already capped the request body with http.MaxBytesReader (see ImportBackup) so
// that an oversized upload is rejected while being read, not after.
func readArchiveUpload(c echo.Context) ([]byte, *apperror.Error) {
	file, err := c.FormFile("file")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return nil, apperror.New(http.StatusRequestEntityTooLarge, "file_too_large", "archive exceeds maximum size")
		}
		return nil, apperror.NewBadRequest("file field is required")
	}

	if file.Size > MaxImportArchiveSize {
		return nil, apperror.New(http.StatusRequestEntityTooLarge, "file_too_large", "archive exceeds maximum size")
	}

	src, err := file.Open()
	if err != nil {
		return nil, apperror.NewInternal("failed to open uploaded file", err)
	}
	defer src.Close()

	data, err := io.ReadAll(src)
	if err != nil {
		return nil, apperror.NewInternal("failed to read uploaded file", err)
	}
	if int64(len(data)) > MaxImportArchiveSize {
		return nil, apperror.New(http.StatusRequestEntityTooLarge, "file_too_large", "archive exceeds maximum size")
	}

	if !isZipArchive(data) {
		return nil, apperror.New(http.StatusUnsupportedMediaType, "unsupported_media_type", "uploaded file must be a ZIP archive")
	}

	return data, nil
}

// ImportBackup accepts a backup archive produced by another deployment and
// registers it as a ready, imported backup for clone restore.
// @Summary      Import backup archive
// @Description  Accepts a backup ZIP archive (multipart/form-data field `file`, max 1 GiB) from another deployment, validates its manifest and checksums, stores it, and registers a `ready` backup flagged `imported`. Imported backups support clone restore only.
// @Tags         backups
// @Accept       multipart/form-data
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        file formData file true "Backup archive (ZIP)"
// @Param        retentionDays formData int false "Retention days (1-365, default 30)"
// @Success      201 {object} Backup "Backup registered (status: ready, imported: true)"
// @Failure      400 {object} apperror.Error "Invalid archive"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Not a member of the target organization"
// @Failure      413 {object} apperror.Error "Archive too large"
// @Failure      415 {object} apperror.Error "Not a ZIP archive"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/organizations/{orgId}/backups/import [post]
// @Security     bearerAuth
func (h *Handler) ImportBackup(c echo.Context) error {
	user := auth.MustGetUser(c)
	orgID := c.Param("orgId")

	// Cap the request body before anything reads the form. Multipart parsing
	// consumes the entire body (spilling parts past maxMemory to temp files), so
	// a limit applied after the first FormValue/FormFile call would let an
	// oversized upload be fully consumed before the cap is ever enforced.
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, MaxImportArchiveSize+1024)

	// Org membership check first: authorization before any body work.
	var memberCount int64
	if err := h.service.repo.db.NewSelect().
		Table("kb.organization_memberships").
		ColumnExpr("count(*)").
		Where("organization_id = ?", orgID).
		Where("user_id = ?", user.ID).
		Scan(c.Request().Context(), &memberCount); err != nil {
		return apperror.NewInternal("failed to verify org membership", err)
	}
	if memberCount == 0 {
		return apperror.NewForbidden("you are not a member of the target organization")
	}

	retentionDays, aerr := parseRetentionDays(c)
	if aerr != nil {
		return aerr
	}

	data, aerr := readArchiveUpload(c)
	if aerr != nil {
		return aerr
	}

	backup, err := h.service.ImportBackup(c.Request().Context(), orgID, user.ID, data, retentionDays)
	if err != nil {
		if errors.Is(err, ErrInvalidArchive) {
			return apperror.NewBadRequest(err.Error())
		}
		h.log.Error("failed to import backup",
			slog.String("org_id", orgID),
			slog.Any("error", err),
		)
		return apperror.NewInternal("failed to import backup", err)
	}

	return c.JSON(http.StatusCreated, backup)
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

// RestoreRequestDTO is the request body for creating a restore job.
type RestoreRequestDTO struct {
	BackupID           string `json:"backupId"`
	Mode               string `json:"mode"`
	PreRestoreSnapshot *bool  `json:"preRestoreSnapshot"`
	IncludeJournal     bool   `json:"includeJournal"`
	TargetProjectName  string `json:"targetProjectName"`
}

// RestoreBackup initiates an async restore. The route determines the mode:
// POST /projects/:projectId/restore is an overwrite; POST
// /organizations/:orgId/restore is a clone into that org.
// @Summary      Restore project from backup
// @Description  Initiates async restore of a project from a backup archive. The project route overwrites the existing project; the organization route clones into a new project.
// @Tags         backups
// @Accept       json
// @Produce      json
// @Param        request body RestoreRequestDTO true "Restore request (backupId, preRestoreSnapshot, includeJournal, targetProjectName)"
// @Success      202 {object} Restore "Restore job created (status: pending)"
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Not a member of the target organization"
// @Failure      404 {object} apperror.Error "Backup not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/projects/{projectId}/restore [post]
// @Security     bearerAuth
func (h *Handler) RestoreBackup(c echo.Context) error {
	user := auth.MustGetUser(c)

	var dto RestoreRequestDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	if dto.BackupID == "" {
		return apperror.NewBadRequest("backupId is required")
	}
	preSnapshot := true
	if dto.PreRestoreSnapshot != nil {
		preSnapshot = *dto.PreRestoreSnapshot
	}

	ctx := c.Request().Context()

	if projectID := c.Param("projectId"); projectID != "" {
		return h.restoreOverwrite(c, ctx, user, projectID, dto, preSnapshot)
	}
	orgID := c.Param("orgId")
	if orgID == "" {
		return apperror.NewBadRequest("restore requires a projectId or orgId route")
	}
	return h.restoreClone(c, ctx, user, orgID, dto)
}

// restoreOverwrite handles POST /projects/:projectId/restore.
func (h *Handler) restoreOverwrite(c echo.Context, ctx context.Context, user *auth.AuthUser, projectID string, dto RestoreRequestDTO, preSnapshot bool) error {
	var orgID string
	if err := h.service.repo.db.NewSelect().
		Table("kb.projects").
		Column("organization_id").
		Where("id = ?", projectID).
		Scan(ctx, &orgID); err != nil {
		h.log.Error("failed to get project org",
			slog.String("project_id", projectID),
			slog.Any("error", err),
		)
		return apperror.NewNotFound("project", projectID)
	}

	backup, err := h.service.GetBackup(ctx, orgID, dto.BackupID)
	if err != nil {
		return apperror.NewInternal("failed to get backup", err)
	}
	if backup == nil {
		return apperror.NewNotFound("backup", dto.BackupID)
	}
	if backup.Status != BackupStatusReady {
		return apperror.NewBadRequest("backup is not ready for restore")
	}
	if backup.Imported {
		return apperror.NewBadRequest("imported backups support clone restore only")
	}
	if backup.ProjectID != projectID {
		return apperror.NewBadRequest("backup does not belong to this project")
	}

	job, err := h.service.CreateRestore(ctx, RestoreRequest{
		BackupID:           dto.BackupID,
		Mode:               RestoreModeOverwrite,
		OrganizationID:     orgID,
		SourceProjectID:    projectID,
		TargetProjectID:    projectID,
		CreatedBy:          user.ID,
		PreRestoreSnapshot: preSnapshot,
		IncludeJournal:     dto.IncludeJournal,
		TargetOrgID:        orgID,
	})
	if err != nil {
		h.log.Error("failed to create restore",
			slog.String("backup_id", dto.BackupID),
			slog.Any("error", err),
		)
		return apperror.NewInternal("failed to create restore", err)
	}

	return c.JSON(http.StatusAccepted, job)
}

// restoreClone handles POST /organizations/:orgId/restore. The path org is the
// clone destination (supports cross-org restore).
func (h *Handler) restoreClone(c echo.Context, ctx context.Context, user *auth.AuthUser, orgID string, dto RestoreRequestDTO) error {
	// The restorer must be a member of the destination org.
	var memberCount int64
	if err := h.service.repo.db.NewSelect().
		Table("kb.organization_memberships").
		ColumnExpr("count(*)").
		Where("organization_id = ?", orgID).
		Where("user_id = ?", user.ID).
		Scan(ctx, &memberCount); err != nil {
		return apperror.NewInternal("failed to verify org membership", err)
	}
	if memberCount == 0 {
		return apperror.NewForbidden("you are not a member of the target organization")
	}

	backup, err := h.fetchBackupByID(ctx, dto.BackupID)
	if err != nil {
		return apperror.NewInternal("failed to get backup", err)
	}
	if backup == nil {
		return apperror.NewNotFound("backup", dto.BackupID)
	}
	if backup.Status != BackupStatusReady {
		return apperror.NewBadRequest("backup is not ready for restore")
	}

	job, err := h.service.CreateRestore(ctx, RestoreRequest{
		BackupID:          dto.BackupID,
		Mode:              RestoreModeClone,
		OrganizationID:    orgID,
		SourceProjectID:   backup.ProjectID,
		CreatedBy:         user.ID,
		IncludeJournal:    dto.IncludeJournal,
		TargetProjectName: dto.TargetProjectName,
		TargetOrgID:       orgID,
	})
	if err != nil {
		h.log.Error("failed to create restore",
			slog.String("backup_id", dto.BackupID),
			slog.Any("error", err),
		)
		return apperror.NewInternal("failed to create restore", err)
	}

	return c.JSON(http.StatusAccepted, job)
}

// fetchBackupByID loads a backup regardless of its org (needed for cross-org clone).
func (h *Handler) fetchBackupByID(ctx context.Context, backupID string) (*Backup, error) {
	var backup Backup
	err := h.service.repo.db.NewSelect().
		Model(&backup).
		Where("id = ?", backupID).
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &backup, nil
}

// GetRestoreStatus retrieves restore job status.
// @Summary      Get restore job status
// @Description  Returns restore job status, progress, and error message (if failed)
// @Tags         backups
// @Produce      json
// @Param        restoreId path string true "Restore job ID (UUID)"
// @Success      200 {object} Restore "Restore job status"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Restore job not found"
// @Router       /api/v1/restores/{restoreId} [get]
// @Security     bearerAuth
func (h *Handler) GetRestoreStatus(c echo.Context) error {

	restoreID := c.Param("restoreId")
	job, err := h.service.GetRestore(c.Request().Context(), restoreID)
	if err != nil {
		h.log.Error("failed to get restore",
			slog.String("restore_id", restoreID),
			slog.Any("error", err),
		)
		return apperror.NewInternal("failed to get restore", err)
	}
	if job == nil {
		return apperror.NewNotFound("restore", restoreID)
	}

	return c.JSON(http.StatusOK, job)
}

// ListDatabaseBackups lists all database-level backups (superadmin only)
// @Summary      List database backups
// @Description  Returns all full database backup records ordered by creation date descending
// @Tags         superadmin
// @Produce      json
// @Success      200 {array} backups.DatabaseBackupResponse "List of database backups"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/superadmin/database-backups [get]
// @Security     bearerAuth
func (h *Handler) ListDatabaseBackups(c echo.Context) error {

	backups, err := h.service.ListDatabaseBackups(c.Request().Context())
	if err != nil {
		h.log.Error("failed to list database backups", slog.Any("error", err))
		return apperror.NewInternal("failed to list database backups", err)
	}

	return c.JSON(http.StatusOK, backups)
}

// DownloadDatabaseBackup generates a presigned URL and redirects to it (superadmin only)
// @Summary      Download database backup
// @Description  Generates a 15-minute presigned URL for downloading a database backup dump file and redirects to it
// @Tags         superadmin
// @Produce      json
// @Param        id path string true "Database Backup ID (UUID)"
// @Success      302 "Redirect to presigned download URL"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Backup not found"
// @Failure      500 {object} apperror.Error "Failed to generate download URL"
// @Router       /api/superadmin/database-backups/{id}/download [get]
// @Security     bearerAuth
func (h *Handler) DownloadDatabaseBackup(c echo.Context) error {

	id := c.Param("id")
	url, err := h.service.GetDatabaseBackupDownloadURL(c.Request().Context(), id)
	if err != nil {
		h.log.Error("failed to get database backup download URL",
			slog.String("id", id),
			slog.Any("error", err),
		)
		return apperror.NewInternal("failed to generate download URL", err)
	}

	return c.Redirect(http.StatusFound, url)
}
