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

// newCarriedConfigTestService builds a DB-backed Service with an encryption key
// so sources can be created and updated without reaching an external provider.
func newCarriedConfigTestService(t *testing.T, db *testdb.TestDB) *Service {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewService(NewRepository(db.DB, log), cfg, log, NewDefaultRegistry())
}

// TestUpdate_PreservesCarriedConfigKeys asserts the settings form (which never
// submits folders, the granted strategy, or the account email) cannot drop them.
func TestUpdate_PreservesCarriedConfigKeys(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcesupdatecarried")
	t.Cleanup(tdb.Close)
	svc := newCarriedConfigTestService(t, tdb)

	projectID := uuid.New()
	created, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:         "Drive",
		ProviderType: ProviderTypeGoogleDrive,
		Config: map[string]any{
			"refresh_token":         "rt",
			"folders":               []any{map[string]any{"folder_id": "f1", "name": "Reports", "drive_id": "d1"}},
			"mime_types":            []any{"application/pdf"},
			oauthGrantedStrategyKey: string(ScopeStrategyDriveReadonly),
			oauthGrantedScopesKey:   []any{"https://www.googleapis.com/auth/drive.readonly"},
			oauthAccountEmailKey:    "ops@example.com",
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	id := uuid.MustParse(created.ID)

	name := "Drive (renamed)"
	updated, err := svc.Update(ctx, projectID, id, &UpdateIntegrationRequest{
		Name: &name,
		// The settings form submits only its own fields.
		Config: map[string]any{"mime_types": []any{"text/plain"}, "recursive": true},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != name {
		t.Errorf("Name = %q, want %q", updated.Name, name)
	}
	folders, ok := updated.Config["folders"].([]any)
	if !ok || len(folders) != 1 {
		t.Fatalf("folders = %#v, want the stored target preserved", updated.Config["folders"])
	}
	entry, _ := folders[0].(map[string]any)
	if entry["folder_id"] != "f1" || entry["name"] != "Reports" || entry["drive_id"] != "d1" {
		t.Errorf("folder = %#v, want the authoritative stored values", entry)
	}
	if got := updated.Config[oauthGrantedStrategyKey]; got != string(ScopeStrategyDriveReadonly) {
		t.Errorf("%s = %v, want the granted strategy preserved", oauthGrantedStrategyKey, got)
	}
	if got := updated.Config[oauthAccountEmailKey]; got != "ops@example.com" {
		t.Errorf("%s = %v, want the account email preserved", oauthAccountEmailKey, got)
	}
	if scopes, ok := updated.Config[oauthGrantedScopesKey].([]any); !ok || len(scopes) != 1 || scopes[0] != "https://www.googleapis.com/auth/drive.readonly" {
		t.Errorf("%s = %#v, want the granted scopes preserved", oauthGrantedScopesKey, updated.Config[oauthGrantedScopesKey])
	}
	// Submitted keys still replace the stored ones.
	if mimeTypes, ok := updated.Config["mime_types"].([]any); !ok || len(mimeTypes) != 1 || mimeTypes[0] != "text/plain" {
		t.Errorf("mime_types = %#v, want the submitted value", updated.Config["mime_types"])
	}
	if recursive, _ := updated.Config["recursive"].(bool); !recursive {
		t.Errorf("recursive = %#v, want true", updated.Config["recursive"])
	}
}

// TestUpdate_AllowsSettingsSaveBeforeTargetChosen asserts a source created by the
// OAuth callback (no folder yet) can still be renamed, while a complete source
// cannot have its target removed by an update.
func TestUpdate_AllowsSettingsSaveBeforeTargetChosen(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcesupdateincomplete")
	t.Cleanup(tdb.Close)
	svc := newCarriedConfigTestService(t, tdb)
	projectID := uuid.New()

	incomplete, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:                  "Drive (awaiting folders)",
		ProviderType:          ProviderTypeGoogleDrive,
		Config:                map[string]any{"refresh_token": "rt"},
		AllowIncompleteConfig: true,
	})
	if err != nil {
		t.Fatalf("Create(incomplete) error = %v", err)
	}
	name := "Drive (renamed before folders)"
	if _, err := svc.Update(ctx, projectID, uuid.MustParse(incomplete.ID), &UpdateIntegrationRequest{
		Name:   &name,
		Config: map[string]any{"recursive": true},
	}); err != nil {
		t.Fatalf("Update(incomplete) error = %v", err)
	}

	complete, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:         "Drive",
		ProviderType: ProviderTypeGoogleDrive,
		Config: map[string]any{
			"refresh_token": "rt",
			"folders":       []any{map[string]any{"folder_id": "f1"}},
		},
	})
	if err != nil {
		t.Fatalf("Create(complete) error = %v", err)
	}
	// Explicitly clearing the target must fail: only an already-incomplete source
	// may save without one.
	_, err = svc.Update(ctx, projectID, uuid.MustParse(complete.ID), &UpdateIntegrationRequest{
		Config: map[string]any{"folders": []any{}},
	})
	if err == nil {
		t.Fatal("Update(clear target) = nil, want a validation error")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("Update(clear target) error type = %T, want *apperror.Error", err)
	}
	if !strings.Contains(appErr.Message, "folders") {
		t.Errorf("Update(clear target) message = %q, want a folders validation error", appErr.Message)
	}
}
