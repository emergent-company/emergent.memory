package graph_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// reconcileStub is a scripted graph.ConflictReconciler for merge tests.
type reconcileStub struct {
	fn    func(ctx context.Context, req graph.ConflictReconcileRequest) (map[string]any, string, error)
	calls int
}

func (s *reconcileStub) Reconcile(ctx context.Context, req graph.ConflictReconcileRequest) (map[string]any, string, error) {
	s.calls++
	if s.fn != nil {
		return s.fn(ctx, req)
	}
	return nil, "stub", nil
}

// setupReconcileMergeTest seeds a target object with `role` = targetRole and a
// source-branch object with `role` = sourceRole (plus an additive source-only
// key), wires the given reconciler, and returns the context, db, service,
// project id and source branch id.
func setupReconcileMergeTest(t *testing.T, suffix, targetRole, sourceRole string, reconciler graph.ConflictReconciler) (context.Context, bun.IDB, *graph.Service, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx, db, repo, svc, pid := setupDeterministicMergeTest(t, suffix)
	name := "Barry Farber"

	_, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Person", Key: strPtr("barry-farber"), Name: &name,
		Properties: map[string]any{
			"name":     name,
			"role":     targetRole,
			"_sources": seedSources("doc-tgt", "job-tgt", "2024-01-01T00:00:00Z"),
		},
	}, nil)
	require.NoError(t, err)

	sourceBranch := &graph.Branch{ID: uuid.New(), ProjectID: pid, Name: "reconcile-src"}
	require.NoError(t, repo.CreateBranch(ctx, sourceBranch))
	_, err = svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Person", Key: strPtr("barry-farber"), Name: &name,
		BranchID: &sourceBranch.ID,
		Properties: map[string]any{
			"name":     name,
			"role":     sourceRole,
			"city":     "New York",
			"_sources": seedSources("doc-src", "job-src", "2024-02-01T00:00:00Z"),
		},
	}, nil)
	require.NoError(t, err)

	if reconciler != nil {
		svc.SetConflictReconciler(reconciler)
	}

	return ctx, db, svc, pid, sourceBranch.ID
}

// mergedObjectState reads the merged main-graph object's role, city, change
// summary and `_sources` count.
type mergedObjectState struct {
	Role        string
	City        string
	ChangeSum   map[string]any
	SourceCount int
}

func readMergedObjectState(t *testing.T, ctx context.Context, db bun.IDB, pid uuid.UUID) mergedObjectState {
	t.Helper()
	var props map[string]any
	var changeSum map[string]any
	require.NoError(t, db.NewRaw(`
		SELECT properties, change_summary FROM kb.graph_objects
		WHERE project_id = ? AND type = 'Person' AND key = 'barry-farber'
		  AND branch_id IS NULL AND supersedes_id IS NULL`,
		pid).Scan(ctx, &props, &changeSum))

	state := mergedObjectState{ChangeSum: changeSum}
	if v, ok := props["role"].(string); ok {
		state.Role = v
	}
	if v, ok := props["city"].(string); ok {
		state.City = v
	}
	if srcs, ok := props["_sources"].([]any); ok {
		state.SourceCount = len(srcs)
	}
	return state
}

