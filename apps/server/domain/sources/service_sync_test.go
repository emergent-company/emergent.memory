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
