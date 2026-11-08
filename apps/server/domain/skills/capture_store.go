package skills

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
)

// --- Reviews ---

// CreateReview inserts a pending review row. A unique index on agent_run_id
// makes a duplicate insert a no-op via ON CONFLICT DO NOTHING.
func (r *Repository) CreateReview(ctx context.Context, review *SkillReview) error {
	_, err := r.db.NewInsert().
		Model(review).
		// Generic DO NOTHING: the per-run index dedups a replayed run and the
		// one-active-review-per-agent index closes the HasActiveReview race.
		On("CONFLICT DO NOTHING").
		Exec(ctx)
	if err != nil {
		return apperror.NewInternal("failed to create skill review", err)
	}
	return nil
}

// ClaimPendingReview atomically claims one pending review (or a running review
// whose worker died more than 30 minutes ago). Returns nil when nothing is due.
func (r *Repository) ClaimPendingReview(ctx context.Context) (*SkillReview, error) {
	var review SkillReview
	err := r.db.NewRaw(`
		UPDATE kb.skill_reviews
		   SET status = 'running', started_at = now()
		 WHERE id = (
		       SELECT id
		         FROM kb.skill_reviews
		        WHERE status = 'pending'
		           OR (status = 'running' AND started_at < now() - interval '30 minutes')
		        ORDER BY created_at
		        LIMIT 1
		        FOR UPDATE SKIP LOCKED
		 )
		RETURNING *`).Scan(ctx, &review)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, apperror.NewInternal("failed to claim skill review", err)
	}
	return &review, nil
}

// HasActiveReview reports whether an agent already has a pending/running
// review. Used by the nudge to avoid queueing duplicates.
func (r *Repository) HasActiveReview(ctx context.Context, agentID uuid.UUID) (bool, error) {
	return r.db.NewSelect().
		TableExpr("kb.skill_reviews").
		Where("agent_id = ?", agentID).
		Where("status IN ('pending', 'running')").
		Exists(ctx)
}

// LastReviewCreatedAt returns the most recent review time for an agent, or the
// zero time when none exists.
func (r *Repository) LastReviewCreatedAt(ctx context.Context, agentID uuid.UUID) (time.Time, error) {
	var at time.Time
	err := r.db.NewSelect().
		TableExpr("kb.skill_reviews").
		ColumnExpr("COALESCE(max(created_at), 'epoch'::timestamptz)").
		Where("agent_id = ?", agentID).
		Scan(ctx, &at)
	if err != nil {
		return time.Time{}, apperror.NewInternal("failed to read last skill review", err)
	}
	return at, nil
}

// CountCompletedRunsSince counts successful runs for an agent definition after
// t. Runs are recorded against the runtime kb.agents row, so join through it;
// a runtime-only agent (no definition) falls back to the agent id.
func (r *Repository) CountCompletedRunsSince(ctx context.Context, agentID uuid.UUID, t time.Time) (int, error) {
	n, err := r.db.NewSelect().
		TableExpr("kb.agent_runs AS ar").
		Join("JOIN kb.agents AS a ON a.id = ar.agent_id").
		Where("(a.agent_definition_id = ? OR ar.agent_id = ?)", agentID, agentID).
		// runstatus.Success is stored as "completed" (pkg/runstatus).
		Where("ar.status = 'completed'").
		Where("ar.created_at > ?", t).
		Count(ctx)
	if err != nil {
		return 0, apperror.NewInternal("failed to count runs since skill review", err)
	}
	return n, nil
}

// FinishReview closes a claimed review.
func (r *Repository) FinishReview(ctx context.Context, id uuid.UUID, status, model string,
	inputTokens, outputTokens, candidates int, errMsg string) error {
	q := r.db.NewUpdate().
		TableExpr("kb.skill_reviews").
		Set("status = ?", status).
		Set("model = ?", model).
		Set("input_tokens = ?", inputTokens).
		Set("output_tokens = ?", outputTokens).
		Set("candidate_count = ?", candidates).
		Set("finished_at = now()")
	if errMsg != "" {
		q = q.Set("error = ?", errMsg)
	}
	_, err := q.Where("id = ?", id).Exec(ctx)
	if err != nil {
		return apperror.NewInternal("failed to finish skill review", err)
	}
	return nil
}

// --- Proposals ---

// proposalClaimStaleAfter is how long an `applying` claim must be idle before
// another reviewer may reclaim it (or the inbox sweep may release it). It is the
// single source of truth: the SQL predicate, the sweep and the service pre-check
// all derive from it, so the claim path and the sweep can never disagree.
const proposalClaimStaleAfter = 10 * time.Minute

