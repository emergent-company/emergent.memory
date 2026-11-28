package scheduler

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// pv768 returns a pgvector literal with the 768 dimensions of embedding_v2.
func pv768() string {
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < 768; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('1')
	}
	b.WriteByte(']')
	return b.String()
}

// mergeSweepFixture opens a throwaway DB, seeds an org+project, and returns a
// graph service wired with a real branch store (so a full merge stamps
// merged_at), the repository (to create branches), the raw *bun.DB handle, and
// the project ID. It seeds org/project with raw SQL rather than testutil, whose
// import path would cycle back into the scheduler package.
func mergeSweepFixture(t *testing.T, suffix string) (*bun.DB, *graph.Repository, *graph.Service, uuid.UUID) {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, suffix)
	t.Cleanup(tdb.Close)
	db, ok := tdb.GetDB().(*bun.DB)
	require.True(t, ok, "test database handle must be a *bun.DB")

	orgID := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, 'Sweep Org', now(), now())`, orgID).Exec(ctx)
	require.NoError(t, err)
	projectID := uuid.NewString()
	_, err = db.NewRaw(`INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at)
		VALUES (?, 'Sweep Project', ?, now(), now())`, projectID, orgID).Exec(ctx)
	require.NoError(t, err)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	repo := graph.NewRepository(db, log, cfg)
	svc := graph.NewService(repo, log, nil, nil, nil, nil, nil, graph.NoopEventSink{}, branches.NewStore(db), nil)

	return db, repo, svc, uuid.MustParse(projectID)
}

// seedExtractionBranch creates an extraction branch older than the sweep grace,
// with one live object on it. When embedded is true the object's embedding_v2 is
// stamped, otherwise it is left NULL (pending).
func seedExtractionBranch(t *testing.T, ctx context.Context, db *bun.DB, repo *graph.Repository, svc *graph.Service, pid uuid.UUID, key string, embedded bool) uuid.UUID {
	t.Helper()

	branch := &graph.Branch{
		ID:        uuid.New(),
		ProjectID: pid,
		Name:      "extraction/doc/" + key,
		Origin:    "extraction",
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	require.NoError(t, repo.CreateBranch(ctx, branch))

	k := key
	obj, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Entity", Key: &k, BranchID: &branch.ID,
	}, nil)
	require.NoError(t, err)

	if embedded {
		_, err := db.NewRaw(
			`UPDATE kb.graph_objects SET embedding_v2 = ?::vector, embedding_updated_at = now() WHERE id = ?`,
			pv768(), obj.ID,
		).Exec(ctx)
		require.NoError(t, err)
	}
	return branch.ID
}

// branchMergedAt reads a branch's merged_at column (nil when open).
func branchMergedAt(t *testing.T, ctx context.Context, db *bun.DB, branchID uuid.UUID) *time.Time {
	t.Helper()
	var mergedAt *time.Time
	require.NoError(t, db.NewRaw(`SELECT merged_at FROM kb.branches WHERE id = ?`, branchID).Scan(ctx, &mergedAt))
	return mergedAt
}

// mainObjectCount counts the live HEAD objects of a project on the main graph.
func mainObjectCount(t *testing.T, ctx context.Context, db *bun.DB, pid uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, db.NewRaw(
		`SELECT COUNT(*) FROM kb.graph_objects WHERE project_id = ? AND branch_id IS NULL AND supersedes_id IS NULL AND deleted_at IS NULL`,
		pid,
	).Scan(ctx, &n))
	return n
}

// TestMergeConvergenceSweepMergesReadyBranch is the core regression: an unmerged
// extraction branch whose objects are fully embedded and whose age has passed
// the grace window must be merged by the sweep (merged_at stamped, content on
// the main graph), while a still-pending and a too-recent branch are left alone.
func TestMergeConvergenceSweepMergesReadyBranch(t *testing.T) {
	ctx := context.Background()
	db, repo, svc, pid := mergeSweepFixture(t, "sched_merge_ready")

	ready := seedExtractionBranch(t, ctx, db, repo, svc, pid, "ready", true)
	pending := seedExtractionBranch(t, ctx, db, repo, svc, pid, "pending", false)

	// A ready branch that is too young for the grace window must be left for the
	// worker's own auto-merge (or a later sweep).
	tooRecent := &graph.Branch{
		ID:        uuid.New(),
		ProjectID: pid,
		Name:      "extraction/doc/too-recent",
		Origin:    "extraction",
		CreatedAt: time.Now(),
	}
	require.NoError(t, repo.CreateBranch(ctx, tooRecent))
	k := "too-recent"
	obj, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{Type: "Entity", Key: &k, BranchID: &tooRecent.ID}, nil)
	require.NoError(t, err)
	_, err = db.NewRaw(`UPDATE kb.graph_objects SET embedding_v2 = ?::vector WHERE id = ?`, pv768(), obj.ID).Exec(ctx)
	require.NoError(t, err)

	task := NewMergeConvergenceSweepTask(db, slog.New(slog.NewTextHandler(io.Discard, nil)), svc, time.Minute, 50)
	require.NoError(t, task.Run(ctx))

	require.NotNil(t, branchMergedAt(t, ctx, db, ready), "a ready extraction branch must be merged")
	require.Nil(t, branchMergedAt(t, ctx, db, pending), "a branch still pending embeddings must not be merged")
	require.Nil(t, branchMergedAt(t, ctx, db, tooRecent.ID), "a ready but too-recent branch must not be swept")

	// The ready branch's object must now be visible on the main graph.
	var mainKey string
	require.NoError(t, db.NewRaw(
		`SELECT key FROM kb.graph_objects WHERE project_id = ? AND key = ? AND branch_id IS NULL AND supersedes_id IS NULL`,
		pid, "ready",
	).Scan(ctx, &mainKey))
	require.Equal(t, "ready", mainKey, "the merged object must be on the main graph")

	// The pending branch's object must still be stranded on its staging branch.
	var strandedKey string
	require.NoError(t, db.NewRaw(
		`SELECT key FROM kb.graph_objects WHERE project_id = ? AND key = ? AND branch_id IS NOT NULL AND supersedes_id IS NULL`,
		pid, "pending",
	).Scan(ctx, &strandedKey))
	require.Equal(t, "pending", strandedKey, "the pending object must stay on its staging branch")
}

// TestMergeConvergenceSweepIdempotent pins idempotency: a second run over an
// already-merged branch must not re-apply or double-write it.
func TestMergeConvergenceSweepIdempotent(t *testing.T) {
	ctx := context.Background()
	db, repo, svc, pid := mergeSweepFixture(t, "sched_merge_idem")

	branchID := seedExtractionBranch(t, ctx, db, repo, svc, pid, "idem", true)

	task := NewMergeConvergenceSweepTask(db, slog.New(slog.NewTextHandler(io.Discard, nil)), svc, time.Minute, 50)
	require.NoError(t, task.Run(ctx))
	require.NotNil(t, branchMergedAt(t, ctx, db, branchID))
	countAfterFirst := mainObjectCount(t, ctx, db, pid)
	require.Equal(t, 1, countAfterFirst)

	// A second sweep must be a no-op: the branch is already merged and its
	// object is already on main, so nothing is duplicated.
	require.NoError(t, task.Run(ctx))
	require.Equal(t, countAfterFirst, mainObjectCount(t, ctx, db, pid), "a second sweep must not duplicate merged content")
}

// TestMergeConvergenceSweepBoundedBatch pins the batch bound: a sweep with a
// batch size of 3 over 5 ready branches merges exactly 3 in one run, and the
// remaining 2 drain on the next run.
func TestMergeConvergenceSweepBoundedBatch(t *testing.T) {
	ctx := context.Background()
	db, repo, svc, pid := mergeSweepFixture(t, "sched_merge_batch")

	var ids []uuid.UUID
	for i := 0; i < 5; i++ {
		ids = append(ids, seedExtractionBranch(t, ctx, db, repo, svc, pid, "batch-"+string(rune('a'+i)), true))
	}

	task := NewMergeConvergenceSweepTask(db, slog.New(slog.NewTextHandler(io.Discard, nil)), svc, time.Minute, 3)
	require.NoError(t, task.Run(ctx))

	mergedAfterFirst := 0
	for _, id := range ids {
		if branchMergedAt(t, ctx, db, id) != nil {
			mergedAfterFirst++
		}
	}
	require.Equal(t, 3, mergedAfterFirst, "one run must merge at most batchSize branches")

	require.NoError(t, task.Run(ctx))
	mergedAfterSecond := 0
	for _, id := range ids {
		if branchMergedAt(t, ctx, db, id) != nil {
			mergedAfterSecond++
		}
	}
	require.Equal(t, 5, mergedAfterSecond, "the second run must drain the remaining branches")
}

// TestMergeConvergenceSweepPolicyResolvers pins the pure policy mapping helpers.
func TestMergeConvergenceSweepPolicyResolvers(t *testing.T) {
	for _, p := range []string{"partial", "enrich", "partial_newest", "newest", "reconcile"} {
		require.Equal(t, p, resolveSweepMergePolicy(p))
	}
	require.Equal(t, "partial", resolveSweepMergePolicy(""))
	require.Equal(t, "partial", resolveSweepMergePolicy("unknown"))

	require.Equal(t, float32(0.92), resolveSweepThreshold(0.92))
	require.Equal(t, float32(0), resolveSweepThreshold(0))
	require.Equal(t, float32(0), resolveSweepThreshold(1.5))
	require.Equal(t, float32(0), resolveSweepThreshold(-1))
}

// TestNewMergeConvergenceSweepTask_Defaults pins the constructor contract: a
// non-positive age or batch size falls back to the defaults.
func TestNewMergeConvergenceSweepTask_Defaults(t *testing.T) {
	task := NewMergeConvergenceSweepTask(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, 0, -1)
	require.Equal(t, defaultMergeConvergenceMinAge, task.minAge)
	require.Equal(t, defaultMergeConvergenceBatchSize, task.batchSize)
}
