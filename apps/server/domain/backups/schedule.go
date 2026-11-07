package backups

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/uptrace/bun"
)

const (
	// BackupScheduleMinIntervalMinutes is the smallest gap the scheduler accepts
	// between two fires of a backup schedule. A project backup walks the whole
	// exported surface and writes an archive to object storage, so a schedule
	// that fires more often than this is a mistake rather than an intent.
	BackupScheduleMinIntervalMinutes = 15

	// BackupScheduleTimezone is the single timezone schedules are evaluated in.
	// The stored `timezone` column exists so the wire contract can carry the
	// value the UI renders; evaluation stays UTC until multi-timezone support
	// is a real requirement.
	BackupScheduleTimezone = "UTC"

	// BackupScheduleDefaultRetentionDays matches the manual-create default.
	BackupScheduleDefaultRetentionDays = 30

	// backupCronIntervalSamples is how many consecutive fires ValidateBackupCron
	// compares. A cron expression's minute/hour pattern repeats within a day, so
	// the shortest gap always appears in the first few fires; sampling a hundred
	// covers more than four days of an hourly schedule and catches composite
	// minute sets ("5,55 0,1 * * *" has a 50-minute gap followed by a 10-minute
	// one) that a single first-gap check misses.
	backupCronIntervalSamples = 100
)

// BackupSchedule is a project's recurring backup configuration
// (kb.backup_schedules). There is at most one per project.
type BackupSchedule struct {
	bun.BaseModel `bun:"table:kb.backup_schedules,alias:bs"`

	ID             string `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	OrganizationID string `bun:"organization_id,notnull,type:uuid" json:"organizationId"`
	ProjectID      string `bun:"project_id,notnull,type:uuid" json:"projectId"`

	Enabled      bool   `bun:"enabled,notnull,default:true" json:"enabled"`
	CronSchedule string `bun:"cron_schedule,notnull" json:"cronSchedule"`
	Timezone     string `bun:"timezone,notnull,default:'UTC'" json:"timezone"`

	RetentionDays  int  `bun:"retention_days,notnull,default:30" json:"retentionDays"`
	IncludeChat    bool `bun:"include_chat,notnull,default:false" json:"includeChat"`
	IncludeJournal bool `bun:"include_journal,notnull,default:false" json:"includeJournal"`
	IncludeDeleted bool `bun:"include_deleted,notnull,default:false" json:"includeDeleted"`

	NextRunAt    time.Time  `bun:"next_run_at,notnull" json:"nextRunAt"`
	LastRunAt    *time.Time `bun:"last_run_at" json:"lastRunAt,omitempty"`
	LastBackupID *string    `bun:"last_backup_id,type:uuid" json:"lastBackupId,omitempty"`

	CreatedBy *string   `bun:"created_by,type:uuid" json:"createdBy,omitempty"`
	CreatedAt time.Time `bun:"created_at,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt time.Time `bun:"updated_at,notnull,default:current_timestamp" json:"updatedAt"`
}

// backupCronParser parses standard 5-field cron expressions. The seconds field
// is deliberately absent: schedule authors write "0 2 * * *", not six fields,
// and the scheduler's own six-field dialect stays an internal implementation
// detail of the tick task.
var backupCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// ParseBackupCron parses a 5-field cron expression pinned to UTC.
func ParseBackupCron(expr string) (cron.Schedule, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("cron schedule is required")
	}
	if fields := strings.Fields(expr); len(fields) != 5 {
		return nil, fmt.Errorf("cron schedule %q must have 5 fields (minute hour day-of-month month day-of-week)", expr)
	}
	// CRON_TZ pins the schedule's location so Next() does not depend on the
	// server's local timezone.
	schedule, err := backupCronParser.Parse("CRON_TZ=" + BackupScheduleTimezone + " " + expr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron schedule %q: %w", expr, err)
	}
	return schedule, nil
}

// NextBackupRun returns the first fire strictly after from, in UTC.
func NextBackupRun(expr string, from time.Time) (time.Time, error) {
	schedule, err := ParseBackupCron(expr)
	if err != nil {
		return time.Time{}, err
	}
	return schedule.Next(from.UTC()), nil
}

// ValidateBackupCron validates the expression and enforces the minimum
// interval between consecutive fires. Every gap in the first
// backupCronIntervalSamples fires is checked, not just the first: composite
// field sets can hide a short gap behind a long one (see the constant above).
func ValidateBackupCron(expr string, from time.Time) error {
	schedule, err := ParseBackupCron(expr)
	if err != nil {
		return err
	}

	prev := schedule.Next(from.UTC())
	for i := 0; i < backupCronIntervalSamples; i++ {
		next := schedule.Next(prev)
		if gap := next.Sub(prev); gap < BackupScheduleMinIntervalMinutes*time.Minute {
			return fmt.Errorf("cron schedule %q fires every %s; the minimum interval is %d minutes", expr, gap, BackupScheduleMinIntervalMinutes)
		}
		prev = next
	}
	return nil
}
