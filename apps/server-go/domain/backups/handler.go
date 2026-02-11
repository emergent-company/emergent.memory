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

func (h *Handler) RestoreBackup(c echo.Context) error {
	return c.JSON(http.StatusNotImplemented, map[string]any{
		"message": "restore functionality coming in next phase",
	})
}

func (h *Handler) GetRestoreStatus(c echo.Context) error {
	return c.JSON(http.StatusNotImplemented, map[string]any{
		"message": "restore functionality coming in next phase",
	})
}
