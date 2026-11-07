package backups

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// fakeObjectStore is a minimal S3-compatible endpoint for the archive calls the
// retention sweep makes. It records DELETE requests and answers with a
// configurable status, so both the success path and the "storage is broken, keep
// the row" path are testable without a real bucket.
type fakeObjectStore struct {
	mu           sync.Mutex
	deleted      []string
	deleteStatus int
}

func (f *fakeObjectStore) Deletes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deleted...)
}

// newFakeObjectStore starts the endpoint and returns a storage.Service enabled
// against it (Enabled is what the schedule runner gates on).
func newFakeObjectStore(t *testing.T, deleteStatus int) (*fakeObjectStore, *storage.Service) {
	t.Helper()

	store := &fakeObjectStore{deleteStatus: deleteStatus}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			store.mu.Lock()
			store.deleted = append(store.deleted, r.URL.Path)
			store.mu.Unlock()
			w.WriteHeader(deleteStatus)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	svc, err := storage.NewService(&storage.Config{
		Endpoint:        srv.URL,
		AccessKey:       "test-key",
		SecretKey:       "test-secret",
		Region:          "us-east-1",
		BucketDocuments: "documents",
	}, testLogger())
	if err != nil {
		t.Fatalf("storage service: %v", err)
	}
	if !svc.Enabled() {
		t.Fatal("storage service is not enabled; the runner would skip everything")
	}
	return store, svc
}

// scheduleTasksFixture builds a Service over a real database and a fake object
// store, so the runner and the retention sweep can be exercised as they run in
// production (the same code path, not a reimplementation).
func scheduleTasksFixture(t *testing.T, ctx context.Context, deleteStatus int) (projectID, orgID string, repo *Repository, svc *Service, store *fakeObjectStore) {
	t.Helper()

	projectID, orgID, repo = scheduleFixture(t, ctx)
	store, storageSvc := newFakeObjectStore(t, deleteStatus)
	log := testLogger()
	creator := NewCreator(repo.db, storageSvc, repo, log)
	svc = NewService(repo, creator, nil, nil, storageSvc, log)

	return projectID, orgID, repo, svc, store
}

// seedSchedule inserts an enabled, overdue schedule so the next claim fires it.
func seedSchedule(t *testing.T, ctx context.Context, repo *Repository, projectID, orgID, cron string, now time.Time) *BackupSchedule {
	t.Helper()

	schedule := &BackupSchedule{
		OrganizationID: orgID,
		ProjectID:      projectID,
		Enabled:        true,
		CronSchedule:   cron,
		Timezone:       BackupScheduleTimezone,
		RetentionDays:  7,
		IncludeChat:    true,
		IncludeJournal: true,
		NextRunAt:      now.Add(-48 * time.Hour),
	}
	if err := repo.UpsertSchedule(ctx, schedule); err != nil {
		t.Fatalf("seed schedule: %v", err)
	}
	return schedule
}

// seedBackup inserts one backup row for the project in the given lifecycle state.
func seedBackup(t *testing.T, ctx context.Context, repo *Repository, projectID, orgID, status string, expiresAt time.Time, deletedAt *time.Time) *Backup {
	t.Helper()

	backup := &Backup{
		ID:             uuid.NewString(),
		OrganizationID: orgID,
		ProjectID:      projectID,
		ProjectName:    "Schedule Project",
		StorageKey:     "backups/" + orgID + "/" + uuid.NewString() + "/backup.zip",
		Status:         status,
		BackupType:     BackupTypeFull,
		Includes:       map[string]any{"documents": true},
		CreatedAt:      time.Now().UTC(),
		ExpiresAt:      &expiresAt,
		DeletedAt:      deletedAt,
	}
	if err := repo.Create(ctx, backup); err != nil {
		t.Fatalf("seed backup: %v", err)
	}
	return backup
}

