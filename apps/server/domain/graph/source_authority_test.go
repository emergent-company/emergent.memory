package graph_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// source_authority_test.go covers slice 2 of #2376 (issue #152): a caller must
// hold a grant to act as the source it names, and a source-attributed write to
// a row another source owns is refused (or adopted, when a grant covers the
// owner). These are the adversarial tests — the ones that prove the binding is
// real and not just recorded.

// grantSourceTo binds credential (kind, id) to sourceID, using the repository
// insert so the test exercises the same path a management surface would.
func grantSourceTo(t *testing.T, ctx context.Context, repo *graph.Repository, projectID, sourceID uuid.UUID, kind string, id *uuid.UUID) {
	t.Helper()
	require.NoError(t, repo.InsertSourceGrant(ctx, repo.DB(), &graph.SourceGrant{
		ProjectID:      projectID,
		SourceID:       sourceID,
		CredentialKind: kind,
		CredentialID:   id,
		GrantedByType:  graph.ActorUser,
	}))
}

// TestSourceAuthorityRefusesUngrantedCaller is the binding in the negative: a
// credential with no grant for the source it names is refused, and the refused
// push writes nothing — no version row, no contribution, no generation.
func TestSourceAuthorityRefusesUngrantedCaller(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSourceUngranted(t, ctx, db, projectID, "no-grant")

	keyID := uuid.New()
	pushCtx := auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &keyID)

	_, err := svc.ApplyChangeset(pushCtx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada"},
			ExternalID:          "no-grant-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owner_conflict")

	var objects, contributions, generations int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.graph_objects WHERE project_id = ?`, projectID).Scan(ctx, &objects))
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.provenance_contributions WHERE project_id = ?`, projectID).Scan(ctx, &contributions))
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.source_generations WHERE project_id = ?`, projectID).Scan(ctx, &generations))
	assert.Zero(t, objects, "a refused push must write no version row")
	assert.Zero(t, contributions, "a refused push must write no contribution row")
	assert.Zero(t, generations, "a refused push must write no generation row")
}

// TestSourceAuthorityGrantedCallerSucceeds is the positive half: the same push
// succeeds once the credential holds an explicit grant for the source.
func TestSourceAuthorityGrantedCallerSucceeds(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSourceUngranted(t, ctx, db, projectID, "granted")

	keyID := uuid.New()
	grantSourceTo(t, ctx, repo, projectID, sourceID, auth.CredentialAPIKey, &keyID)
	pushCtx := auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &keyID)

	resp, err := svc.ApplyChangeset(pushCtx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada"},
			ExternalID:          "granted-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, graph.ChangesetOutcomeCreated, resp.Results[0].Outcome)
}

// TestSourceAuthorityRevocationRefusesNextWrite: deleting the grant immediately
// refuses the next write, with no new rows for it.
func TestSourceAuthorityRevocationRefusesNextWrite(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSourceUngranted(t, ctx, db, projectID, "revoke")

	keyID := uuid.New()
	grantSourceTo(t, ctx, repo, projectID, sourceID, auth.CredentialAPIKey, &keyID)
	pushCtx := auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &keyID)

	first, err := svc.ApplyChangeset(pushCtx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			ExternalID:          "revoke-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, graph.ChangesetOutcomeCreated, first.Results[0].Outcome)

	require.NoError(t, repo.DeleteSourceGrant(ctx, db, projectID, sourceID, auth.CredentialAPIKey, &keyID))

	_, err = svc.ApplyChangeset(pushCtx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			ExternalID:          "revoke-1",
			ExternalRevisionSeq: seq(2),
		}},
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owner_conflict")
}

// TestSourceAuthorityAdoptionWithoutGrantRefused: a source that already holds a
// grant for itself but none for the row's owner is refused per item — the item
// is not applied, yet the rest of the changeset (and its generation row) still
// commits.
func TestSourceAuthorityAdoptionWithoutGrantRefused(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)
	sourceA := createTestSourceUngranted(t, ctx, db, projectID, "owner")
	sourceB := createTestSourceUngranted(t, ctx, db, projectID, "intruder")

	// A holds an explicit grant for itself and pushes its own record.
	keyA := uuid.New()
	grantSourceTo(t, ctx, repo, projectID, sourceA, auth.CredentialAPIKey, &keyA)
	pushA := auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &keyA)

	applied, err := svc.ApplyChangeset(pushA, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada"},
			ExternalID:          "own-a",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	v1 := *applied.Results[0].SubjectID

	// B contributes a different external id to A's object — the shape a merge
	// union leaves behind — so B's push addresses a row it does not own.
	require.NoError(t, repo.InsertContribution(ctx, db, &graph.ProvenanceContribution{
		ProjectID:           projectID,
		SubjectKind:         graph.ContributionSubjectObject,
		SubjectVersionID:    v1,
		SourceID:            sourceB,
		ExternalID:          "own-b",
		ExternalRevisionSeq: 1,
	}))

	// B holds a grant for itself (passes the changeset-level binding) but no
	// grant for A (the owner), so the item is refused per item.
	keyB := uuid.New()
	grantSourceTo(t, ctx, repo, projectID, sourceB, auth.CredentialAPIKey, &keyB)
	pushB := auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &keyB)

	resp, err := svc.ApplyChangeset(pushB, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceB,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Countess"},
			ExternalID:          "own-b",
			ExternalRevisionSeq: seq(2),
		}},
	}, nil)
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, graph.ChangesetOutcomeRefused, resp.Results[0].Outcome)

	// The head is unchanged and still owned by A.
	var head struct {
		DataClass     string     `bun:"data_class"`
		OwnerSourceID *uuid.UUID `bun:"owner_source_id"`
	}
	require.NoError(t, db.NewRaw(`
		SELECT data_class, owner_source_id FROM kb.graph_objects
		WHERE canonical_id = (SELECT canonical_id FROM kb.graph_objects WHERE id = ?)
		  AND supersedes_id IS NULL`, v1).Scan(ctx, &head.DataClass, &head.OwnerSourceID))
	assert.Equal(t, graph.DataClassSource, head.DataClass)
	require.NotNil(t, head.OwnerSourceID)
	assert.Equal(t, sourceA, *head.OwnerSourceID)

	// The generation row for the (otherwise empty) push is still written, and
	// its item_count records that nothing was applied — the single item was
	// refused, so it must not be counted (changeset.go ItemCount).
	var gen struct {
		ItemCount int `bun:"item_count"`
	}
	require.NoError(t, db.NewRaw(`SELECT item_count FROM kb.source_generations WHERE project_id = ? AND source_id = ?`, projectID, sourceB).Scan(ctx, &gen.ItemCount))
	assert.Equal(t, 0, gen.ItemCount)
}

// TestSourceAuthorityAdoptionWithGrantPreservesPreviousOwner: with a grant for
// the owner, the cross-owner write adopts — the caller becomes the owner with
// data_class 'adopted' and the previous owner's contribution stays present.
func TestSourceAuthorityAdoptionWithGrantPreservesPreviousOwner(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)
	sourceA := createTestSourceUngranted(t, ctx, db, projectID, "prior-owner")
	sourceB := createTestSourceUngranted(t, ctx, db, projectID, "adopter")

	keyA := uuid.New()
	grantSourceTo(t, ctx, repo, projectID, sourceA, auth.CredentialAPIKey, &keyA)
	pushA := auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &keyA)

	applied, err := svc.ApplyChangeset(pushA, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada"},
			ExternalID:          "adopt-a",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	v1 := *applied.Results[0].SubjectID
	canonicalID := *applied.Results[0].CanonicalID

	require.NoError(t, repo.InsertContribution(ctx, db, &graph.ProvenanceContribution{
		ProjectID:           projectID,
		SubjectKind:         graph.ContributionSubjectObject,
		SubjectVersionID:    v1,
		SourceID:            sourceB,
		ExternalID:          "adopt-b",
		ExternalRevisionSeq: 1,
	}))

	// B holds a grant for itself and for A, the row's owner, so the write
	// adopts A's row rather than being refused.
	keyB := uuid.New()
	grantSourceTo(t, ctx, repo, projectID, sourceB, auth.CredentialAPIKey, &keyB)
	grantSourceTo(t, ctx, repo, projectID, sourceA, auth.CredentialAPIKey, &keyB)
	pushB := auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &keyB)

	resp, err := svc.ApplyChangeset(pushB, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceB,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada Lovelace"},
			ExternalID:          "adopt-b",
			ExternalRevisionSeq: seq(2),
		}},
	}, nil)
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, graph.ChangesetOutcomeUpdated, resp.Results[0].Outcome)

	var head struct {
		DataClass     string     `bun:"data_class"`
		OwnerSourceID *uuid.UUID `bun:"owner_source_id"`
	}
	require.NoError(t, db.NewRaw(`
		SELECT data_class, owner_source_id FROM kb.graph_objects
		WHERE canonical_id = ? AND supersedes_id IS NULL`, canonicalID).Scan(ctx, &head.DataClass, &head.OwnerSourceID))
	assert.Equal(t, graph.DataClassAdopted, head.DataClass)
	require.NotNil(t, head.OwnerSourceID)
	assert.Equal(t, sourceB, *head.OwnerSourceID, "the adopter becomes the owner")

	// The previous owner's contribution is still present on the new head.
	var aPresent int
	require.NoError(t, db.NewRaw(`
		SELECT count(*) FROM kb.provenance_contributions
		WHERE project_id = ? AND source_id = ? AND external_id = 'adopt-a' AND state = 'present'`,
		projectID, sourceA).Scan(ctx, &aPresent))
	assert.Equal(t, 1, aPresent, "the previous owner's contribution must survive adoption")

	// The previous owner can no longer write the adopted row: A holds no grant
	// for B, so A's next push is refused per item.
	respA, err := svc.ApplyChangeset(pushA, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Grace"},
			ExternalID:          "adopt-a",
			ExternalRevisionSeq: seq(2),
		}},
	}, nil)
	require.NoError(t, err)
	require.Len(t, respA.Results, 1)
	assert.Equal(t, graph.ChangesetOutcomeRefused, respA.Results[0].Outcome)
}

// TestSourceAuthoritySystemPrincipalNeedsNoGrant: the system principal is the
// explicit internal writer, so it pushes without holding a grant — even for a
// source that has no grant at all.
func TestSourceAuthoritySystemPrincipalNeedsNoGrant(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSourceUngranted(t, ctx, db, projectID, "system")

	sysCtx := auth.WithActor(ctx, graph.ActorSystem, nil)
	resp, err := svc.ApplyChangeset(sysCtx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada"},
			ExternalID:          "sys-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, graph.ChangesetOutcomeCreated, resp.Results[0].Outcome)
}

// TestSourceAuthorityUserEditStillVersions is the no-regression guard: a plain
// user edit of a source-owned row versions it as it does today and never
// consults a grant, so enforcement does not turn an ordinary edit into a
// conflict.
func TestSourceAuthorityUserEditStillVersions(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "user-edit-owner")

	// Push the source-owned row as a concrete credential (the backfill wildcard
	// authorises any project credential, never a caller with no credential).
	keyID := uuid.New()
	pushCtx := auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &keyID)
	applied, err := svc.ApplyChangeset(pushCtx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada"},
			ExternalID:          "user-edit-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	canonicalID := *applied.Results[0].CanonicalID

	// A human edits the object: no source, no grant, a session credential.
	userCtx := auth.ContextWithCredential(ctx, auth.CredentialSession, nil)
	patched, err := svc.Patch(userCtx, projectID, canonicalID, &graph.PatchGraphObjectRequest{
		Properties: map[string]any{"name": "Ada Lovelace"},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 2, patched.Version, "a user edit must version the source-owned row")

	var head struct {
		DataClass     string     `bun:"data_class"`
		OwnerSourceID *uuid.UUID `bun:"owner_source_id"`
	}
	require.NoError(t, db.NewRaw(`
		SELECT data_class, owner_source_id FROM kb.graph_objects
		WHERE canonical_id = ? AND supersedes_id IS NULL`, canonicalID).Scan(ctx, &head.DataClass, &head.OwnerSourceID))
	assert.Equal(t, graph.DataClassSource, head.DataClass, "a user edit must not change the data class")
	require.NotNil(t, head.OwnerSourceID)
	assert.Equal(t, sourceID, *head.OwnerSourceID, "a user edit must not reassign the owner")
}

// TestHasSourceGrantWildcardExcludesNoCredential pins should-fix 1: the backfill
// wildcard (credential_kind='project', credential_id NULL) authorises any
// *project credential*, but never a caller with no credential at all. Background
// or internal work that stamps CredentialNone must not ride a backfill wildcard.
func TestHasSourceGrantWildcardExcludesNoCredential(t *testing.T) {
	ctx, db, repo, _, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "wildcard-none") // carries the backfill wildcard

	keyID := uuid.New()
	ok, err := repo.HasSourceGrant(ctx, db, projectID, sourceID, auth.CredentialAPIKey, &keyID)
	require.NoError(t, err)
	assert.True(t, ok, "a concrete project credential satisfies the wildcard")

	ok, err = repo.HasSourceGrant(ctx, db, projectID, sourceID, auth.CredentialNone, nil)
	require.NoError(t, err)
	assert.False(t, ok, "a caller with no credential must not ride the wildcard")
}

// TestSourceGrantRejectsNullCredentialIDForConcreteKind pins should-fix 2: the
// 00265 CHECK (credential_kind = 'project' OR credential_id IS NOT NULL) forbids
// the hidden-wildcard row — a ('session', NULL) or ('api_key', NULL) grant that
// would authorise every session / every api_key.
func TestSourceGrantRejectsNullCredentialIDForConcreteKind(t *testing.T) {
	ctx, db, _, _, projectID := setupProvenanceTest(t)
	sourceID := createTestSourceUngranted(t, ctx, db, projectID, "no-null-session")

	_, err := db.ExecContext(ctx, `
		INSERT INTO kb.source_grants (project_id, source_id, credential_kind, credential_id, granted_by_type)
		VALUES (?, ?, 'session', NULL, 'system')
	`, projectID, sourceID)
	require.Error(t, err, "a ('session', NULL) hidden wildcard must be rejected by the 00265 CHECK")
}

// TestSourceAuthorityGrantDoesNotCrossProjects: a grant is project-scoped — the
// same credential that holds a grant in project A is still refused when it
// pushes a source it has no grant for in project B.
func TestSourceAuthorityGrantDoesNotCrossProjects(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)

	otherOrgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, otherOrgID, "Other Org"))
	otherProjectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID: otherProjectID, OrgID: otherOrgID, Name: "Other Project",
	}, testutil.AdminUser.ID))
	otherPID := uuid.MustParse(otherProjectID)

	keyID := uuid.New()
	sourceA := createTestSourceUngranted(t, ctx, db, projectID, "scoped-a")
	grantSourceTo(t, ctx, repo, projectID, sourceA, auth.CredentialAPIKey, &keyID)

	sourceB := createTestSourceUngranted(t, ctx, db, otherPID, "scoped-b")
	pushCtx := auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &keyID)

	_, err := svc.ApplyChangeset(pushCtx, otherPID, &graph.ApplyChangesetRequest{
		SourceID: sourceB,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			ExternalID:          "scoped-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owner_conflict")
}

// TestSourceAuthorityOnlySystemActorExempts: the system exemption is the only
// actor-based bypass. A forged non-system actor — an agent stamp, or the HTTP
// fallback user id — does not satisfy the grant check; only a context-stamped
// system principal does (TestSourceAuthoritySystemPrincipalNeedsNoGrant).
func TestSourceAuthorityOnlySystemActorExempts(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSourceUngranted(t, ctx, db, projectID, "forged-actor")

	// An agent actor (which a client can never become) still needs a grant.
	agentID := uuid.New()
	agentCtx := auth.WithActor(ctx, graph.ActorAgent, &agentID)
	_, err := svc.ApplyChangeset(agentCtx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			ExternalID:          "forged-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owner_conflict")

	// The HTTP fallback user id is likewise not the system principal.
	userID := uuid.New()
	_, err = svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			ExternalID:          "forged-2",
			ExternalRevisionSeq: seq(1),
		}},
	}, &userID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owner_conflict")
}

// TestSourceAuthorityWildcardAllowsDifferentCredential: a pre-existing source
// (with the migration backfill's wildcard grant) may be pushed as by any project
// credential — including one that never created it — because the backfill is
// "any project credential", not "the credential that created the source".
func TestSourceAuthorityWildcardAllowsDifferentCredential(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "wildcard") // carries the backfill wildcard

	keyID := uuid.New()
	pushCtx := auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &keyID)

	resp, err := svc.ApplyChangeset(pushCtx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			ExternalID:          "wildcard-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, graph.ChangesetOutcomeCreated, resp.Results[0].Outcome)
}
