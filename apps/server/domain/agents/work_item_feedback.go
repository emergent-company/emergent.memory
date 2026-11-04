package agents

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// WorkItemFeedback is a single append-only rework request round for an
// object-driven work item. It is keyed by (project_id, canonical_id, round);
// the revision count used by the revision-cap escalation is derived from
// MAX(round). Table: kb.work_item_feedback
type WorkItemFeedback struct {
	bun.BaseModel `bun:"table:kb.work_item_feedback,alias:wif"`

	ID          string    `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID   string    `bun:"project_id,type:uuid,notnull" json:"projectId"`
	CanonicalID string    `bun:"canonical_id,type:uuid,notnull" json:"canonicalId"`
	Round       int       `bun:"round,notnull" json:"round"`
	Author      *string   `bun:"author" json:"author,omitempty"`
	Text        string    `bun:"text,notnull" json:"text"`
	RunID       *string   `bun:"run_id,type:uuid" json:"runId,omitempty"`
	CreatedAt   time.Time `bun:"created_at,notnull,default:now()" json:"createdAt"`
}

// AppendWorkFeedback records a new feedback round for a work item, deriving the
// round number from the current MAX(round) under a transaction so concurrent
// change requests never collide on the same round. It returns the assigned
// round.
func (r *Repository) AppendWorkFeedback(ctx context.Context, projectID, canonicalID, author, text string, runID *string) (int, error) {
	round := 0
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := tx.NewRaw(
			`SELECT COALESCE(MAX(round), 0) FROM kb.work_item_feedback WHERE project_id = ?::uuid AND canonical_id = ?::uuid`,
			projectID, canonicalID,
		).Scan(ctx, &round); err != nil {
			return err
		}
		round++
		fb := &WorkItemFeedback{
			ProjectID:   projectID,
			CanonicalID: canonicalID,
			Round:       round,
			Text:        text,
		}
		if author != "" {
			fb.Author = &author
		}
		fb.RunID = runID
		_, err := tx.NewInsert().Model(fb).Exec(ctx)
		return err
	})
	if err != nil {
		return 0, err
	}
	return round, nil
}

// ListWorkFeedback returns the feedback history for a work item in ascending
// round order. A nil slice is returned when there is no feedback.
func (r *Repository) ListWorkFeedback(ctx context.Context, projectID, canonicalID string) ([]*WorkItemFeedback, error) {
	var out []*WorkItemFeedback
	err := r.db.NewSelect().
		Model(&out).
		Where("project_id = ?", projectID).
		Where("canonical_id = ?", canonicalID).
		Order("round ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// FeedbackRoundCount returns the highest feedback round recorded for a work
// item (0 when there is none). This is the revision count for escalation.
func (r *Repository) FeedbackRoundCount(ctx context.Context, projectID, canonicalID string) (int, error) {
	var n int
	err := r.db.NewRaw(
		`SELECT COALESCE(MAX(round), 0) FROM kb.work_item_feedback WHERE project_id = ?::uuid AND canonical_id = ?::uuid`,
		projectID, canonicalID,
	).Scan(ctx, &n)
	if err != nil {
		return 0, err
	}
	return n, nil
}