// listProjectBackups returns the project's live (non-deleted) backups.
func listProjectBackups(t *testing.T, ctx context.Context, repo *Repository, orgID, projectID string) []Backup {
	t.Helper()

	result, err := repo.List(ctx, ListParams{
		OrganizationID: orgID,
		ProjectID:      &projectID,
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	return result.Backups
}

// TestScheduleRunnerStartsBackupAndRecordsIt is the end-to-end tick: a due
// schedule starts a backup with the schedule's includes and retention, and the
// schedule records which backup it started.
func TestScheduleRunnerStartsBackupAndRecordsIt(t *testing.T) {
	ctx := context.Background()
	projectID, orgID, repo, svc, _ := scheduleTasksFixture(t, ctx, http.StatusNoContent)

	now := time.Now().UTC()
	schedule := seedSchedule(t, ctx, repo, projectID, orgID, "0 2 * * *", now)
	schedule.CreatedBy = &testutil.AdminUser.ID
	if err := repo.UpsertSchedule(ctx, schedule); err != nil {
		t.Fatalf("record created_by: %v", err)
	}

	runner := NewScheduleRunner(svc, testLogger())
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("runner tick: %v", err)
	}

	backups := listProjectBackups(t, ctx, repo, orgID, projectID)
	if len(backups) != 1 {
		t.Fatalf("created %d backups, want exactly 1", len(backups))
	}
	created := backups[0]
	if created.Includes["chat"] != true || created.Includes["journal"] != true {
		t.Errorf("backup includes = %+v, want chat+journal from the schedule", created.Includes)
	}
	if created.CreatedBy == nil || *created.CreatedBy != testutil.AdminUser.ID {
		t.Errorf("created_by = %v, want the user who saved the schedule", created.CreatedBy)
	}
	if created.ExpiresAt == nil {
		t.Fatal("backup has no expires_at; retention_days was not applied")
	}
	if got := created.ExpiresAt.Sub(created.CreatedAt); got < 6*24*time.Hour || got > 8*24*time.Hour {
		t.Errorf("expires_at is %s after creation, want ~7 days from retention_days", got)
	}

	updated, err := repo.GetScheduleByProject(ctx, projectID)
	if err != nil {
		t.Fatalf("get schedule: %v", err)
	}
	if updated.LastBackupID == nil || *updated.LastBackupID != created.ID {
		t.Errorf("last_backup_id = %v, want %s", updated.LastBackupID, created.ID)
	}
	if updated.LastRunAt == nil {
		t.Error("last_run_at was not stamped when the backup started")
	}
	if !updated.NextRunAt.After(now) {
		t.Errorf("next_run_at = %s, want a future fire", updated.NextRunAt)
	}
}

// TestScheduleRunnerSkipsWhileBackupInProgress is the pile-up guard: a fire
// claimed while the project already has a creating backup starts nothing, and
// the schedule still advances so no backlog accumulates.
func TestScheduleRunnerSkipsWhileBackupInProgress(t *testing.T) {
	ctx := context.Background()
	projectID, orgID, repo, svc, _ := scheduleTasksFixture(t, ctx, http.StatusNoContent)

	now := time.Now().UTC()
	seedSchedule(t, ctx, repo, projectID, orgID, "0 2 * * *", now)
	inProgress := seedBackup(t, ctx, repo, projectID, orgID, BackupStatusCreating, now.Add(24*time.Hour), nil)

	runner := NewScheduleRunner(svc, testLogger())
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("runner tick: %v", err)
	}

	backups := listProjectBackups(t, ctx, repo, orgID, projectID)
	if len(backups) != 1 || backups[0].ID != inProgress.ID {
		t.Fatalf("backups = %+v, want only the in-progress one", backups)
	}

	updated, err := repo.GetScheduleByProject(ctx, projectID)
	if err != nil {
		t.Fatalf("get schedule: %v", err)
	}
	if !updated.NextRunAt.After(now) {
		t.Errorf("next_run_at = %s, a skipped fire must still advance", updated.NextRunAt)
	}
	if updated.LastBackupID != nil {
		t.Errorf("last_backup_id = %s, want nil: nothing was started", *updated.LastBackupID)
	}
	if updated.LastRunAt != nil {
		t.Error("last_run_at was stamped for a skipped fire")
	}
}

// TestScheduleRunnerSkipsDisabledSchedules: a disabled row is never a fire, even
// when overdue.
func TestScheduleRunnerSkipsDisabledSchedules(t *testing.T) {
	ctx := context.Background()
	projectID, orgID, repo, svc, _ := scheduleTasksFixture(t, ctx, http.StatusNoContent)

	now := time.Now().UTC()
	schedule := seedSchedule(t, ctx, repo, projectID, orgID, "0 2 * * *", now)
	schedule.Enabled = false
	if err := repo.UpsertSchedule(ctx, schedule); err != nil {
		t.Fatalf("disable schedule: %v", err)
	}

	if err := NewScheduleRunner(svc, testLogger()).Run(ctx); err != nil {
		t.Fatalf("runner tick: %v", err)
	}

	backups := listProjectBackups(t, ctx, repo, orgID, projectID)
	if len(backups) != 0 {
		t.Fatalf("created %d backups for a disabled schedule, want 0", len(backups))
	}
}

