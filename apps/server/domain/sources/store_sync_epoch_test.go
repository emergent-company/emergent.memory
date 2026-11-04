package sources

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestGrantEpoch_MovesOnlyWhenTheCredentialChanges pins the property the park
// guard depends on (#2425): the epoch names *which grant a source reads*, not when
// its row was last written.
//
// The distinction is load-bearing. A successful run writes the row too —
// `UpdateCredentials` persists the rotated refresh token a rotating authorization
// server (Trale) issues on nearly every use. If rotation moved the epoch, the guard
// would suppress the park for every rotating provider and a dead grant would
// produce a doomed job on every sweep forever, which is the loop #2406 exists to
// end.
func TestGrantEpoch_MovesOnlyWhenTheCredentialChanges(t *testing.T) {
	fx := newParkedSourceFixture(t)
	ctx := context.Background()

	start := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
	if start.CredentialsUpdatedAt.IsZero() {
		t.Fatal("CredentialsUpdatedAt = zero, want the migration default to have supplied one")
	}

	t.Run("a rename leaves it alone", func(t *testing.T) {
		renamed := *start
		renamed.DisplayName = "Renamed"
		if _, err := fx.repo.Update(ctx, &renamed); err != nil {
			t.Fatalf("Update(rename) error = %v", err)
		}
		got := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
		if !got.CredentialsUpdatedAt.Equal(start.CredentialsUpdatedAt) {
			t.Errorf("epoch moved on a rename: %v → %v", start.CredentialsUpdatedAt, got.CredentialsUpdatedAt)
		}
	})

	t.Run("rotating the token leaves it alone", func(t *testing.T) {
		if err := fx.repo.UpdateCredentials(ctx, fx.sourceID, fx.projectID, []byte("rotated-grant")); err != nil {
			t.Fatalf("UpdateCredentials() error = %v", err)
		}
		got := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
		if !got.CredentialsUpdatedAt.Equal(start.CredentialsUpdatedAt) {
			t.Errorf("epoch moved on a token rotation, which the same grant carries forward: %v → %v",
				start.CredentialsUpdatedAt, got.CredentialsUpdatedAt)
		}
	})

	t.Run("a consent moves it", func(t *testing.T) {
		before := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
		if err := fx.repo.ReplaceOAuthGrant(ctx, fx.sourceID, fx.projectID, []byte("consented-grant"), before.Config); err != nil {
			t.Fatalf("ReplaceOAuthGrant() error = %v", err)
		}
		got := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
		if got.CredentialsUpdatedAt.Equal(before.CredentialsUpdatedAt) {
			t.Error("epoch did not move on a consent, so an in-flight job could still park the repaired source")
		}
	})

	t.Run("attaching a credential moves it", func(t *testing.T) {
		before := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
		attached := *before
		attached.EncryptedCredentials = []byte("attached-grant")
		if _, err := fx.repo.Update(ctx, &attached); err != nil {
			t.Fatalf("Update(attach) error = %v", err)
		}
		got := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
		if got.CredentialsUpdatedAt.Equal(before.CredentialsUpdatedAt) {
			t.Error("epoch did not move when a settings save replaced the credential")
		}
	})
}

// TestMarkSourceNeedsReconnect_RefusesAParkFromBeforeTheGrantChanged is the
// repository half of #2425: a job that started before a reconnect and failed after
// it must not park the source the user has just repaired.
func TestMarkSourceNeedsReconnect_RefusesAParkFromBeforeTheGrantChanged(t *testing.T) {
	fx := newParkedSourceFixture(t)
	ctx := context.Background()

	// The snapshot a sync job takes at the start of its run.
	inFlight := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)

	// The user reconnects while that job is still running.
	if err := fx.repo.ReplaceOAuthGrant(ctx, fx.sourceID, fx.projectID, []byte("sealed-new-grant"), inFlight.Config); err != nil {
		t.Fatalf("ReplaceOAuthGrant() error = %v", err)
	}

	applied, err := fx.repo.MarkSourceNeedsReconnect(ctx, fx.projectID, fx.sourceID, driveReconnectRequired, &inFlight.CredentialsUpdatedAt)
	if err != nil {
		t.Fatalf("MarkSourceNeedsReconnect(stale epoch) error = %v", err)
	}
	if applied {
		t.Error("park applied for a grant the source no longer reads, so a reconnect would be undone by the job it interrupted")
	}
	after := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
	if after.Status != SourceStatusActive {
		t.Errorf("Status = %q, want %q after a suppressed park", after.Status, SourceStatusActive)
	}
	if after.LastError != nil {
		t.Errorf("LastError = %q, want nil: nothing may be recorded by a park that did not apply", *after.LastError)
	}

	// A failure against the grant the source reads *now* parks it as before.
	current := after.CredentialsUpdatedAt
	applied, err = fx.repo.MarkSourceNeedsReconnect(ctx, fx.projectID, fx.sourceID, driveReconnectRequired, &current)
	if err != nil {
		t.Fatalf("MarkSourceNeedsReconnect(current epoch) error = %v", err)
	}
	if !applied {
		t.Fatal("park not applied for the current grant")
	}
	parked := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
	if parked.Status != SourceStatusNeedsReconnect {
		t.Errorf("Status = %q, want %q", parked.Status, SourceStatusNeedsReconnect)
	}
	if parked.LastError == nil || *parked.LastError != driveReconnectRequired {
		t.Errorf("LastError = %v, want the recorded failure", parked.LastError)
	}
}

