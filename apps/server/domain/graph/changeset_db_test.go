package graph_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// changeset_db_test.go covers the source write path: a changeset of upserts and
// withdrawals addressed by the source's own external ids and revision sequences
// (issue #20, archived #2462).
//
// The fixtures write the `kb.sources` row the changeset must name, because the
// source is a project-scoped domain parameter of the request rather than a
// write-stamp field.

func createTestSource(t *testing.T, ctx context.Context, db bun.IDB, projectID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.ExecContext(ctx, `
		INSERT INTO kb.sources (id, project_id, provider, display_name)
		VALUES (?, ?, 'test-provider', ?)
	`, id, projectID, name)
	require.NoError(t, err)
	return id
}

func seq(v int64) *int64 { return &v }

func resultFor(t *testing.T, resp *graph.ApplyChangesetResponse, externalID string) graph.ChangesetItemResult {
	t.Helper()
	for _, r := range resp.Results {
		if r.ExternalID == externalID {
			return r
		}
	}
	t.Fatalf("no result for external_id %q in %+v", externalID, resp.Results)
	return graph.ChangesetItemResult{}
}

type contributionRow struct {
	SourceID            uuid.UUID  `bun:"source_id"`
	SubjectVersionID    uuid.UUID  `bun:"subject_version_id"`
	State               string     `bun:"state"`
	ExternalRevisionSeq int64      `bun:"external_revision_seq"`
	BranchID            *uuid.UUID `bun:"branch_id"`
	WithdrawnAt         *string    `bun:"withdrawn_at"`
	MergedFrom          *uuid.UUID `bun:"merged_from_version_id"`
}

func contributionsFor(t *testing.T, ctx context.Context, db bun.IDB, sourceID uuid.UUID, externalID string) []contributionRow {
	t.Helper()
	var rows []contributionRow
	require.NoError(t, db.NewRaw(`
		SELECT source_id, subject_version_id, state, external_revision_seq, branch_id, withdrawn_at::text AS withdrawn_at
		FROM kb.provenance_contributions
		WHERE source_id = ? AND external_id = ?
		ORDER BY first_seen_at ASC, id ASC
	`, sourceID, externalID).Scan(ctx, &rows))
	return rows
}

// contributionsByVersion keys a contribution set by the version it sits on.
// contributionsFor orders by first_seen_at, but an inherited row keeps the
// first_seen_at of the version it came from, so the old and new rows tie and
// the id tiebreak is a random UUID. Tests that care which version a row is on
// must match on identity rather than slice position.
func contributionsByVersion(t *testing.T, rows []contributionRow, versionIDs ...uuid.UUID) map[uuid.UUID]contributionRow {
	t.Helper()
	byVersion := make(map[uuid.UUID]contributionRow, len(rows))
	for _, row := range rows {
		byVersion[row.SubjectVersionID] = row
	}
	require.Len(t, byVersion, len(versionIDs), "contributions must cover each expected version exactly once")
	for _, id := range versionIDs {
		require.Contains(t, byVersion, id, "no contribution row on version %s", id)
	}
	return byVersion
}

// TestApplyChangesetCreatesSourceOwnedObject is the create half: a first push
// makes an object the source owns and a present contribution pointing at the
// version it arrived on.
func TestApplyChangesetCreatesSourceOwnedObject(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "create-half")

	resp, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada"},
			ExternalID:          "person-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, graph.ChangesetOutcomeCreated, resp.Results[0].Outcome)
	assert.Equal(t, int64(1), resp.Generation)

	var obj struct {
		ID            uuid.UUID  `bun:"id"`
		Version       int        `bun:"version"`
		DataClass     string     `bun:"data_class"`
		OwnerSourceID *uuid.UUID `bun:"owner_source_id"`
	}
	require.NoError(t, db.NewRaw(`
		SELECT id, version, data_class, owner_source_id
		FROM kb.graph_objects WHERE id = ?
	`, *resp.Results[0].SubjectID).Scan(ctx, &obj.ID, &obj.Version, &obj.DataClass, &obj.OwnerSourceID))

	assert.Equal(t, graph.DataClassSource, obj.DataClass)
	require.NotNil(t, obj.OwnerSourceID)
	assert.Equal(t, sourceID, *obj.OwnerSourceID)

	contribs := contributionsFor(t, ctx, db, sourceID, "person-1")
	require.Len(t, contribs, 1)
	assert.Equal(t, graph.ContributionStatePresent, contribs[0].State)
	assert.Equal(t, obj.ID, contribs[0].SubjectVersionID)
	assert.Equal(t, int64(1), contribs[0].ExternalRevisionSeq)

	// The read side returns the contribution in one call.
	read, err := svc.GetObjectContributions(ctx, projectID, obj.ID, true)
	require.NoError(t, err)
	require.Len(t, read.Contributions, 1)
	assert.Equal(t, "person-1", read.Contributions[0].ExternalID)
}