// TestMergeBranchReconcileCombinesResidualConflict pins the reconcile happy path:
// a genuine value conflict on a real property is resolved by the reconciler
// (combining the richer detail), applied, provenance is unioned, additive
// source-only keys are kept, and the branch fully merges.
func TestMergeBranchReconcileCombinesResidualConflict(t *testing.T) {
	stub := &reconcileStub{fn: func(_ context.Context, req graph.ConflictReconcileRequest) (map[string]any, string, error) {
		require.Contains(t, req.Conflicts, "role")
		return map[string]any{"role": "radio host and author"}, "stub-model", nil
	}}
	ctx, db, svc, pid, branchID := setupReconcileMergeTest(t, "graph_reconcile_ok", "radio host", "author", stub)

	resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID: branchID, Execute: true, Policy: "reconcile",
	})
	require.NoError(t, err)

	require.Equal(t, 1, resp.ConflictCount, "a genuine value conflict must still be classified as a conflict")
	require.Zero(t, resp.LeftForReviewCount, "the reconciler resolves the conflict, nothing left for review")
	require.Equal(t, 1, stub.calls, "the reconciler must be called exactly once")

	require.NotNil(t, branchMergedAt(t, ctx, db, branchID), "a fully-reconciled merge must close the branch")

	state := readMergedObjectState(t, ctx, db, pid)
	require.Equal(t, "radio host and author", state.Role, "the combined value must be applied")
	require.Equal(t, "New York", state.City, "the additive source-only key must be kept")
	require.Equal(t, 2, state.SourceCount, "provenance from both sides must be unioned")

	raw, _ := json.Marshal(state.ChangeSum)
	require.Contains(t, string(raw), "_reconciled", "the change_summary must carry the reconcile audit marker")
	reconciled, ok := state.ChangeSum["_reconciled"].(map[string]any)
	require.True(t, ok, "_reconciled must be an object")
	require.Equal(t, "stub-model", reconciled["model"])
	require.Contains(t, reconciled["keys"], "role")
}

// TestMergeBranchReconcileFallsBackOnReconcilerError pins the fallback: a wired
// reconciler that errors falls back to deterministic newest-wins (the newer
// source survives), so the branch still merges instead of stranding the data.
func TestMergeBranchReconcileFallsBackOnReconcilerError(t *testing.T) {
	stub := &reconcileStub{fn: func(context.Context, graph.ConflictReconcileRequest) (map[string]any, string, error) {
		return nil, "", errStubReconcile
	}}
	ctx, db, svc, pid, branchID := setupReconcileMergeTest(t, "graph_reconcile_err", "radio host", "author", stub)

	resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID: branchID, Execute: true, Policy: "reconcile",
	})
	require.NoError(t, err)

	require.Equal(t, 1, resp.ConflictCount)
	require.Zero(t, resp.LeftForReviewCount, "a reconciler error falls back to newest, so the branch converges")
	require.NotNil(t, branchMergedAt(t, ctx, db, branchID), "a newest-resolved reconcile must close the branch")

	state := readMergedObjectState(t, ctx, db, pid)
	require.Equal(t, "author", state.Role, "the newer source value must win on fallback")
	raw, _ := json.Marshal(state.ChangeSum)
	require.Contains(t, string(raw), "reconcile_fallback_newest", "the change_summary must record the newest fallback resolution")
}

// TestMergeBranchReconcileFallsBackOnPartialAnswer pins the fallback when the
// reconciler does not answer every conflicting key (treated as invalid): it too
// falls back to deterministic newest-wins rather than leaving the object open.
func TestMergeBranchReconcileFallsBackOnPartialAnswer(t *testing.T) {
	stub := &reconcileStub{fn: func(context.Context, graph.ConflictReconcileRequest) (map[string]any, string, error) {
		return map[string]any{}, "stub", nil // no answer for "role"
	}}
	ctx, db, svc, pid, branchID := setupReconcileMergeTest(t, "graph_reconcile_partial", "radio host", "author", stub)

	resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID: branchID, Execute: true, Policy: "reconcile",
	})
	require.NoError(t, err)

	require.Zero(t, resp.LeftForReviewCount, "a partial reconciler answer falls back to newest")
	require.NotNil(t, branchMergedAt(t, ctx, db, branchID))

	state := readMergedObjectState(t, ctx, db, pid)
	require.Equal(t, "author", state.Role, "the newer source value must win on fallback")
}