// TestScheduleRunnerStopsWhenStorageDisabled: with object storage unconfigured a
// scheduled backup could only fail, so the tick claims nothing (the schedule
// stays due until storage returns).
func TestScheduleRunnerStopsWhenStorageDisabled(t *testing.T) {
	ctx := context.Background()
	projectID, orgID, repo, svc, _ := scheduleTasksFixture(t, ctx, http.StatusNoContent)
	svc.storage = disabledStorageService()

	now := time.Now().UTC()
	seedSchedule(t, ctx, repo, projectID, orgID, "0 2 * * *", now)

	if err := NewScheduleRunner(svc, testLogger()).Run(ctx); err != nil {
		t.Fatalf("runner tick: %v", err)
	}

	updated, err := repo.GetScheduleByProject(ctx, projectID)
	if err != nil {
		t.Fatalf("get schedule: %v", err)
	}
	if !updated.NextRunAt.Before(now) {
		t.Errorf("next_run_at = %s, want it still due: nothing may be claimed without storage", updated.NextRunAt)
	}
}

// TestRetentionTaskExpiresThenPurges covers both stages: an expired backup is
// soft-deleted (archive untouched), and once past the grace window its archive
// object and row are removed.
func TestRetentionTaskExpiresThenPurges(t *testing.T) {
	ctx := context.Background()
	projectID, orgID, repo, svc, store := scheduleTasksFixture(t, ctx, http.StatusNoContent)

	now := time.Now().UTC()
	expired := seedBackup(t, ctx, repo, projectID, orgID, BackupStatusReady, now.Add(-24*time.Hour), nil)
	withinGrace := seedBackup(t, ctx, repo, projectID, orgID, BackupStatusReady, now.Add(48*time.Hour), nil)

	task := NewRetentionTask(svc, testLogger())
	if err := task.Run(ctx); err != nil {
		t.Fatalf("retention sweep: %v", err)
	}

	afterExpiry, err := repo.GetByID(ctx, orgID, expired.ID)
	if err != nil {
		t.Fatalf("get expired backup: %v", err)
	}
	if afterExpiry == nil || afterExpiry.DeletedAt == nil {
		t.Fatalf("expired backup was not soft-deleted: %+v", afterExpiry)
	}
	if afterExpiry.Status != BackupStatusDeleted {
		t.Errorf("status = %q, want %q", afterExpiry.Status, BackupStatusDeleted)
	}
	if deletes := store.Deletes(); len(deletes) != 0 {
		t.Errorf("archive deleted during the expiry stage: %v", deletes)
	}

	stillLive, err := repo.GetByID(ctx, orgID, withinGrace.ID)
	if err != nil {
		t.Fatalf("get live backup: %v", err)
	}
	if stillLive == nil || stillLive.DeletedAt != nil {
		t.Fatal("a backup whose expiry is in the future must not be touched")
	}

	// Age the soft delete past the grace window.
	pastGrace := now.Add(-(retentionSoftDeleteGraceDays + 1) * 24 * time.Hour)
	if _, err := repo.db.NewUpdate().
		Model((*Backup)(nil)).
		Set("deleted_at = ?", pastGrace).
		Where("id = ?", expired.ID).
		Exec(ctx); err != nil {
		t.Fatalf("age soft delete: %v", err)
	}

	if err := task.Run(ctx); err != nil {
		t.Fatalf("second retention sweep: %v", err)
	}

	purged, err := repo.GetByID(ctx, orgID, expired.ID)
	if err != nil {
		t.Fatalf("get purged backup: %v", err)
	}
	if purged != nil {
		t.Fatalf("backup row still present after the grace window: %+v", purged)
	}
	deletes := store.Deletes()
	if len(deletes) != 1 {
		t.Fatalf("archive deletes = %v, want exactly the purged backup's key", deletes)
	}
	if want := "/documents/" + expired.StorageKey; deletes[0] != want {
		t.Errorf("deleted key path = %q, want %q", deletes[0], want)
	}
}