// TestUpdateIntegration_GrantEpochFollowsTheValueNotTheBlob is the service-path
// companion to TestGrantEpoch_MovesOnlyWhenTheCredentialChanges (#2425).
//
// Service.Update re-reads the row with GetByIDWithCredentials, so every settings
// save carries the stored credential blob. A rule keyed on "a blob is present"
// therefore moves the epoch on a *rename* — which would let a rename suppress a
// legitimate park and put back the every-cadence failing sync #2406 removed. The
// discriminator has to be whether the value changed.
func TestUpdateIntegration_GrantEpochFollowsTheValueNotTheBlob(t *testing.T) {
	svc, repo, _ := newCredentialTestService(t, "sourcesepochservice")
	ctx := context.Background()
	projectID := uuid.New()

	created, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:         "Docs",
		ProviderType: ProviderTypeGitHub,
		Config:       map[string]any{"repo": "acme/docs", "access_token": "ghp_original"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	srcID := uuid.MustParse(created.ID)
	start := mustGetSource(t, ctx, repo, projectID, srcID)

	t.Run("a rename leaves the epoch alone", func(t *testing.T) {
		name := "Docs (renamed)"
		if _, err := svc.Update(ctx, projectID, srcID, &UpdateIntegrationRequest{Name: &name}); err != nil {
			t.Fatalf("Update(rename) error = %v", err)
		}
		got := mustGetSource(t, ctx, repo, projectID, srcID)
		if !got.CredentialsUpdatedAt.Equal(start.CredentialsUpdatedAt) {
			t.Errorf("epoch moved on a rename (%v → %v): a rename would then suppress a legitimate park",
				start.CredentialsUpdatedAt, got.CredentialsUpdatedAt)
		}
		if got.DisplayName != name {
			t.Errorf("DisplayName = %q, want %q", got.DisplayName, name)
		}
	})

	t.Run("replacing the token in a settings save moves it", func(t *testing.T) {
		before := mustGetSource(t, ctx, repo, projectID, srcID)
		if _, err := svc.Update(ctx, projectID, srcID, &UpdateIntegrationRequest{
			Config: map[string]any{"repo": "acme/docs", "access_token": "ghp_rotated"},
		}); err != nil {
			t.Fatalf("Update(new token) error = %v", err)
		}
		got := mustGetSource(t, ctx, repo, projectID, srcID)
		if got.CredentialsUpdatedAt.Equal(before.CredentialsUpdatedAt) {
			t.Error("epoch did not move when a settings save replaced the credential")
		}
	})
}

// TestReplaceSavedCredentialGrant_MovesTheEpochForEverySourceOnIt is the #2418
// half of the epoch (#2425): a reconnect of a shared credential replaces the grant
// that every source on it reads, so each of their epochs must move. Otherwise a
// job that started before the reconnect could park a sibling the user has just
// repaired — one park per source, for one repair.
func TestReplaceSavedCredentialGrant_MovesTheEpochForEverySourceOnIt(t *testing.T) {
	svc, repo, _ := newCredentialTestService(t, "sourcesepochsaved")
	ctx := context.Background()
	projectID := uuid.New()

	saved, err := svc.CreateSavedCredential(ctx, projectID, ProviderTypeGitHub, "team PAT", map[string]any{"access_token": "ghp_old"})
	if err != nil {
		t.Fatalf("CreateSavedCredential() error = %v", err)
	}
	credentialID := uuid.MustParse(saved.ID)

	createOnCredential := func(name string) uuid.UUID {
		t.Helper()
		created, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
			Name:              name,
			ProviderType:      ProviderTypeGitHub,
			Config:            map[string]any{"repo": "acme/" + name},
			SavedCredentialID: &saved.ID,
		})
		if err != nil {
			t.Fatalf("Create(%s) error = %v", name, err)
		}
		return uuid.MustParse(created.ID)
	}

	firstID := createOnCredential("first")
	secondID := createOnCredential("second")
	firstBefore := mustGetSource(t, ctx, repo, projectID, firstID)
	secondBefore := mustGetSource(t, ctx, repo, projectID, secondID)

	blob, err := svc.encryptCredentials(Credentials{"access_token": "ghp_new"})
	if err != nil {
		t.Fatalf("encryptCredentials() error = %v", err)
	}
	if _, err := repo.ReplaceSavedCredentialGrant(ctx, credentialID, projectID, blob, map[string]any{}); err != nil {
		t.Fatalf("ReplaceSavedCredentialGrant() error = %v", err)
	}

	cases := []struct {
		name   string
		id     uuid.UUID
		before *Source
	}{
		{"first", firstID, firstBefore},
		{"second", secondID, secondBefore},
	}
	for _, tc := range cases {
		got := mustGetSource(t, ctx, repo, projectID, tc.id)
		if got.CredentialsUpdatedAt.Equal(tc.before.CredentialsUpdatedAt) {
			t.Errorf("%s: epoch did not move when the credential it reads was replaced (%v)",
				tc.name, tc.before.CredentialsUpdatedAt)
		}
		applied, err := repo.MarkSourceNeedsReconnect(ctx, projectID, tc.id, driveReconnectRequired, &tc.before.CredentialsUpdatedAt)
		if err != nil {
			t.Fatalf("%s: MarkSourceNeedsReconnect() error = %v", tc.name, err)
		}
		if applied {
			t.Errorf("%s: parked against a credential the reconnect had already replaced", tc.name)
		}
		if after := mustGetSource(t, ctx, repo, projectID, tc.id); after.Status != SourceStatusActive {
			t.Errorf("%s: Status = %q, want %q", tc.name, after.Status, SourceStatusActive)
		}
	}
}

