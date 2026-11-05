package agents

import (
	"context"
	"database/sql"
	"time"

	"github.com/uptrace/bun"
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
