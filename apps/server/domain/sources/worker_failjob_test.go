package sources

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// createParkedTestJob inserts a scheduled sync job for a source and returns it, so
// a test can drive it to a terminal state through the worker.
func createParkedTestJob(t *testing.T, ctx context.Context, repo *Repository, projectID, sourceID uuid.UUID) *SyncJob {
	t.Helper()
	now := time.Now().UTC()
	job, err := repo.CreateSyncJob(ctx, &SyncJob{
		ID:          uuid.New(),
		Status:      SyncStatusPending,
		ScheduledAt: now,
		ProjectID:   projectID,
		SourceID:    sourceID,
		TriggerType: TriggerTypeScheduled,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("CreateSyncJob() error = %v", err)
	}
	return job
}

// TestFailJob_ParksOnlyTerminalFailures drives the worker's failure seam end to
// end. The same call that marks a job failed must park a source whose credential
// the provider rejected, and must leave a retryable failure's source due so the
// sweep still retries it. Without this the parking branch could be deleted with
// the rest of the suite green: the repository primitives would still pass, and
// nothing would reproduce the "every minute, forever" storm this fixes.
func TestFailJob_ParksOnlyTerminalFailures(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcesfailjob")
	t.Cleanup(tdb.Close)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := NewRepository(tdb.DB, log)

	key := make([]byte, 32)
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)}}
	svc := NewService(repo, cfg, log, NewDefaultRegistry())
	worker := NewSyncWorker(svc, nil, nil, nil, nil, log)

	projectID := uuid.New()
	interval := 60
	revokedID := createParkedTestSource(t, ctx, repo, projectID, &interval)
	retryableID := createParkedTestSource(t, ctx, repo, projectID, &interval)
	revokedJob := createParkedTestJob(t, ctx, repo, projectID, revokedID)
	retryableJob := createParkedTestJob(t, ctx, repo, projectID, retryableID)

	// Both failures reach failJob wrapped the way ProcessJob wraps them. failJob
	// returns the error it recorded (ProcessJob propagates it), so the assertion
	// here is about the durable state it left behind, not its return value.
	revokedSrc := mustGetSource(t, ctx, repo, projectID, revokedID)
	retryableSrc := mustGetSource(t, ctx, repo, projectID, retryableID)
	if err := worker.failJob(ctx, revokedJob, revokedSrc, fmt.Errorf("enumerate source: %w", errDriveGrantRejected)); err == nil {
		t.Fatal("failJob(revoked grant) = nil, want the recorded failure returned")
	}
	if err := worker.failJob(ctx, retryableJob, retryableSrc, errors.New("enumerate source: google drive: 503 Service Unavailable")); err == nil {
		t.Fatal("failJob(transient) = nil, want the recorded failure returned")
	}

	revoked := mustGetSource(t, ctx, repo, projectID, revokedID)
	if revoked.Status != SourceStatusNeedsReconnect {
		t.Errorf("revoked source status = %q, want %q", revoked.Status, SourceStatusNeedsReconnect)
	}
	if revoked.LastError == nil {
		t.Error("revoked source LastError = nil, want the recorded failure")
	}
	retryable := mustGetSource(t, ctx, repo, projectID, retryableID)
	if retryable.Status != SourceStatusActive {
		t.Errorf("transient failure status = %q, want %q (a retry must still happen)", retryable.Status, SourceStatusActive)
	}

	due, err := repo.ListDueSources(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ListDueSources() error = %v", err)
	}
	if len(due) != 1 || due[0].ID != retryableID {
		t.Errorf("ListDueSources() = %d sources, want only the retryable one (%v)", len(due), retryableID)
	}

	for _, job := range []*SyncJob{revokedJob, retryableJob} {
		got, err := repo.GetSyncJobByID(ctx, job.ID)
		if err != nil || got == nil {
			t.Fatalf("GetSyncJobByID(%v) = %v, %v", job.ID, got, err)
		}
		if got.Status != SyncStatusFailed {
			t.Errorf("job %v status = %q, want %q", job.ID, got.Status, SyncStatusFailed)
		}
	}
}

// TestFailJob_LeavesTheSourceActiveWhenTheGrantWasReplaced is the end-to-end shape
// of #2425: a job read the grant, the user reconnected while it was in flight, and
// the job then failed against the grant it had. Parking the source here would undo
// that repair and hand the user a second reconnect for the same problem, so the
// failure is recorded but the source stays admitted.
func TestFailJob_LeavesTheSourceActiveWhenTheGrantWasReplaced(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcesfailjobrace")
	t.Cleanup(tdb.Close)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := NewRepository(tdb.DB, log)

	key := make([]byte, 32)
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)}}
	svc := NewService(repo, cfg, log, NewDefaultRegistry())
	worker := NewSyncWorker(svc, nil, nil, nil, nil, log)

	projectID := uuid.New()
	interval := 60
	sourceID := createParkedTestSource(t, ctx, repo, projectID, &interval)

	// The snapshot ProcessJob takes at the top of its run, before the user acts.
	inFlight := mustGetSource(t, ctx, repo, projectID, sourceID)
	job := createParkedTestJob(t, ctx, repo, projectID, sourceID)

	// The user reconnects mid-run: a fresh consent on the same source.
	if err := repo.ReplaceOAuthGrant(ctx, sourceID, projectID, []byte("sealed-new-grant"), inFlight.Config); err != nil {
		t.Fatalf("ReplaceOAuthGrant() error = %v", err)
	}

	if err := worker.failJob(ctx, job, inFlight, fmt.Errorf("enumerate source: %w", errDriveGrantRejected)); err == nil {
		t.Fatal("failJob() = nil, want the recorded failure returned")
	}

	got := mustGetSource(t, ctx, repo, projectID, sourceID)
	if got.Status != SourceStatusActive {
		t.Errorf("Status = %q, want %q: the grant this run failed against was replaced while it ran",
			got.Status, SourceStatusActive)
	}
	if got.LastError == nil {
		t.Error("LastError = nil, want the failure recorded even though the park was suppressed")
	}
	due, err := repo.ListDueSources(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ListDueSources() error = %v", err)
	}
	if len(due) != 1 || due[0].ID != sourceID {
		t.Errorf("ListDueSources() = %d sources, want the repaired source due again (%v)", len(due), sourceID)
	}
	gotJob, err := repo.GetSyncJobByID(ctx, job.ID)
	if err != nil || gotJob == nil {
		t.Fatalf("GetSyncJobByID(%v) = %v, %v", job.ID, gotJob, err)
	}
	if gotJob.Status != SyncStatusFailed {
		t.Errorf("job status = %q, want %q: the run really did fail and its history must say so", gotJob.Status, SyncStatusFailed)
	}
}
