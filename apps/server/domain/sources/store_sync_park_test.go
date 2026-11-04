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

// parkedSourceFixture is a scheduled Drive source that has never synced, so
// ListDueSources admits it until something parks it.
type parkedSourceFixture struct {
	repo      *Repository
	projectID uuid.UUID
	sourceID  uuid.UUID
}

func newParkedSourceFixture(t *testing.T) parkedSourceFixture {
	t.Helper()
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcespark")
	t.Cleanup(tdb.Close)
	repo := NewRepository(tdb.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	interval := 60
	projectID := uuid.New()
	return parkedSourceFixture{
		repo:      repo,
		projectID: projectID,
		sourceID:  createParkedTestSource(t, ctx, repo, projectID, &interval),
	}
}

// createParkedTestSource inserts one scheduled source into projectID.
func createParkedTestSource(t *testing.T, ctx context.Context, repo *Repository, projectID uuid.UUID, interval *int) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	src, err := repo.Create(ctx, &Source{
		ID:                   uuid.New(),
		ProjectID:            projectID,
		Provider:             ProviderTypeGoogleDrive,
		DisplayName:          "Drive",
		Config:               map[string]any{"folders": []any{"folder-1"}},
		EncryptedCredentials: []byte("sealed-grant"),
		SyncPolicy:           SyncPolicyScheduled,
		SyncIntervalMinutes:  interval,
		Cursor:               map[string]any{},
		Status:               SourceStatusActive,
		CreatedAt:            now,
		UpdatedAt:            now,
	})
	if err != nil {
		t.Fatalf("Create(source) error = %v", err)
	}
	return src.ID
}

func mustGetSource(t *testing.T, ctx context.Context, repo *Repository, projectID, sourceID uuid.UUID) *Source {
	t.Helper()
	src, err := repo.GetByID(ctx, sourceID, projectID)
	if err != nil || src == nil {
		t.Fatalf("GetByID() = %v, %v", src, err)
	}
	return src
}

// TestMarkSourceNeedsReconnect_LeavesDueSources is the scheduler contract: a
// parked source is recorded with its error and stops being admitted, which is what
// ends the every-minute failed-sync loop; a retryable failure keeps the source
// due so its retry still happens.
func TestMarkSourceNeedsReconnect_LeavesDueSources(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	fx := newParkedSourceFixture(t)
	interval := 60
	retryableID := createParkedTestSource(t, ctx, fx.repo, fx.projectID, &interval)

	due, err := fx.repo.ListDueSources(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ListDueSources() error = %v", err)
	}
	if len(due) != 2 {
		t.Fatalf("ListDueSources() returned %d sources before any failure, want 2", len(due))
	}

	if _, err := fx.repo.MarkSourceNeedsReconnect(ctx, fx.projectID, fx.sourceID, driveReconnectRequired, nil); err != nil {
		t.Fatalf("MarkSourceNeedsReconnect(, nil) error = %v", err)
	}
	if err := fx.repo.RecordSourceError(ctx, fx.projectID, retryableID, "google drive: 503 Service Unavailable"); err != nil {
		t.Fatalf("RecordSourceError() error = %v", err)
	}

	parked := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
	if parked.Status != SourceStatusNeedsReconnect {
		t.Errorf("Status = %q, want %q", parked.Status, SourceStatusNeedsReconnect)
	}
	if parked.LastError == nil || *parked.LastError != driveReconnectRequired {
		t.Errorf("LastError = %v, want the recorded failure", parked.LastError)
	}
	if parked.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", parked.ErrorCount)
	}

	due, err = fx.repo.ListDueSources(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ListDueSources() error = %v", err)
	}
	if len(due) != 1 || due[0].ID != retryableID {
		ids := make([]uuid.UUID, 0, len(due))
		for _, src := range due {
			ids = append(ids, src.ID)
		}
		t.Fatalf("ListDueSources() after parking = %v, want only the retryable source %v", ids, retryableID)
	}
}