// TestRetentionTaskKeepsRowWhenStorageDeleteFails is the safety property: a
// storage failure must leave the row in place so the next sweep retries rather
// than losing the record of an archive that still exists.
func TestRetentionTaskKeepsRowWhenStorageDeleteFails(t *testing.T) {
	ctx := context.Background()
	projectID, orgID, repo, svc, store := scheduleTasksFixture(t, ctx, http.StatusInternalServerError)

	now := time.Now().UTC()
	backup := seedBackup(t, ctx, repo, projectID, orgID, BackupStatusReady, now.Add(-24*time.Hour), nil)

	task := NewRetentionTask(svc, testLogger())
	if err := task.Run(ctx); err != nil {
		t.Fatalf("first sweep: %v", err)
	}

	pastGrace := now.Add(-(retentionSoftDeleteGraceDays + 1) * 24 * time.Hour)
	if _, err := repo.db.NewUpdate().
		Model((*Backup)(nil)).
		Set("deleted_at = ?", pastGrace).
		Where("id = ?", backup.ID).
		Exec(ctx); err != nil {
		t.Fatalf("age soft delete: %v", err)
	}

	if err := task.Run(ctx); err != nil {
		t.Fatalf("second sweep: %v", err)
	}

	// The S3 SDK retries a 5xx, so assert on attempts rather than an exact count.
	deletes := store.Deletes()
	if len(deletes) == 0 {
		t.Fatal("no archive delete was attempted")
	}
	for _, path := range deletes {
		if want := "/documents/" + backup.StorageKey; path != want {
			t.Errorf("deleted key path = %q, want %q", path, want)
		}
	}
	kept, err := repo.GetByID(ctx, orgID, backup.ID)
	if err != nil {
		t.Fatalf("get backup: %v", err)
	}
	if kept == nil {
		t.Fatal("row was purged even though the archive delete failed")
	}
	if kept.DeletedAt == nil {
		t.Error("row lost its soft-delete marker")
	}
}

// TestRetentionTaskExpiresFailedBackups: a failed backup holds no archive worth
// keeping and nothing else removes it, so the sweep expires it like a ready one.
func TestRetentionTaskExpiresFailedBackups(t *testing.T) {
	ctx := context.Background()
	projectID, orgID, repo, svc, _ := scheduleTasksFixture(t, ctx, http.StatusNoContent)

	now := time.Now().UTC()
	failed := seedBackup(t, ctx, repo, projectID, orgID, BackupStatusFailed, now.Add(-24*time.Hour), nil)

	if err := NewRetentionTask(svc, testLogger()).Run(ctx); err != nil {
		t.Fatalf("retention sweep: %v", err)
	}

	after, err := repo.GetByID(ctx, orgID, failed.ID)
	if err != nil {
		t.Fatalf("get failed backup: %v", err)
	}
	if after == nil || after.DeletedAt == nil {
		t.Fatalf("expired failed backup was not soft-deleted: %+v", after)
	}
}

// TestExpiredBackupsAreBoundedAndOrdered proves the sweep's work is bounded in
// SQL, oldest expiry first, so a backlog drains across runs instead of being
// re-read wholesale on every tick.
func TestExpiredBackupsAreBoundedAndOrdered(t *testing.T) {
	ctx := context.Background()
	projectID, orgID, repo, _, _ := scheduleTasksFixture(t, ctx, http.StatusNoContent)

	now := time.Now().UTC()
	older := seedBackup(t, ctx, repo, projectID, orgID, BackupStatusReady, now.Add(-72*time.Hour), nil)
	newer := seedBackup(t, ctx, repo, projectID, orgID, BackupStatusReady, now.Add(-24*time.Hour), nil)

	got, err := repo.GetExpiredBackups(ctx, 1)
	if err != nil {
		t.Fatalf("get expired backups: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("limit 1 returned %d rows", len(got))
	}
	if got[0].ID != older.ID {
		t.Errorf("returned %s first, want the oldest expiry %s", got[0].ID, older.ID)
	}

	got, err = repo.GetExpiredBackups(ctx, 10)
	if err != nil {
		t.Fatalf("get expired backups: %v", err)
	}
	if len(got) != 2 || got[0].ID != older.ID || got[1].ID != newer.ID {
		t.Errorf("rows = %+v, want oldest-expiry-first", got)
	}
}
