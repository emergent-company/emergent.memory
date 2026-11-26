package extraction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// branchSweepBatchSize caps how many stale branches one pass reaps, so a large
// backlog cannot make a single sweep unbounded.
const branchSweepBatchSize = 100

// BranchSweepConfig configures the extraction-branch retention sweep.
type BranchSweepConfig struct {
	// Interval is how often the sweep runs.
	Interval time.Duration
	// Retention is how old an unmerged extraction branch must be before the
	// sweep reaps it. Non-positive disables the sweep (nothing is reaped).
	Retention time.Duration
}

// DefaultBranchSweepConfig returns the default sweep configuration, overridable
// with BRANCH_SWEEP_INTERVAL_MINUTES / BRANCH_SWEEP_RETENTION_DAYS. A non-positive
// or absurdly large value falls back to the default, so a bad env var can never
// produce a zero (ticker panic) or negative (overflow) interval. Retention 0 is
// a valid "disabled" value.
func DefaultBranchSweepConfig() *BranchSweepConfig {
	return &BranchSweepConfig{
		Interval:  time.Duration(envInt("BRANCH_SWEEP_INTERVAL_MINUTES", 30, 1, 100000)) * time.Minute,
		Retention: time.Duration(envInt("BRANCH_SWEEP_RETENTION_DAYS", 7, 0, 10000)) * 24 * time.Hour,
	}
}

// envInt reads a non-negative integer env var, falling back to def when unset,
// unparsable, or outside [min, max].
func envInt(name string, def, min, max int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= min && n <= max {
			return n
		}
	}
	return def
}

// BranchSweepResult reports one sweep pass.
type BranchSweepResult struct {
	BranchesReaped       int `json:"branches_reaped"`
	ObjectsDeleted       int `json:"objects_deleted"`
	RelationshipsDeleted int `json:"relationships_deleted"`
}

// BranchSweepWorker reaps stale, unmerged extraction branches. An extraction
// branch that the pipeline never auto-merged (objects left for review, or an
// auto-merge failure) would otherwise sit on the branch list forever; after the
// retention window the sweep soft-deletes the branch's objects and relationships
// and removes the branch, so dead staging branches do not accumulate.
type BranchSweepWorker struct {
	db  bun.IDB
	log *slog.Logger
	cfg *BranchSweepConfig

	mu        sync.Mutex
	running   bool
	stopCh    chan struct{}
	stoppedCh chan struct{}
}

// NewBranchSweepWorker creates a branch sweep worker. A nil cfg uses the default.
func NewBranchSweepWorker(db bun.IDB, log *slog.Logger, cfg *BranchSweepConfig) *BranchSweepWorker {
	if cfg == nil {
		cfg = DefaultBranchSweepConfig()
	}
	return &BranchSweepWorker{
		db:  db,
		log: log.With(logger.Scope("branch-sweep")),
		cfg: cfg,
	}
}

// Start begins the sweep loop. It is idempotent.
func (w *BranchSweepWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return nil
	}
	w.running = true
	stopCh := make(chan struct{})
	stoppedCh := make(chan struct{})
	w.stopCh = stopCh
	w.stoppedCh = stoppedCh

	w.log.Info("branch sweep worker starting",
		slog.Duration("interval", w.cfg.Interval),
		slog.Duration("retention", w.cfg.Retention))
	go w.run(ctx, stopCh, stoppedCh)
	return nil
}

// Stop gracefully stops the sweep loop.
func (w *BranchSweepWorker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = false
	stopCh := w.stopCh
	stoppedCh := w.stoppedCh
	close(stopCh)
	w.mu.Unlock()

	select {
	case <-stoppedCh:
	case <-ctx.Done():
	}
	return nil
}

// run is the sweep loop. stopCh/stoppedCh are passed in as locals so the loop
// never reads the struct fields (which Start reassigns) without the mutex.
func (w *BranchSweepWorker) run(ctx context.Context, stopCh, stoppedCh chan struct{}) {
	defer close(stoppedCh)
	// One immediate pass so a restart does not wait a full interval before the
	// first reap.
	w.sweepOnce(ctx)

	// A non-positive interval means "sweep once then stop" — never a ticker,
	// which panics for d <= 0.
	if w.cfg.Interval <= 0 {
		return
	}

	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.sweepOnce(ctx)
		}
	}
}

