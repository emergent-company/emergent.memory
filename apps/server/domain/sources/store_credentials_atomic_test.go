package sources

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestReplaceSavedCredentialGrant_RollsBackTheCredentialWhenTheSourceUpdateFails
// is the #2457 invariant: replacing the grant and returning the sources that read
// it to active is one unit. A reconnect that dies between the two used to leave a
// fresh credential with every referencing source still parked on the old epoch —
// a state nothing retries, because the scheduler never admits a parked source.
//
// The second statement is failed deterministically with a trigger, which is the
// same shape as a statement timeout or a dropped connection: the credential write
// must already be undone, not half applied.
func TestReplaceSavedCredentialGrant_RollsBackTheCredentialWhenTheSourceUpdateFails(t *testing.T) {
	svc, repo, tdb := newCredentialTestService(t, "savedcredatomic")
	ctx := context.Background()
	projectID := uuid.New()

	saved, err := svc.CreateSavedCredential(ctx, projectID, ProviderTypeGitHub, "team PAT", map[string]any{"access_token": "ghp_old"})
	if err != nil {
		t.Fatalf("CreateSavedCredential() error = %v", err)
	}
	credentialID := uuid.MustParse(saved.ID)

	created, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:              "docs",
		ProviderType:      ProviderTypeGitHub,
		Config:            map[string]any{"repo": "acme/docs"},
		SavedCredentialID: &saved.ID,
	})
	if err != nil {
		t.Fatalf("Create(source) error = %v", err)
	}
	sourceID := uuid.MustParse(created.ID)
	if _, err := repo.MarkSourceNeedsReconnect(ctx, projectID, sourceID, "the GitHub connection for this source has expired or was revoked; reconnect the source to continue", nil); err != nil {
		t.Fatalf("MarkSourceNeedsReconnect() error = %v", err)
	}

	before, err := repo.GetCredentialByID(ctx, credentialID, projectID)
	if err != nil {
		t.Fatalf("GetCredentialByID(before) error = %v", err)
	}

	// Fail every source UPDATE after this point, i.e. the second statement of the
	// reconnect. The credential UPDATE is on kb.source_credentials and is
	// unaffected, so the failure really is "after the blob was written".
	if _, err := tdb.Pool.Exec(ctx, `CREATE OR REPLACE FUNCTION public.sources_atomic_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced failure updating a source'; END $$`); err != nil {
		t.Fatalf("create trigger function: %v", err)
	}
	if _, err := tdb.Pool.Exec(ctx, `CREATE TRIGGER sources_atomic_fail BEFORE UPDATE ON kb.sources FOR EACH ROW EXECUTE FUNCTION public.sources_atomic_fail()`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	newBlob, err := svc.encryptCredentials(Credentials{"access_token": "ghp_new"})
	if err != nil {
		t.Fatalf("encryptCredentials() error = %v", err)
	}
	if _, err := repo.ReplaceSavedCredentialGrant(ctx, credentialID, projectID, newBlob, map[string]any{}); err == nil {
		t.Fatal("ReplaceSavedCredentialGrant() error = nil, want the forced failure to surface")
	}

	after, err := repo.GetCredentialByID(ctx, credentialID, projectID)
	if err != nil {
		t.Fatalf("GetCredentialByID(after) error = %v", err)
	}
	if !bytes.Equal(after.EncryptedCredentials, before.EncryptedCredentials) {
		t.Errorf("saved credential was replaced despite the failed reconnect: want the write rolled back")
	}

	got := mustGetSource(t, ctx, repo, projectID, sourceID)
	if got.Status != SourceStatusNeedsReconnect {
		t.Errorf("Status = %q, want the source still parked after the failed reconnect", got.Status)
	}
}
