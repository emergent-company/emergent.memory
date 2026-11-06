package graph_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// contribution_occupant_db_test.go covers the destination-address guard in
// copyContributions (#125): the behaviour on the main line, and the cost of the
// lookup that implements it.
//
// The guard exists because the addressing indexes
// (uq_provenance_contributions_present_main / _branch) allow one present row per
// (project, branch, subject_kind, source_id, external_id): a cross-branch copy
// whose address the destination already answers must be skipped, not inserted.

// branchPushAndMainPushForTheSameRecord creates the collision the guard handles:
// the source pushes a record on main, and a branch (which existed before it) owns
// a *different* object for the same source + external id.
func branchPushAndMainPushForTheSameRecord(
	t *testing.T, ctx context.Context, db bun.IDB, repo *graph.Repository, svc *graph.Service,
	projectID uuid.UUID, sourceID uuid.UUID, branchName, externalID string,
) (branchID uuid.UUID, mainVersion, mainCanonical, branchVersion uuid.UUID) {
	t.Helper()

	branch := &graph.Branch{ID: uuid.New(), ProjectID: projectID, Name: branchName}
	require.NoError(t, repo.CreateBranch(ctx, branch))
	branchID = branch.ID

	mainApplied, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "main owner"},
			ExternalID:          externalID,
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	mainVersion = *mainApplied.Results[0].SubjectID
	mainCanonical = *mainApplied.Results[0].CanonicalID

	branchApplied, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		BranchID: &branchID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "branch copy"},
			ExternalID:          externalID,
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	branchVersion = *branchApplied.Results[0].SubjectID
	branchCanonical := *branchApplied.Results[0].CanonicalID

	require.NotEqual(t, mainCanonical, branchCanonical,
		"the branch must own a different object, or the merge revisions main instead of adding a clone")
	return branchID, mainVersion, mainCanonical, branchVersion
}

// TestMergeIntoMainSkipsRecordMainAlreadyCarries is the main-line counterpart of
// TestBranchTombstoneSkipsRecordTheBranchAlreadyCarries. The destination is nil,
// so the address index in play is uq_provenance_contributions_present_main —
// a different index with a different predicate than the branch case.
//
// The merge adds the branch's object to main as a clone. Its contribution names
// an address main already answers, so the guard must skip the copy: the merge
// succeeds, main keeps exactly one present contribution for the address, and
// that row stays attributed to the object that owns it.
func TestMergeIntoMainSkipsRecordMainAlreadyCarries(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "merge-main-guard")

	branchID, mainVersion, mainCanonical, branchVersion := branchPushAndMainPushForTheSameRecord(
		t, ctx, db, repo, svc, projectID, sourceID, "main-guard", "main-guard-1")

	_, err := svc.MergeBranch(ctx, projectID, nil, &graph.BranchMergeRequest{
		SourceBranchID: branchID,
		Execute:        true,
		Policy:         "enrich_no_sim",
	})
	require.NoError(t, err, "the merge must skip the colliding copy instead of violating uq_provenance_contributions_present_main")

	// Exactly one present row on main for the address, still the main object's.
	var held []contributionRow
	require.NoError(t, db.NewRaw(`
		SELECT source_id, subject_version_id, state, external_revision_seq, branch_id, merged_from_version_id
		FROM kb.provenance_contributions
		WHERE project_id = ? AND subject_kind = 'object' AND branch_id IS NULL
		  AND source_id = ? AND external_id = ? AND state = 'present'`,
		projectID, sourceID, "main-guard-1").Scan(ctx, &held))
	require.Len(t, held, 1, "main must address the record exactly once")

	var ownerCanonical uuid.UUID
	require.NoError(t, db.NewRaw(`SELECT canonical_id FROM kb.graph_objects WHERE id = ?`,
		held[0].SubjectVersionID).Scan(ctx, &ownerCanonical))
	assert.Equal(t, mainCanonical, ownerCanonical, "the live row must stay attributed to the object that owns the address")
	assert.Equal(t, mainVersion, held[0].SubjectVersionID, "the main head keeps its own contribution")

	// The branch's object did land on main (as a clone), it just did not take
	// the address away from its owner. The merge stamps the source object with
	// the canonical id of the clone it created.
	var cloneCanonical *uuid.UUID
	require.NoError(t, db.NewRaw(`SELECT merged_to_canonical_id FROM kb.graph_objects WHERE id = ?`,
		branchVersion).Scan(ctx, &cloneCanonical))
	require.NotNil(t, cloneCanonical, "the merge must have applied the branch object as a clone")

	var cloneHead uuid.UUID
	require.NoError(t, db.NewRaw(`
		SELECT id FROM kb.graph_objects
		WHERE project_id = ? AND branch_id IS NULL AND canonical_id = ? AND supersedes_id IS NULL`,
		projectID, *cloneCanonical).Scan(ctx, &cloneHead))

	var clonePresent int
	require.NoError(t, db.NewRaw(`
		SELECT count(*) FROM kb.provenance_contributions
		WHERE project_id = ? AND branch_id IS NULL AND subject_version_id = ? AND state = 'present'`,
		projectID, cloneHead).Scan(ctx, &clonePresent))
	assert.Equal(t, 0, clonePresent, "the skipped copy must not leave the clone addressing the record too")
}

