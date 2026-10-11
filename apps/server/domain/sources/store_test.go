package sources

import (
	"context"
	"encoding/hex"
	"encoding/json"
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

// TestSourceStore_CRUD_ProjectScoped is the persistence contract: a source is
// created project-scoped, is read back without credential material, is
// inaccessible cross-project (not-found), and is deleted.
func TestSourceStore_CRUD_ProjectScoped(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcescrud")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)}}
	repo := NewRepository(db, log)
	svc := NewService(repo, cfg, log, NewDefaultRegistry())

	projectA := uuid.New()
	projectB := uuid.New()

	created, err := svc.Create(ctx, projectA, &CreateIntegrationRequest{
		Name:         "Drive A",
		ProviderType: ProviderTypeGoogleDrive,
		Config: map[string]any{
			"folder_id":     "folder-a",
			"mime_types":    []any{"application/pdf"},
			"recursive":     true,
			"client_id":     "cid-a",
			"client_secret": "secret-a",
			"refresh_token": "refresh-a",
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create() returned empty id")
	}

	// Read back via service: no secrets.
	got, err := svc.Get(ctx, projectA, uuid.MustParse(created.ID))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Name != "Drive A" {
		t.Errorf("Get().Name = %q, want %q", got.Name, "Drive A")
	}
	b, _ := json.Marshal(got)
	for _, secret := range []string{"secret-a", "refresh-a", "cid-a"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("Get() response leaked secret %q: %s", secret, b)
		}
	}

	// The persisted config jsonb must not carry the secrets.
	var storedConfig string
	if err := db.NewSelect().
		TableExpr("kb.sources").
		Column("config").
		Where("id = ?", created.ID).
		Scan(ctx, &storedConfig); err != nil {
		t.Fatalf("select config: %v", err)
	}
	for _, secret := range []string{"secret-a", "refresh-a", "cid-a"} {
		if strings.Contains(storedConfig, secret) {
			t.Errorf("stored config jsonb leaked secret %q: %s", secret, storedConfig)
		}
	}

	// Credentials are encrypted at rest (the plaintext refresh token is absent
	// from the stored bytea column's raw bytes).
	var blob []byte
	if err := db.NewSelect().
		TableExpr("kb.sources").
		Column("encrypted_credentials").
		Where("id = ?", created.ID).
		Scan(ctx, &blob); err != nil {
		t.Fatalf("select encrypted_credentials: %v", err)
	}
	if len(blob) == 0 {
		t.Fatal("encrypted_credentials column is empty")
	}
	if strings.Contains(string(blob), "refresh-a") {
		t.Error("encrypted_credentials column contains plaintext refresh token")
	}

	// Cross-project read resolves to not-found.
	if _, err := svc.Get(ctx, projectB, uuid.MustParse(created.ID)); err == nil {
		t.Fatal("cross-project Get() = nil error, want not-found")
	} else {
		var appErr *apperror.Error
		if !errors.As(err, &appErr) || appErr.HTTPStatus != 404 {
			t.Fatalf("cross-project Get() error = %v, want 404 not-found", err)
		}
	}

	// List is project-scoped.
	listA, err := svc.List(ctx, projectA)
	if err != nil {
		t.Fatalf("List(A) error = %v", err)
	}
	if len(listA) != 1 {
		t.Fatalf("List(A) = %d items, want 1", len(listA))
	}
	listB, err := svc.List(ctx, projectB)
	if err != nil {
		t.Fatalf("List(B) error = %v", err)
	}
	if len(listB) != 0 {
		t.Fatalf("List(B) = %d items, want 0", len(listB))
	}

	// Delete, then read → not-found.
	if err := svc.Delete(ctx, projectA, uuid.MustParse(created.ID)); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := svc.Get(ctx, projectA, uuid.MustParse(created.ID)); err == nil {
		t.Fatal("Get() after delete = nil error, want not-found")
	}
}

// TestSourceStore_Update_PreservesCredentials verifies that a config update
// without credential keys keeps the existing encrypted blob, and that a config
// update with credential keys rotates it.
func TestSourceStore_Update_PreservesCredentials(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcesupd")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)}}
	repo := NewRepository(db, log)
	svc := NewService(repo, cfg, log, NewDefaultRegistry())

	projectID := uuid.New()
	created, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:         "Drive",
		ProviderType: ProviderTypeGoogleDrive,
		Config: map[string]any{
			"folder_id":     "folder",
			"refresh_token": "refresh-1",
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	id := uuid.MustParse(created.ID)

	var blobBefore []byte
	if err := db.NewSelect().TableExpr("kb.sources").Column("encrypted_credentials").Where("id = ?", id).Scan(ctx, &blobBefore); err != nil {
		t.Fatalf("select blob before: %v", err)
	}

	// Update name + config (no credentials): blob must be preserved.
	newName := "Drive Renamed"
	updated, err := svc.Update(ctx, projectID, id, &UpdateIntegrationRequest{
		Name:   &newName,
		Config: map[string]any{"folder_id": "folder", "recursive": true},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != newName {
		t.Errorf("Update().Name = %q, want %q", updated.Name, newName)
	}

	var blobAfter []byte
	if err := db.NewSelect().TableExpr("kb.sources").Column("encrypted_credentials").Where("id = ?", id).Scan(ctx, &blobAfter); err != nil {
		t.Fatalf("select blob after: %v", err)
	}
	if string(blobBefore) != string(blobAfter) {
		t.Error("Update without credentials rotated the encrypted blob")
	}

	// Update with credentials: blob must rotate.
	if _, err := svc.Update(ctx, projectID, id, &UpdateIntegrationRequest{
		Config: map[string]any{"folder_id": "folder", "refresh_token": "refresh-2"},
	}); err != nil {
		t.Fatalf("Update(credentials) error = %v", err)
	}
	var blobRotated []byte
	if err := db.NewSelect().TableExpr("kb.sources").Column("encrypted_credentials").Where("id = ?", id).Scan(ctx, &blobRotated); err != nil {
		t.Fatalf("select rotated blob: %v", err)
	}
	if string(blobBefore) == string(blobRotated) {
		t.Error("Update with credentials did not rotate the encrypted blob")
	}
}
