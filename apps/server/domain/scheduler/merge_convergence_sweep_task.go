package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"encoding/json"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/pipeline"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// MergeConvergenceSweepTask retries merges for unmerged, embedding-ready
// extraction branches. It is the safety net for the auto-merge-on-extraction
// path: when a job's auto-merge is skipped because embeddings were not yet
// settled (waitForEmbeddings timed out), the branch is never retried by the
// worker, so its data strands forever on the staging branch. This sweep
// re-selects those branches once they are fully embedded and merges them under
// the project's resolved pipeline policy.
//
// It is deliberately a safety net, not a replacement: auto-merge-on-extraction
// is unchanged, and this task only heals branches that auto-merge left behind.
type MergeConvergenceSweepTask struct {
	db        *bun.DB
	log       *slog.Logger
	graph     *graph.Service
	minAge    time.Duration
	batchSize int
}

// Defaults for the sweep. minAge leaves room for a branch whose embeddings are
// still settling (a freshly extracted branch is not ready anyway, but the grace
// also avoids racing the worker's own auto-merge); batchSize bounds the burst a
// first run over a large backlog creates.
const (
	defaultMergeConvergenceMinAge    = 10 * time.Minute
	defaultMergeConvergenceBatchSize = 50
)

// mergeConvergenceLockKey is a stable, arbitrary Postgres session-level
// advisory-lock key so concurrent sweepers (several server replicas share one
// database) cannot both act on the same branch set.
const mergeConvergenceLockKey int64 = 0x6d656d636f6e76 // "memconv"

// NewMergeConvergenceSweepTask creates the sweep. A non-positive minAge or
// batchSize falls back to the defaults.
func NewMergeConvergenceSweepTask(db *bun.DB, log *slog.Logger, graphSvc *graph.Service, minAge time.Duration, batchSize int) *MergeConvergenceSweepTask {
	if minAge <= 0 {
		minAge = defaultMergeConvergenceMinAge
	}
	if batchSize <= 0 {
		batchSize = defaultMergeConvergenceBatchSize
	}
	return &MergeConvergenceSweepTask{
		db:        db,
		log:       log.With(logger.Scope("scheduler.merge_convergence_sweep")),
		graph:     graphSvc,
		minAge:    minAge,
		batchSize: batchSize,
	}
}

// mergeConvergenceCandidate is one branch the sweep selected for a merge retry.
type mergeConvergenceCandidate struct {
	BranchID  uuid.UUID `bun:"id"`
	ProjectID uuid.UUID `bun:"project_id"`
}

// mergeConvergenceQuery selects a bounded FIFO batch of unmerged extraction
// branches that are ready to merge: they carry at least one live object, every
// live object is embedded (pending_embeddings == 0, reusing the MergeReadiness
// semantics of graph.CountUnembeddedObjectsInBranch), and they are older than
// the grace window so the worker's own auto-merge gets a chance to run first.
//
// The selection is a plain read; each branch is re-checked (merged_at and
// readiness) before its merge, and the advisory lock in Run serialises the
// remaining race between concurrent sweepers.
const mergeConvergenceQuery = `
	SELECT b.id, b.project_id
	FROM kb.branches b
	WHERE b.origin = 'extraction'
	  AND b.merged_at IS NULL
	  AND b.created_at < now() - make_interval(mins => ?)
	  AND EXISTS (
	      SELECT 1 FROM kb.graph_objects go
	      WHERE go.project_id = b.project_id
	        AND go.branch_id = b.id
	        AND go.supersedes_id IS NULL
	        AND go.deleted_at IS NULL
	  )
	  AND NOT EXISTS (
	      SELECT 1 FROM kb.graph_objects go
	      WHERE go.project_id = b.project_id
	        AND go.branch_id = b.id
	        AND go.supersedes_id IS NULL
	        AND go.deleted_at IS NULL
	        AND go.embedding_v2 IS NULL
	  )
	ORDER BY b.created_at ASC, b.id ASC
	LIMIT ?`

