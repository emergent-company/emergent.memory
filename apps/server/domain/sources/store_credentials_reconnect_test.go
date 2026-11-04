package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestReplaceSavedCredentialGrant_UnparksEverySourceOnTheCredential pins the two
// effects a reconnect of a saved-credential source must have (#2418): the new blob
// lands on the credential the sync actually reads, and every parked source on that
// credential is returned to active — a parked source is never admitted by the
// scheduler, so a sibling left parked could not notice the repair.
func TestReplaceSavedCredentialGrant_UnparksEverySourceOnTheCredential(t *testing.T) {
	svc, repo, _ := newCredentialTestService(t, "savedcredreconnect")
	ctx := context.Background()
	projectID := uuid.New()

	saved, err := svc.CreateSavedCredential(ctx, projectID, ProviderTypeGitHub, "team PAT", map[string]any{"access_token": "ghp_old"})
	if err != nil {
		t.Fatalf("CreateSavedCredential() error = %v", err)
	}
	credentialID := uuid.MustParse(saved.ID)

	createOnCredential := func(name, account string) uuid.UUID {
		t.Helper()
		created, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
			Name:              name,
			ProviderType:      ProviderTypeGitHub,
			Config:            map[string]any{"repo": "acme/" + name, oauthAccountEmailKey: account},
			SavedCredentialID: &saved.ID,
		})
		if err != nil {
			t.Fatalf("Create(%s) error = %v", name, err)
		}
		return uuid.MustParse(created.ID)
	}

	parkedID := createOnCredential("parked", "old@example.com")
	failingID := createOnCredential("failing", "old@example.com")
	if _, err := repo.MarkSourceNeedsReconnect(ctx, projectID, parkedID, "the GitHub connection for this source has expired or was revoked; reconnect the source to continue", nil); err != nil {
		t.Fatalf("MarkSourceNeedsReconnect() error = %v", err)
	}
	if err := repo.RecordSourceError(ctx, projectID, failingID, "github: 503 Service Unavailable"); err != nil {
		t.Fatalf("RecordSourceError() error = %v", err)
	}

	// A second credential, with its own parked source, must not be touched: the
	// new grant belongs to one credential only.
	other, err := svc.CreateSavedCredential(ctx, projectID, ProviderTypeGitHub, "other PAT", map[string]any{"access_token": "ghp_other"})
	if err != nil {
		t.Fatalf("CreateSavedCredential(other) error = %v", err)
	}
	otherCreated, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:              "other",
		ProviderType:      ProviderTypeGitHub,
		Config:            map[string]any{"repo": "acme/other"},
		SavedCredentialID: &other.ID,
	})
	if err != nil {
		t.Fatalf("Create(other) error = %v", err)
	}
	otherID := uuid.MustParse(otherCreated.ID)
	if _, err := repo.MarkSourceNeedsReconnect(ctx, projectID, otherID, "other dead grant; reconnect the source to continue", nil); err != nil {
		t.Fatalf("MarkSourceNeedsReconnect(other) error = %v", err)
	}

	newBlob, err := svc.encryptCredentials(Credentials{"access_token": "ghp_new"})
	if err != nil {
		t.Fatalf("encryptCredentials() error = %v", err)
	}
	grantMeta := map[string]any{oauthGrantedScopesKey: []any{"repo"}, oauthGrantedStrategyKey: "readonly"}
	if _, err := repo.ReplaceSavedCredentialGrant(ctx, credentialID, projectID, newBlob, grantMeta); err != nil {
		t.Fatalf("ReplaceSavedCredentialGrant() error = %v", err)
	}

	cred, err := repo.GetCredentialByID(ctx, credentialID, projectID)
	if err != nil {
		t.Fatalf("GetCredentialByID() error = %v", err)
	}
	if string(cred.EncryptedCredentials) != string(newBlob) {
		t.Errorf("saved credential blob = %q, want the new grant", cred.EncryptedCredentials)
	}
	creds, err := svc.credentialsForSource(ctx, &Source{CredentialID: &credentialID, ProjectID: projectID, Provider: ProviderTypeGitHub})
	if err != nil {
		t.Fatalf("credentialsForSource() error = %v", err)
	}
	if creds["access_token"] != "ghp_new" {
		t.Errorf("credentialsForSource() = %v, want the new grant the sync would read", creds)
	}

	t.Run("parked source is reactivated and its failure cleared", func(t *testing.T) {
		got := mustGetSource(t, ctx, repo, projectID, parkedID)
		if got.Status != SourceStatusActive {
			t.Errorf("Status = %q, want %q", got.Status, SourceStatusActive)
		}
		if got.LastError != nil {
			t.Errorf("LastError = %q, want it cleared with the park", *got.LastError)
		}
		if got.ErrorCount != 0 {
			t.Errorf("ErrorCount = %d, want 0", got.ErrorCount)
		}
		// The card must describe the account that consented now, not the one the
		// dead grant belonged to — and a value the exchange could not supply is
		// removed rather than left behind.
		if _, present := got.Config[oauthAccountEmailKey]; present {
			t.Errorf("config[%s] = %v, want it removed when the new grant has no account", oauthAccountEmailKey, got.Config[oauthAccountEmailKey])
		}
		if got.Config[oauthGrantedStrategyKey] != "readonly" {
			t.Errorf("config[%s] = %v, want readonly", oauthGrantedStrategyKey, got.Config[oauthGrantedStrategyKey])
		}
	})

	t.Run("active source keeps the error its last sync recorded", func(t *testing.T) {
		got := mustGetSource(t, ctx, repo, projectID, failingID)
		if got.Status != SourceStatusActive {
			t.Errorf("Status = %q, want it left alone", got.Status)
		}
		if got.LastError == nil || *got.LastError != "github: 503 Service Unavailable" {
			t.Errorf("LastError = %v, want the live error preserved", got.LastError)
		}
		if got.ErrorCount != 1 {
			t.Errorf("ErrorCount = %d, want 1", got.ErrorCount)
		}
	})

	t.Run("a source on another credential is untouched", func(t *testing.T) {
		got := mustGetSource(t, ctx, repo, projectID, otherID)
		if got.Status != SourceStatusNeedsReconnect {
			t.Errorf("Status = %q, want it still parked", got.Status)
		}
		if got.LastError == nil {
			t.Error("LastError = nil, want its own failure kept")
		}
	})
}

