package graph_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// seedRelHead inserts a HEAD relationship row (supersedes_id IS NULL) with an
// explicit branch, canonical id and content hash, so a merge fixture can place
// the same canonical relationship on main, the source branch and the target
// branch independently.
func seedRelHead(
	t *testing.T,
	ctx context.Context,
	db bun.IDB,
	projectID uuid.UUID,
	id, canonicalID, srcID, dstID uuid.UUID,
	branchID *uuid.UUID,
	typ, props string,
	contentHash []byte,
) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
		INSERT INTO kb.graph_relationships
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, src_id, dst_id,
			 properties, content_hash, created_at)
		VALUES (?, ?, ?, ?, NULL, 1, ?, ?, ?, ?::jsonb, ?, NOW())
	`, id, projectID, branchID, canonicalID, typ, srcID, dstID, props, contentHash)
	require.NoError(t, err)
}

// TestMergeRelationshipFastForwardTargetsTargetBranch is the regression test for
// the relationship fast-forward path writing onto main.
//
// Before the fix, applyMerge resolved the previous relationship HEAD with the
// branch-less GetRelationshipHeadByCanonicalID, whose #1247 contract is
// main-preferring. For a branch→branch merge (targetBranchID non-nil) that
// returned the *main* HEAD, and CreateRelationshipVersion then inherited
// prevHead.BranchID, silently writing the fast-forwarded version onto main
// while leaving the target branch untouched.
func TestMergeRelationshipFastForwardTargetsTargetBranch(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)

	srcBranch := &graph.Branch{ID: uuid.New(), ProjectID: projectID, Name: "rel-ff-src"}
	require.NoError(t, repo.CreateBranch(ctx, srcBranch))
	tgtBranch := &graph.Branch{ID: uuid.New(), ProjectID: projectID, Name: "rel-ff-tgt"}
	require.NoError(t, repo.CreateBranch(ctx, tgtBranch))

	canonicalID := uuid.New()
	srcID, dstID := uuid.New(), uuid.New()
	baseHash := []byte("merge-rel-ff-base-hash")
	srcHash := []byte("merge-rel-ff-source-hash")

	// The same canonical relationship lives on main, the source branch and the
	// target branch. Source content differs from the target/main content, so the
	// merge classifies it fast_forward (same key set, no conflicting values).
	seedRelHead(t, ctx, db, projectID, uuid.New(), canonicalID, srcID, dstID, nil, "rel_ff", `{}`, baseHash)
	seedRelHead(t, ctx, db, projectID, uuid.New(), canonicalID, srcID, dstID, &srcBranch.ID, "rel_ff", `{"v":2}`, srcHash)
	seedRelHead(t, ctx, db, projectID, uuid.New(), canonicalID, srcID, dstID, &tgtBranch.ID, "rel_ff", `{}`, baseHash)

	// Merge source → target branch (NOT main).
	resp, err := svc.MergeBranch(ctx, projectID, &tgtBranch.ID, &graph.BranchMergeRequest{
		SourceBranchID: srcBranch.ID,
		Execute:        true,
		Policy:         "enrich_no_sim",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.RelationshipsFastForwardCount)
	require.Equal(t, 1, *resp.RelationshipsFastForwardCount)

	readHeadProps := func(branchID *uuid.UUID) string {
		var props string
		require.NoError(t, db.NewRaw(`
			SELECT properties::text FROM kb.graph_relationships
			WHERE project_id = ? AND branch_id IS NOT DISTINCT FROM ? AND canonical_id = ?
			  AND supersedes_id IS NULL`,
			projectID, branchID, canonicalID).Scan(ctx, &props))
		return props
	}

	// The fast-forwarded version must land on the target branch...
	assert.JSONEq(t, `{"v":2}`, readHeadProps(&tgtBranch.ID),
		"relationship fast-forward must write to the target branch")

	// ...and main must be left exactly as it was (still the base content).
	assert.JSONEq(t, `{}`, readHeadProps(nil),
		"main must be untouched by a branch→branch relationship fast-forward")
}
