package agents

import (
	"context"
	"database/sql"
	"time"

	"github.com/uptrace/bun"
)

// WorkItemState is the sidecar ledger for per-work-item failure accounting. It
// survives re-enqueue and version churn because it is keyed by (project_id,
// canonical_id), which is stable across object versions — unlike the versioned
// kb.graph_objects rows, whose physical id changes on every transition.
// Table: kb.work_item_state
type WorkItemState struct {
	bun.BaseModel `bun:"table:kb.work_item_state,alias:wis"`

	ProjectID        string    `bun:"project_id,type:uuid,pk" json:"projectId"`
	CanonicalID      string    `bun:"canonical_id,type:uuid,pk" json:"canonicalId"`
	FailureCount     int       `bun:"failure_count,notnull,default:0" json:"failureCount"`
	LastFailureClass string    `bun:"last_failure_class" json:"lastFailureClass,omitempty"`
	RequeueBackoffAt time.Time `bun:"requeue_backoff_at,nullzero" json:"requeueBackoffAt,omitempty"`
	UpdatedAt        time.Time `bun:"updated_at" json:"updatedAt"`
}

// GetWorkItemState returns the sidecar state for a work item, or nil when no
// row exists.
func (r *Repository) GetWorkItemState(ctx context.Context, projectID, canonicalID string) (*WorkItemState, error) {
	var s WorkItemState
	err := r.db.NewSelect().
		Model(&s).
		Where("project_id = ?", projectID).
		Where("canonical_id = ?", canonicalID).
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// IncrementWorkItemFailure bumps the failure count for a work item and records
// the last failure class, returning the new count. It optionally sets a requeue
// backoff (used by capability failures and timeouts so a re-enqueued item waits
// before the next attempt).
//
// The increment is a single atomic INSERT ... ON CONFLICT ... DO UPDATE so
// concurrent failures across replicas cannot double-increment (the read-then-
// insert/update race).
func (r *Repository) IncrementWorkItemFailure(ctx context.Context, projectID, canonicalID, failureClass string, requeueBackoffAt *time.Time) (int, error) {
	now := time.Now()
	var backoffArg any
	if requeueBackoffAt != nil {
		backoffArg = *requeueBackoffAt
	}
	var count int
	err := r.db.NewRaw(`
		INSERT INTO kb.work_item_state (project_id, canonical_id, failure_count, last_failure_class, requeue_backoff_at, updated_at)
		VALUES (?::uuid, ?::uuid, 1, ?, ?, ?)
		ON CONFLICT (project_id, canonical_id)
		DO UPDATE SET
			failure_count = kb.work_item_state.failure_count + 1,
			last_failure_class = EXCLUDED.last_failure_class,
			requeue_backoff_at = COALESCE(EXCLUDED.requeue_backoff_at, kb.work_item_state.requeue_backoff_at),
			updated_at = EXCLUDED.updated_at
		RETURNING failure_count`,
		projectID, canonicalID, failureClass, backoffArg, now,
	).Scan(ctx, &count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// ClearWorkItemState removes the failure ledger for a work item (a successful
// terminator, or a human retry/reset). It is a no-op when no row exists.
func (r *Repository) ClearWorkItemState(ctx context.Context, projectID, canonicalID string) error {
	_, err := r.db.NewDelete().
		Model((*WorkItemState)(nil)).
		Where("project_id = ?", projectID).
		Where("canonical_id = ?", canonicalID).
		Exec(ctx)
	return err
}