// Run retries merges for embedding-ready extraction branches. It is best-effort:
// a per-branch failure is logged and skipped (the next tick retries it), and the
// sweep never fails hard, so one poison branch cannot stall the whole backlog.
func (t *MergeConvergenceSweepTask) Run(ctx context.Context) error {
	start := time.Now()

	release, err := t.acquireLock(ctx)
	if err != nil {
		t.log.Warn("merge convergence sweep skipped: could not determine lock", logger.Error(err))
		return nil
	}
	if release == nil {
		t.log.Debug("merge convergence sweep skipped: another sweep is running")
		return nil
	}
	defer release()

	var candidates []mergeConvergenceCandidate
	if err := t.db.NewRaw(mergeConvergenceQuery, int(t.minAge.Minutes()), t.batchSize).Scan(ctx, &candidates); err != nil {
		return fmt.Errorf("select unmerged extraction branches: %w", err)
	}
	if len(candidates) == 0 {
		t.log.Debug("merge convergence sweep: no unmerged extraction branches")
		return nil
	}

	merged := 0
	skippedEmbedding := 0
	skippedError := 0
	for _, c := range candidates {
		if ctx.Err() != nil {
			break
		}
		if t.mergeCandidate(ctx, c) {
			merged++
		} else if t.isPendingEmbeddings(ctx, c) {
			skippedEmbedding++
		} else {
			skippedError++
		}
	}

	if merged > 0 || skippedEmbedding > 0 || skippedError > 0 {
		t.log.Info("merge convergence sweep completed",
			slog.Int("selected", len(candidates)),
			slog.Int("merged", merged),
			slog.Int("skipped_pending_embeddings", skippedEmbedding),
			slog.Int("skipped_error", skippedError),
			slog.Duration("duration", time.Since(start)))
	} else {
		t.log.Debug("merge convergence sweep completed",
			slog.Int("selected", len(candidates)),
			slog.Duration("duration", time.Since(start)))
	}
	return nil
}

// mergeCandidate merges one branch into its project's main graph. It returns
// true when the branch was merged (or was already merged by a concurrent
// sweeper). It re-checks merged_at and readiness so a branch that another
// process just merged, or whose embeddings reset, is never double-applied.
func (t *MergeConvergenceSweepTask) mergeCandidate(ctx context.Context, c mergeConvergenceCandidate) bool {
	if t.graph == nil {
		t.log.Warn("merge convergence sweep skipped: graph service not wired")
		return false
	}

	merged, err := t.branchMerged(ctx, c.BranchID)
	if err != nil {
		t.log.Warn("merge convergence sweep: could not read branch, skipping",
			slog.String("branch_id", c.BranchID.String()), logger.Error(err))
		return false
	}
	if merged {
		return true
	}

	if t.isPendingEmbeddings(ctx, c) {
		return false
	}

	policy, threshold, reconcileCap := t.resolvePolicy(ctx, c.ProjectID)
	req := &graph.BranchMergeRequest{
		SourceBranchID:      c.BranchID,
		Execute:             true,
		Policy:              policy,
		SimilarityThreshold: threshold,
		ReconcileMaxPerRun:  reconcileCap,
	}
	if _, err := t.graph.MergeBranch(ctx, c.ProjectID, nil, req); err != nil {
		t.log.Warn("merge convergence sweep: merge failed, branch left for next tick",
			slog.String("project_id", c.ProjectID.String()),
			slog.String("branch_id", c.BranchID.String()),
			logger.Error(err))
		return false
	}

	t.log.Info("merge convergence sweep: merged stranded extraction branch",
		slog.String("project_id", c.ProjectID.String()),
		slog.String("branch_id", c.BranchID.String()),
		slog.String("policy", policy))
	return true
}

