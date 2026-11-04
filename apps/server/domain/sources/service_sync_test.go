package sources

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestTriggerSync_IncompleteConfig_Rejected asserts a Drive source created by the
// OAuth callback before its sync target was chosen is refused at admission: the
// error leads with the actionable "choose a sync target before syncing" message
// and no sync job row is created.
func TestTriggerSync_IncompleteConfig_Rejected(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcessyncincomplete")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)}}
	repo := NewRepository(db, log)
	svc := NewService(repo, cfg, log, NewDefaultRegistry())

	projectID := uuid.New()
	created, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:                  "Drive (awaiting folder)",
		ProviderType:          ProviderTypeGoogleDrive,
		Config:                map[string]any{"refresh_token": "refresh-oauth"},
		AllowIncompleteConfig: true,
	})
	if err != nil {
		t.Fatalf("Create(incomplete) error = %v", err)
	}
	id := uuid.MustParse(created.ID)

	_, err = svc.TriggerSync(ctx, projectID, id)
	if err == nil {
		t.Fatal("TriggerSync(incomplete) = nil error, want validation error")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("TriggerSync(incomplete) error type = %T, want *apperror.Error", err)
	}
	if appErr.HTTPStatus != 422 {
		t.Errorf("TriggerSync(incomplete) HTTP status = %d, want 422", appErr.HTTPStatus)
	}
	// apperror renders "<code>: <message>", so the actionable lead lives in the
	// message rather than at the front of the rendered error.
	if !strings.HasPrefix(appErr.Message, "choose a sync target before syncing") {
		t.Errorf("TriggerSync(incomplete) message = %q, want prefix %q", appErr.Message, "choose a sync target before syncing")
	}
	// The provider detail is the canonical `folders` message now that the
	// single-folder key is legacy, and it must survive into the rendered error.
	if !strings.Contains(err.Error(), "config.folders must name at least one folder") {
		t.Errorf("TriggerSync(incomplete) error = %q, want provider detail retained", err)
	}

	// No sync job may be admitted for a source that failed validation.
	count, err := db.NewSelect().Model((*SyncJob)(nil)).Where("source_id = ?", id).Count(ctx)
	if err != nil {
		t.Fatalf("count sync jobs: %v", err)
	}
	if count != 0 {
		t.Errorf("TriggerSync(incomplete) admitted %d sync job(s), want 0", count)
	}
}

// TestTriggerSync_ParkedSource_Rejected asserts a parked source cannot be synced
// manually either: the stored credential is rejected by the provider, so one more
// job could only fail and re-record the same error. The refusal carries the
// provider's reconnect copy, and no job is admitted.
func TestTriggerSync_ParkedSource_Rejected(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcessyncparked")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)}}
	repo := NewRepository(db, log)
	svc := NewService(repo, cfg, log, NewDefaultRegistry())

	projectID := uuid.New()
	created, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:                  "Drive (revoked grant)",
		ProviderType:          ProviderTypeGoogleDrive,
		Config:                map[string]any{"refresh_token": "refresh-oauth"},
		AllowIncompleteConfig: true,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	id := uuid.MustParse(created.ID)
	if _, err := repo.MarkSourceNeedsReconnect(ctx, projectID, id, driveReconnectRequired, nil); err != nil {
		t.Fatalf("MarkSourceNeedsReconnect(, nil) error = %v", err)
	}

	_, err = svc.TriggerSync(ctx, projectID, id)
	if err == nil {
		t.Fatal("TriggerSync(parked) = nil error, want validation error")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("TriggerSync(parked) error type = %T, want *apperror.Error", err)
	}
	if appErr.HTTPStatus != 422 {
		t.Errorf("TriggerSync(parked) HTTP status = %d, want 422", appErr.HTTPStatus)
	}
	// The parked branch runs before config validation, and it returns the
	// provider's own wording so the user reads the fix, not an admission detail.
	if appErr.Message != driveReconnectRequired {
		t.Errorf("TriggerSync(parked) message = %q, want %q", appErr.Message, driveReconnectRequired)
	}

	count, err := db.NewSelect().Model((*SyncJob)(nil)).Where("source_id = ?", id).Count(ctx)
	if err != nil {
		t.Fatalf("count sync jobs: %v", err)
	}
	if count != 0 {
		t.Errorf("TriggerSync(parked) admitted %d sync job(s), want 0", count)
	}
}

// TestUpdateSource_EnableClearsParkedState pins the modelled exit from the parked
// state: an explicit enable is the user's decision to retry, so it returns the
// source to active without a fresh consent. The grant is untouched, so a still-dead
// one parks the source again on the next sync — the enable cannot hide the problem,
// it only buys one attempt.
func TestUpdateSource_EnableClearsParkedState(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcessyncenableparked")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)}}
	repo := NewRepository(db, log)
	svc := NewService(repo, cfg, log, NewDefaultRegistry())

	projectID := uuid.New()
	created, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:                  "Drive (revoked grant)",
		ProviderType:          ProviderTypeGoogleDrive,
		Config:                map[string]any{"refresh_token": "refresh-oauth"},
		AllowIncompleteConfig: true,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	id := uuid.MustParse(created.ID)
	if _, err := repo.MarkSourceNeedsReconnect(ctx, projectID, id, driveReconnectRequired, nil); err != nil {
		t.Fatalf("MarkSourceNeedsReconnect(, nil) error = %v", err)
	}

	enabled := true
	if _, err := svc.Update(ctx, projectID, id, &UpdateIntegrationRequest{Enabled: &enabled}); err != nil {
		t.Fatalf("Update(enabled=true) error = %v", err)
	}

	got := mustGetSource(t, ctx, repo, projectID, id)
	if got.Status != SourceStatusActive {
		t.Errorf("Status = %q, want %q after an explicit enable", got.Status, SourceStatusActive)
	}
	// The recorded failure goes with the park: an active source still carrying
	// "the connection expired or was revoked; reconnect the source" reads as a
	// live problem on a source that was just retried (#2421). The sync history
	// keeps the failed run, which is where the evidence belongs.
	if got.LastError != nil {
		t.Errorf("LastError = %q, want it cleared by the enable", *got.LastError)
	}
	if got.LastErrorAt != nil {
		t.Errorf("LastErrorAt = %v, want it cleared by the enable", *got.LastErrorAt)
	}
	if got.ErrorCount != 0 {
		t.Errorf("ErrorCount = %d, want 0 after the enable", got.ErrorCount)
	}
	// One attempt, then straight back to parked: the enable did not repair the
	// grant, so the next failure must not leave the source looping in the sweep.
	if _, err := repo.MarkSourceNeedsReconnect(ctx, projectID, id, driveReconnectRequired, nil); err != nil {
		t.Fatalf("MarkSourceNeedsReconnect(, nil) after retry error = %v", err)
	}
	got = mustGetSource(t, ctx, repo, projectID, id)
	if got.Status != SourceStatusNeedsReconnect {
		t.Errorf("Status = %q, want %q after the retry failed again", got.Status, SourceStatusNeedsReconnect)
	}
}