// TestApplyChangesetRePushVersionsAndMovesContribution is the update half: a
// higher revision sequence writes version N+1 on the same object and moves the
// contribution onto it, superseding — never deleting — the row it left behind.
func TestApplyChangesetRePushVersionsAndMovesContribution(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "update-half")

	first, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada"},
			ExternalID:          "person-2",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	objectID := *first.Results[0].SubjectID
	canonicalID := *first.Results[0].CanonicalID

	second, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada Lovelace"},
			ExternalID:          "person-2",
			ExternalRevision:    strPtrCS("rev-2"),
			ExternalRevisionSeq: seq(2),
		}},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, graph.ChangesetOutcomeUpdated, second.Results[0].Outcome)
	assert.Equal(t, 2, *second.Results[0].Version)
	assert.Equal(t, canonicalID, *second.Results[0].CanonicalID)
	assert.NotEqual(t, objectID, *second.Results[0].SubjectID)

	// One object, two versions, one HEAD.
	var heads int
	require.NoError(t, db.NewRaw(`
		SELECT count(*) FROM kb.graph_objects
		WHERE project_id = ? AND canonical_id = ? AND supersedes_id IS NULL
	`, projectID, canonicalID).Scan(ctx, &heads))
	assert.Equal(t, 1, heads)

	contribs := contributionsFor(t, ctx, db, sourceID, "person-2")
	require.Len(t, contribs, 2)
	newVersionID := *second.Results[0].SubjectID
	byVersion := contributionsByVersion(t, contribs, objectID, newVersionID)
	assert.Equal(t, graph.ContributionStateSuperseded, byVersion[objectID].State)
	assert.Equal(t, graph.ContributionStatePresent, byVersion[newVersionID].State)
	assert.Equal(t, int64(2), byVersion[newVersionID].ExternalRevisionSeq)
}

// TestApplyChangesetStaleRevisionIsSkipped: the opaque revision string never
// orders anything; a push whose numeric sequence is not greater than the stored
// one writes nothing, which is what makes a retry safe.
func TestApplyChangesetStaleRevisionIsSkipped(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "stale")

	newer, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Newer"},
			ExternalID:          "person-3",
			ExternalRevisionSeq: seq(5),
		}},
	}, nil)
	require.NoError(t, err)

	for _, staleSeq := range []int64{5, 3} {
		stale, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
			SourceID: sourceID,
			Objects: []graph.ChangesetObjectItem{{
				Type:                "Person",
				Properties:          map[string]any{"name": "Older"},
				ExternalID:          "person-3",
				ExternalRevisionSeq: seq(staleSeq),
			}},
		}, nil)
		require.NoError(t, err)
		res := resultFor(t, stale, "person-3")
		assert.Equal(t, graph.ChangesetOutcomeSkipped, res.Outcome)
		assert.NotEmpty(t, res.Reason)

		// Still the version the newer push wrote.
		var version int
		require.NoError(t, db.NewRaw(`SELECT version FROM kb.graph_objects WHERE id = ?`, *newer.Results[0].SubjectID).Scan(ctx, &version))
		assert.Equal(t, 1, version)
	}

	// And the stored properties were never overwritten.
	var name string
	require.NoError(t, db.NewRaw(
		`SELECT properties->>'name' FROM kb.graph_objects WHERE id = ?`, *newer.Results[0].SubjectID,
	).Scan(ctx, &name))
	assert.Equal(t, "Newer", name)
}

// TestApplyChangesetRollsBackOnRejectedItem: one bad item rejects the whole
// changeset — no object, no contribution, no generation survives.
func TestApplyChangesetRollsBackOnRejectedItem(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "rollback")

	key := "taken-key"
	_, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Key:                 &key,
			ExternalID:          "seed-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)

	var contributionsBefore int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.provenance_contributions WHERE project_id = ?`, projectID).Scan(ctx, &contributionsBefore))

	// The second item collides on (project, type, key) inside the transaction.
	_, err = svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{
			{Type: "Person", ExternalID: "fresh-1", ExternalRevisionSeq: seq(1)},
			{Type: "Person", Key: &key, ExternalID: "collides", ExternalRevisionSeq: seq(1)},
		},
	}, nil)
	require.Error(t, err)

	var contributionsAfter, freshObjects, generations int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.provenance_contributions WHERE project_id = ?`, projectID).Scan(ctx, &contributionsAfter))
	// The seed carries the key; both changeset items either lack one (fresh-1)
	// or collide on it, so any keyless row proves the first item leaked.
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.graph_objects WHERE project_id = ? AND key IS NULL`, projectID).Scan(ctx, &freshObjects))
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.source_generations WHERE project_id = ?`, projectID).Scan(ctx, &generations))

	assert.Equal(t, contributionsBefore, contributionsAfter, "the failed changeset left a contribution behind")
	assert.Equal(t, 0, freshObjects, "the failed changeset left the first object behind")
	assert.Equal(t, 1, generations, "only the successful seed changeset may record a generation")
}

// TestApplyChangesetReplayedGenerationConflicts: naming a generation makes the
// push replayable as a unit, and a replay must not apply a second time.
func TestApplyChangesetReplayedGenerationConflicts(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "generation")

	gen := int64(7)
	req := &graph.ApplyChangesetRequest{
		SourceID:   sourceID,
		Generation: &gen,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			ExternalID:          "person-4",
			ExternalRevisionSeq: seq(1),
		}},
	}
	first, err := svc.ApplyChangeset(ctx, projectID, req, nil)
	require.NoError(t, err)
	assert.Equal(t, gen, first.Generation)

	_, err = svc.ApplyChangeset(ctx, projectID, req, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already been applied")

	var objects int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.graph_objects WHERE project_id = ?`, projectID).Scan(ctx, &objects))
	assert.Equal(t, 1, objects)
}

// TestApplyChangesetWithdrawalLeavesTheOtherContribution: two sources back one
// object; withdrawing one leaves the object readable and the other contribution
// present, and records when the withdrawal happened.
func TestApplyChangesetWithdrawalLeavesTheOtherContribution(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)
	sourceA := createTestSource(t, ctx, db, projectID, "withdraw-a")
	sourceB := createTestSource(t, ctx, db, projectID, "withdraw-b")

	applied, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Shared"},
			ExternalID:          "shared-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	objectID := *applied.Results[0].SubjectID

	// A contribution from a second source on the same version, as a merge with
	// an explicit owner would produce it.
	require.NoError(t, repo.InsertContribution(ctx, db, &graph.ProvenanceContribution{
		ProjectID:           projectID,
		SubjectKind:         graph.ContributionSubjectObject,
		SubjectVersionID:    objectID,
		SourceID:            sourceB,
		ExternalID:          "shared-1-b",
		ExternalRevisionSeq: 1,
	}))

	withdrawn, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID:    sourceA,
		Withdrawals: []graph.ChangesetWithdrawalItem{{ExternalID: "shared-1"}},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, graph.ChangesetOutcomeWithdrawn, withdrawn.Results[0].Outcome)

	contribsA := contributionsFor(t, ctx, db, sourceA, "shared-1")
	require.Len(t, contribsA, 1)
	assert.Equal(t, graph.ContributionStateWithdrawn, contribsA[0].State)
	require.NotNil(t, contribsA[0].WithdrawnAt, "withdrawn_at must record when")

	contribsB := contributionsFor(t, ctx, db, sourceB, "shared-1-b")
	require.Len(t, contribsB, 1)
	assert.Equal(t, graph.ContributionStatePresent, contribsB[0].State)

	// The object itself is untouched.
	var objects int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.graph_objects WHERE id = ?`, objectID).Scan(ctx, &objects))
	assert.Equal(t, 1, objects)

	// Withdrawing an unknown record is reported per item, not an error.
	unknown, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID:    sourceA,
		Withdrawals: []graph.ChangesetWithdrawalItem{{ExternalID: "never-pushed"}},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, graph.ChangesetOutcomeNotFound, unknown.Results[0].Outcome)
}

