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

func newCredentialTestService(t *testing.T, suffix string) (*Service, *Repository, *testdb.TestDB) {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, suffix)
	t.Cleanup(tdb.Close)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 11)
	}
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)}}
	repo := NewRepository(tdb.DB, log)
	return NewService(repo, cfg, log, NewDefaultRegistry()), repo, tdb
}

// TestSavedCredential_CRUD_ProjectScoped pins create/list/update/delete, the
// encryption-at-rest contract, and cross-project isolation.
func TestSavedCredential_CRUD_ProjectScoped(t *testing.T) {
	svc, repo, _ := newCredentialTestService(t, "savedcreds")
	ctx := context.Background()
	projectA, projectB := uuid.New(), uuid.New()

	created, err := svc.CreateSavedCredential(ctx, projectA, ProviderTypeGitHub, "  ops PAT  ", map[string]any{
		"access_token": "ghp_secret",
		"unknown":      "ignored",
	})
	if err != nil {
		t.Fatalf("CreateSavedCredential() error = %v", err)
	}
	if created.Name != "ops PAT" || created.ProviderType != ProviderTypeGitHub {
		t.Errorf("created = %+v, want trimmed name and provider", created)
	}
	if created.UsedByCount != 0 {
		t.Errorf("UsedByCount = %d, want 0", created.UsedByCount)
	}
	raw, _ := json.Marshal(created)
	if strings.Contains(string(raw), "ghp_secret") || strings.Contains(string(raw), "access_token") {
		t.Fatalf("created DTO leaked credential material: %s", raw)
	}

	// Only the provider's declared keys are stored; the encrypted blob never
	// contains the plaintext token.
	row, err := repo.GetCredentialByID(ctx, uuid.MustParse(created.ID), projectA)
	if err != nil {
		t.Fatalf("GetCredentialByID() error = %v", err)
	}
	if len(row.EncryptedCredentials) == 0 {
		t.Fatal("encrypted_credentials is empty")
	}
	if strings.Contains(string(row.EncryptedCredentials), "ghp_secret") {
		t.Error("encrypted_credentials contains the plaintext token")
	}

	// Cross-project reads and writes are not-found.
	if _, err := repo.GetCredentialByID(ctx, row.ID, projectB); !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("cross-project GetCredentialByID() error = %v, want ErrNotFound", err)
	}
	if _, err := svc.UpdateSavedCredential(ctx, projectB, row.ID, nil, map[string]any{"access_token": "other"}); !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("cross-project UpdateSavedCredential() error = %v, want ErrNotFound", err)
	}
	if err := svc.DeleteSavedCredential(ctx, projectB, row.ID); !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("cross-project DeleteSavedCredential() error = %v, want ErrNotFound", err)
	}

	// List is project- and provider-scoped.
	items, err := svc.ListSavedCredentials(ctx, projectA, ProviderTypeGitHub)
	if err != nil {
		t.Fatalf("ListSavedCredentials() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("ListSavedCredentials() = %+v, want the created credential", items)
	}
	empty, err := svc.ListSavedCredentials(ctx, projectB, ProviderTypeGitHub)
	if err != nil {
		t.Fatalf("ListSavedCredentials(B) error = %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("ListSavedCredentials(B) = %d items, want 0", len(empty))
	}

	// Rename and rotate the secret.
	newName := "rotated PAT"
	updated, err := svc.UpdateSavedCredential(ctx, projectA, row.ID, &newName, map[string]any{"access_token": "ghp_rotated"})
	if err != nil {
		t.Fatalf("UpdateSavedCredential() error = %v", err)
	}
	if updated.Name != newName {
		t.Errorf("updated.Name = %q, want %q", updated.Name, newName)
	}
	creds, err := svc.credentialsForSource(ctx, &Source{ProjectID: projectA, Provider: ProviderTypeGitHub, CredentialID: &row.ID})
	if err != nil {
		t.Fatalf("credentialsForSource() error = %v", err)
	}
	if creds["access_token"] != "ghp_rotated" {
		t.Errorf("credentialsForSource() token = %v, want rotated token", creds["access_token"])
	}

	// An un-referenced credential deletes cleanly.
	if err := svc.DeleteSavedCredential(ctx, projectA, row.ID); err != nil {
		t.Fatalf("DeleteSavedCredential() error = %v", err)
	}
	if _, err := repo.GetCredentialByID(ctx, row.ID, projectA); !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("GetCredentialByID() after delete error = %v, want ErrNotFound", err)
	}
}