func (w *BranchSweepWorker) sweepOnce(ctx context.Context) {
	res, err := w.Sweep(ctx)
	if err != nil {
		w.log.Warn("branch sweep failed", logger.Error(err))
		return
	}
	if res.BranchesReaped > 0 {
		w.log.Info("branch sweep reaped stale extraction branches",
			slog.Int("branches", res.BranchesReaped),
			slog.Int("objects", res.ObjectsDeleted),
			slog.Int("relationships", res.RelationshipsDeleted))
	}
}

// Sweep runs one pass: every stale unmerged extraction branch is reaped — its
// objects and relationships are soft-deleted and the branch removed. A
// non-positive retention disables the sweep (nothing is reaped).
func (w *BranchSweepWorker) Sweep(ctx context.Context) (BranchSweepResult, error) {
	var res BranchSweepResult
	if w.cfg.Retention <= 0 {
		return res, nil
	}
	cutoff := time.Now().Add(-w.cfg.Retention)

	var ids []string
	err := w.db.NewSelect().
		Table("kb.branches").
		Column("id").
		Where("origin = 'extraction'").
		Where("merged_at IS NULL").
		Where("created_at < ?", cutoff).
		Order("created_at ASC").
		Limit(branchSweepBatchSize).
		Scan(ctx, &ids)
	if err != nil {
		return res, fmt.Errorf("list stale extraction branches: %w", err)
	}

	for _, id := range ids {
		objects, relationships, reaped, err := w.reapBranch(ctx, id)
		if err != nil {
			w.log.Warn("failed to reap extraction branch",
				slog.String("branch_id", id), logger.Error(err))
			continue
		}
		if !reaped {
			continue
		}
		res.BranchesReaped++
		res.ObjectsDeleted += objects
		res.RelationshipsDeleted += relationships
	}
	return res, nil
}

// reapBranch soft-deletes the branch's objects and relationships (clearing their
// branch_id so no dangling reference survives the branch removal), then removes
// the branch row and its lineage, all in one transaction. The branch row is
// locked and re-checked for merged_at IS NULL inside the transaction, so a
// branch that a concurrent merge (or another replica) claimed after the
// candidate list was read is left untouched.
func (w *BranchSweepWorker) reapBranch(ctx context.Context, branchID string) (objects, relationships int, reaped bool, err error) {
	err = w.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var mergedAt *time.Time
		err := tx.NewRaw(`SELECT merged_at FROM kb.branches WHERE id = ? FOR UPDATE`, branchID).Scan(ctx, &mergedAt)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil // already gone
			}
			return fmt.Errorf("lock branch: %w", err)
		}
		if mergedAt != nil {
			return nil // merged concurrently — do not touch it
		}

		objRes, err := tx.NewRaw(`
			UPDATE kb.graph_objects
			SET deleted_at = now(), updated_at = now(), branch_id = NULL
			WHERE branch_id = ? AND deleted_at IS NULL`, branchID).Exec(ctx)
		if err != nil {
			return fmt.Errorf("soft-delete objects: %w", err)
		}
		if n, err := objRes.RowsAffected(); err == nil {
			objects = int(n)
		}

		relRes, err := tx.NewRaw(`
			UPDATE kb.graph_relationships
			SET deleted_at = now(), branch_id = NULL
			WHERE branch_id = ? AND deleted_at IS NULL`, branchID).Exec(ctx)
		if err != nil {
			return fmt.Errorf("soft-delete relationships: %w", err)
		}
		if n, err := relRes.RowsAffected(); err == nil {
			relationships = int(n)
		}

		if _, err := tx.NewRaw(`DELETE FROM kb.branch_lineage WHERE branch_id = ?`, branchID).Exec(ctx); err != nil {
			return fmt.Errorf("delete branch lineage: %w", err)
		}
		if _, err := tx.NewRaw(`DELETE FROM kb.branches WHERE id = ? AND merged_at IS NULL`, branchID).Exec(ctx); err != nil {
			return fmt.Errorf("delete branch: %w", err)
		}
		reaped = true
		return nil
	})
	return objects, relationships, reaped, err
}
