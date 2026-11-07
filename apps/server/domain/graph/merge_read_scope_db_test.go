package graph_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/pkg/branchscope"
)

// TestMergeAddedObjectEdgesHitMain is the fail-first regression for issue #331:
// after a branch merge, a merged object's main copy must own its edges, and a
// main-only read must resolve keys and edge lookups to main, never the branch
// original.
func TestMergeAddedObjectEdgesHitMain(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)

	branch := &graph.Branch{ID: uuid.New(), ProjectID: projectID, Name: "migration/v4", Origin: "manual"}
	require.NoError(t, repo.CreateBranch(ctx, branch))

	selfKey := "le-pl-awesome-vanlife-jdg"
	trailerKey := "veh-camping-trailer"

	selfResp, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type: "Person", Key: &selfKey, BranchID: &branch.ID, Properties: map[string]any{"name": "self"},
	}, nil)
	require.NoError(t, err)
	trailerResp, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type: "Vehicle", Key: &trailerKey, BranchID: &branch.ID, Properties: map[string]any{"name": "trailer"},
	}, nil)
	require.NoError(t, err)

	_, err = svc.CreateRelationship(ctx, projectID, &graph.CreateGraphRelationshipRequest{
		Type: "owns", SrcID: selfResp.CanonicalID, DstID: trailerResp.CanonicalID, BranchID: &branch.ID,
	}, nil)
	require.NoError(t, err)

	// Branch object version-1 rows share id == canonical_id.
	branchTrailerCanonical := trailerResp.CanonicalID
	branchSelfCanonical := selfResp.CanonicalID

	_, err = svc.MergeBranch(ctx, projectID, nil, &graph.BranchMergeRequest{
		SourceBranchID: branch.ID, Execute: true, Policy: "enrich_no_sim",
	})
	require.NoError(t, err)

	// Locate the main copies by key.
	var mainTrailer struct {
		ID          uuid.UUID  `bun:"id"`
		CanonicalID uuid.UUID  `bun:"canonical_id"`
		BranchID    *uuid.UUID `bun:"branch_id"`
	}
	require.NoError(t, db.NewRaw(`
		SELECT id, canonical_id, branch_id FROM kb.graph_objects
		WHERE project_id = ? AND key = ? AND branch_id IS NULL AND supersedes_id IS NULL AND deleted_at IS NULL`,
		projectID, trailerKey).Scan(ctx, &mainTrailer.ID, &mainTrailer.CanonicalID, &mainTrailer.BranchID))
	require.NotEqual(t, uuid.Nil, mainTrailer.CanonicalID)
	require.NotEqual(t, branchTrailerCanonical, mainTrailer.CanonicalID,
		"merged main copy must get its own canonical id, not the branch original's")

	var mainSelf struct {
		CanonicalID uuid.UUID `bun:"canonical_id"`
	}
	require.NoError(t, db.NewRaw(`
		SELECT canonical_id FROM kb.graph_objects
		WHERE project_id = ? AND key = ? AND branch_id IS NULL AND supersedes_id IS NULL AND deleted_at IS NULL`,
		projectID, selfKey).Scan(ctx, &mainSelf.CanonicalID))
	require.NotEqual(t, branchSelfCanonical, mainSelf.CanonicalID)

	// The main relationship must reference the main copies' canonical ids.
	var rel struct {
		SrcID uuid.UUID `bun:"src_id"`
		DstID uuid.UUID `bun:"dst_id"`
	}
	require.NoError(t, db.NewRaw(`
		SELECT src_id, dst_id FROM kb.graph_relationships
		WHERE project_id = ? AND branch_id IS NULL AND type = 'owns' AND supersedes_id IS NULL AND deleted_at IS NULL`,
		projectID).Scan(ctx, &rel.SrcID, &rel.DstID))
	require.Equal(t, mainSelf.CanonicalID, rel.SrcID, "main relationship must reference main self")
	require.Equal(t, mainTrailer.CanonicalID, rel.DstID, "main relationship must reference main trailer")

	// A main-only read of the main copy's edges must return the owns edge.
	mainOnlyCtx := branchscope.ContextWith(ctx, nil)
	incoming, outgoing, err := repo.GetEdges(mainOnlyCtx, projectID, mainTrailer.CanonicalID, graph.GetEdgesParams{Direction: "both"})
	require.NoError(t, err)
	edges := append(incoming, outgoing...)
	require.Len(t, edges, 1, "main-only read of the main copy must find the merged edge")
	require.Equal(t, "owns", edges[0].Type)

	// The branch copy's edges still resolve to the branch relationship.
	incoming, outgoing, err = repo.GetEdges(mainOnlyCtx, projectID, branchTrailerCanonical, graph.GetEdgesParams{Direction: "both"})
	require.NoError(t, err)
	require.Empty(t, append(incoming, outgoing...), "a main-only read must not surface the branch's relationship")
}
