package backups

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// scheduleFixture seeds one org + project for schedule tests.
func scheduleFixture(t *testing.T, ctx context.Context) (projectID, orgID string, repo *Repository) {
	t.Helper()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "bkupsched")
	t.Cleanup(testDB.Close)

	orgID = uuid.NewString()
	if err := testutil.CreateTestOrganization(ctx, testDB.DB, orgID, "Schedule Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := testutil.CreateTestUser(ctx, testDB.DB, testutil.AdminUser); err != nil {
		t.Fatalf("create user: %v", err)
	}
	projectID = uuid.NewString()
	if err := testutil.CreateTestProject(ctx, testDB.DB, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Schedule Project",
	}, testutil.AdminUser.ID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return projectID, orgID, NewRepository(testDB.DB, slog.Default())
}

func TestScheduleUpsertCreateThenUpdate(t *testing.T) {
	ctx := context.Background()
	projectID, orgID, repo := scheduleFixture(t, ctx)

	next := time.Now().UTC().Add(time.Hour)
	created := &BackupSchedule{
		OrganizationID: orgID,
		ProjectID:      projectID,
		Enabled:        true,
		CronSchedule:   "0 2 * * *",
		Timezone:       BackupScheduleTimezone,
		RetentionDays:  30,
		NextRunAt:      next,
	}
	if err := repo.UpsertSchedule(ctx, created); err != nil {
		t.Fatalf("insert schedule: %v", err)
	}
	if created.ID == "" {
		t.Fatal("insert did not return an id")
	}
	if created.CreatedAt.IsZero() {
		t.Fatal("insert did not return created_at")
	}

	got, err := repo.GetScheduleByProject(ctx, projectID)
	if err != nil {
		t.Fatalf("get schedule: %v", err)
	}
	if got == nil {
		t.Fatal("schedule not found after insert")
	}
	if got.CronSchedule != "0 2 * * *" || !got.Enabled || got.RetentionDays != 30 {
		t.Fatalf("unexpected schedule round-trip: %+v", got)
	}

	// A second save is an update of the same row, not a duplicate.
	later := next.Add(24 * time.Hour)
	updated := &BackupSchedule{
		OrganizationID: orgID,
		ProjectID:      projectID,
		Enabled:        false,
		CronSchedule:   "0 4 * * *",
		Timezone:       BackupScheduleTimezone,
		RetentionDays:  7,
		IncludeChat:    true,
		NextRunAt:      later,
	}
	if err := repo.UpsertSchedule(ctx, updated); err != nil {
		t.Fatalf("update schedule: %v", err)
	}
	if updated.ID != created.ID {
		t.Fatalf("update produced a new id: %s != %s", updated.ID, created.ID)
	}

	got, err = repo.GetScheduleByProject(ctx, projectID)
	if err != nil {
		t.Fatalf("get schedule after update: %v", err)
	}
	if got.CronSchedule != "0 4 * * *" || got.Enabled || got.RetentionDays != 7 || !got.IncludeChat {
		t.Fatalf("unexpected updated schedule: %+v", got)
	}
	if !got.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("created_at changed on update: %s -> %s", created.CreatedAt, got.CreatedAt)
	}
}

func TestScheduleClaimIsExclusiveAndAdvances(t *testing.T) {
	ctx := context.Background()
	projectID, orgID, repo := scheduleFixture(t, ctx)

	now := time.Now().UTC()
	// Seed a schedule that is already overdue so the claim fires it.
	schedule := &BackupSchedule{
		OrganizationID: orgID,
		ProjectID:      projectID,
		Enabled:        true,
		CronSchedule:   "0 2 * * *",
		Timezone:       BackupScheduleTimezone,
		RetentionDays:  30,
		NextRunAt:      now.Add(-48 * time.Hour),
	}
	if err := repo.UpsertSchedule(ctx, schedule); err != nil {
		t.Fatalf("insert schedule: %v", err)
	}

	claimed, err := repo.ClaimDueSchedules(ctx, now, 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed %d schedules, want 1", len(claimed))
	}
	if claimed[0].LastRunAt == nil {
		t.Fatal("claim did not stamp last_run_at")
	}
	if !claimed[0].NextRunAt.After(now) {
		t.Fatalf("claim left next_run_at in the past: %s", claimed[0].NextRunAt)
	}
	if gap := claimed[0].NextRunAt.Sub(now); gap > 25*time.Hour {
		t.Fatalf("catch-up should pick the next slot, got %s ahead", gap)
	}

	// The same tick repeated (or a second replica) must not re-claim the row.
	again, err := repo.ClaimDueSchedules(ctx, now, 10)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second claim returned %d schedules, want 0", len(again))
	}

	// A disabled schedule is never claimed.
	got, err := repo.GetScheduleByProject(ctx, projectID)
	if err != nil {
		t.Fatalf("get schedule: %v", err)
	}
	got.Enabled = false
	got.NextRunAt = now.Add(-time.Hour)
	if err := repo.UpsertSchedule(ctx, got); err != nil {
		t.Fatalf("disable schedule: %v", err)
	}
	disabled, err := repo.ClaimDueSchedules(ctx, now, 10)
	if err != nil {
		t.Fatalf("claim disabled: %v", err)
	}
	if len(disabled) != 0 {
		t.Fatalf("claimed %d disabled schedules, want 0", len(disabled))
	}
}