// branchMerged reports whether the branch's merged_at is already stamped.
func (t *MergeConvergenceSweepTask) branchMerged(ctx context.Context, branchID uuid.UUID) (bool, error) {
	var mergedAt *time.Time
	err := t.db.NewRaw(`SELECT merged_at FROM kb.branches WHERE id = ?`, branchID).Scan(ctx, &mergedAt)
	if err != nil {
		return false, err
	}
	return mergedAt != nil, nil
}

// isPendingEmbeddings reports whether the branch still has live objects without
// embeddings. It mirrors graph.BranchMergeReadiness's readiness definition.
func (t *MergeConvergenceSweepTask) isPendingEmbeddings(ctx context.Context, c mergeConvergenceCandidate) bool {
	if t.graph == nil {
		return false
	}
	_, pending, err := t.graph.BranchMergeReadiness(ctx, c.ProjectID, c.BranchID)
	if err != nil {
		t.log.Warn("merge convergence sweep: readiness check failed, skipping",
			slog.String("branch_id", c.BranchID.String()), logger.Error(err))
		return true
	}
	return pending > 0
}

// resolvePolicy resolves the project's effective pipeline merge policy into the
// values BranchMergeRequest accepts, mirroring the worker's autoMergePolicy.
// It reads kb.projects directly (the projects domain package would create an
// import cycle with the scheduler). Any failure falls back to the safe "partial"
// preset so a broken project row can never block, or degrade, the safety net.
func (t *MergeConvergenceSweepTask) resolvePolicy(ctx context.Context, projectID uuid.UUID) (policy string, threshold float32, reconcileCap int) {
	fallback := func() (string, float32, int) { return "partial", 0, 0 }

	var row struct {
		PipelineMode   string `bun:"pipeline_mode"`
		PipelinePolicy string `bun:"pipeline_policy"`
	}
	if err := t.db.NewRaw(
		`SELECT pipeline_mode, pipeline_policy::text AS pipeline_policy FROM kb.projects WHERE id = ?`,
		projectID,
	).Scan(ctx, &row); err != nil {
		return fallback()
	}

	pol, err := pipeline.Resolve(row.PipelineMode, json.RawMessage(row.PipelinePolicy))
	if err != nil {
		return fallback()
	}
	return resolveSweepMergePolicy(pol.Merge.Policy),
		resolveSweepThreshold(pol.Merge.SimilarityThreshold),
		pol.Merge.ReconcileMaxPerRun
}

// resolveSweepMergePolicy maps a stored merge-policy enum to a value
// BranchMergeRequest accepts; anything unrecognised falls back to partial.
func resolveSweepMergePolicy(policy string) string {
	switch policy {
	case "partial", "enrich", "partial_newest", "newest", "reconcile":
		return policy
	default:
		return "partial"
	}
}

// resolveSweepThreshold returns the pipeline's similarity cutoff for the merge,
// or 0 when the policy leaves it unset (0 = the graph preset's default).
func resolveSweepThreshold(threshold float32) float32 {
	if threshold > 0 && threshold <= 1 {
		return threshold
	}
	return 0
}

// acquireLock takes a cross-process session-level advisory lock so concurrent
// sweepers do not both process the same branch set. It returns a release func on
// success, or (nil, nil) when another process holds the lock (caller skips).
func (t *MergeConvergenceSweepTask) acquireLock(ctx context.Context) (func(), error) {
	conn, err := t.db.DB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire merge convergence lock connection: %w", err)
	}
	var locked bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", mergeConvergenceLockKey).Scan(&locked); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("acquire merge convergence advisory lock: %w", err)
	}
	if !locked {
		_ = conn.Close()
		return nil, nil
	}
	return func() {
		defer conn.Close()
		releaseCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(releaseCtx, "SELECT pg_advisory_unlock($1)", mergeConvergenceLockKey); err != nil {
			t.log.Warn("failed to release merge convergence advisory lock", logger.Error(err))
		}
	}, nil
}