// TestSavedCredential_RejectsNonCredentialProviders covers provider support,
// required name, and secret validation before persistence.
func TestSavedCredential_RejectsNonCredentialProviders(t *testing.T) {
	svc, _, _ := newCredentialTestService(t, "savedcredsvalidation")
	ctx := context.Background()
	projectID := uuid.New()

	cases := []struct {
		name         string
		providerType string
		credName     string
		creds        map[string]any
	}{
		{"unknown provider", "imap", "x", map[string]any{"access_token": "t"}},
		{"oauth-only provider", ProviderTypeGoogleDrive, "x", map[string]any{"refresh_token": "r"}},
		{"missing name", ProviderTypeGitHub, "  ", map[string]any{"access_token": "t"}},
		{"missing secret", ProviderTypeGitHub, "x", map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.CreateSavedCredential(ctx, projectID, tc.providerType, tc.credName, tc.creds); err == nil {
				t.Fatal("CreateSavedCredential() = nil error, want a validation error")
			}
		})
	}

	if _, err := svc.ListSavedCredentials(ctx, projectID, ProviderTypeGoogleDrive); err == nil {
		t.Error("ListSavedCredentials(oauth-only provider) = nil error, want validation error")
	}
}

// TestSavedCredential_SourceReference covers the reference model end to end: a
// source storing credential_id and no inline blob, usedByCount, the delete
// conflict, credentialsForSource, and the legacy inline fallback.
func TestSavedCredential_SourceReference(t *testing.T) {
	svc, repo, tdb := newCredentialTestService(t, "savedcredsref")
	ctx := context.Background()
	projectID := uuid.New()

	saved, err := svc.CreateSavedCredential(ctx, projectID, ProviderTypeGitHub, "team PAT", map[string]any{"access_token": "ghp_team"})
	if err != nil {
		t.Fatalf("CreateSavedCredential() error = %v", err)
	}
	credentialID := uuid.MustParse(saved.ID)

	created, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:              "Docs repo",
		ProviderType:      ProviderTypeGitHub,
		Config:            map[string]any{"repo": "acme/docs"},
		SavedCredentialID: &saved.ID,
	})
	if err != nil {
		t.Fatalf("Create(referenced credential) error = %v", err)
	}
	src, err := repo.GetByIDWithCredentials(ctx, uuid.MustParse(created.ID), projectID)
	if err != nil {
		t.Fatalf("GetByIDWithCredentials() error = %v", err)
	}
	if src.CredentialID == nil || *src.CredentialID != credentialID {
		t.Errorf("source credential_id = %v, want %s", src.CredentialID, credentialID)
	}
	if len(src.EncryptedCredentials) != 0 {
		t.Errorf("source encrypted_credentials = %q, want nil for a referenced credential", src.EncryptedCredentials)
	}

	items, err := svc.ListSavedCredentials(ctx, projectID, ProviderTypeGitHub)
	if err != nil {
		t.Fatalf("ListSavedCredentials() error = %v", err)
	}
	if len(items) != 1 || items[0].UsedByCount != 1 {
		t.Fatalf("ListSavedCredentials() = %+v, want usedByCount 1", items)
	}

	creds, err := svc.credentialsForSource(ctx, src)
	if err != nil {
		t.Fatalf("credentialsForSource() error = %v", err)
	}
	if creds["access_token"] != "ghp_team" {
		t.Errorf("credentialsForSource() token = %v, want ghp_team", creds["access_token"])
	}

	// Deleting a referenced credential is a 409 conflict.
	err = svc.DeleteSavedCredential(ctx, projectID, credentialID)
	if err == nil {
		t.Fatal("DeleteSavedCredential(in use) = nil error, want conflict")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.HTTPStatus != 409 {
		t.Fatalf("DeleteSavedCredential(in use) error = %v, want 409", err)
	}
	if !strings.Contains(appErr.Message, "in use by 1 data source(s)") {
		t.Errorf("DeleteSavedCredential(in use) message = %q", appErr.Message)
	}

	// Clearing the reference without replacement credentials is rejected and
	// leaves the source's reference intact.
	empty := ""
	if _, err := svc.Update(ctx, projectID, uuid.MustParse(created.ID), &UpdateIntegrationRequest{SavedCredentialID: &empty}); err == nil {
		t.Fatal("Update(clear credential reference alone) = nil error, want validation error")
	}
	srcAfterClear, err := repo.GetByIDWithCredentials(ctx, uuid.MustParse(created.ID), projectID)
	if err != nil {
		t.Fatalf("GetByIDWithCredentials() after rejected clear error = %v", err)
	}
	if srcAfterClear.CredentialID == nil || *srcAfterClear.CredentialID != credentialID {
		t.Errorf("source credential_id after rejected clear = %v, want %s", srcAfterClear.CredentialID, credentialID)
	}

	// Clearing with replacement inline fields succeeds and releases the saved
	// credential.
	if _, err := svc.Update(ctx, projectID, uuid.MustParse(created.ID), &UpdateIntegrationRequest{
		SavedCredentialID: &empty,
		Config:            map[string]any{"repo": "acme/docs", "access_token": "ghp_inline_after_clear"},
	}); err != nil {
		t.Fatalf("Update(clear credential reference with inline fields) error = %v", err)
	}
	srcInline, err := repo.GetByIDWithCredentials(ctx, uuid.MustParse(created.ID), projectID)
	if err != nil {
		t.Fatalf("GetByIDWithCredentials() after clear error = %v", err)
	}
	if srcInline.CredentialID != nil {
		t.Errorf("source credential_id after clear = %v, want nil", srcInline.CredentialID)
	}
	inlineCreds, err := svc.credentialsForSource(ctx, srcInline)
	if err != nil {
		t.Fatalf("credentialsForSource() after clear error = %v", err)
	}
	if inlineCreds["access_token"] != "ghp_inline_after_clear" {
		t.Errorf("credentialsForSource() token after clear = %v, want ghp_inline_after_clear", inlineCreds["access_token"])
	}
	if err := svc.DeleteSavedCredential(ctx, projectID, credentialID); err != nil {
		t.Fatalf("DeleteSavedCredential() after clearing reference error = %v", err)
	}

	// Legacy inline credentials still decrypt without a saved reference.
	legacy, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:         "Legacy repo",
		ProviderType: ProviderTypeGitHub,
		Config:       map[string]any{"repo": "acme/legacy", "access_token": "ghp_inline"},
	})
	if err != nil {
		t.Fatalf("Create(inline credential) error = %v", err)
	}
	legacySrc, err := repo.GetByIDWithCredentials(ctx, uuid.MustParse(legacy.ID), projectID)
	if err != nil {
		t.Fatalf("GetByIDWithCredentials(legacy) error = %v", err)
	}
	if legacySrc.CredentialID != nil {
		t.Errorf("legacy source credential_id = %v, want nil", legacySrc.CredentialID)
	}
	if len(legacySrc.EncryptedCredentials) == 0 {
		t.Error("legacy source encrypted_credentials is empty")
	}
	legacyCreds, err := svc.credentialsForSource(ctx, legacySrc)
	if err != nil {
		t.Fatalf("credentialsForSource(legacy) error = %v", err)
	}
	if legacyCreds["access_token"] != "ghp_inline" {
		t.Errorf("legacy credentialsForSource() token = %v, want ghp_inline", legacyCreds["access_token"])
	}

	// The source's stored config never carries the plaintext token.
	var storedConfig string
	if err := tdb.DB.NewSelect().TableExpr("kb.sources").Column("config").Where("id = ?", legacy.ID).Scan(ctx, &storedConfig); err != nil {
		t.Fatalf("select legacy config: %v", err)
	}
	if strings.Contains(storedConfig, "ghp_inline") {
		t.Errorf("stored config leaked credential: %s", storedConfig)
	}
}

