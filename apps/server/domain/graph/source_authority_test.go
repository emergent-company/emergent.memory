package graph_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
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

	// The generation row for the (otherwise empty) push is still written.
	var generations int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.source_generations WHERE project_id = ? AND source_id = ?`, projectID, sourceB).Scan(ctx, &generations))
	assert.Equal(t, 1, generations)
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

	applied, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
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