// proposalReclaimablePredicate is the single definition of "a reviewer may take
// this proposal": it is pending, or it was left `applying` with a claim older
// than proposalClaimStaleAfter (crashed between claim and write). A fresh
// `applying` row is deliberately excluded — that is the double-review guard.
// The staleness seconds are passed as an argument so the interval has one
// definition (this constant), never a duplicated SQL literal.
const proposalReclaimablePredicate = "(status = ? OR (status = ? AND (claimed_at IS NULL OR claimed_at < now() - make_interval(secs => ?))))"

// CreateProposal inserts a staged proposal.
func (r *Repository) CreateProposal(ctx context.Context, p *SkillProposal) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	if p.Status == "" {
		p.Status = ProposalStatusPending
	}
	if p.Source == "" {
		p.Source = ProposalSourceBackground
	}
	_, err := r.db.NewInsert().Model(p).Exec(ctx)
	if err != nil {
		return apperror.NewInternal("failed to create skill proposal", err)
	}
	return nil
}

// FindProposalByID loads a proposal by id.
func (r *Repository) FindProposalByID(ctx context.Context, id uuid.UUID) (*SkillProposal, error) {
	p := new(SkillProposal)
	err := r.db.NewSelect().Model(p).Where("sp.id = ?", id).Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperror.NewNotFound("skill proposal", id.String())
		}
		return nil, apperror.NewInternal("failed to get skill proposal", err)
	}
	return p, nil
}

// ListProposals returns proposals for a project, newest first. status "" means
// all statuses.
func (r *Repository) ListProposals(ctx context.Context, projectID uuid.UUID, status string, limit int) ([]*SkillProposal, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	out := make([]*SkillProposal, 0, limit)
	q := r.db.NewSelect().Model(&out).Where("sp.project_id = ?", projectID)
	if status != "" {
		q = q.Where("sp.status = ?", status)
	}
	if err := q.OrderExpr("sp.created_at DESC").Limit(limit).Scan(ctx); err != nil {
		return nil, apperror.NewInternal("failed to list skill proposals", err)
	}
	return out, nil
}

