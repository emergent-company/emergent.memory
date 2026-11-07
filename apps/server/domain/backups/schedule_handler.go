package backups

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/labstack/echo/v4"
)

// BackupScheduleRequestDTO is the request body for creating or replacing a
// project's backup schedule. cronSchedule is a 5-field expression evaluated in
// UTC; retentionDays defaults to 30 and must be 1–365.
type BackupScheduleRequestDTO struct {
	Enabled        *bool  `json:"enabled"`
	CronSchedule   string `json:"cronSchedule"`
	RetentionDays  int    `json:"retentionDays"`
	IncludeChat    bool   `json:"includeChat"`
	IncludeJournal bool   `json:"includeJournal"`
	IncludeDeleted bool   `json:"includeDeleted"`
}

// GetBackupSchedule returns the project's recurring backup configuration.
// @Summary      Get project backup schedule
// @Description  Returns the project's recurring backup configuration, or 404 when no schedule is configured.
// @Tags         backups
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 {object} BackupSchedule "Backup schedule"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Not a project admin or org admin"
// @Failure      404 {object} apperror.Error "No schedule configured"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/projects/{projectId}/backup-schedule [get]
// @Security     bearerAuth
func (h *Handler) GetBackupSchedule(c echo.Context) error {
	projectID, _, err := h.requireProjectBackupAuthority(c)
	if err != nil {
		return err
	}

	schedule, err := h.service.GetSchedule(c.Request().Context(), projectID)
	if err != nil {
		return apperror.NewInternal("failed to load backup schedule", err)
	}
	if schedule == nil {
		return apperror.NewNotFound("backup schedule", projectID)
	}
	return c.JSON(http.StatusOK, schedule)
}

// PutBackupSchedule creates or replaces the project's recurring backup
// configuration.
// @Summary      Create or replace project backup schedule
// @Description  Creates or replaces the project's one recurring backup schedule. cronSchedule is a standard 5-field cron expression evaluated in UTC (minimum interval 15 minutes); retentionDays defaults to 30 and must be 1-365.
// @Tags         backups
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        body body BackupScheduleRequestDTO true "Schedule configuration"
// @Success      200 {object} BackupSchedule "Saved schedule"
// @Failure      400 {object} apperror.Error "Invalid cron expression or retention"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Not a project admin or org admin"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/projects/{projectId}/backup-schedule [put]
// @Security     bearerAuth
func (h *Handler) PutBackupSchedule(c echo.Context) error {
	projectID, orgID, err := h.requireProjectBackupAuthority(c)
	if err != nil {
		return err
	}
	user := auth.MustGetUser(c)

	var req BackupScheduleRequestDTO
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid backup schedule body")
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	schedule, err := h.service.UpsertSchedule(c.Request().Context(), projectID, orgID, user.ID, ScheduleParams{
		Enabled:        enabled,
		CronSchedule:   req.CronSchedule,
		RetentionDays:  req.RetentionDays,
		IncludeChat:    req.IncludeChat,
		IncludeJournal: req.IncludeJournal,
		IncludeDeleted: req.IncludeDeleted,
	})
	if err != nil {
		if errors.Is(err, ErrInvalidSchedule) {
			return apperror.NewBadRequest(err.Error())
		}
		h.log.Error("failed to save backup schedule",
			slog.String("project_id", projectID),
			slog.Any("error", err),
		)
		return apperror.NewInternal("failed to save backup schedule", err)
	}
	return c.JSON(http.StatusOK, schedule)
}

// DeleteBackupSchedule removes the project's recurring backup configuration.
// @Summary      Delete project backup schedule
// @Description  Removes the project's recurring backup configuration. Returns 404 when no schedule is configured.
// @Tags         backups
// @Param        projectId path string true "Project ID (UUID)"
// @Success      204 "Schedule deleted"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Not a project admin or org admin"
// @Failure      404 {object} apperror.Error "No schedule configured"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/projects/{projectId}/backup-schedule [delete]
// @Security     bearerAuth
func (h *Handler) DeleteBackupSchedule(c echo.Context) error {
	projectID, _, err := h.requireProjectBackupAuthority(c)
	if err != nil {
		return err
	}

	deleted, err := h.service.DeleteSchedule(c.Request().Context(), projectID)
	if err != nil {
		return apperror.NewInternal("failed to delete backup schedule", err)
	}
	if !deleted {
		return apperror.NewNotFound("backup schedule", projectID)
	}
	return c.NoContent(http.StatusNoContent)
}
