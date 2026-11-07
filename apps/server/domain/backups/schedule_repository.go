package backups

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/uptrace/bun"
)

// GetScheduleByProject returns the project's backup schedule, or nil when the
// project has none configured.
func (r *Repository) GetScheduleByProject(ctx context.Context, projectID string) (*BackupSchedule, error) {
	var schedule BackupSchedule
	err := r.db.NewSelect().
		Model(&schedule).
		Where("project_id = ?", projectID).
		Scan(ctx)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		r.log.Error("failed to get backup schedule",
			slog.String("project_id", projectID),
			slog.Any("error", err),
		)
		return nil, fmt.Errorf("get backup schedule: %w", err)
	}
	return &schedule, nil
}

// UpsertSchedule creates or updates the project's single schedule. Row locking
// serializes concurrent saves for one project without relying on an ON CONFLICT
// clause whose target-table references are easy to get wrong.
func (r *Repository) UpsertSchedule(ctx context.Context, schedule *BackupSchedule) error {
	schedule.UpdatedAt = time.Now().UTC()

	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var existing BackupSchedule
		err := tx.NewSelect().
			Model(&existing).
			Where("project_id = ?", schedule.ProjectID).
			For("UPDATE").
			Scan(ctx)

		switch {
		case err == sql.ErrNoRows:
			if _, err := tx.NewInsert().
				Model(schedule).
				Returning("id, created_at").
				Exec(ctx); err != nil {
				return fmt.Errorf("insert backup schedule: %w", err)
			}
			return nil
		case err != nil:
			return fmt.Errorf("lock backup schedule: %w", err)
		}

		// Preserve identity and runner bookkeeping the caller does not own.
		schedule.ID = existing.ID
		schedule.CreatedAt = existing.CreatedAt
		schedule.LastRunAt = existing.LastRunAt
		schedule.LastBackupID = existing.LastBackupID
		if schedule.CreatedBy == nil {
			schedule.CreatedBy = existing.CreatedBy
		}

		if _, err := tx.NewUpdate().
			Model(schedule).
			Column(
				"enabled", "cron_schedule", "timezone", "retention_days",
				"include_chat", "include_journal", "include_deleted",
				"next_run_at", "created_by", "updated_at",
			).
			WherePK().
			Exec(ctx); err != nil {
			return fmt.Errorf("update backup schedule: %w", err)
		}
		return nil
	})
}

// DeleteSchedule removes the project's schedule. It reports whether a row was
// deleted so the caller can answer 404 for an absent schedule.
func (r *Repository) DeleteSchedule(ctx context.Context, projectID string) (bool, error) {
	res, err := r.db.NewDelete().
		Model((*BackupSchedule)(nil)).
		Where("project_id = ?", projectID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to delete backup schedule",
			slog.String("project_id", projectID),
			slog.Any("error", err),
		)
		return false, fmt.Errorf("delete backup schedule: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete backup schedule rows affected: %w", err)
	}
	return n > 0, nil
}

// ClaimDueSchedules selects up to limit enabled schedules that are due at now,
// advances each one to its next fire, and returns them. Row locks
// (FOR UPDATE SKIP LOCKED) make the claim safe across API replicas: a second
// process skips rows the first has claimed instead of starting the same backup
// twice. next_run_at is recomputed from now rather than from the previous slot,
// so a process that was down for several fires produces one catch-up run instead
// of a burst.
func (r *Repository) ClaimDueSchedules(ctx context.Context, now time.Time, limit int) ([]BackupSchedule, error) {
	if limit <= 0 {
		return nil, nil
	}

	var claimed []BackupSchedule
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var due []BackupSchedule
		if err := tx.NewSelect().
			Model(&due).
			Where("enabled").
			Where("next_run_at <= ?", now).
			Order("next_run_at ASC").
			Limit(limit).
			For("UPDATE SKIP LOCKED").
			Scan(ctx); err != nil {
			return fmt.Errorf("select due backup schedules: %w", err)
		}

		for i := range due {
			schedule := &due[i]

			next, err := NextBackupRun(schedule.CronSchedule, now)
			if err != nil {
				// A row that cannot be scheduled any more must not be retried on
				// every tick: disable it and leave the reason in the log.
				schedule.Enabled = false
				schedule.UpdatedAt = now
				r.log.Warn("disabling backup schedule with invalid cron expression",
					slog.String("schedule_id", schedule.ID),
					slog.String("project_id", schedule.ProjectID),
					slog.String("cron", schedule.CronSchedule),
					slog.String("error", err.Error()),
				)
				if _, uerr := tx.NewUpdate().
					Model(schedule).
					Column("enabled", "updated_at").
					WherePK().
					Exec(ctx); uerr != nil {
					return fmt.Errorf("disable backup schedule %s: %w", schedule.ID, uerr)
				}
				continue
			}

			runAt := now
			schedule.NextRunAt = next
			schedule.LastRunAt = &runAt
			schedule.UpdatedAt = now
			if _, err := tx.NewUpdate().
				Model(schedule).
				Column("next_run_at", "last_run_at", "updated_at").
				WherePK().
				Exec(ctx); err != nil {
				return fmt.Errorf("advance backup schedule %s: %w", schedule.ID, err)
			}

			claimed = append(claimed, *schedule)
		}
		return nil
	})
	if err != nil {
		r.log.Error("failed to claim due backup schedules", slog.Any("error", err))
		return nil, err
	}
	return claimed, nil
}

// SetLastBackup records the backup a schedule most recently started.
func (r *Repository) SetLastBackup(ctx context.Context, scheduleID, backupID string) error {
	_, err := r.db.NewUpdate().
		Model((*BackupSchedule)(nil)).
		Set("last_backup_id = ?", backupID).
		Where("id = ?", scheduleID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to record schedule backup",
			slog.String("schedule_id", scheduleID),
			slog.String("backup_id", backupID),
			slog.Any("error", err),
		)
		return fmt.Errorf("record schedule backup: %w", err)
	}
	return nil
}

// ProjectHasCreatingBackup reports whether the project already has a backup in
// progress. The runner skips a due fire while one is outstanding so a slow
// archive cannot pile up behind the next schedule slot.
func (r *Repository) ProjectHasCreatingBackup(ctx context.Context, projectID string) (bool, error) {
	exists, err := r.db.NewSelect().
		Model((*Backup)(nil)).
		Where("project_id = ?", projectID).
		Where("status = ?", BackupStatusCreating).
		Where("deleted_at IS NULL").
		Exists(ctx)
	if err != nil {
		r.log.Error("failed to check for in-progress backups",
			slog.String("project_id", projectID),
			slog.Any("error", err),
		)
		return false, fmt.Errorf("check in-progress backups: %w", err)
	}
	return exists, nil
}
