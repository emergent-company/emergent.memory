package backups

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/emergent-company/emergent.memory/domain/scheduler"
)

const (
	// scheduleRunnerTickInterval is how often the runner looks for due
	// schedules. A one-minute tick bounds lateness to a minute without keeping
	// per-schedule cron entries in memory (which would need reconciling on every
	// edit and would double-run under API replicas).
	scheduleRunnerTickInterval = time.Minute

	// scheduleRunnerClaimBatch caps how many projects one tick starts. A stamp
	// backlog is drained over successive ticks instead of in one burst.
	scheduleRunnerClaimBatch = 25

	// scheduleTaskName and retentionTaskName are the scheduler entry names.
	scheduleTaskName  = "backups:project_schedules"
	retentionTaskName = "backups:retention"

	// retentionCron fires the expiry sweep daily at 03:30 server-local time,
	// clear of the whole-database backup window.
	retentionCron = "0 30 3 * * *"

	// retentionSoftDeleteGraceDays is how long an expired backup stays
	// recoverable after the sweep marks it deleted, before the archive object
	// and row are purged. This is what makes the sweep safe to ship: an expiry
	// mistake costs a support ticket, not data.
	retentionSoftDeleteGraceDays = 14

	// retentionMaxPerRun bounds one sweep so a large backlog cannot monopolise
	// the scheduler.
	retentionMaxPerRun = 200
)

// ScheduleRunner starts a backup for every project whose schedule is due. It
// owns no per-schedule state: due-ness lives in kb.backup_schedules and is
// claimed transactionally, so a restart, an edit or a second API replica cannot
// double-fire or lose a schedule.
type ScheduleRunner struct {
	service *Service
	log     *slog.Logger
}

// NewScheduleRunner creates a ScheduleRunner.
func NewScheduleRunner(service *Service, log *slog.Logger) *ScheduleRunner {
	return &ScheduleRunner{
		service: service,
		log:     log.With(slog.String("component", "backups.schedule_runner")),
	}
}

// Run claims due schedules and starts one backup per project.
func (r *ScheduleRunner) Run(ctx context.Context) error {
	if !r.service.storage.Enabled() {
		// Object storage is disabled: a scheduled backup could only fail. Stay
		// quiet (debug) rather than logging an error every minute.
		r.log.Debug("object storage disabled; skipping scheduled backups")
		return nil
	}

	now := time.Now().UTC()
	due, err := r.service.repo.ClaimDueSchedules(ctx, now, scheduleRunnerClaimBatch)
	if err != nil {
		return fmt.Errorf("claim due backup schedules: %w", err)
	}
	if len(due) == 0 {
		return nil
	}

	for i := range due {
		schedule := &due[i]

		creating, err := r.service.repo.ProjectHasCreatingBackup(ctx, schedule.ProjectID)
		if err != nil {
			r.log.Error("failed to check for in-progress backup; skipping scheduled backup",
				slog.String("project_id", schedule.ProjectID),
				slog.String("schedule_id", schedule.ID),
				slog.String("error", err.Error()),
			)
			continue
		}
		if creating {
			r.log.Info("skipping scheduled backup: a backup is already in progress",
				slog.String("project_id", schedule.ProjectID),
				slog.String("schedule_id", schedule.ID),
			)
			continue
		}

		backup, err := r.service.CreateBackup(ctx, CreateBackupRequest{
			ProjectID:      schedule.ProjectID,
			OrganizationID: schedule.OrganizationID,
			CreatedBy:      schedule.CreatedBy,
			IncludeChat:    schedule.IncludeChat,
			IncludeJournal: schedule.IncludeJournal,
			IncludeDeleted: schedule.IncludeDeleted,
			RetentionDays:  schedule.RetentionDays,
		})
		if err != nil {
			r.log.Error("failed to start scheduled backup",
				slog.String("project_id", schedule.ProjectID),
				slog.String("schedule_id", schedule.ID),
				slog.String("error", err.Error()),
			)
			continue
		}

		if err := r.service.repo.SetLastBackup(ctx, schedule.ID, backup.ID); err != nil {
			// The backup itself is running; a bookkeeping miss only costs the
			// "last backup" link in the UI.
			r.log.Warn("failed to record scheduled backup id",
				slog.String("schedule_id", schedule.ID),
				slog.String("backup_id", backup.ID),
				slog.String("error", err.Error()),
			)
		}

		r.log.Info("scheduled backup started",
			slog.String("project_id", schedule.ProjectID),
			slog.String("schedule_id", schedule.ID),
			slog.String("backup_id", backup.ID),
			slog.String("cron", schedule.CronSchedule),
		)
	}

	return nil
}