// occupantSelectHook captures the address-lookup SELECTs copyContributions
// issues during a merge. `subject_version_id <>` is the lookup's own predicate:
// it is the only contribution SELECT that excludes the destination version.
type occupantSelectHook struct {
	queries []string
}

func (h *occupantSelectHook) BeforeQuery(ctx context.Context, event *bun.QueryEvent) context.Context {
	if event.Operation() == "SELECT" &&
		strings.Contains(event.Query, "provenance_contributions") &&
		strings.Contains(event.Query, "subject_version_id <>") {
		h.queries = append(h.queries, event.Query)
	}
	return ctx
}

func (h *occupantSelectHook) AfterQuery(ctx context.Context, event *bun.QueryEvent) {}

// seedUnrelatedPresentContributions writes n present contributions on the
// destination branch that no merge in this test will ever address.
func seedUnrelatedPresentContributions(
	t *testing.T, ctx context.Context, db bun.IDB,
	projectID, sourceID uuid.UUID, branchID *uuid.UUID, n int, prefix string,
) {
	t.Helper()
	_, err := db.NewRaw(`
		INSERT INTO kb.provenance_contributions
			(project_id, branch_id, subject_kind, subject_version_id, source_id,
			 external_id, external_revision, external_revision_seq, state)
		SELECT ?, ?, 'object', public.uuid_generate_v4(), ?, ? || '-' || g, '', 1, 'present'
		FROM generate_series(1, ?) AS g`, projectID, branchID, sourceID, prefix, n).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`ANALYZE kb.provenance_contributions`).Exec(ctx)
	require.NoError(t, err)
}

type planNode struct {
	RelationName        string     `json:"Relation Name"`
	ActualRows          float64    `json:"Actual Rows"`
	ActualLoops         float64    `json:"Actual Loops"`
	RowsRemovedByFilter float64    `json:"Rows Removed by Filter"`
	Plans               []planNode `json:"Plans"`
}

type planResult struct {
	Plan planNode `json:"Plan"`
}

// rowsInspectedOn returns how many provenance_contributions rows the plan
// actually read: rows produced by the scan plus rows it had to discard. A full
// branch scan shows up here whether the planner used a sequential scan or walked
// the whole index.
func rowsInspectedOn(node planNode, relation string) float64 {
	loops := node.ActualLoops
	if loops == 0 {
		loops = 1
	}
	total := 0.0
	if node.RelationName == relation {
		total += (node.ActualRows + node.RowsRemovedByFilter) * loops
	}
	for _, child := range node.Plans {
		total += rowsInspectedOn(child, relation)
	}
	return total
}