// TestApplyChangesetResolvesRefsAndCreatesRelationship covers the in-changeset
// reference resolution and the relationship half of the write path.
func TestApplyChangesetResolvesRefsAndCreatesRelationship(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "refs")

	resp, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{
			{Ref: "ada", Type: "Person", ExternalID: "ref-ada", ExternalRevisionSeq: seq(1)},
			{Ref: "bob", Type: "Person", ExternalID: "ref-bob", ExternalRevisionSeq: seq(1)},
		},
		Relationships: []graph.ChangesetRelationshipItem{{
			Type:                "KNOWS",
			SrcRef:              "ada",
			DstRef:              "bob",
			ExternalID:          "knows-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	require.Len(t, resp.Results, 3)
	assert.Equal(t, graph.ChangesetOutcomeCreated, resultFor(t, resp, "knows-1").Outcome)

	relID := *resultFor(t, resp, "knows-1").SubjectID
	contribs := contributionsFor(t, ctx, db, sourceID, "knows-1")
	require.Len(t, contribs, 1)
	assert.Equal(t, graph.ContributionStatePresent, contribs[0].State)
	assert.Equal(t, relID, contribs[0].SubjectVersionID)

	// An unresolved reference rejects the whole changeset.
	_, err = svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects:  []graph.ChangesetObjectItem{{Ref: "x", Type: "Person", ExternalID: "ref-x", ExternalRevisionSeq: seq(1)}},
		Relationships: []graph.ChangesetRelationshipItem{{
			Type: "KNOWS", SrcRef: "x", DstRef: "missing", ExternalID: "knows-2", ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in objects")

	var leaked int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.provenance_contributions WHERE project_id = ? AND external_id = 'ref-x'`, projectID).Scan(ctx, &leaked))
	assert.Equal(t, 0, leaked, "the rejected changeset left its object behind")
}

// TestApplyChangesetRejectsOverCapAndForeignSource: the cap is named in the
// error, and a source from another project cannot be pushed for.
func TestApplyChangesetRejectsOverCapAndForeignSource(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "cap")

	const capLimit = 500 // matches changesetMaxObjectItems
	objects := make([]graph.ChangesetObjectItem, 0, capLimit+1)
	for i := 0; i <= capLimit; i++ {
		objects = append(objects, graph.ChangesetObjectItem{
			Type:                "Person",
			ExternalID:          uuid.NewString(),
			ExternalRevisionSeq: seq(1),
		})
	}
	_, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{SourceID: sourceID, Objects: objects}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not exceed")

	otherProject := uuid.New()
	var foreign uuid.UUID
	require.NoError(t, db.NewRaw(`
		INSERT INTO kb.sources (id, project_id, provider) VALUES (?, ?, 'other')
		RETURNING id
	`, uuid.New(), otherProject).Scan(ctx, &foreign))

	_, err = svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: foreign,
		Objects:  []graph.ChangesetObjectItem{{Type: "Person", ExternalID: "foreign-1", ExternalRevisionSeq: seq(1)}},
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source not found")
}

func strPtrCS(s string) *string { return &s }

// mainBranchContributions returns the present contributions on the main line
// keyed by source, which is how a merged object reports who backs it.
func mainBranchContributions(t *testing.T, ctx context.Context, db bun.IDB, projectID uuid.UUID, externalID string) map[uuid.UUID]int64 {
	t.Helper()
	var rows []struct {
		SourceID uuid.UUID `bun:"source_id"`
		Seq      int64     `bun:"external_revision_seq"`
	}
	require.NoError(t, db.NewRaw(`
		SELECT source_id, external_revision_seq
		FROM kb.provenance_contributions
		WHERE project_id = ? AND branch_id IS NULL AND external_id = ? AND state = 'present'`,
		projectID, externalID).Scan(ctx, &rows))
	out := make(map[uuid.UUID]int64, len(rows))
	for _, r := range rows {
		out[r.SourceID] = r.Seq
	}
	return out
}

// TestForkAndMergeCarryContributions covers the two structural copies: forking
// a branch carries contributions onto the fork's own rows, and merging carries
// the union of both sides onto the version the merge writes — for an object the
// branch revised and for one it introduced.
func TestForkAndMergeCarryContributions(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)
	sourceA := createTestSource(t, ctx, db, projectID, "merge-a")
	sourceB := createTestSource(t, ctx, db, projectID, "merge-b")

	applied, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Grace"},
			ExternalID:          "merge-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	mainVersion := *applied.Results[0].SubjectID
	canonicalID := *applied.Results[0].CanonicalID

	forkResp, err := svc.ForkBranch(ctx, projectID, nil, &graph.ForkBranchRequest{Name: "merge-contrib"})
	require.NoError(t, err)
	branchID := uuid.MustParse(forkResp.BranchID)

	// The fork copy carries the contribution, on the fork's own branch, and
	// remembers the row it was copied from.
	var forkCopyVersion uuid.UUID
	var forkCopyFrom *uuid.UUID
	require.NoError(t, db.NewRaw(`
		SELECT id FROM kb.graph_objects
		WHERE project_id = ? AND branch_id = ? AND canonical_id = ? AND supersedes_id IS NULL`,
		projectID, branchID, canonicalID).Scan(ctx, &forkCopyVersion))
	require.NoError(t, db.NewRaw(`
		SELECT merged_from_version_id FROM kb.provenance_contributions
		WHERE project_id = ? AND branch_id = ? AND source_id = ? AND external_id = 'merge-1'`,
		projectID, branchID, sourceA).Scan(ctx, &forkCopyFrom))
	require.NotNil(t, forkCopyFrom)
	assert.Equal(t, mainVersion, *forkCopyFrom)

	// On the branch the object accrues a second source, and source A revises
	// its own record.
	require.NoError(t, repo.InsertContribution(ctx, db, &graph.ProvenanceContribution{
		ProjectID:           projectID,
		BranchID:            &branchID,
		SubjectKind:         graph.ContributionSubjectObject,
		SubjectVersionID:    forkCopyVersion,
		SourceID:            sourceB,
		ExternalID:          "merge-1-b",
		ExternalRevisionSeq: 1,
	}))
	branchApplied, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		BranchID: &branchID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Grace Hopper"},
			ExternalID:          "merge-1",
			ExternalRevisionSeq: seq(2),
		}},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, graph.ChangesetOutcomeUpdated, branchApplied.Results[0].Outcome)
	branchVersion := *branchApplied.Results[0].SubjectID

	// The revision by source A did not evict source B: its contribution moved
	// onto the new branch version, with the version it arrived on recorded.
	var carriedForward int
	require.NoError(t, db.NewRaw(`
		SELECT count(*) FROM kb.provenance_contributions
		WHERE subject_version_id = ? AND source_id = ? AND state = 'present' AND merged_from_version_id IS NOT NULL`,
		branchVersion, sourceB).Scan(ctx, &carriedForward))
	assert.Equal(t, 1, carriedForward, "a revision by one source must not evict another source")

	// An object that exists only on the branch arrives as an added clone.
	_, err = svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceB,
		BranchID: &branchID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			ExternalID:          "merge-added",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)

	_, err = svc.MergeBranch(ctx, projectID, nil, &graph.BranchMergeRequest{
		SourceBranchID: branchID,
		Execute:        true,
		Policy:         "enrich_no_sim",
	})
	require.NoError(t, err)

	// The fast-forwarded object keeps the union of both sides on the main HEAD.
	var mergedVersion uuid.UUID
	require.NoError(t, db.NewRaw(`
		SELECT id FROM kb.graph_objects
		WHERE project_id = ? AND canonical_id = ? AND branch_id IS NULL AND supersedes_id IS NULL`,
		projectID, canonicalID).Scan(ctx, &mergedVersion))
	assert.NotEqual(t, branchVersion, mergedVersion, "the branch version must not be reused as the main HEAD")

	var onMerged []contributionRow
	require.NoError(t, db.NewRaw(`
		SELECT source_id, subject_version_id, state, external_revision_seq, branch_id, merged_from_version_id
		FROM kb.provenance_contributions
		WHERE subject_version_id = ? AND state = 'present'`,
		mergedVersion).Scan(ctx, &onMerged))
	require.Len(t, onMerged, 2, "both sources must be present on the main HEAD")
	bySource := map[uuid.UUID]int64{}
	for _, c := range onMerged {
		bySource[c.SourceID] = c.ExternalRevisionSeq
		require.NotNil(t, c.MergedFrom, "a merged contribution records the version it arrived on")
	}
	assert.Equal(t, map[uuid.UUID]int64{sourceA: 2, sourceB: 1}, bySource)

	// Nothing is left present on the superseded versions.
	var stillPresent int
	require.NoError(t, db.NewRaw(`
		SELECT count(*) FROM kb.provenance_contributions
		WHERE project_id = ? AND subject_version_id <> ?
		  AND state = 'present' AND branch_id IS NULL`,
		projectID, mergedVersion).Scan(ctx, &stillPresent))
	assert.Equal(t, 1, stillPresent, "only the added clone's contribution stays present besides the merged object's")

	// The added clone carries its source too.
	assert.Equal(t, map[uuid.UUID]int64{sourceB: 1}, mainBranchContributions(t, ctx, db, projectID, "merge-added"))
}

// TestConflictResolvedMergeUnionsContributions covers the third merge path: when
// both sides revised the same object since the fork, the resolved version still
// reports every source of both sides.
func TestConflictResolvedMergeUnionsContributions(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)
	sourceA := createTestSource(t, ctx, db, projectID, "conflict-a")
	sourceB := createTestSource(t, ctx, db, projectID, "conflict-b")

	applied, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Base"},
			ExternalID:          "conflict-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	canonicalID := *applied.Results[0].CanonicalID

	forkResp, err := svc.ForkBranch(ctx, projectID, nil, &graph.ForkBranchRequest{Name: "conflict-contrib"})
	require.NoError(t, err)
	branchID := uuid.MustParse(forkResp.BranchID)

	branchApplied, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		BranchID: &branchID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Branch side"},
			ExternalID:          "conflict-1",
			ExternalRevisionSeq: seq(2),
		}},
	}, nil)
	require.NoError(t, err)
	branchVersion := *branchApplied.Results[0].SubjectID

	require.NoError(t, repo.InsertContribution(ctx, db, &graph.ProvenanceContribution{
		ProjectID:           projectID,
		BranchID:            &branchID,
		SubjectKind:         graph.ContributionSubjectObject,
		SubjectVersionID:    branchVersion,
		SourceID:            sourceB,
		ExternalID:          "conflict-1-b",
		ExternalRevisionSeq: 1,
	}))

	// The main line moves on independently, so the merge cannot fast-forward.
	_, err = svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Main side"},
			ExternalID:          "conflict-1",
			ExternalRevisionSeq: seq(2),
		}},
	}, nil)
	require.NoError(t, err)

	merge, err := svc.MergeBranch(ctx, projectID, nil, &graph.BranchMergeRequest{
		SourceBranchID: branchID,
		Execute:        true,
		Policy:         "enrich_no_sim",
	})
	require.NoError(t, err)
	require.NotZero(t, merge.ConflictCount, "both sides changed, so the merge must resolve a conflict")
	require.True(t, merge.Applied)

	var mergedVersion uuid.UUID
	require.NoError(t, db.NewRaw(`
		SELECT id FROM kb.graph_objects
		WHERE project_id = ? AND canonical_id = ? AND branch_id IS NULL AND supersedes_id IS NULL`,
		projectID, canonicalID).Scan(ctx, &mergedVersion))
	require.NotEqual(t, branchVersion, mergedVersion)

	var onMerged []contributionRow
	require.NoError(t, db.NewRaw(`
		SELECT source_id, subject_version_id, state, external_revision_seq, branch_id, merged_from_version_id
		FROM kb.provenance_contributions
		WHERE subject_version_id = ? AND state = 'present'`, mergedVersion).Scan(ctx, &onMerged))
	require.Len(t, onMerged, 2, "the resolved version must report both sources")
	bySource := map[uuid.UUID]bool{}
	for _, c := range onMerged {
		bySource[c.SourceID] = true
		require.NotNil(t, c.MergedFrom, "a merged contribution records the version it arrived on")
	}
	assert.True(t, bySource[sourceA] && bySource[sourceB], "both sources must survive the conflict resolution")
}

// similarityVector is a fixed 768-dimension vector, so two rows can be given the
// same embedding and the merge's similarity probe sees distance zero.
func similarityVector() string {
	parts := make([]string, 768)
	for i := range parts {
		parts[i] = "0.01"
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// TestSimilarityAbsorbedMergeUnionsContributions covers the fourth merge path: a
// source object that matches an existing target object semantically rather than
// by canonical id is absorbed into it, and the absorbed version reports every
// record of both sides.
//
// Both objects are owned by the same source, which is what allows the absorb:
// two different owners are refused (see the test below).
func TestSimilarityAbsorbedMergeUnionsContributions(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)
	sourceA := createTestSource(t, ctx, db, projectID, "sim-a")
	sourceB := createTestSource(t, ctx, db, projectID, "sim-b")

	target, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada"},
			ExternalID:          "sim-target",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	targetVersion := *target.Results[0].SubjectID
	targetCanonical := *target.Results[0].CanonicalID

	// A second source also backs the target.
	require.NoError(t, repo.InsertContribution(ctx, db, &graph.ProvenanceContribution{
		ProjectID:           projectID,
		SubjectKind:         graph.ContributionSubjectObject,
		SubjectVersionID:    targetVersion,
		SourceID:            sourceB,
		ExternalID:          "sim-target-b",
		ExternalRevisionSeq: 1,
	}))

	forkResp, err := svc.ForkBranch(ctx, projectID, nil, &graph.ForkBranchRequest{Name: "sim-contrib"})
	require.NoError(t, err)
	branchID := uuid.MustParse(forkResp.BranchID)

	intruder, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		BranchID: &branchID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"nickname": "the countess"},
			ExternalID:          "sim-source",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	sourceVersion := *intruder.Results[0].SubjectID

	vec := similarityVector()
	_, err = db.NewRaw(`
		UPDATE kb.graph_objects SET embedding_v2 = ?::vector WHERE id IN (?, ?)`,
		vec, targetVersion, sourceVersion).Exec(ctx)
	require.NoError(t, err)

	merge, err := svc.MergeBranch(ctx, projectID, nil, &graph.BranchMergeRequest{
		SourceBranchID: branchID,
		Execute:        true,
		Policy:         "enrich",
	})
	require.NoError(t, err)
	require.Equal(t, 1, merge.SimilarCount, "the branch object must be matched by similarity")

	var mergedVersion uuid.UUID
	require.NoError(t, db.NewRaw(`
		SELECT id FROM kb.graph_objects
		WHERE project_id = ? AND canonical_id = ? AND branch_id IS NULL AND supersedes_id IS NULL`,
		projectID, targetCanonical).Scan(ctx, &mergedVersion))
	assert.NotEqual(t, targetVersion, mergedVersion, "the absorb must write a new version")

	var onMerged []contributionRow
	require.NoError(t, db.NewRaw(`
		SELECT source_id, subject_version_id, state, external_revision_seq, branch_id, merged_from_version_id
		FROM kb.provenance_contributions
		WHERE subject_version_id = ? AND state = 'present'`, mergedVersion).Scan(ctx, &onMerged))
	// One row per source, as the version row allows.
	require.Len(t, onMerged, 2, "the absorbed version must report both sources")
	bySource := map[uuid.UUID]bool{}
	for _, c := range onMerged {
		bySource[c.SourceID] = true
		require.NotNil(t, c.MergedFrom, "a merged contribution records the version it arrived on")
	}
	assert.True(t, bySource[sourceA] && bySource[sourceB], "both sources must survive the absorb")
}

// TestMergeOfDifferentSourceOwnersIsRefused: an absorb that would join two
// objects owned by different sources is refused rather than silently picking an
// owner, and it writes no contribution.
func TestMergeOfDifferentSourceOwnersIsRefused(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceA := createTestSource(t, ctx, db, projectID, "own-a")
	sourceB := createTestSource(t, ctx, db, projectID, "own-b")

	target, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada"},
			ExternalID:          "own-target",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	targetVersion := *target.Results[0].SubjectID

	forkResp, err := svc.ForkBranch(ctx, projectID, nil, &graph.ForkBranchRequest{Name: "own-contrib"})
	require.NoError(t, err)
	branchID := uuid.MustParse(forkResp.BranchID)

	intruder, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceB,
		BranchID: &branchID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"nickname": "the countess"},
			ExternalID:          "own-source",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	sourceVersion := *intruder.Results[0].SubjectID

	vec := similarityVector()
	_, err = db.NewRaw(`
		UPDATE kb.graph_objects SET embedding_v2 = ?::vector WHERE id IN (?, ?)`,
		vec, targetVersion, sourceVersion).Exec(ctx)
	require.NoError(t, err)

	var contributionsBefore int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.provenance_contributions WHERE project_id = ?`, projectID).Scan(ctx, &contributionsBefore))

	_, err = svc.MergeBranch(ctx, projectID, nil, &graph.BranchMergeRequest{
		SourceBranchID: branchID,
		Execute:        true,
		Policy:         "enrich",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owner_conflict")

	var contributionsAfter int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.provenance_contributions WHERE project_id = ?`, projectID).Scan(ctx, &contributionsAfter))
	assert.Equal(t, contributionsBefore, contributionsAfter, "a refused merge must write no contribution")

	var head uuid.UUID
	require.NoError(t, db.NewRaw(`SELECT id FROM kb.graph_objects WHERE id = ?`, targetVersion).Scan(ctx, &head))
	assert.Equal(t, targetVersion, head, "the target must be unchanged")
}

// TestRePushRecordsTheSecondWritersStamp is the regression for a version that
// inherited the superseded row's write stamp: version 2 of a record pushed with
// a second key must name that key and its run, not the first writer's.
func TestRePushRecordsTheSecondWritersStamp(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "stamp")

	keyA, keyB := uuid.New(), uuid.New()
	runA, runB := uuid.New(), uuid.New()
	agentA, agentB := uuid.New(), uuid.New()

	ctxA := auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &keyA)
	ctxA = auth.ContextWithRunIDs(ctxA, &runA, &runA)
	ctxA = auth.WithActor(ctxA, graph.ActorAgent, &agentA)

	first, err := svc.ApplyChangeset(ctxA, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "First"},
			ExternalID:          "stamp-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	firstVersion := *first.Results[0].SubjectID

	ctxB := auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &keyB)
	ctxB = auth.ContextWithRunIDs(ctxB, &runB, &runB)
	ctxB = auth.WithActor(ctxB, graph.ActorAgent, &agentB)

	second, err := svc.ApplyChangeset(ctxB, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Second"},
			ExternalID:          "stamp-1",
			ExternalRevisionSeq: seq(2),
		}},
	}, nil)
	require.NoError(t, err)
	secondVersion := *second.Results[0].SubjectID

	type stamp struct {
		CredentialKind string     `bun:"credential_kind"`
		CredentialID   *uuid.UUID `bun:"credential_id"`
		RunID          *uuid.UUID `bun:"run_id"`
		RootRunID      *uuid.UUID `bun:"root_run_id"`
		ActorType      *string    `bun:"actor_type"`
		ActorID        *uuid.UUID `bun:"actor_id"`
	}
	readStamp := func(id uuid.UUID) stamp {
		var s stamp
		require.NoError(t, db.NewRaw(`
			SELECT credential_kind, credential_id, run_id, root_run_id, actor_type, actor_id
			FROM kb.graph_objects WHERE id = ?`, id).Scan(ctx, &s.CredentialKind, &s.CredentialID, &s.RunID, &s.RootRunID, &s.ActorType, &s.ActorID))
		return s
	}

	v1 := readStamp(firstVersion)
	require.NotNil(t, v1.CredentialID)
	assert.Equal(t, keyA, *v1.CredentialID)
	require.NotNil(t, v1.RunID)
	assert.Equal(t, runA, *v1.RunID)

	v2 := readStamp(secondVersion)
	assert.Equal(t, auth.CredentialAPIKey, v2.CredentialKind)
	require.NotNil(t, v2.CredentialID)
	assert.Equal(t, keyB, *v2.CredentialID, "version 2 must name the key that pushed it")
	require.NotNil(t, v2.RunID)
	assert.Equal(t, runB, *v2.RunID, "version 2 must name the run it was pushed in")
	require.NotNil(t, v2.RootRunID)
	assert.Equal(t, runB, *v2.RootRunID)
	require.NotNil(t, v2.ActorID)
	assert.Equal(t, agentB, *v2.ActorID)
}

// TestPushAfterMergeAddressesTheBranchCopy: a merge must not take the branch's
// contribution away from it, or the next push on that branch would fail to find
// its anchor and create a duplicate object.
func TestPushAfterMergeAddressesTheBranchCopy(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "post-merge")

	created, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "One"},
			ExternalID:          "post-merge-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	canonicalID := *created.Results[0].CanonicalID

	forkResp, err := svc.ForkBranch(ctx, projectID, nil, &graph.ForkBranchRequest{Name: "post-merge"})
	require.NoError(t, err)
	branchID := uuid.MustParse(forkResp.BranchID)

	onBranch, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		BranchID: &branchID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Two"},
			ExternalID:          "post-merge-1",
			ExternalRevisionSeq: seq(2),
		}},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, graph.ChangesetOutcomeUpdated, onBranch.Results[0].Outcome)

	_, err = svc.MergeBranch(ctx, projectID, nil, &graph.BranchMergeRequest{
		SourceBranchID: branchID,
		Execute:        true,
		Policy:         "enrich_no_sim",
	})
	require.NoError(t, err)

	// The branch still owns its record: a further push addresses the copy.
	after, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		BranchID: &branchID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Three"},
			ExternalID:          "post-merge-1",
			ExternalRevisionSeq: seq(3),
		}},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, graph.ChangesetOutcomeUpdated, after.Results[0].Outcome, "the branch push must version its copy, not create a second object")

	var branchObjects int
	require.NoError(t, db.NewRaw(`
		SELECT count(*) FROM kb.graph_objects
		WHERE project_id = ? AND canonical_id = ? AND branch_id = ? AND supersedes_id IS NULL`,
		projectID, canonicalID, branchID).Scan(ctx, &branchObjects))
	assert.Equal(t, 1, branchObjects, "one branch object, not a duplicate")
	assert.Equal(t, 3, *after.Results[0].Version)
}

// TestCrossBranchRelationshipEndpointIsRejected: a relationship item that
// resolves an existing endpoint on another branch fails the changeset.
func TestCrossBranchRelationshipEndpointIsRejected(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "cross-branch")

	mainObj, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type: "Person", ExternalID: "main-endpoint", ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	mainID := *mainObj.Results[0].SubjectID

	forkResp, err := svc.ForkBranch(ctx, projectID, nil, &graph.ForkBranchRequest{Name: "cross-branch"})
	require.NoError(t, err)
	branchID := uuid.MustParse(forkResp.BranchID)

	_, err = svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		BranchID: &branchID,
		Objects: []graph.ChangesetObjectItem{{
			Ref: "local", Type: "Person", ExternalID: "branch-endpoint", ExternalRevisionSeq: seq(1),
		}},
		Relationships: []graph.ChangesetRelationshipItem{{
			Type: "KNOWS", SrcRef: "local", DstID: &mainID, ExternalID: "cross-rel", ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changeset's branch")

	var leaked int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.provenance_contributions WHERE project_id = ? AND external_id IN ('branch-endpoint', 'cross-rel')`, projectID).Scan(ctx, &leaked))
	assert.Equal(t, 0, leaked, "the rejected changeset must write nothing")
}

// TestApplyLeavesSourcesArrayIntact: the apply path never rewrites `_sources`,
// so a document-extracted object keeps the array it arrived with.
func TestApplyLeavesSourcesArrayIntact(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "sources-array")

	created, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Alice"},
			ExternalID:          "src-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	objectID := *created.Results[0].SubjectID

	sources := `[{"document_id":"11111111-1111-1111-1111-111111111111","title":"original.txt"}]`
	_, err = db.NewRaw(`UPDATE kb.graph_objects SET properties = properties || jsonb_build_object('_sources', ?::jsonb) WHERE id = ?`, sources, objectID).Exec(ctx)
	require.NoError(t, err)

	_, err = svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Alicia"},
			ExternalID:          "src-1",
			ExternalRevisionSeq: seq(2),
		}},
	}, nil)
	require.NoError(t, err)

	var kept string
	require.NoError(t, db.NewRaw(`
		SELECT properties->'_sources'::text FROM kb.graph_objects
		WHERE project_id = ? AND canonical_id = (
			SELECT canonical_id FROM kb.graph_objects WHERE id = ?
		) AND supersedes_id IS NULL`, projectID, objectID).Scan(ctx, &kept))
	assert.JSONEq(t, sources, kept, "the apply path must leave _sources exactly as it found it")

	var contributionsFromSources int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.provenance_contributions WHERE project_id = ?`, projectID).Scan(ctx, &contributionsFromSources))
	assert.Equal(t, 2, contributionsFromSources, "one contribution per version, none derived from _sources")
}

// TestWithdrawalIsIdempotent: withdrawing twice reports the second attempt as
// not found and leaves the recorded withdrawal time alone.
func TestWithdrawalIsIdempotent(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "double-withdraw")

	_, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type: "Person", ExternalID: "double-1", ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)

	first, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID:    sourceID,
		Withdrawals: []graph.ChangesetWithdrawalItem{{ExternalID: "double-1"}},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, graph.ChangesetOutcomeWithdrawn, first.Results[0].Outcome)

	var withdrawnAt string
	require.NoError(t, db.NewRaw(`SELECT withdrawn_at::text FROM kb.provenance_contributions WHERE project_id = ? AND external_id = 'double-1'`, projectID).Scan(ctx, &withdrawnAt))

	second, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID:    sourceID,
		Withdrawals: []graph.ChangesetWithdrawalItem{{ExternalID: "double-1"}},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, graph.ChangesetOutcomeNotFound, second.Results[0].Outcome)

	var withdrawnAtAfter string
	require.NoError(t, db.NewRaw(`SELECT withdrawn_at::text FROM kb.provenance_contributions WHERE project_id = ? AND external_id = 'double-1'`, projectID).Scan(ctx, &withdrawnAtAfter))
	assert.Equal(t, withdrawnAt, withdrawnAtAfter, "a repeat withdrawal must not move the recorded time")
}

// TestPatchKeepsContributions is #112: an ordinary edit of a contributed object
// mints a version that inherits the object's live contributions. Before this,
// PATCH went through Repository.CreateVersion, which carried data_class and
// owner_source_id forward but not the contribution rows — so HEAD reported zero
// contributors and the other sources' rows stayed `present` on the superseded
// version, orphaned for good (the next source push versions from the
// contribution-less HEAD and carries nothing forward).
func TestPatchKeepsContributions(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)
	sourceA := createTestSource(t, ctx, db, projectID, "patch-a")
	sourceB := createTestSource(t, ctx, db, projectID, "patch-b")

	applied, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada"},
			ExternalID:          "patch-1",
			ExternalRevisionSeq: seq(3),
		}},
	}, nil)
	require.NoError(t, err)
	v1 := *applied.Results[0].SubjectID
	canonicalID := *applied.Results[0].CanonicalID

	// A second source backs the same version — the shape a merge union leaves
	// behind, without needing a branch to produce it here.
	require.NoError(t, repo.InsertContribution(ctx, db, &graph.ProvenanceContribution{
		ProjectID:           projectID,
		SubjectKind:         graph.ContributionSubjectObject,
		SubjectVersionID:    v1,
		SourceID:            sourceB,
		ExternalID:          "patch-1-b",
		ExternalRevisionSeq: 5,
	}))

	patched, err := svc.Patch(ctx, projectID, canonicalID, &graph.PatchGraphObjectRequest{
		Properties: map[string]any{"name": "Ada Lovelace"},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 2, patched.Version)
	v2 := patched.ID

	var rows []contributionRow
	require.NoError(t, db.NewRaw(`
		SELECT source_id, subject_version_id, state, external_revision_seq, branch_id, merged_from_version_id
		FROM kb.provenance_contributions
		WHERE project_id = ? AND subject_kind = 'object'`, projectID).Scan(ctx, &rows))

	present := map[uuid.UUID]contributionRow{}
	for _, row := range rows {
		if row.State == graph.ContributionStatePresent {
			present[row.SourceID] = row
		}
	}
	require.Len(t, present, 2, "an ordinary edit must not empty the object's provenance")
	assert.Equal(t, v2, present[sourceA].SubjectVersionID)
	assert.Equal(t, v2, present[sourceB].SubjectVersionID)
	assert.Equal(t, int64(3), present[sourceA].ExternalRevisionSeq, "the inherited row keeps its own revision")
	assert.Equal(t, int64(5), present[sourceB].ExternalRevisionSeq, "the other source's record is carried, not rewritten")

	var superseded int
	require.NoError(t, db.NewRaw(`
		SELECT count(*) FROM kb.provenance_contributions
		WHERE subject_version_id = ? AND state = 'superseded'`, v1).Scan(ctx, &superseded))
	assert.Equal(t, 2, superseded, "the superseded version keeps its rows as history")

	// The HEAD read a client gets agrees with the table.
	read, err := svc.GetObjectContributions(ctx, projectID, v2, true)
	require.NoError(t, err)
	require.Len(t, read.Contributions, 2)

	// A later revision by source A replaces its own inherited row and leaves
	// source B's alone: one present row per (version, source), no duplicate.
	revised, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada L."},
			ExternalID:          "patch-1",
			ExternalRevisionSeq: seq(4),
		}},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, graph.ChangesetOutcomeUpdated, revised.Results[0].Outcome)
	v3 := *revised.Results[0].SubjectID

	var onV3 []contributionRow
	require.NoError(t, db.NewRaw(`
		SELECT source_id, subject_version_id, state, external_revision_seq, branch_id, merged_from_version_id
		FROM kb.provenance_contributions
		WHERE subject_version_id = ? AND state = 'present'`, v3).Scan(ctx, &onV3))
	require.Len(t, onV3, 2, "one present row per source on the new version")
	seqBySource := map[uuid.UUID]int64{}
	for _, row := range onV3 {
		seqBySource[row.SourceID] = row.ExternalRevisionSeq
	}
	assert.Equal(t, map[uuid.UUID]int64{sourceA: 4, sourceB: 5}, seqBySource)
}

// TestPatchRelationshipKeepsContributions is the relationship half of #112: a
// relationship patched after a source pushed it keeps its contribution on the
// new version, and the version it arrived on stays superseded rather than
// disappearing.
func TestPatchRelationshipKeepsContributions(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "patch-rel")

	applied, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{
			{Ref: "a", Type: "Person", ExternalID: "patch-rel-a", ExternalRevisionSeq: seq(1)},
			{Ref: "b", Type: "Person", ExternalID: "patch-rel-b", ExternalRevisionSeq: seq(1)},
		},
		Relationships: []graph.ChangesetRelationshipItem{{
			Type:                "KNOWS",
			SrcRef:              "a",
			DstRef:              "b",
			ExternalID:          "patch-rel-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	relID := *resultFor(t, applied, "patch-rel-1").SubjectID

	weight := float32(0.75)
	patched, err := svc.PatchRelationship(ctx, projectID, relID, &graph.PatchGraphRelationshipRequest{
		Weight: &weight,
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 2, patched.Version)

	contribs := contributionsFor(t, ctx, db, sourceID, "patch-rel-1")
	require.Len(t, contribs, 2)
	byVersion := contributionsByVersion(t, contribs, relID, patched.ID)
	assert.Equal(t, graph.ContributionStateSuperseded, byVersion[relID].State, "the version it arrived on keeps its row as history")
	assert.Equal(t, graph.ContributionStatePresent, byVersion[patched.ID].State, "the relationship edit must keep its contribution")
}