// TestCompleteOAuth_ReconnectInstallsTheGrantOnTheSavedCredential is the
// end-to-end shape of #2418: a source created from a saved credential that is
// reconnected must read the new grant on its next sync, not the dead one — which is
// what used to happen, so the reconnect un-parked the source and the next sync
// parked it again. GitHub is the provider this can actually happen to: it is the
// one that offers both an OAuth connect and a saved-credential connect.
func TestCompleteOAuth_ReconnectInstallsTheGrantOnTheSavedCredential(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcesreconnectsaved")
	t.Cleanup(tdb.Close)

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"ghp_new","token_type":"bearer","scope":"repo"}`))
	}))
	defer tokenSrv.Close()

	svc := newReconnectTestService(t, tdb, tokenSrv.URL, tokenSrv.Client())
	projectID := uuid.New()

	blob, err := svc.encryptCredentials(Credentials{"access_token": "ghp_old"})
	if err != nil {
		t.Fatalf("encryptCredentials() error = %v", err)
	}
	saved, err := svc.repo.CreateCredential(ctx, &SourceCredential{
		ID:                   uuid.New(),
		ProjectID:            projectID,
		Provider:             ProviderTypeGitHub,
		Name:                 "Team PAT",
		EncryptedCredentials: blob,
	})
	if err != nil {
		t.Fatalf("CreateCredential() error = %v", err)
	}
	savedID := saved.ID.String()

	created, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:              "Docs",
		ProviderType:      ProviderTypeGitHub,
		Config:            map[string]any{"repo": "acme/docs"},
		SavedCredentialID: &savedID,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	srcID := uuid.MustParse(created.ID)
	if _, err := svc.repo.MarkSourceNeedsReconnect(ctx, projectID, srcID, "the GitHub connection for this source has expired or was revoked; reconnect the source to continue", nil); err != nil {
		t.Fatalf("MarkSourceNeedsReconnect() error = %v", err)
	}

	start, err := svc.StartOAuth(ctx, projectID, ProviderTypeGitHub, StartOAuthRequest{ReconnectSourceID: &srcID})
	if err != nil {
		t.Fatalf("StartOAuth(reconnect) error = %v", err)
	}
	if _, err := svc.CompleteOAuth(ctx, ProviderTypeGitHub, authorizeState(t, start.AuthorizeURL), "code-1"); err != nil {
		t.Fatalf("CompleteOAuth(reconnect) error = %v", err)
	}

	row, err := svc.repo.GetByIDWithCredentials(ctx, srcID, projectID)
	if err != nil {
		t.Fatalf("GetByIDWithCredentials() error = %v", err)
	}
	if row.Status != SourceStatusActive {
		t.Errorf("Status = %q, want active after the reconnect", row.Status)
	}
	if row.LastError != nil {
		t.Errorf("LastError = %q, want it cleared", *row.LastError)
	}
	if len(row.EncryptedCredentials) != 0 {
		t.Errorf("source kept an inline credential copy (%d bytes); the sync reads the saved credential", len(row.EncryptedCredentials))
	}
	creds, err := svc.credentialsForSource(ctx, row)
	if err != nil {
		t.Fatalf("credentialsForSource() error = %v", err)
	}
	if creds["access_token"] != "ghp_new" {
		t.Errorf("sync would read access_token = %v, want ghp_new", creds["access_token"])
	}
}

// TestReplaceSavedCredentialGrant_EmptyMetadataAndPausedSibling covers the two
// shapes the first version got wrong in review: a nil grant-metadata map, and a
// sibling the operator had paused.
//
// A nil map marshals to `null`, and `config || 'null'::jsonb` turns the source's
// config object into an array, after which every read of that source fails to
// unmarshal — an unrepairable row (config is NOT NULL). A paused source must keep
// its pause: one account's consent restores access, it does not reverse an
// operator's decision to stop syncing, which is also the rule ReplaceOAuthGrant
// follows for the source it repairs.
func TestReplaceSavedCredentialGrant_EmptyMetadataAndPausedSibling(t *testing.T) {
	svc, repo, _ := newCredentialTestService(t, "savedcredreconnectsiblings")
	ctx := context.Background()
	projectID := uuid.New()

	saved, err := svc.CreateSavedCredential(ctx, projectID, ProviderTypeGitHub, "team PAT", map[string]any{"access_token": "ghp_old"})
	if err != nil {
		t.Fatalf("CreateSavedCredential() error = %v", err)
	}
	credentialID := uuid.MustParse(saved.ID)

	created, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:              "paused repo",
		ProviderType:      ProviderTypeGitHub,
		Config:            map[string]any{"repo": "acme/paused"},
		SavedCredentialID: &saved.ID,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	srcID := uuid.MustParse(created.ID)
	if _, err := repo.MarkSourceNeedsReconnect(ctx, projectID, srcID, "the GitHub connection for this source has expired or was revoked; reconnect the source to continue", nil); err != nil {
		t.Fatalf("MarkSourceNeedsReconnect() error = %v", err)
	}
	if err := repo.UpdateStatus(ctx, srcID, projectID, SourceStatusDisabled); err != nil {
		t.Fatalf("UpdateStatus(disabled) error = %v", err)
	}

	newBlob, err := svc.encryptCredentials(Credentials{"access_token": "ghp_new"})
	if err != nil {
		t.Fatalf("encryptCredentials() error = %v", err)
	}
	if _, err := repo.ReplaceSavedCredentialGrant(ctx, credentialID, projectID, newBlob, nil); err != nil {
		t.Fatalf("ReplaceSavedCredentialGrant(nil metadata) error = %v", err)
	}

	got := mustGetSource(t, ctx, repo, projectID, srcID)
	// A successful scan is the assertion: the null-merge damage surfaces as a
	// jsonb array that cannot unmarshal into the config map.
	if got.Config == nil || got.Config["repo"] != "acme/paused" {
		t.Errorf("config after an empty-metadata replace = %v, want the source's own keys intact", got.Config)
	}
	if got.Status != SourceStatusDisabled {
		t.Errorf("Status = %q, want the pause kept", got.Status)
	}
	if got.LastError == nil {
		t.Error("LastError = nil, want the failure kept while paused")
	}
	creds, err := svc.credentialsForSource(ctx, got)
	if err != nil {
		t.Fatalf("credentialsForSource() error = %v", err)
	}
	if creds["access_token"] != "ghp_new" {
		t.Errorf("credentialsForSource() token = %v, want the new grant", creds["access_token"])
	}
}
