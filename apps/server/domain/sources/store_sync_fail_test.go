package sources

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// syncJobFixture is a web source plus one processing sync job advertising the
// "enumerating" phase, so terminal transitions can be asserted against a
// realistic in-progress row.
type syncJobFixture struct {
	repo      *Repository
	projectID uuid.UUID
	sourceID  uuid.UUID
	jobID     uuid.UUID
}

func newSyncJobFixture(t *testing.T) syncJobFixture {
	t.Helper()
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcesjobterm")
	t.Cleanup(tdb.Close)
	repo := NewRepository(tdb.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	now := time.Now().UTC()
	src, err := repo.Create(ctx, &Source{
		ID:          uuid.New(),
		ProjectID:   uuid.New(),
		Provider:    ProviderTypeWeb,
		DisplayName: "web",
		Config:      map[string]any{"start_urls": []any{"http://169.254.169.254/"}},
		SyncPolicy:  "manual",
		Cursor:      map[string]any{},
		Status:      SourceStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("Create(source) error = %v", err)
	}

	job, err := repo.CreateSyncJob(ctx, &SyncJob{
		ID:          uuid.New(),
		Status:      SyncStatusPending,
		ScheduledAt: now,
		ProjectID:   src.ProjectID,
		SourceID:    src.ID,
		TriggerType: TriggerTypeManual,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("CreateSyncJob() error = %v", err)
	}

	phase := "enumerating"
	if err := repo.UpdateSyncJobProgress(ctx, job.ID, SyncProgress{Phase: &phase}); err != nil {
		t.Fatalf("UpdateSyncJobProgress() error = %v", err)
	}
	return syncJobFixture{repo: repo, projectID: src.ProjectID, sourceID: src.ID, jobID: job.ID}
}

// TestSyncJobStore_FailedClearsPhase is the terminal-state contract for a sync
// job: failing it records the error, leaves the counters alone (nothing was
// enumerated), and clears the progress phase — a stale "enumerating" phase next
// to a failed status contradicts the terminal state in the UI.
func TestSyncJobStore_FailedClearsPhase(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	fx := newSyncJobFixture(t)

	const errMsg = "enumerate source: web: crawl produced no pages: http://169.254.169.254/: web: blocked URL: URL host 169.254.169.254 is in a disallowed address range"
	if err := fx.repo.MarkSyncJobFailed(ctx, fx.jobID, errMsg, map[string]any{"message": errMsg}); err != nil {
		t.Fatalf("MarkSyncJobFailed() error = %v", err)
	}

	got, err := fx.repo.GetSyncJobByID(ctx, fx.jobID)
	if err != nil || got == nil {
		t.Fatalf("GetSyncJobByID() = %v, %v", got, err)
	}
	if got.Status != SyncStatusFailed {
		t.Errorf("Status = %q, want %q", got.Status, SyncStatusFailed)
	}
	if got.CurrentPhase != nil {
		t.Errorf("CurrentPhase = %q, want nil (a failed job is not still enumerating)", *got.CurrentPhase)
	}
	if got.LastError == nil || *got.LastError != errMsg {
		t.Errorf("LastError = %v, want the sync error", got.LastError)
	}
	if got.TotalItems != 0 || got.ProcessedItems != 0 || got.FailedItems != 0 {
		t.Errorf("counters = total %d / processed %d / failed %d, want 0 (nothing was enumerated)",
			got.TotalItems, got.ProcessedItems, got.FailedItems)
	}
	if got.AttemptCount != 1 {
		t.Errorf("AttemptCount = %d, want 1", got.AttemptCount)
	}
}

// TestSyncJobStore_CancelClearsPhase covers the same terminal-state contract for
// cancellation: a cancelled job must not keep advertising a progress phase.
func TestSyncJobStore_CancelClearsPhase(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	fx := newSyncJobFixture(t)

	job, err := fx.repo.CancelSyncJob(ctx, fx.projectID, fx.sourceID, fx.jobID)
	if err != nil || job == nil {
		t.Fatalf("CancelSyncJob() = %v, %v", job, err)
	}
	if job.Status != SyncStatusCancelled {
		t.Errorf("Status = %q, want %q", job.Status, SyncStatusCancelled)
	}
	if job.CurrentPhase != nil {
		t.Errorf("CurrentPhase = %q, want nil (a cancelled job is not still enumerating)", *job.CurrentPhase)
	}
}

// TestSyncJobStore_CompletedClearsPhase closes the same contract for the success
// path: a completed job reports its summary message, not a stale phase.
func TestSyncJobStore_CompletedClearsPhase(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	fx := newSyncJobFixture(t)

	if err := fx.repo.MarkSyncJobCompleted(ctx, fx.jobID, "sync completed"); err != nil {
		t.Fatalf("MarkSyncJobCompleted() error = %v", err)
	}
	got, err := fx.repo.GetSyncJobByID(ctx, fx.jobID)
	if err != nil || got == nil {
		t.Fatalf("GetSyncJobByID() = %v, %v", got, err)
	}
	if got.Status != SyncStatusCompleted {
		t.Errorf("Status = %q, want %q", got.Status, SyncStatusCompleted)
	}
	if got.CurrentPhase != nil {
		t.Errorf("CurrentPhase = %q, want nil (a completed job is not still syncing)", *got.CurrentPhase)
	}
	if got.StatusMessage == nil || *got.StatusMessage != "sync completed" {
		t.Errorf("StatusMessage = %v, want the completion summary", got.StatusMessage)
	}
}