// TestMarkSourceNeedsReconnect_KeepsDisabledPaused pins the #2107 guarantee: a
// paused source stays paused. If parking replaced the pause, the next reconnect
// would silently resume a source the operator stopped.
func TestMarkSourceNeedsReconnect_KeepsDisabledPaused(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	fx := newParkedSourceFixture(t)

	if err := fx.repo.UpdateStatus(ctx, fx.sourceID, fx.projectID, SourceStatusDisabled); err != nil {
		t.Fatalf("UpdateStatus(status disabled) error = %v", err)
	}

	if _, err := fx.repo.MarkSourceNeedsReconnect(ctx, fx.projectID, fx.sourceID, driveReconnectRequired, nil); err != nil {
		t.Fatalf("MarkSourceNeedsReconnect(, nil) error = %v", err)
	}
	got := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
	if got.Status != SourceStatusDisabled {
		t.Errorf("Status = %q, want %q (parking must not replace a pause)", got.Status, SourceStatusDisabled)
	}
	if got.LastError == nil {
		t.Error("LastError = nil, want the failure recorded even for a paused source")
	}
}

// TestReplaceOAuthGrant_RestoresParkedSource is the recovery path: a fresh
// consent is the only thing that clears a dead grant, so it must restore the
// source to active and leave the scheduler admitting it again.
func TestReplaceOAuthGrant_RestoresParkedSource(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	fx := newParkedSourceFixture(t)

	if _, err := fx.repo.MarkSourceNeedsReconnect(ctx, fx.projectID, fx.sourceID, driveReconnectRequired, nil); err != nil {
		t.Fatalf("MarkSourceNeedsReconnect(, nil) error = %v", err)
	}
	if err := fx.repo.ReplaceOAuthGrant(ctx, fx.sourceID, fx.projectID, []byte("fresh-grant"), map[string]any{"folders": []any{"folder-1"}}); err != nil {
		t.Fatalf("ReplaceOAuthGrant() error = %v", err)
	}

	got := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
	if got.Status != SourceStatusActive {
		t.Errorf("Status = %q, want %q after reconnect", got.Status, SourceStatusActive)
	}
	if got.LastError != nil || got.ErrorCount != 0 {
		t.Errorf("error state = %v / count %d, want cleared", got.LastError, got.ErrorCount)
	}
	due, err := fx.repo.ListDueSources(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ListDueSources() error = %v", err)
	}
	if len(due) != 1 || due[0].ID != fx.sourceID {
		t.Errorf("ListDueSources() after reconnect = %d sources, want the reconnected source", len(due))
	}
}

// TestReplaceOAuthGrant_KeepsDisabledPaused guards the other half of the #2107
// guarantee: reconnecting a paused source restores access, not the decision to
// resume syncing.
func TestReplaceOAuthGrant_KeepsDisabledPaused(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	fx := newParkedSourceFixture(t)

	if err := fx.repo.UpdateStatus(ctx, fx.sourceID, fx.projectID, SourceStatusDisabled); err != nil {
		t.Fatalf("UpdateStatus(status disabled) error = %v", err)
	}

	if err := fx.repo.ReplaceOAuthGrant(ctx, fx.sourceID, fx.projectID, []byte("fresh-grant"), map[string]any{}); err != nil {
		t.Fatalf("ReplaceOAuthGrant() error = %v", err)
	}
	got := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
	if got.Status != SourceStatusDisabled {
		t.Errorf("Status = %q, want %q after reconnecting a paused source", got.Status, SourceStatusDisabled)
	}
}

// TestAdvanceSourceCursor_ClearsParkedState covers recovery through the sync path
// itself: a batch that succeeds cannot leave the source parked, or a transiently
// misclassified failure would silence the source forever.
func TestAdvanceSourceCursor_ClearsParkedState(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	fx := newParkedSourceFixture(t)

	if _, err := fx.repo.MarkSourceNeedsReconnect(ctx, fx.projectID, fx.sourceID, driveReconnectRequired, nil); err != nil {
		t.Fatalf("MarkSourceNeedsReconnect(, nil) error = %v", err)
	}
	if err := fx.repo.AdvanceSourceCursor(ctx, fx.projectID, fx.sourceID, map[string]any{"page": "next"}, time.Now().UTC()); err != nil {
		t.Fatalf("AdvanceSourceCursor() error = %v", err)
	}

	got := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
	if got.Status != SourceStatusActive {
		t.Errorf("Status = %q, want %q after a successful batch", got.Status, SourceStatusActive)
	}
	if got.LastError != nil {
		t.Errorf("LastError = %v, want cleared", got.LastError)
	}
}