// TestMergeBranchReconcileNoReconcilerFallsBackToNewest pins the no-reconciler
// guard: a "reconcile" merge with no wired reconciler falls back to deterministic
// newest-wins (never silently dropping the source value), so a determinable-
// recency conflict still converges.
func TestMergeBranchReconcileNoReconcilerFallsBackToNewest(t *testing.T) {
	ctx, db, svc, pid, branchID := setupReconcileMergeTest(t, "graph_reconcile_nil", "radio host", "author", nil)

	resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID: branchID, Execute: true, Policy: "reconcile",
	})
	require.NoError(t, err)

	require.Equal(t, 1, resp.ConflictCount)
	require.Zero(t, resp.LeftForReviewCount, "no reconciler falls back to newest, so the branch converges")
	require.NotNil(t, branchMergedAt(t, ctx, db, branchID), "a newest-resolved reconcile must close the branch")

	state := readMergedObjectState(t, ctx, db, pid)
	require.Equal(t, "author", state.Role, "the newer source value must win on fallback")
	require.Equal(t, "New York", state.City, "the additive source-only key must be kept")
	require.Equal(t, 2, state.SourceCount, "provenance from both sides must be unioned")
	raw, _ := json.Marshal(state.ChangeSum)
	require.Contains(t, string(raw), "reconcile_fallback_newest", "the change_summary must record the newest fallback resolution")
}

// TestMergeBranchReconcileUndeterminableRecencyLeavesForReview pins the last
// resort: a residual conflict with a recency tie (same extracted_at) and no
// reconciler falls back to preserve_target — it is left for review, not dropped.
func TestMergeBranchReconcileUndeterminableRecencyLeavesForReview(t *testing.T) {
	ctx, db, repo, svc, pid := setupDeterministicMergeTest(t, "graph_reconcile_tie")
	name := "Barry Farber"

	_, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Person", Key: strPtr("barry-farber"), Name: &name,
		Properties: map[string]any{
			"name":     name,
			"role":     "radio host",
			"_sources": seedSources("doc-tgt", "job-tgt", "2024-01-01T00:00:00Z"),
		},
	}, nil)
	require.NoError(t, err)

	sourceBranch := &graph.Branch{ID: uuid.New(), ProjectID: pid, Name: "tie-src"}
	require.NoError(t, repo.CreateBranch(ctx, sourceBranch))
	_, err = svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Person", Key: strPtr("barry-farber"), Name: &name,
		BranchID: &sourceBranch.ID,
		Properties: map[string]any{
			"name":     name,
			"role":     "author",
			"_sources": seedSources("doc-src", "job-src", "2024-01-01T00:00:00Z"),
		},
	}, nil)
	require.NoError(t, err)

	resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID: sourceBranch.ID, Execute: true, Policy: "reconcile",
	})
	require.NoError(t, err)

	require.Equal(t, 1, resp.ConflictCount)
	require.Equal(t, 1, resp.LeftForReviewCount, "an undeterminable-recency residual is the last-resort preserve_target")
	require.Nil(t, branchMergedAt(t, ctx, db, sourceBranch.ID), "an undeterminable-recency residual keeps the branch open")

	state := readMergedObjectState(t, ctx, db, pid)
	require.Equal(t, "radio host", state.Role, "the target value must be preserved on the last-resort fallback")
}