// TestSavedCredential_ProviderMismatchRejected pins the same-project provider
// check on both source create and saved-credential decryption.
func TestSavedCredential_ProviderMismatchRejected(t *testing.T) {
	svc, _, _ := newCredentialTestService(t, "savedcredsmismatch")
	ctx := context.Background()
	projectID := uuid.New()

	saved, err := svc.CreateSavedCredential(ctx, projectID, ProviderTypeGitHub, "team PAT", map[string]any{"access_token": "ghp_team"})
	if err != nil {
		t.Fatalf("CreateSavedCredential() error = %v", err)
	}

	_, err = svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:              "Drive with GitHub PAT",
		ProviderType:      ProviderTypeGoogleDrive,
		Config:            map[string]any{"folder_id": "folder"},
		SavedCredentialID: &saved.ID,
	})
	if err == nil {
		t.Fatal("Create(provider mismatch) = nil error, want validation error")
	}

	credentialID := uuid.MustParse(saved.ID)
	if _, err := svc.credentialsForSource(ctx, &Source{ProjectID: projectID, Provider: ProviderTypeGoogleDrive, CredentialID: &credentialID}); err == nil {
		t.Fatal("credentialsForSource(provider mismatch) = nil error, want validation error")
	}
}

// TestTestConfig_UsesSavedCredential verifies test-config resolves a referenced
// credential instead of requiring inline credential keys.
func TestTestConfig_UsesSavedCredential(t *testing.T) {
	svc, _, _ := newCredentialTestService(t, "savedcredstestcfg")
	ctx := context.Background()
	projectID := uuid.New()

	// Substitute the GitHub transport so TestConnection is hermetic.
	fake := &fakeGitHubTransport{}
	svc.providers = NewRegistry(newFakeGitHubProvider(fake))

	saved, err := svc.CreateSavedCredential(ctx, projectID, ProviderTypeGitHub, "team PAT", map[string]any{"access_token": "ghp_team"})
	if err != nil {
		t.Fatalf("CreateSavedCredential() error = %v", err)
	}

	resp, err := svc.TestConfig(ctx, projectID, &TestConfigRequest{
		ProviderType:      ProviderTypeGitHub,
		Config:            map[string]any{"repo": "acme/docs"},
		SavedCredentialID: &saved.ID,
	})
	if err != nil {
		t.Fatalf("TestConfig(saved credential) error = %v", err)
	}
	if !resp.Success {
		t.Errorf("TestConfig(saved credential) = %+v, want success", resp)
	}
}
