package graph_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// seedSources builds a single-entry `_sources` provenance array with an explicit
// extracted_at, so merge recency is deterministic in tests.
func seedSources(documentID, jobID, extractedAt string) []any {
	return []any{map[string]any{
		"document_id":  documentID,
		"job_id":       jobID,
		"extracted_at": extractedAt,
	}}
}

// setupDeterministicMergeTest opens a throwaway DB, seeds an org+project, and
// returns the raw DB handle, graph service, repo and project ID.
func setupDeterministicMergeTest(t *testing.T, suffix string) (context.Context, bun.IDB, *graph.Repository, *graph.Service, uuid.UUID) {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, suffix)
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, suffix+" Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID: projectID, OrgID: orgID, Name: suffix + " Project",
	}, testutil.AdminUser.ID))

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	repo := graph.NewRepository(db, log, cfg)
	svc := graph.NewService(repo, log, nil, nil, nil, nil, nil, graph.NoopEventSink{}, branches.NewStore(db), nil)

	return ctx, db, repo, svc, uuid.MustParse(projectID)
}

// branchMergedAt reads a branch's merged_at column (nil when the branch is open).
func branchMergedAt(t *testing.T, ctx context.Context, db bun.IDB, branchID uuid.UUID) *time.Time {
	t.Helper()
	var mergedAt *time.Time
	require.NoError(t, db.NewRaw(`SELECT merged_at FROM kb.branches WHERE id = ?`, branchID).Scan(ctx, &mergedAt))
	return mergedAt
}

// TestMergeBranchProvenanceOnlyFastForwards pins L0 end-to-end: a same-entity
// re-extraction whose content differs only in provenance keys (`_sources`,
// `_extraction_job_id`) is a clean fast-forward, not a conflict, and merges.
func TestMergeBranchProvenanceOnlyFastForwards(t *testing.T) {
	ctx, db, repo, svc, pid := setupDeterministicMergeTest(t, "graph_det_l0")
	name := "Barry Farber"

	_, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Person", Key: strPtr("barry-farber"), Name: &name,
		Properties: map[string]any{
			"name":     name,
			"role":     "radio host",
			"_sources": seedSources("doc-a", "job-a", "2024-01-01T00:00:00Z"),
		},
	}, nil)
	require.NoError(t, err)

	sourceBranch := &graph.Branch{ID: uuid.New(), ProjectID: pid, Name: "l0-src"}
	require.NoError(t, repo.CreateBranch(ctx, sourceBranch))
	_, err = svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Person", Key: strPtr("barry-farber"), Name: &name,
		BranchID: &sourceBranch.ID,
		Properties: map[string]any{
			"name":     name,
			"role":     "radio host",
			"_sources": seedSources("doc-b", "job-b", "2024-02-01T00:00:00Z"),
		},
	}, nil)
	require.NoError(t, err)

	resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID: sourceBranch.ID, Execute: true, Policy: "partial",
	})
	require.NoError(t, err)

	require.Zero(t, resp.ConflictCount, "provenance-only drift must not be a conflict")
	require.Zero(t, resp.LeftForReviewCount, "provenance-only drift must merge")
	require.Equal(t, 1, resp.FastForwardCount, "provenance-only drift must fast-forward")
	require.NotNil(t, branchMergedAt(t, ctx, db, sourceBranch.ID), "provenance-only drift must close the branch")
}

// TestMergeBranchDescriptionOnlyNewestWins pins L3 end-to-end: a description-only
// difference is not a conflict and the newer side's description survives.
func TestMergeBranchDescriptionOnlyNewestWins(t *testing.T) {
	ctx, db, repo, svc, pid := setupDeterministicMergeTest(t, "graph_det_l3")
	name := "Barry Farber"

	_, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Person", Key: strPtr("barry-farber"), Name: &name,
		Properties: map[string]any{
			"name":        name,
			"role":        "radio host",
			"description": "Radio host",
			"_sources":    seedSources("doc-a", "job-a", "2024-01-01T00:00:00Z"),
		},
	}, nil)
	require.NoError(t, err)

	sourceBranch := &graph.Branch{ID: uuid.New(), ProjectID: pid, Name: "l3-src"}
	require.NoError(t, repo.CreateBranch(ctx, sourceBranch))
	_, err = svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Person", Key: strPtr("barry-farber"), Name: &name,
		BranchID: &sourceBranch.ID,
		Properties: map[string]any{
			"name":        name,
			"role":        "radio host",
			"description": "American talk radio host",
			"_sources":    seedSources("doc-b", "job-b", "2024-02-01T00:00:00Z"),
		},
	}, nil)
	require.NoError(t, err)

	resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID: sourceBranch.ID, Execute: true, Policy: "partial",
	})
	require.NoError(t, err)

	require.Zero(t, resp.ConflictCount, "description-only drift must not be a conflict")
	require.Zero(t, resp.LeftForReviewCount, "description-only drift must merge")

	var desc string
	require.NoError(t, db.NewRaw(`
		SELECT coalesce(properties->>'description', '') FROM kb.graph_objects
		WHERE project_id = ? AND type = 'Person' AND key = 'barry-farber'
		  AND branch_id IS NULL AND supersedes_id IS NULL`,
		pid).Scan(ctx, &desc))
	require.Equal(t, "American talk radio host", desc, "the newer description must win")
}

