package sources

import (
	"bytes"
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

// TestSourceStore_Create_IncompleteConfig verifies the OAuth callback path:
// AllowIncompleteConfig skips config validation so a source can be created
// before its sync target is chosen, the refresh_token is still extracted and
// encrypted, and the persisted config carries no credential keys.
func TestSourceStore_Create_IncompleteConfig(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcesincomplete")
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
	if created.ID == "" {
		t.Fatal("Create(incomplete) returned empty id")
	}

	// Read back: config has no sync target yet, and no credential keys leak.
	got, err := svc.Get(ctx, projectID, uuid.MustParse(created.ID))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if _, ok := got.Config["folder_id"]; ok {
		t.Errorf("incomplete source should have no folder_id yet: %+v", got.Config)
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "refresh-oauth") || strings.Contains(string(b), "refresh_token") {
		t.Errorf("incomplete source response leaked credential material: %s", b)
	}

	// The credential blob is still encrypted at rest.
	var blob []byte
	if err := db.NewSelect().TableExpr("kb.sources").Column("encrypted_credentials").Where("id = ?", created.ID).Scan(ctx, &blob); err != nil {
		t.Fatalf("select encrypted_credentials: %v", err)
	}
	if len(blob) == 0 || strings.Contains(string(blob), "refresh-oauth") {
		t.Error("encrypted_credentials not stored encrypted")
	}
}

// mustCredentialBlob reads a source's encrypted credential blob directly.
func mustCredentialBlob(t *testing.T, repo *Repository, id, project uuid.UUID) []byte {
	t.Helper()
	src, err := repo.GetByIDWithCredentials(context.Background(), id, project)
	if err != nil {
		t.Fatalf("GetByIDWithCredentials(%s) error = %v", id, err)
	}
	return src.EncryptedCredentials
}

// TestSourceStore_UpdateCredentials_ProjectScoped pins the write-back contract
// the sync worker uses for rotated OAuth tokens (#2091): it replaces one
// source's encrypted blob, rejects an empty blob, and is project-scoped so a
// worker holding one project's job cannot write another project's credentials.
func TestSourceStore_UpdateCredentials_ProjectScoped(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcescreds")
	t.Cleanup(tdb.Close)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)}}
	repo := NewRepository(tdb.DB, log)
	svc := NewService(repo, cfg, log, NewDefaultRegistry())

	projectA, projectB := uuid.New(), uuid.New()
	created, err := svc.Create(ctx, projectA, &CreateIntegrationRequest{
		Name:         "Drive A",
		ProviderType: ProviderTypeGoogleDrive,
		Config: map[string]any{
			"folder_id":     "folder-a",
			"mime_types":    []any{"application/pdf"},
			"refresh_token": "refresh-a",
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	idA := uuid.MustParse(created.ID)

	if err := repo.UpdateCredentials(ctx, idA, projectA, nil); err == nil {
		t.Error("UpdateCredentials(empty blob) = nil, want a validation error")
	}

	rotated := []byte("rotated-credential-blob")
	if err := repo.UpdateCredentials(ctx, idA, projectA, rotated); err != nil {
		t.Fatalf("UpdateCredentials() error = %v", err)
	}
	if got := mustCredentialBlob(t, repo, idA, projectA); !bytes.Equal(got, rotated) {
		t.Errorf("stored blob = %q, want %q", got, rotated)
	}

	// A cross-project write (a job belonging to another project) must not land.
	if err := repo.UpdateCredentials(ctx, idA, projectB, []byte("hijacked")); !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("cross-project UpdateCredentials() error = %v, want ErrNotFound", err)
	}
	if got := mustCredentialBlob(t, repo, idA, projectA); !bytes.Equal(got, rotated) {
		t.Errorf("cross-project write changed the blob to %q", got)
	}

	// Only the credential column moved: the source is still readable and named.
	got, err := svc.Get(ctx, projectA, idA)
	if err != nil {
		t.Fatalf("Get() after credential write error = %v", err)
	}
	if got.Name != "Drive A" {
		t.Errorf("Get().Name = %q after credential write, want %q", got.Name, "Drive A")
	}
}