// TestUpdateSavedCredential_MovesTheEpochOnlyWhenTheSecretIsReplaced covers the
// other writer that changes what a source reads (#2425): editing a saved
// credential. A rename is not a new grant; a new secret is — for every source on
// the credential, not just the one the user happened to be looking at.
func TestUpdateSavedCredential_MovesTheEpochOnlyWhenTheSecretIsReplaced(t *testing.T) {
	svc, repo, _ := newCredentialTestService(t, "sourcesepochcredential")
	ctx := context.Background()
	projectID := uuid.New()

	saved, err := svc.CreateSavedCredential(ctx, projectID, ProviderTypeGitHub, "team PAT", map[string]any{"access_token": "ghp_old"})
	if err != nil {
		t.Fatalf("CreateSavedCredential() error = %v", err)
	}
	credentialID := uuid.MustParse(saved.ID)

	createOnCredential := func(name string) uuid.UUID {
		t.Helper()
		created, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
			Name:              name,
			ProviderType:      ProviderTypeGitHub,
			Config:            map[string]any{"repo": "acme/" + name},
			SavedCredentialID: &saved.ID,
		})
		if err != nil {
			t.Fatalf("Create(%s) error = %v", name, err)
		}
		return uuid.MustParse(created.ID)
	}

	firstID := createOnCredential("first")
	secondID := createOnCredential("second")
	firstBefore := mustGetSource(t, ctx, repo, projectID, firstID)
	secondBefore := mustGetSource(t, ctx, repo, projectID, secondID)

	t.Run("renaming the credential leaves the epoch alone", func(t *testing.T) {
		renamed := "team PAT (renamed)"
		if _, err := svc.UpdateSavedCredential(ctx, projectID, credentialID, &renamed, nil); err != nil {
			t.Fatalf("UpdateSavedCredential(rename) error = %v", err)
		}
		for _, id := range []uuid.UUID{firstID, secondID} {
			got := mustGetSource(t, ctx, repo, projectID, id)
			want := firstBefore
			if id == secondID {
				want = secondBefore
			}
			if !got.CredentialsUpdatedAt.Equal(want.CredentialsUpdatedAt) {
				t.Errorf("source %v: epoch moved on a rename of the shared credential", id)
			}
		}
	})

	t.Run("replacing the secret moves every source's epoch", func(t *testing.T) {
		if _, err := svc.UpdateSavedCredential(ctx, projectID, credentialID, nil, map[string]any{"access_token": "ghp_new"}); err != nil {
			t.Fatalf("UpdateSavedCredential(new secret) error = %v", err)
		}
		for _, id := range []uuid.UUID{firstID, secondID} {
			got := mustGetSource(t, ctx, repo, projectID, id)
			if got.CredentialsUpdatedAt.Equal(firstBefore.CredentialsUpdatedAt) ||
				got.CredentialsUpdatedAt.Equal(secondBefore.CredentialsUpdatedAt) {
				t.Errorf("source %v: epoch did not move when the credential it reads was edited", id)
			}
		}
	})

	t.Run("a park from before the edit is refused", func(t *testing.T) {
		applied, err := repo.MarkSourceNeedsReconnect(ctx, projectID, firstID, driveReconnectRequired, &firstBefore.CredentialsUpdatedAt)
		if err != nil {
			t.Fatalf("MarkSourceNeedsReconnect() error = %v", err)
		}
		if applied {
			t.Error("parked a source whose credential had been replaced by an edit")
		}
		if got := mustGetSource(t, ctx, repo, projectID, firstID); got.Status != SourceStatusActive {
			t.Errorf("Status = %q, want %q", got.Status, SourceStatusActive)
		}
	})
}