// TestMergeBranchNewestResolvesByRecency pins the deterministic "newest"
// strategy: source-newer wins, target-newer wins, and a tie is left for review.
func TestMergeBranchNewestResolvesByRecency(t *testing.T) {
	newest := func(t *testing.T, suffix, srcAt, tgtAt, wantRole, wantResolution string, wantLeftForReview bool) {
		ctx, db, repo, svc, pid := setupDeterministicMergeTest(t, suffix)
		name := "Barry Farber"

		_, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
			Type: "Person", Key: strPtr("barry-farber"), Name: &name,
			Properties: map[string]any{
				"name":     name,
				"role":     "radio host",
				"_sources": seedSources("doc-tgt", "job-tgt", tgtAt),
			},
		}, nil)
		require.NoError(t, err)

		sourceBranch := &graph.Branch{ID: uuid.New(), ProjectID: pid, Name: "newest-src"}
		require.NoError(t, repo.CreateBranch(ctx, sourceBranch))
		src, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
			Type: "Person", Key: strPtr("barry-farber"), Name: &name,
			BranchID: &sourceBranch.ID,
			Properties: map[string]any{
				"name":     name,
				"role":     "author",
				"_sources": seedSources("doc-src", "job-src", srcAt),
			},
		}, nil)
		require.NoError(t, err)

		resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
			SourceBranchID: sourceBranch.ID, Execute: true, Policy: "newest",
		})
		require.NoError(t, err)

		if wantLeftForReview {
			require.Equal(t, 1, resp.LeftForReviewCount, "a recency tie must be left for review")
			require.Nil(t, branchMergedAt(t, ctx, db, sourceBranch.ID), "a tie must keep the branch open")
			return
		}

		require.Zero(t, resp.LeftForReviewCount)

		var resolution string
		for _, o := range resp.Objects {
			if o.CanonicalID == src.CanonicalID {
				resolution = o.Resolution
			}
		}
		require.Equal(t, wantResolution, resolution)

		var role string
		require.NoError(t, db.NewRaw(`
			SELECT coalesce(properties->>'role', '') FROM kb.graph_objects
			WHERE project_id = ? AND type = 'Person' AND key = 'barry-farber'
			  AND branch_id IS NULL AND supersedes_id IS NULL`,
			pid).Scan(ctx, &role))
		require.Equal(t, wantRole, role, "the newer side's value must win")
	}

	t.Run("source newer wins", func(t *testing.T) {
		newest(t, "graph_det_newest_src", "2024-02-01T00:00:00Z", "2024-01-01T00:00:00Z", "author", "newest_source", false)
	})
	t.Run("target newer wins", func(t *testing.T) {
		newest(t, "graph_det_newest_tgt", "2024-01-01T00:00:00Z", "2024-02-01T00:00:00Z", "radio host", "newest_target", false)
	})
	t.Run("tie left for review", func(t *testing.T) {
		newest(t, "graph_det_newest_tie", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z", "", "", true)
	})
}

// TestMergeBranchPartialLeavesGenuineConflict pins the guard: a genuine value
// conflict on a real property under the default partial policy is still left for
// review — the deterministic improvements must not over-merge.
func TestMergeBranchPartialLeavesGenuineConflict(t *testing.T) {
	ctx, db, repo, svc, pid := setupDeterministicMergeTest(t, "graph_det_guard")
	name := "Barry Farber"

	_, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Person", Key: strPtr("barry-farber"), Name: &name,
		Properties: map[string]any{
			"name":     name,
			"role":     "radio host",
			"_sources": seedSources("doc-a", "job-a", "2024-01-01T00:00:00Z"),
		},
	}, nil)
	require.NoError(t, err)

	sourceBranch := &graph.Branch{ID: uuid.New(), ProjectID: pid, Name: "guard-src"}
	require.NoError(t, repo.CreateBranch(ctx, sourceBranch))
	_, err = svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Person", Key: strPtr("barry-farber"), Name: &name,
		BranchID: &sourceBranch.ID,
		Properties: map[string]any{
			"name":     name,
			"role":     "author",
			"_sources": seedSources("doc-b", "job-b", "2024-02-01T00:00:00Z"),
		},
	}, nil)
	require.NoError(t, err)

	resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID: sourceBranch.ID, Execute: true, Policy: "partial",
	})
	require.NoError(t, err)

	require.Equal(t, 1, resp.ConflictCount, "a genuine value conflict must still be classified as a conflict")
	require.Equal(t, 1, resp.LeftForReviewCount, "a genuine value conflict under partial must be left for review")

	var role string
	require.NoError(t, db.NewRaw(`
		SELECT coalesce(properties->>'role', '') FROM kb.graph_objects
		WHERE project_id = ? AND type = 'Person' AND key = 'barry-farber'
		  AND branch_id IS NULL AND supersedes_id IS NULL`,
		pid).Scan(ctx, &role))
	require.Equal(t, "radio host", role, "the target value must be preserved")
}