// ClaimProposal atomically moves a pending proposal into `applying` (or
// reclaims one left `applying` by a crashed reviewer). Exactly one caller wins,
// so two concurrent approvals cannot apply the same proposal twice.
func (r *Repository) ClaimProposal(ctx context.Context, id uuid.UUID) (bool, error) {
	res, err := r.db.NewUpdate().
		TableExpr("kb.skill_proposals").
		Set("status = ?", ProposalStatusApplying).
		Set("claimed_at = now()").
		Where("id = ?", id).
		Where(proposalReclaimablePredicate, ProposalStatusPending, ProposalStatusApplying, proposalClaimStaleAfter.Seconds()).
		Exec(ctx)
	if err != nil {
		return false, apperror.NewInternal("failed to claim skill proposal", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, apperror.NewInternal("failed to read skill proposal claim", err)
	}
	return n > 0, nil
}

// MarkProposalApplied records a successful apply of a claimed proposal. It also
// accepts a row the inbox sweep reclaimed to `pending` (claimed_at cleared): the
// write already landed, so leaving it reviewable would let a second reviewer
// apply it again — or reject a skill that exists. A human decision that raced
// the write (rejected/applied) still wins; the service logs that inconsistency.
func (r *Repository) MarkProposalApplied(ctx context.Context, id uuid.UUID, reviewedBy *uuid.UUID, auto bool, note string) error {
	res, err := r.db.NewUpdate().
		TableExpr("kb.skill_proposals").
		Set("status = ?", ProposalStatusApplied).
		Set("auto_applied = ?", auto).
		Set("applied_at = now()").
		Set("reviewed_by = ?", reviewedBy).
		Set("review_note = ?", note).
		Where("id = ?", id).
		Where("(status = ? OR (status = ? AND claimed_at IS NULL))", ProposalStatusApplying, ProposalStatusPending).
		Exec(ctx)
	if err != nil {
		return apperror.NewInternal("failed to mark skill proposal applied", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return apperror.NewInternal("failed to read skill proposal apply result", err)
	}
	if n == 0 {
		return apperror.ErrConflict.WithMessage("proposal is no longer claimed by this reviewer")
	}
	return nil
}

// MarkProposalRejected records a human rejection. It accepts a pending row or
// one whose `applying` claim went stale (a crashed reviewer), so a stranded
// proposal can always be dismissed. Returns false when the row was no longer
// reviewable (another reviewer won, or the proposal is mid-apply).
func (r *Repository) MarkProposalRejected(ctx context.Context, id uuid.UUID, reviewedBy uuid.UUID, note string) (bool, error) {
	res, err := r.db.NewUpdate().
		TableExpr("kb.skill_proposals").
		Set("status = ?", ProposalStatusRejected).
		Set("claimed_at = NULL").
		Set("reviewed_by = ?", reviewedBy).
		Set("review_note = ?", note).
		Where("id = ?", id).
		Where(proposalReclaimablePredicate, ProposalStatusPending, ProposalStatusApplying, proposalClaimStaleAfter.Seconds()).
		Exec(ctx)
	if err != nil {
		return false, apperror.NewInternal("failed to reject skill proposal", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, apperror.NewInternal("failed to read skill proposal rejection", err)
	}
	return n > 0, nil
}

// MarkProposalPending releases a claim after a human apply failed, so the row
// stays reviewable.
func (r *Repository) MarkProposalPending(ctx context.Context, id uuid.UUID, note string) error {
	_, err := r.db.NewUpdate().
		TableExpr("kb.skill_proposals").
		Set("status = ?", ProposalStatusPending).
		Set("claimed_at = NULL").
		Set("review_note = ?", note).
		Where("id = ?", id).
		Where("status = ?", ProposalStatusApplying).
		Exec(ctx)
	if err != nil {
		return apperror.NewInternal("failed to release skill proposal", err)
	}
	return nil
}

// ReleaseStaleProposals sweeps proposals stranded in `applying` for longer than
// proposalClaimStaleAfter back to `pending`, so they reappear in the review
// inbox. It uses the same staleness predicate/interval as ClaimProposal, so it
// can never release a claim the claim path would still consider live. A fresh
// claim is never touched. Returns the number of rows released.
func (r *Repository) ReleaseStaleProposals(ctx context.Context, projectID uuid.UUID) (int64, error) {
	res, err := r.db.NewUpdate().
		TableExpr("kb.skill_proposals").
		Set("status = ?", ProposalStatusPending).
		Set("claimed_at = NULL").
		Where("project_id = ?", projectID).
		Where("status = ?", ProposalStatusApplying).
		Where("(claimed_at IS NULL OR claimed_at < now() - make_interval(secs => ?))", proposalClaimStaleAfter.Seconds()).
		Exec(ctx)
	if err != nil {
		return 0, apperror.NewInternal("failed to release stale skill proposals", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, apperror.NewInternal("failed to read released skill proposals", err)
	}
	return n, nil
}

// MarkProposalFailed records an apply failure (the row stays visible for audit
// and can be rejected by a human).
func (r *Repository) MarkProposalFailed(ctx context.Context, id uuid.UUID, note string) error {
	_, err := r.db.NewUpdate().
		TableExpr("kb.skill_proposals").
		Set("status = ?", ProposalStatusFailed).
		Set("review_note = ?", note).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return apperror.NewInternal("failed to mark skill proposal failed", err)
	}
	return nil
}

// CountAppliedProposals counts applied proposals for an agent, optionally of
// one kind and after a cutoff. humanOnly counts only human-approved rows, which
// is the earned-trust signal.
func (r *Repository) CountAppliedProposals(ctx context.Context, agentID uuid.UUID, kind string, since *time.Time, humanOnly bool) (int, error) {
	q := r.db.NewSelect().
		TableExpr("kb.skill_proposals").
		Where("agent_id = ?", agentID).
		Where("status = 'applied'")
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	if since != nil {
		// Rate caps count applications, not stagings: a proposal staged
		// yesterday and approved today belongs to today's budget.
		q = q.Where("applied_at >= ?", *since)
	}
	if humanOnly {
		q = q.Where("auto_applied = false")
	}
	n, err := q.Count(ctx)
	if err != nil {
		return 0, apperror.NewInternal("failed to count applied skill proposals", err)
	}
	return n, nil
}

// --- Usage ---

// RecordSkillUse increments the use counter for a skill.
func (r *Repository) RecordSkillUse(ctx context.Context, skillID uuid.UUID, projectID string) {
	if skillID == uuid.Nil {
		return
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO kb.skill_usage (skill_id, project_id, use_count, last_used_at, created_at, updated_at)
		VALUES (?, NULLIF(?, '')::uuid, 1, now(), now(), now())
		ON CONFLICT (skill_id) DO UPDATE
		   SET use_count = kb.skill_usage.use_count + 1,
		       last_used_at = now(),
		       updated_at = now()`, skillID, projectID)
	if err != nil {
		r.log.Warn("skills: failed to record skill use",
			logger.Error(err),
		)
	}
}

// RecordSkillPatch increments the patch counter for a skill.
func (r *Repository) RecordSkillPatch(ctx context.Context, skillID uuid.UUID, projectID string) {
	if skillID == uuid.Nil {
		return
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO kb.skill_usage (skill_id, project_id, patch_count, last_patched_at, created_at, updated_at)
		VALUES (?, NULLIF(?, '')::uuid, 1, now(), now(), now())
		ON CONFLICT (skill_id) DO UPDATE
		   SET patch_count = kb.skill_usage.patch_count + 1,
		       last_patched_at = now(),
		       updated_at = now()`, skillID, projectID)
	if err != nil {
		r.log.Warn("skills: failed to record skill patch",
			logger.Error(err),
		)
	}
}

// GetSkillUsage returns the telemetry row for a skill, or nil.
func (r *Repository) GetSkillUsage(ctx context.Context, skillID uuid.UUID) (*SkillUsage, error) {
	u := new(SkillUsage)
	err := r.db.NewSelect().Model(u).Where("su.skill_id = ?", skillID).Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, apperror.NewInternal("failed to get skill usage", err)
	}
	return u, nil
}

// UsageBySkillIDs returns telemetry keyed by skill id for a set of ids.
func (r *Repository) UsageBySkillIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*SkillUsage, error) {
	out := make(map[uuid.UUID]*SkillUsage, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []*SkillUsage
	if err := r.db.NewSelect().Model(&rows).Where("su.skill_id IN (?)", bun.In(ids)).Scan(ctx); err != nil {
		return nil, apperror.NewInternal("failed to list skill usage", err)
	}
	for _, u := range rows {
		out[u.SkillID] = u
	}
	return out, nil
}

// FindAgentCreatedSkills returns the agent-created skills of a project.
func (r *Repository) FindAgentCreatedSkills(ctx context.Context, projectID string) ([]*Skill, error) {
	var out []*Skill
	err := r.db.NewSelect().
		Model(&out).
		Where("s.project_id = ?", projectID).
		Where("((s.metadata->>'created_by') = 'agent' OR (s.metadata->>'source') = ?)", SourceAgent).
		OrderExpr("s.created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, apperror.NewInternal("failed to list agent-created skills", err)
	}
	return out, nil
}

// NearestSkillSimilarity returns the closest project skill to text and its
// cosine similarity (1.0 = identical). uuid.Nil means no comparable skill.
func (r *Repository) NearestSkillSimilarity(ctx context.Context, projectID, orgID, text string) (uuid.UUID, float64, error) {
	if strings.TrimSpace(text) == "" || r.embedder == nil {
		return uuid.Nil, 0, nil
	}
	vec := r.embedDescription(ctx, text)
	if vec == nil {
		return uuid.Nil, 0, nil
	}
	var row struct {
		ID         uuid.UUID `bun:"id"`
		Similarity float64   `bun:"similarity"`
	}
	// Match the visibility of FindForAgent: the project library plus organisation
	// and global skills the agent can also see. Archived skills are excluded
	// because a new lesson may legitimately replace one.
	query := `
		SELECT s.id, 1 - (s.description_embedding <=> ?::vector) AS similarity
		  FROM kb.skills s
		 WHERE s.description_embedding IS NOT NULL
		   AND COALESCE(s.metadata->>'lifecycle_state', '') <> ?
		   AND (s.project_id = ?::uuid
		        OR (s.project_id IS NULL AND (s.org_id = NULLIF(?, '')::uuid OR s.org_id IS NULL)))
		 ORDER BY s.description_embedding <=> ?::vector ASC, s.id ASC
		 LIMIT 1`
	err := r.db.NewRaw(query,
		pgutils.FormatVector(vec),
		SkillLifecycleArchived,
		projectID,
		orgID,
		pgutils.FormatVector(vec),
	).Scan(ctx, &row)
	if err != nil {
		if err == sql.ErrNoRows {
			return uuid.Nil, 0, nil
		}
		return uuid.Nil, 0, apperror.NewInternal("failed to find nearest skill", err)
	}
	return row.ID, row.Similarity, nil
}

// SetSkillLifecycleState updates metadata.lifecycle_state without touching
// updated_at, so the curator's age clock is not reset by its own transitions.
func (r *Repository) SetSkillLifecycleState(ctx context.Context, id uuid.UUID, state string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE kb.skills
		   SET metadata = jsonb_set(COALESCE(metadata, '{}'::jsonb), '{lifecycle_state}', to_jsonb(?::text), true)
		 WHERE id = ?`, state, id)
	if err != nil {
		return apperror.NewInternal("failed to set skill lifecycle state", err)
	}
	return nil
}