// explainAndCountRows runs EXPLAIN (ANALYZE) on the query the merge actually
// issued and reports (rows returned, rows inspected). Executing a SELECT here is
// side-effect free.
func explainAndCountRows(t *testing.T, ctx context.Context, db bun.IDB, query string) (int, float64) {
	t.Helper()

	var returned int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM (`+query+`) AS occupant`).Scan(ctx, &returned))

	var raw []byte
	require.NoError(t, db.NewRaw(`EXPLAIN (ANALYZE, FORMAT JSON) `+query).Scan(ctx, &raw))
	var plans []planResult
	require.NoError(t, json.Unmarshal(raw, &plans), "EXPLAIN output: %s", raw)
	require.Len(t, plans, 1)
	return returned, rowsInspectedOn(plans[0].Plan, "provenance_contributions")
}

// TestMergeOccupantLookupStaysBounded is the #125 cost half: the destination
// branch already answers the candidate address, and holds thousands of present
// rows that have nothing to do with the merge. The occupant lookup must read
// only the candidate addresses — before the narrowing it returned every present
// row of the destination branch, which a merge repeated once per merged object.
//
// Both destinations are covered because they hit different partial indexes, and
// the branch one is only usable when the predicate is a plain equality.
func TestMergeOccupantLookupStaysBounded(t *testing.T) {
	const seeded = 2000

	t.Run("main destination", func(t *testing.T) {
		ctx, db, repo, svc, projectID := setupProvenanceTest(t)
		sourceID := createTestSource(t, ctx, db, projectID, "occupant-main")

		branchID, _, _, _ := branchPushAndMainPushForTheSameRecord(
			t, ctx, db, repo, svc, projectID, sourceID, "occupant-main", "occupant-main-1")
		seedUnrelatedPresentContributions(t, ctx, db, projectID, sourceID, nil, seeded, "unrelated-main")

		bdb, ok := db.(*bun.DB)
		require.True(t, ok)
		hook := &occupantSelectHook{}
		bdb.AddQueryHook(hook)

		_, err := svc.MergeBranch(ctx, projectID, nil, &graph.BranchMergeRequest{
			SourceBranchID: branchID, Execute: true, Policy: "enrich_no_sim",
		})
		require.NoError(t, err)
		require.Len(t, hook.queries, 1, "one merged object must issue exactly one occupant lookup")

		returned, inspected := explainAndCountRows(t, ctx, db, hook.queries[0])
		t.Logf("occupant lookup: %d rows returned, %.0f rows inspected on provenance_contributions (%d unrelated present rows seeded)",
			returned, inspected, seeded)
		assert.LessOrEqual(t, returned, 1, "the lookup must not return rows the merge never asked about")
		assert.Less(t, inspected, float64(seeded)/10,
			"the lookup must not read the branch's unrelated present rows")

		var untouched int
		require.NoError(t, db.NewRaw(`
			SELECT count(*) FROM kb.provenance_contributions
			WHERE project_id = ? AND branch_id IS NULL AND external_id LIKE 'unrelated-main-%' AND state = 'present'`,
			projectID).Scan(ctx, &untouched))
		assert.Equal(t, seeded, untouched, "the seeded rows must be left alone")
	})

	t.Run("branch destination", func(t *testing.T) {
		ctx, db, repo, svc, projectID := setupProvenanceTest(t)
		sourceID := createTestSource(t, ctx, db, projectID, "occupant-branch")

		target := &graph.Branch{ID: uuid.New(), ProjectID: projectID, Name: "occupant-target"}
		require.NoError(t, repo.CreateBranch(ctx, target))
		sourceBranch := &graph.Branch{ID: uuid.New(), ProjectID: projectID, Name: "occupant-source"}
		require.NoError(t, repo.CreateBranch(ctx, sourceBranch))

		// The same source owns a record on the target branch and a different
		// object for it on the source branch.
		push := func(branchID *uuid.UUID) *graph.ApplyChangesetResponse {
			resp, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
				SourceID: sourceID, BranchID: branchID,
				Objects: []graph.ChangesetObjectItem{{
					Type: "Person", ExternalID: "occupant-branch-1", ExternalRevisionSeq: seq(1),
				}},
			}, nil)
			require.NoError(t, err)
			return resp
		}
		targetApplied := push(&target.ID)
		sourceApplied := push(&sourceBranch.ID)
		require.NotEqual(t,
			*targetApplied.Results[0].CanonicalID, *sourceApplied.Results[0].CanonicalID,
			"the source branch must own a different object for the merge to add as a clone")

		seedUnrelatedPresentContributions(t, ctx, db, projectID, sourceID, &target.ID, seeded, "unrelated-branch")

		bdb, ok := db.(*bun.DB)
		require.True(t, ok)
		hook := &occupantSelectHook{}
		bdb.AddQueryHook(hook)

		_, err := svc.MergeBranch(ctx, projectID, &target.ID, &graph.BranchMergeRequest{
			SourceBranchID: sourceBranch.ID, Execute: true, Policy: "enrich_no_sim",
		})
		require.NoError(t, err)
		require.Len(t, hook.queries, 1, "one merged object must issue exactly one occupant lookup")

		returned, inspected := explainAndCountRows(t, ctx, db, hook.queries[0])
		t.Logf("occupant lookup: %d rows returned, %.0f rows inspected on provenance_contributions (%d unrelated present rows seeded)",
			returned, inspected, seeded)
		assert.LessOrEqual(t, returned, 1, "the lookup must not return rows the merge never asked about")
		assert.Less(t, inspected, float64(seeded)/10,
			"the lookup must not read the target branch's unrelated present rows")

		// The guard still holds on the branch index.
		var held []contributionRow
		require.NoError(t, db.NewRaw(`
			SELECT source_id, subject_version_id, state, external_revision_seq, branch_id, merged_from_version_id
			FROM kb.provenance_contributions
			WHERE project_id = ? AND subject_kind = 'object' AND branch_id = ?
			  AND source_id = ? AND external_id = ? AND state = 'present'`,
			projectID, target.ID, sourceID, "occupant-branch-1").Scan(ctx, &held))
		require.Len(t, held, 1)
		var ownerCanonical uuid.UUID
		require.NoError(t, db.NewRaw(`SELECT canonical_id FROM kb.graph_objects WHERE id = ?`,
			held[0].SubjectVersionID).Scan(ctx, &ownerCanonical))
		assert.Equal(t, *targetApplied.Results[0].CanonicalID, ownerCanonical)
	})
}
