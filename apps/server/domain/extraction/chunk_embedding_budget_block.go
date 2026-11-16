package extraction

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// budgetBlockStore is the shared "this project is over budget" cache behind the
// chunk embedding worker. It deliberately lives outside the worker process: the
// verdict used to be a per-process map, so when one replica un-parked a project
// (the admin raised or cleared the cap) every other replica kept its warm
// over-budget verdict and re-parked the jobs it dequeued until its own TTL
// expired (issue #793). Shared state makes an un-park visible everywhere at once.
//
// The store is only a cache: it never decides that embedding is allowed, it only
// skips the authoritative budget check for a bounded window.
type budgetBlockStore interface {
	// active reports whether projectID is still inside a cached over-budget window.
	active(ctx context.Context, projectID string) (bool, error)
	// mark caches projectID as over budget for ttl. The deadline is derived
	// inside the database (see dbBudgetBlockStore.mark), so the window is
	// extended relative to the same clock `active` reads (#818).
	mark(ctx context.Context, projectID string, ttl time.Duration) error
	// clear drops the cached verdict so the next job re-reads the budget.
	clear(ctx context.Context, projectID string) error
}

// newBudgetBlockStore returns a database-backed store, or nil when no database
// is wired (the worker then behaves as if nothing is cached).
func newBudgetBlockStore(db bun.IDB) budgetBlockStore {
	if db == nil {
		return nil
	}
	return &dbBudgetBlockStore{db: db}
}

// dbBudgetBlockStore stores one row per blocked project in
// kb.chunk_embedding_budget_blocks, so every replica reads and clears the same
// verdict.
type dbBudgetBlockStore struct {
	db bun.IDB
}

// active answers from the database clock. `mark` writes `blocked_until` on that
// same clock, so a verdict computed by one replica is judged by every other
// replica against one timeline instead of each replica's own skew (#818).
func (s *dbBudgetBlockStore) active(ctx context.Context, projectID string) (bool, error) {
	var active bool
	err := s.db.NewRaw(`SELECT EXISTS (
			SELECT 1 FROM kb.chunk_embedding_budget_blocks
			WHERE project_id = ? AND blocked_until > now()
		)`, projectID).Scan(ctx, &active)
	if err != nil {
		return false, fmt.Errorf("read chunk embedding budget block: %w", err)
	}
	return active, nil
}

// mark caches projectID as over budget for ttl.
//
// The deadline is `now() + ttl` in SQL, not time.Now() in the caller: the write
// and the `blocked_until > now()` read in active must be on one clock, or a
// replica whose clock runs ahead caches a verdict longer than the TTL and one
// that runs behind expires it early (#818).
//
// DO UPDATE ... WHERE excludes a re-mark whose deadline is not later than the
// stored one. A blocked project re-marks whenever a replica runs the budget
// check after its window expired, and concurrent replicas can detect the
// exhaustion together; without the guard each of those rewrote (and vacuumed)
// the row for a strictly shorter window, which changed nothing (#818).
func (s *dbBudgetBlockStore) mark(ctx context.Context, projectID string, ttl time.Duration) error {
	_, err := s.db.NewRaw(`INSERT INTO kb.chunk_embedding_budget_blocks
			(project_id, blocked_until, updated_at)
		VALUES (?, now() + make_interval(secs => ?), now())
		ON CONFLICT (project_id) DO UPDATE
		SET blocked_until = EXCLUDED.blocked_until,
			updated_at = now()
		WHERE chunk_embedding_budget_blocks.blocked_until < EXCLUDED.blocked_until`,
		projectID, ttl.Seconds()).Exec(ctx)
	if err != nil {
		return fmt.Errorf("cache chunk embedding budget block: %w", err)
	}
	return nil
}

func (s *dbBudgetBlockStore) clear(ctx context.Context, projectID string) error {
	_, err := s.db.NewRaw(`DELETE FROM kb.chunk_embedding_budget_blocks
		WHERE project_id = ?`, projectID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("clear chunk embedding budget block: %w", err)
	}
	return nil
}
