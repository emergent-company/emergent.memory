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
func (r *Repository) IncrementWorkItemFailure(ctx context.Context, projectID, canonicalID, failureClass string, requeueBackoffAt *time.Time) (int, error) {
	now := time.Now()
	existing, err := r.GetWorkItemState(ctx, projectID, canonicalID)
	if err != nil {
		return 0, err
	}
	if existing == nil {
		state := &WorkItemState{
			ProjectID:        projectID,
			CanonicalID:      canonicalID,
			FailureCount:     1,
			LastFailureClass: failureClass,
			UpdatedAt:        now,
		}
		if requeueBackoffAt != nil {
			state.RequeueBackoffAt = *requeueBackoffAt
		}
		if _, err := r.db.NewInsert().Model(state).Exec(ctx); err != nil {
			return 0, err
		}
		return 1, nil
	}

	q := r.db.NewUpdate().
		Model((*WorkItemState)(nil)).
		Set("failure_count = failure_count + 1").
		Set("last_failure_class = ?", failureClass).
		Set("updated_at = ?", now).
		Where("project_id = ?", projectID).
		Where("canonical_id = ?", canonicalID)
	if requeueBackoffAt != nil {
		q = q.Set("requeue_backoff_at = ?", *requeueBackoffAt)
	}
	if _, err := q.Exec(ctx); err != nil {
		return 0, err
	}
	return existing.FailureCount + 1, nil
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