// TestMergeBranchReconcileErrorWithUndeterminableRecencyLeavesForReview pins the
// reconcileLeft correction: a wired reconciler that errors on a recency-tie
// residual falls all the way through to preserve_target, and the optimistic
// pre-compute is corrected so the branch is NOT closed over the unmerged object.
func TestMergeBranchReconcileErrorWithUndeterminableRecencyLeavesForReview(t *testing.T) {
	stub := &reconcileStub{fn: func(context.Context, graph.ConflictReconcileRequest) (map[string]any, string, error) {
		return nil, "", errStubReconcile
	}}
	ctx, db, repo, svc, pid := setupDeterministicMergeTest(t, "graph_reconcile_err_tie")
	svc.SetConflictReconciler(stub)
	name := "Barry Farber"

	_, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Person", Key: strPtr("barry-farber"), Name: &name,
		Properties: map[string]any{
			"name":     name,
			"role":     "radio host",
			"_sources": seedSources("doc-tgt", "job-tgt", "2024-01-01T00:00:00Z"),
		},
	}, nil)
	require.NoError(t, err)

	sourceBranch := &graph.Branch{ID: uuid.New(), ProjectID: pid, Name: "err-tie-src"}
	require.NoError(t, repo.CreateBranch(ctx, sourceBranch))
	_, err = svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Person", Key: strPtr("barry-farber"), Name: &name,
		BranchID: &sourceBranch.ID,
		Properties: map[string]any{
			"name":     name,
			"role":     "author",
			"_sources": seedSources("doc-src", "job-src", "2024-01-01T00:00:00Z"),
		},
	}, nil)
	require.NoError(t, err)

	resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID: sourceBranch.ID, Execute: true, Policy: "reconcile",
	})
	require.NoError(t, err)

	require.Equal(t, 1, resp.ConflictCount)
	require.Equal(t, 1, resp.LeftForReviewCount, "an errored reconcile on a recency tie must leave the object for review")
	require.Nil(t, branchMergedAt(t, ctx, db, sourceBranch.ID), "the optimistic pre-compute must be corrected so the branch stays open")

	state := readMergedObjectState(t, ctx, db, pid)
	require.Equal(t, "radio host", state.Role, "the target value must be preserved on the last-resort fallback")
}

// TestMergeBranchReconcileCapExceededFallsBackToNewest pins the per-run cap:
// past ReconcileMaxPerRun, later conflicts fall back to deterministic
// newest-wins (newer source survives) instead of being parked for review.
func TestMergeBranchReconcileCapExceededFallsBackToNewest(t *testing.T) {
	ctx, db, repo, svc, pid := setupDeterministicMergeTest(t, "graph_reconcile_cap")
	stub := &reconcileStub{fn: func(_ context.Context, req graph.ConflictReconcileRequest) (map[string]any, string, error) {
		return map[string]any{"role": "combined"}, "stub", nil
	}}
	svc.SetConflictReconciler(stub)

	create := func(key, name, role, doc, job, extractedAt string, branchID *uuid.UUID) {
		_, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
			Type: "Person", Key: strPtr(key), Name: &name,
			BranchID: branchID,
			Properties: map[string]any{
				"name":     name,
				"role":     role,
				"_sources": seedSources(doc, job, extractedAt),
			},
		}, nil)
		require.NoError(t, err)
	}

	// Two target objects (older) and two source objects (newer).
	create("person-a", "Alpha", "radio host", "doc-tgt-a", "job-tgt-a", "2024-01-01T00:00:00Z", nil)
	create("person-b", "Beta", "radio host", "doc-tgt-b", "job-tgt-b", "2024-01-01T00:00:00Z", nil)

	sourceBranch := &graph.Branch{ID: uuid.New(), ProjectID: pid, Name: "cap-src"}
	require.NoError(t, repo.CreateBranch(ctx, sourceBranch))
	create("person-a", "Alpha", "author", "doc-src-a", "job-src-a", "2024-02-01T00:00:00Z", &sourceBranch.ID)
	create("person-b", "Beta", "author", "doc-src-b", "job-src-b", "2024-02-01T00:00:00Z", &sourceBranch.ID)

	resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID: sourceBranch.ID, Execute: true, Policy: "reconcile", ReconcileMaxPerRun: 1,
	})
	require.NoError(t, err)

	require.Equal(t, 2, resp.ConflictCount)
	require.Zero(t, resp.LeftForReviewCount, "the cap-exceeded conflict falls back to newest, so the branch converges")
	require.Equal(t, 1, stub.calls, "the reconciler must be called at most once under a cap of 1")
	require.NotNil(t, branchMergedAt(t, ctx, db, sourceBranch.ID), "a fully-resolved reconcile (with newest fallback) must close the branch")
}

// errStubReconcile is the sentinel error for the fallback test.
var errStubReconcile = errors.New("reconciler boom")
