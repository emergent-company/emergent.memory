package agents

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// WorkItemComment is a single append-only durable comment on an object-driven
// work item, authored by a human (actor_type='user') or a work agent
// (actor_type='agent'). Unlike kb.work_item_feedback (rework rounds), a comment
// carries no round and never transitions the item. Table: kb.object_comments.
type WorkItemComment struct {
	bun.BaseModel `bun:"table:kb.object_comments,alias:oc"`

	ID          string    `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID   string    `bun:"project_id,type:uuid,notnull" json:"projectId"`
	CanonicalID string    `bun:"canonical_id,type:uuid,notnull" json:"canonicalId"`
	Author      *string   `bun:"author" json:"author,omitempty"`
	Body        string    `bun:"body,notnull" json:"body"`
	ActorType   *string   `bun:"actor_type" json:"actorType,omitempty"`
	RunID       *string   `bun:"run_id,type:uuid" json:"runId,omitempty"`
	CreatedAt   time.Time `bun:"created_at,notnull,default:now()" json:"createdAt"`
}

// AppendWorkComment records a new durable comment on a work item. actorType is
// the authoring party ('user' or 'agent'); runID, when non-nil, ties the comment
// to the work run that produced it. It returns the persisted comment.
func (r *Repository) AppendWorkComment(ctx context.Context, projectID, canonicalID, author, body, actorType string, runID *string) (*WorkItemComment, error) {
	c := &WorkItemComment{
		ProjectID:   projectID,
		CanonicalID: canonicalID,
		Body:        body,
	}
	if author != "" {
		c.Author = &author
	}
	if actorType != "" {
		c.ActorType = &actorType
	}
	c.RunID = runID
	if _, err := r.db.NewInsert().Model(c).Exec(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// ListWorkComments returns the comments for a work item in ascending created_at
// order. A nil slice is returned when there are no comments.
func (r *Repository) ListWorkComments(ctx context.Context, projectID, canonicalID string) ([]*WorkItemComment, error) {
	var out []*WorkItemComment
	err := r.db.NewSelect().
		Model(&out).
		Where("project_id = ?", projectID).
		Where("canonical_id = ?", canonicalID).
		Order("created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return out, nil
}
