package backups

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// ErrInvalidSchedule marks a schedule write the caller can fix (bad cron
// expression, out-of-range retention). Handlers translate it to 400.
var ErrInvalidSchedule = errors.New("invalid backup schedule")

// ScheduleParams is a validated schedule write.
type ScheduleParams struct {
	Enabled        bool
	CronSchedule   string
	RetentionDays  int
	IncludeChat    bool
	IncludeJournal bool
	IncludeDeleted bool
}

// GetSchedule returns the project's schedule, or nil when none is configured.
func (s *Service) GetSchedule(ctx context.Context, projectID string) (*BackupSchedule, error) {
	schedule, err := s.repo.GetScheduleByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get backup schedule: %w", err)
	}
	return schedule, nil
}

// UpsertSchedule creates or replaces the project's schedule. The next fire is
// always computed from now, so an edit that changes the cadence takes effect
// immediately instead of waiting out the old expression.
func (s *Service) UpsertSchedule(ctx context.Context, projectID, orgID, userID string, params ScheduleParams) (*BackupSchedule, error) {
	now := time.Now().UTC()

	if err := ValidateBackupCron(params.CronSchedule, now); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSchedule, err)
	}

	retentionDays := params.RetentionDays
	if retentionDays == 0 {
		retentionDays = BackupScheduleDefaultRetentionDays
	}
	if retentionDays < 1 || retentionDays > 365 {
		return nil, fmt.Errorf("%w: retention days must be between 1 and 365", ErrInvalidSchedule)
	}

	next, err := NextBackupRun(params.CronSchedule, now)
	if err != nil {
		// ValidateBackupCron already parsed the same expression; this is a
		// defensive conversion of an error the caller cannot reach.
		return nil, fmt.Errorf("%w: %v", ErrInvalidSchedule, err)
	}

	schedule := &BackupSchedule{
		OrganizationID: orgID,
		ProjectID:      projectID,
		Enabled:        params.Enabled,
		CronSchedule:   params.CronSchedule,
		Timezone:       BackupScheduleTimezone,
		RetentionDays:  retentionDays,
		IncludeChat:    params.IncludeChat,
		IncludeJournal: params.IncludeJournal,
		IncludeDeleted: params.IncludeDeleted,
		NextRunAt:      next,
	}
	if userID != "" {
		schedule.CreatedBy = &userID
	}

	if err := s.repo.UpsertSchedule(ctx, schedule); err != nil {
		return nil, err
	}

	s.log.Info("backup schedule saved",
		slog.String("project_id", projectID),
		slog.Bool("enabled", schedule.Enabled),
		slog.String("cron", schedule.CronSchedule),
		slog.Time("next_run_at", schedule.NextRunAt),
	)
	return schedule, nil
}

// DeleteSchedule removes the project's schedule. It reports whether one existed.
func (s *Service) DeleteSchedule(ctx context.Context, projectID string) (bool, error) {
	deleted, err := s.repo.DeleteSchedule(ctx, projectID)
	if err != nil {
		return false, err
	}
	if deleted {
		s.log.Info("backup schedule deleted", slog.String("project_id", projectID))
	}
	return deleted, nil
}