// RetentionTask enforces backup expiry in two stages: expired ready backups are
// soft-deleted (still downloadable by an admin via the row, but hidden from the
// list), and soft-deleted backups older than the grace window are purged with
// their archive object. Expiry is written by CreateBackup from retention_days,
// so a schedule's retention only becomes real once this sweep runs.
type RetentionTask struct {
	service *Service
	log     *slog.Logger
}

// NewRetentionTask creates a RetentionTask.
func NewRetentionTask(service *Service, log *slog.Logger) *RetentionTask {
	return &RetentionTask{
		service: service,
		log:     log.With(slog.String("component", "backups.retention")),
	}
}

// Run expires due backups and purges archives past the grace window.
func (t *RetentionTask) Run(ctx context.Context) error {
	now := time.Now().UTC()

	expired, err := t.service.repo.GetExpiredBackups(ctx)
	if err != nil {
		return fmt.Errorf("list expired backups: %w", err)
	}
	if len(expired) > retentionMaxPerRun {
		expired = expired[:retentionMaxPerRun]
	}
	for i := range expired {
		backup := &expired[i]
		if err := t.service.repo.SoftDelete(ctx, backup.OrganizationID, backup.ID); err != nil {
			t.log.Warn("failed to expire backup",
				slog.String("backup_id", backup.ID),
				slog.String("error", err.Error()),
			)
			continue
		}
		t.log.Info("backup expired",
			slog.String("backup_id", backup.ID),
			slog.String("project_id", backup.ProjectID),
			slog.Time("expires_at", derefTime(backup.ExpiresAt)),
		)
	}

	cutoff := now.AddDate(0, 0, -retentionSoftDeleteGraceDays)
	purgeable, err := t.service.repo.GetSoftDeletedBackups(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("list purgeable backups: %w", err)
	}
	if len(purgeable) > retentionMaxPerRun {
		purgeable = purgeable[:retentionMaxPerRun]
	}
	for i := range purgeable {
		backup := &purgeable[i]
		if backup.StorageKey != "" {
			if err := t.service.storage.Delete(ctx, backup.StorageKey); err != nil {
				t.log.Warn("failed to delete backup archive; keeping the row for a later retry",
					slog.String("backup_id", backup.ID),
					slog.String("storage_key", backup.StorageKey),
					slog.String("error", err.Error()),
				)
				continue
			}
		}
		if err := t.service.repo.HardDelete(ctx, backup.ID); err != nil {
			t.log.Warn("failed to purge backup row",
				slog.String("backup_id", backup.ID),
				slog.String("error", err.Error()),
			)
			continue
		}
		t.log.Info("backup purged", slog.String("backup_id", backup.ID))
	}

	return nil
}

// derefTime formats an optional time for logging.
func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// RegisterScheduledBackupTasks wires the schedule runner and the retention sweep
// into the shared scheduler. Invoked from the backups fx module, so neither
// exists when the backups feature is disabled.
func RegisterScheduledBackupTasks(sched *scheduler.Scheduler, runner *ScheduleRunner, retention *RetentionTask, log *slog.Logger) {
	if err := sched.AddIntervalTask(scheduleTaskName, scheduleRunnerTickInterval, runner.Run); err != nil {
		log.Error("failed to register scheduled project backups", slog.String("error", err.Error()))
	}
	if err := sched.AddCronTask(retentionTaskName, retentionCron, retention.Run); err != nil {
		log.Error("failed to register backup retention sweep", slog.String("error", err.Error()))
	}
}