func TestScheduleClaimDisablesInvalidCron(t *testing.T) {
	ctx := context.Background()
	projectID, orgID, repo := scheduleFixture(t, ctx)

	now := time.Now().UTC()
	if _, err := repo.db.NewInsert().Model(&BackupSchedule{
		OrganizationID: orgID,
		ProjectID:      projectID,
		Enabled:        true,
		CronSchedule:   "definitely not cron", // bypasses the service validator on purpose
		Timezone:       BackupScheduleTimezone,
		RetentionDays:  30,
		NextRunAt:      now.Add(-time.Minute),
	}).Exec(ctx); err != nil {
		t.Fatalf("insert schedule: %v", err)
	}

	claimed, err := repo.ClaimDueSchedules(ctx, now, 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed %d schedules with an invalid cron, want 0", len(claimed))
	}

	got, err := repo.GetScheduleByProject(ctx, projectID)
	if err != nil {
		t.Fatalf("get schedule: %v", err)
	}
	if got.Enabled {
		t.Fatal("invalid schedule was not disabled")
	}
}

func TestScheduleDelete(t *testing.T) {
	ctx := context.Background()
	projectID, orgID, repo := scheduleFixture(t, ctx)

	if err := repo.UpsertSchedule(ctx, &BackupSchedule{
		OrganizationID: orgID,
		ProjectID:      projectID,
		Enabled:        true,
		CronSchedule:   "0 2 * * *",
		Timezone:       BackupScheduleTimezone,
		RetentionDays:  30,
		NextRunAt:      time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatalf("insert schedule: %v", err)
	}

	deleted, err := repo.DeleteSchedule(ctx, projectID)
	if err != nil {
		t.Fatalf("delete schedule: %v", err)
	}
	if !deleted {
		t.Fatal("delete reported no row")
	}
	deleted, err = repo.DeleteSchedule(ctx, projectID)
	if err != nil {
		t.Fatalf("second delete schedule: %v", err)
	}
	if deleted {
		t.Fatal("second delete reported a row")
	}
}

func TestProjectHasCreatingBackup(t *testing.T) {
	ctx := context.Background()
	projectID, orgID, repo := scheduleFixture(t, ctx)

	busy, err := repo.ProjectHasCreatingBackup(ctx, projectID)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if busy {
		t.Fatal("project reported busy with no backups")
	}

	backup := &Backup{
		ID:             uuid.NewString(),
		OrganizationID: orgID,
		ProjectID:      projectID,
		ProjectName:    "Schedule Project",
		StorageKey:     "backups/" + orgID + "/" + uuid.NewString() + "/backup.zip",
		Status:         BackupStatusCreating,
		Includes:       map[string]any{},
	}
	if _, err := repo.db.NewInsert().Model(backup).Exec(ctx); err != nil {
		t.Fatalf("insert backup: %v", err)
	}

	busy, err = repo.ProjectHasCreatingBackup(ctx, projectID)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !busy {
		t.Fatal("project should report busy while a backup is creating")
	}

	if _, err := repo.db.NewUpdate().
		Model((*Backup)(nil)).
		Set("status = ?", BackupStatusReady).
		Where("id = ?", backup.ID).
		Exec(ctx); err != nil {
		t.Fatalf("finish backup: %v", err)
	}

	busy, err = repo.ProjectHasCreatingBackup(ctx, projectID)
	if err != nil {
		t.Fatalf("check after finish: %v", err)
	}
	if busy {
		t.Fatal("project should not report busy after the backup finished")
	}
}

func TestServiceUpsertScheduleValidates(t *testing.T) {
	ctx := context.Background()
	projectID, orgID, repo := scheduleFixture(t, ctx)
	service := NewService(repo, nil, nil, nil, nil, slog.Default())

	if _, err := service.UpsertSchedule(ctx, projectID, orgID, "", ScheduleParams{
		Enabled:      true,
		CronSchedule: "*/5 * * * *",
	}); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("sub-interval cron error = %v, want ErrInvalidSchedule", err)
	}

	if _, err := service.UpsertSchedule(ctx, projectID, orgID, "", ScheduleParams{
		Enabled:       true,
		CronSchedule:  "0 2 * * *",
		RetentionDays: 400,
	}); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("out-of-range retention error = %v, want ErrInvalidSchedule", err)
	}

	schedule, err := service.UpsertSchedule(ctx, projectID, orgID, testutil.AdminUser.ID, ScheduleParams{
		Enabled:      true,
		CronSchedule: "0 2 * * *",
	})
	if err != nil {
		t.Fatalf("upsert schedule: %v", err)
	}
	if schedule.RetentionDays != BackupScheduleDefaultRetentionDays {
		t.Fatalf("default retention = %d, want %d", schedule.RetentionDays, BackupScheduleDefaultRetentionDays)
	}
	if schedule.Timezone != BackupScheduleTimezone {
		t.Fatalf("timezone = %q, want %q", schedule.Timezone, BackupScheduleTimezone)
	}
	if schedule.NextRunAt.Before(time.Now()) {
		t.Fatalf("next_run_at is in the past: %s", schedule.NextRunAt)
	}
}