// TestUpdateIntegration_GrantEpochMovesWhenTheCredentialIsAttached pins the branch
// of the Update discriminator that a rename cannot reach (#2425): the epoch names
// which grant the source reads, so attaching or detaching a saved credential is a
// change even when no blob is submitted. Without it, a source moved onto a
// repaired credential mid-run could still be parked by a job reading the old one.
func TestUpdateIntegration_GrantEpochMovesWhenTheCredentialIsAttached(t *testing.T) {
	svc, repo, _ := newCredentialTestService(t, "sourcesepochattach")
	ctx := context.Background()
	projectID := uuid.New()

	saved, err := svc.CreateSavedCredential(ctx, projectID, ProviderTypeGitHub, "team PAT", map[string]any{"access_token": "ghp_old"})
	if err != nil {
		t.Fatalf("CreateSavedCredential() error = %v", err)
	}
	credentialID := uuid.MustParse(saved.ID)

	interval := 60
	now := time.Now().UTC()
	created, err := repo.Create(ctx, &Source{
		ID:                   uuid.New(),
		ProjectID:            projectID,
		Provider:             ProviderTypeGitHub,
		DisplayName:          "inline",
		Config:               map[string]any{},
		EncryptedCredentials: []byte("sealed-grant"),
		SyncPolicy:           SyncPolicyScheduled,
		SyncIntervalMinutes:  &interval,
		Cursor:               map[string]any{},
		Status:               SourceStatusActive,
		CreatedAt:            now,
		UpdatedAt:            now,
	})
	if err != nil {
		t.Fatalf("Create(source) error = %v", err)
	}
	sourceID := created.ID

	inline := mustGetSource(t, ctx, repo, projectID, sourceID)
	if inline.CredentialID != nil {
		t.Fatal("CredentialID set on a freshly created inline-credential source")
	}

	attached := *inline
	attached.CredentialID = &credentialID
	attached.EncryptedCredentials = nil
	if _, err := repo.Update(ctx, &attached); err != nil {
		t.Fatalf("Update(attach credential) error = %v", err)
	}
	afterAttach := mustGetSource(t, ctx, repo, projectID, sourceID)
	if afterAttach.CredentialID == nil {
		t.Fatal("CredentialID = nil after attaching a saved credential")
	}
	if afterAttach.CredentialsUpdatedAt.Equal(inline.CredentialsUpdatedAt) {
		t.Error("epoch did not move when the source was switched onto a saved credential")
	}

	detached := *afterAttach
	detached.CredentialID = nil
	if _, err := repo.Update(ctx, &detached); err != nil {
		t.Fatalf("Update(detach credential) error = %v", err)
	}
	afterDetach := mustGetSource(t, ctx, repo, projectID, sourceID)
	if afterDetach.CredentialID != nil {
		t.Fatal("CredentialID set after detaching")
	}
	if afterDetach.CredentialsUpdatedAt.Equal(afterAttach.CredentialsUpdatedAt) {
		t.Error("epoch did not move when the source was switched off a saved credential")
	}
}

// TestMarkSourceNeedsReconnect_DeletedRowIsANoOp covers the other way a guarded
// park reports applied == false (#2425): the row is gone, not repaired. It must
// stay a clean no-op — no error to confuse the worker, and the failure record that
// follows it is equally harmless.
func TestMarkSourceNeedsReconnect_DeletedRowIsANoOp(t *testing.T) {
	fx := newParkedSourceFixture(t)
	ctx := context.Background()

	src := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
	if err := fx.repo.Delete(ctx, fx.sourceID, fx.projectID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	applied, err := fx.repo.MarkSourceNeedsReconnect(ctx, fx.projectID, fx.sourceID, driveReconnectRequired, &src.CredentialsUpdatedAt)
	if err != nil {
		t.Fatalf("MarkSourceNeedsReconnect() on a deleted row error = %v", err)
	}
	if applied {
		t.Error("park reported applied for a source that no longer exists")
	}
	if err := fx.repo.RecordSourceError(ctx, fx.projectID, fx.sourceID, driveReconnectRequired); err != nil {
		t.Errorf("RecordSourceError() on a deleted row error = %v", err)
	}
}
