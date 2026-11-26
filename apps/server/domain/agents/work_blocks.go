package agents

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/tasks"
)

// WorkBlock is the durable blocked state for a work item. It is keyed by the
// object's stable canonical id (the physical graph row changes per version) and
// suppresses dispatch, claim, and reconciliation while present. The workflow
// status is untouched.
// Table: kb.work_blocks
type WorkBlock struct {
	bun.BaseModel `bun:"table:kb.work_blocks,alias:wb"`

	CanonicalID  string    `bun:"canonical_id,pk,type:uuid" json:"canonicalId"`
	ProjectID    string    `bun:"project_id,type:uuid,notnull" json:"projectId"`
	Reason       string    `bun:"reason,notnull" json:"reason"`
	FailureClass *string   `bun:"failure_class" json:"failureClass,omitempty"`
	BlockedAt    time.Time `bun:"blocked_at,notnull" json:"blockedAt"`
	BlockedBy    *string   `bun:"blocked_by" json:"blockedBy,omitempty"`
	Note         *string   `bun:"note" json:"note,omitempty"`
	RunID        *string   `bun:"run_id,type:uuid" json:"runId,omitempty"`
	CreatedAt    time.Time `bun:"created_at,notnull" json:"createdAt"`
	UpdatedAt    time.Time `bun:"updated_at,notnull" json:"updatedAt"`
}

// UpsertWorkBlock records (or refreshes) a block row.
func (r *Repository) UpsertWorkBlock(ctx context.Context, b *WorkBlock) error {
	now := time.Now()
	if b.BlockedAt.IsZero() {
		b.BlockedAt = now
	}
	_, err := r.db.NewRaw(`
		INSERT INTO kb.work_blocks
			(canonical_id, project_id, reason, failure_class, blocked_at, blocked_by, note, run_id, created_at, updated_at)
		VALUES (?::uuid, ?::uuid, ?, ?, ?, ?, ?, ?::uuid, ?, ?)
		ON CONFLICT (canonical_id) DO UPDATE SET
			project_id = EXCLUDED.project_id,
			reason = EXCLUDED.reason,
			failure_class = EXCLUDED.failure_class,
			blocked_at = EXCLUDED.blocked_at,
			blocked_by = EXCLUDED.blocked_by,
			note = EXCLUDED.note,
			run_id = EXCLUDED.run_id,
			updated_at = EXCLUDED.updated_at`,
		b.CanonicalID, b.ProjectID, b.Reason, b.FailureClass, b.BlockedAt, b.BlockedBy, b.Note, b.RunID, now, now,
	).Exec(ctx)
	return err
}

// ClearWorkBlock removes the block row. It returns whether a row existed.
func (r *Repository) ClearWorkBlock(ctx context.Context, canonicalID string) (bool, error) {
	res, err := r.db.NewDelete().
		Model((*WorkBlock)(nil)).
		Where("canonical_id = ?", canonicalID).
		Exec(ctx)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetWorkBlock returns the block row, or nil when not blocked.
func (r *Repository) GetWorkBlock(ctx context.Context, canonicalID string) (*WorkBlock, error) {
	var b WorkBlock
	err := r.db.NewSelect().
		Model(&b).
		Where("canonical_id = ?", canonicalID).
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &b, nil
}

// IsWorkBlocked reports whether the item has a block row.
func (r *Repository) IsWorkBlocked(ctx context.Context, canonicalID string) (bool, error) {
	exists, err := r.db.NewSelect().
		Model((*WorkBlock)(nil)).
		Where("canonical_id = ?", canonicalID).
		Exists(ctx)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// budgetParkBlockedBy is the blocked_by marker for a durable park recorded by
// the enqueue-time spending gate.
const budgetParkBlockedBy = "budget-gate"

// WorkBudgetParkedEvent describes a work dispatch the project spending gate
// refused before a run or job row could be created. Listeners use it to publish
// a caller-visible state (for example a head-bound review check) instead of
// leaving an in-progress check waiting for a run that was never scheduled.
type WorkBudgetParkedEvent struct {
	ProjectID   string
	CanonicalID string
	ObjectType  string
	AgentID     string
	AgentName   string
	Reason      string
	Head        *graph.WorkObjectHead
}

// WorkBudgetParkListener observes an enqueue-time budget refusal. It runs on the
// dispatch path, so implementations must be quick and must not fail the park.
type WorkBudgetParkListener func(ctx context.Context, ev WorkBudgetParkedEvent)

// AddWorkBudgetParkListener registers a listener for budget-parked dispatches.
// Registered once at construction by the sources module; nil-safe.
func (r *Repository) AddWorkBudgetParkListener(fn WorkBudgetParkListener) {
	if r == nil || fn == nil {
		return
	}
	r.workBudgetParkMu.Lock()
	defer r.workBudgetParkMu.Unlock()
	r.workBudgetParkListeners = append(r.workBudgetParkListeners, fn)
}

// NotifyWorkBudgetParked fans a budget-park event out to every listener. A
// listener panic must not take down the triggering dispatch goroutine, and a
// listener error is that listener's responsibility to log.
func (r *Repository) NotifyWorkBudgetParked(ctx context.Context, ev WorkBudgetParkedEvent) {
	if r == nil {
		return
	}
	r.workBudgetParkMu.RLock()
	listeners := append([]WorkBudgetParkListener(nil), r.workBudgetParkListeners...)
	r.workBudgetParkMu.RUnlock()
	for _, fn := range listeners {
		func() {
			defer func() { _ = recover() }()
			fn(ctx, ev)
		}()
	}
}

// ParkWorkItemForBudget records the durable parked state for an item whose
// dispatch the spending gate refused. The block is the stop condition for the
// reconciler: while it is present the item is neither claimed nor re-enqueued,
// so a capped project stops logging a refusal per tick. The state is
// recoverable: when the cap is raised or the window reset the reconciler
// clears a budget block, and a human can always unblock or retry the item.
//
// A dead-letter task is created only for the first park; refreshing an existing
// block (a duplicate delivery) must not pile up duplicate escalations.
func (r *Repository) ParkWorkItemForBudget(ctx context.Context, projectID, canonicalID, reason string) error {
	if r == nil || canonicalID == "" {
		return nil
	}
	existing, err := r.GetWorkBlock(ctx, canonicalID)
	if err != nil {
		return err
	}
	class := string(FailureClassBudget)
	blockedBy := budgetParkBlockedBy
	if err := r.UpsertWorkBlock(ctx, &WorkBlock{
		CanonicalID:  canonicalID,
		ProjectID:    projectID,
		Reason:       reason,
		FailureClass: &class,
		BlockedBy:    &blockedBy,
	}); err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	desc := fmt.Sprintf("class=%s: %s", FailureClassBudget, reason)
	sourceType := "work_object"
	_, err = r.CreateWorkTask(ctx, &tasks.Task{
		ProjectID:   projectID,
		Title:       "Work item parked (budget)",
		Description: &desc,
		Type:        "work-escalation",
		Status:      "pending",
		SourceType:  &sourceType,
		SourceID:    &canonicalID,
	})
	return err
}
