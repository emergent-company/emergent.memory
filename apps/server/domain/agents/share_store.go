package agents

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
)

// ============================================================================
// Share link CRUD + binding resolver
// ============================================================================

// CreateShareLink inserts a new share link row.
func (r *Repository) CreateShareLink(ctx context.Context, link *AgentShareLink) error {
	_, err := r.db.NewInsert().Model(link).Exec(ctx)
	if err != nil {
		if pgutils.IsUniqueViolation(err) {
			return apperror.New(409, "share_link_exists", "A share link with this label already exists for this agent")
		}
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// ListShareLinksByProject returns all share links for a project, newest first.
func (r *Repository) ListShareLinksByProject(ctx context.Context, projectID string) ([]*AgentShareLink, error) {
	var links []*AgentShareLink
	err := r.db.NewSelect().
		Model(&links).
		Where("project_id = ?", projectID).
		Order("created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return links, nil
}

// GetShareLinkByID returns a share link by ID, optionally scoped to a project.
func (r *Repository) GetShareLinkByID(ctx context.Context, linkID string, projectID *string) (*AgentShareLink, error) {
	link := new(AgentShareLink)
	q := r.db.NewSelect().Model(link).Where("id = ?", linkID)
	if projectID != nil {
		q = q.Where("project_id = ?", *projectID)
	}
	err := q.Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return link, nil
}

// GetShareLinkByTokenID returns the share link bound to an API token. This is
// the binding resolver: scope alone never authorizes — the token must resolve
// to a live link.
func (r *Repository) GetShareLinkByTokenID(ctx context.Context, apiTokenID string) (*AgentShareLink, error) {
	link := new(AgentShareLink)
	err := r.db.NewSelect().Model(link).Where("api_token_id = ?", apiTokenID).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return link, nil
}

// UpdateShareLink updates the label and config of a non-revoked link.
func (r *Repository) UpdateShareLink(ctx context.Context, linkID, projectID, label string, config *ShareLinkConfig) (bool, error) {
	now := time.Now()
	res, err := r.db.NewUpdate().
		Model((*AgentShareLink)(nil)).
		Set("label = ?", label).
		Set("config = ?", config).
		Set("updated_at = ?", now).
		Where("id = ?", linkID).
		Where("project_id = ?", projectID).
		Where("revoked_at IS NULL").
		Exec(ctx)
	if err != nil {
		if pgutils.IsUniqueViolation(err) {
			return false, apperror.New(409, "share_link_exists", "A share link with this label already exists for this agent")
		}
		return false, apperror.ErrDatabase.WithInternal(err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RevokeShareLink sets revoked_at on a non-revoked link.
func (r *Repository) RevokeShareLink(ctx context.Context, linkID, projectID string) (bool, error) {
	now := time.Now()
	res, err := r.db.NewUpdate().
		Model((*AgentShareLink)(nil)).
		Set("revoked_at = ?", now).
		Set("updated_at = ?", now).
		Where("id = ?", linkID).
		Where("project_id = ?", projectID).
		Where("revoked_at IS NULL").
		Exec(ctx)
	if err != nil {
		return false, apperror.ErrDatabase.WithInternal(err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ReplaceShareLinkToken atomically swaps the API token backing a link within a
// transaction (rotate keeps the link id). The caller mints the new token and
// passes its id; this is invoked inside RegenerateWith's after-hook.
func (r *Repository) ReplaceShareLinkToken(ctx context.Context, tx bun.Tx, linkID, newTokenID string) error {
	now := time.Now()
	_, err := tx.NewUpdate().
		Model((*AgentShareLink)(nil)).
		Set("api_token_id = ?", newTokenID).
		Set("updated_at = ?", now).
		Where("id = ?", linkID).
		Exec(ctx)
	return err
}

// TouchShareLink sets last_used_at on a link (best-effort).
func (r *Repository) TouchShareLink(ctx context.Context, linkID string) error {
	now := time.Now()
	_, err := r.db.NewUpdate().
		Model((*AgentShareLink)(nil)).
		Set("last_used_at = ?", now).
		Where("id = ?", linkID).
		Exec(ctx)
	return err
}

// ============================================================================
// Share sessions
// ============================================================================

// CreateShareSession inserts a new share session row.
func (r *Repository) CreateShareSession(ctx context.Context, s *AgentShareSession) error {
	_, err := r.db.NewInsert().Model(s).Exec(ctx)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// GetShareSessionByID returns a share session belonging to (link, end user).
func (r *Repository) GetShareSessionByID(ctx context.Context, sessionID, linkID, endUserRef string) (*AgentShareSession, error) {
	s := new(AgentShareSession)
	err := r.db.NewSelect().
		Model(s).
		Where("id = ?", sessionID).
		Where("share_link_id = ?", linkID).
		Where("end_user_ref = ?", endUserRef).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return s, nil
}

// ListShareSessionsByEndUser lists an end user's sessions for a link.
func (r *Repository) ListShareSessionsByEndUser(ctx context.Context, linkID, endUserRef string, includeArchived bool) ([]*AgentShareSession, error) {
	var sessions []*AgentShareSession
	q := r.db.NewSelect().
		Model(&sessions).
		Where("share_link_id = ?", linkID).
		Where("end_user_ref = ?", endUserRef)
	if !includeArchived {
		q = q.Where("is_archived = false")
	}
	q = q.Order("last_activity_at DESC NULLS LAST")
	err := q.Scan(ctx)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return sessions, nil
}

// CountActiveShareSessions returns the number of non-archived sessions for an
// end user on a link (session cap enforcement).
func (r *Repository) CountActiveShareSessions(ctx context.Context, linkID, endUserRef string) (int, error) {
	var count int
	err := r.db.NewSelect().
		TableExpr("kb.agent_share_sessions").
		ColumnExpr("COUNT(*)").
		Where("share_link_id = ?", linkID).
		Where("end_user_ref = ?", endUserRef).
		Where("is_archived = false").
		Scan(ctx, &count)
	if err != nil {
		return 0, apperror.ErrDatabase.WithInternal(err)
	}
	return count, nil
}

// ArchiveShareSession marks a session archived (returns true when changed).
func (r *Repository) ArchiveShareSession(ctx context.Context, sessionID, linkID, endUserRef string) (bool, error) {
	res, err := r.db.NewUpdate().
		Model((*AgentShareSession)(nil)).
		Set("is_archived = true").
		Where("id = ?", sessionID).
		Where("share_link_id = ?", linkID).
		Where("end_user_ref = ?", endUserRef).
		Where("is_archived = false").
		Exec(ctx)
	if err != nil {
		return false, apperror.ErrDatabase.WithInternal(err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// TouchShareSession updates a session's last_activity_at (best-effort).
func (r *Repository) TouchShareSession(ctx context.Context, sessionID string) error {
	now := time.Now()
	_, err := r.db.NewUpdate().
		Model((*AgentShareSession)(nil)).
		Set("last_activity_at = ?", now).
		Where("id = ?", sessionID).
		Exec(ctx)
	return err
}

// FindShareSessionByRunAndEndUser verifies that a run's acp_session_id maps to a
// share session for (link, end user). Used by the share approval path so a
// foreign end_user_ref can never answer another user's question.
func (r *Repository) FindShareSessionByRunAndEndUser(ctx context.Context, linkID, endUserRef, runID string) (*AgentShareSession, error) {
	s := new(AgentShareSession)
	err := r.db.NewSelect().
		Model(s).
		Join("JOIN kb.agent_runs AS ar ON ar.acp_session_id = ass.acp_session_id").
		Where("ass.share_link_id = ?", linkID).
		Where("ass.end_user_ref = ?", endUserRef).
		Where("ar.id = ?", runID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return s, nil
}

// CountActiveShareRuns counts in-flight runs (working/submitted) whose
// acp_session is in one of the link's share sessions.
func (r *Repository) CountActiveShareRuns(ctx context.Context, linkID string) (int, error) {
	var count int
	err := r.db.NewSelect().
		TableExpr("kb.agent_runs AS ar").
		ColumnExpr("COUNT(*)").
		Join("JOIN kb.agent_share_sessions AS ass ON ass.acp_session_id = ar.acp_session_id").
		Where("ass.share_link_id = ?", linkID).
		Where("ar.status IN (?)", bun.In([]string{string(RunStatusRunning), string(RunStatusQueued)})).
		Scan(ctx, &count)
	if err != nil {
		return 0, apperror.ErrDatabase.WithInternal(err)
	}
	return count, nil
}

// ============================================================================
// End users
// ============================================================================

// UpsertShareEndUser inserts or updates an end-user record keyed by
// (share_link_id, end_user_ref).
func (r *Repository) UpsertShareEndUser(ctx context.Context, u *AgentShareEndUser) error {
	now := time.Now()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	_, err := r.db.NewInsert().
		Model(u).
		On("CONFLICT (share_link_id, end_user_ref) DO UPDATE").
		Set("email = EXCLUDED.email").
		Set("email_normalized = EXCLUDED.email_normalized").
		Set("verified = EXCLUDED.verified").
		Set("consent_at = EXCLUDED.consent_at").
		Set("last_seen_at = EXCLUDED.last_seen_at").
		Exec(ctx)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// FindShareEndUser returns an end-user record for (link, ref), or nil.
func (r *Repository) FindShareEndUser(ctx context.Context, linkID, endUserRef string) (*AgentShareEndUser, error) {
	u := new(AgentShareEndUser)
	err := r.db.NewSelect().
		Model(u).
		Where("share_link_id = ?", linkID).
		Where("end_user_ref = ?", endUserRef).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return u, nil
}

// ============================================================================
// Usage / budget
// ============================================================================

// ShareUsageAggregate is a summed usage window.
type ShareUsageAggregate struct {
	Messages int
	Tokens   int64
	CostUSD  float64
}

// IncrementShareUsage upserts usage into a (link, period) bucket.
func (r *Repository) IncrementShareUsage(ctx context.Context, linkID string, periodStart time.Time, messages int, tokens int64, costUSD float64) error {
	_, err := r.db.NewRaw(`
		INSERT INTO kb.agent_share_usage (link_id, period_start, messages, tokens, cost_usd, updated_at)
		VALUES (?, ?, ?, ?, ?, NOW())
		ON CONFLICT (link_id, period_start) DO UPDATE SET
			messages = kb.agent_share_usage.messages + EXCLUDED.messages,
			tokens   = kb.agent_share_usage.tokens   + EXCLUDED.tokens,
			cost_usd = kb.agent_share_usage.cost_usd + EXCLUDED.cost_usd,
			updated_at = NOW()
	`, linkID, periodStart, messages, tokens, costUSD).Exec(ctx)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// ReserveShareBudget atomically reserves one message slot for a share turn and
// enforces the rolling budget. It locks the (link, period) usage row FOR UPDATE
// so concurrent turns serialize instead of all passing a check-then-act gate.
// Returns a 429 share_budget_exceeded error when the budget is exhausted (the
// transaction is rolled back and nothing is reserved).
func (r *Repository) ReserveShareBudget(ctx context.Context, linkID string, periodStart time.Time, maxMessages int, maxTokens int64, maxCostUSD float64) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Ensure the current period row exists so FOR UPDATE has a row to lock.
		if _, err := tx.NewRaw(`
			INSERT INTO kb.agent_share_usage (link_id, period_start, messages, tokens, cost_usd, updated_at)
			VALUES (?, ?, 0, 0, 0, NOW())
			ON CONFLICT (link_id, period_start) DO NOTHING
		`, linkID, periodStart).Exec(ctx); err != nil {
			return apperror.ErrDatabase.WithInternal(err)
		}

		var messages int64
		var tokens int64
		var costUSD float64
		if err := tx.NewRaw(`
			SELECT messages, tokens, cost_usd
			FROM kb.agent_share_usage
			WHERE link_id = ? AND period_start = ?
			FOR UPDATE
		`, linkID, periodStart).Scan(ctx, &messages, &tokens, &costUSD); err != nil {
			return apperror.ErrDatabase.WithInternal(err)
		}

		// Budget check against current settled totals + the reserved message slot.
		if maxMessages > 0 && int(messages)+1 > maxMessages {
			return apperror.New(429, "share_budget_exceeded", "share link message budget exceeded")
		}
		if maxTokens > 0 && tokens >= maxTokens {
			return apperror.New(429, "share_budget_exceeded", "share link token budget exceeded")
		}
		if maxCostUSD > 0 && costUSD >= maxCostUSD {
			return apperror.New(429, "share_budget_exceeded", "share link cost budget exceeded")
		}

		// Reserve one message slot.
		if _, err := tx.NewRaw(`
			UPDATE kb.agent_share_usage
			SET messages = messages + 1, updated_at = NOW()
			WHERE link_id = ? AND period_start = ?
		`, linkID, periodStart).Exec(ctx); err != nil {
			return apperror.ErrDatabase.WithInternal(err)
		}
		return nil
	})
}

// SumShareUsageSince sums usage across all buckets at/after the given period
// start (the current rolling window).
func (r *Repository) SumShareUsageSince(ctx context.Context, linkID string, since time.Time) (*ShareUsageAggregate, error) {
	var agg struct {
		Messages int64   `bun:"messages"`
		Tokens   int64   `bun:"tokens"`
		CostUSD  float64 `bun:"cost_usd"`
	}
	err := r.db.NewSelect().
		TableExpr("kb.agent_share_usage").
		ColumnExpr("COALESCE(SUM(messages), 0) AS messages").
		ColumnExpr("COALESCE(SUM(tokens), 0) AS tokens").
		ColumnExpr("COALESCE(SUM(cost_usd), 0) AS cost_usd").
		Where("link_id = ?", linkID).
		Where("period_start >= ?", since).
		Scan(ctx, &agg)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return &ShareUsageAggregate{
		Messages: int(agg.Messages),
		Tokens:   agg.Tokens,
		CostUSD:  agg.CostUSD,
	}, nil
}

// ListShareUsage returns recent usage buckets for a link.
func (r *Repository) ListShareUsage(ctx context.Context, linkID string, limit int) ([]*AgentShareUsage, error) {
	if limit <= 0 {
		limit = 90
	}
	var rows []*AgentShareUsage
	err := r.db.NewSelect().
		Model(&rows).
		Where("link_id = ?", linkID).
		Order("period_start DESC").
		Limit(limit).
		Scan(ctx)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return rows, nil
}

// ============================================================================
// Access log + reaper
// ============================================================================

// CreateShareAccessLog records a hashed-IP access event.
func (r *Repository) CreateShareAccessLog(ctx context.Context, linkID, endUserRef, ipHash, action string) error {
	_, err := r.db.NewRaw(`
		INSERT INTO kb.agent_share_access_log (share_link_id, end_user_ref, ip_hash, action)
		VALUES (?, ?, ?, ?)
	`, linkID, endUserRef, ipHash, action).Exec(ctx)
	return err
}

// shareReapCandidate is a paused share run eligible for TTL cancellation.
type shareReapCandidate struct {
	RunID        string
	ShareLinkID  string
	LastActivity *time.Time
}

// ListPausedShareRuns returns paused (input-required) runs belonging to share
// sessions, with their last activity timestamp for per-link TTL evaluation.
func (r *Repository) ListPausedShareRuns(ctx context.Context) ([]shareReapCandidate, error) {
	var rows []struct {
		RunID        string     `bun:"run_id"`
		ShareLinkID  string     `bun:"share_link_id"`
		LastActivity *time.Time `bun:"last_activity_at"`
	}
	err := r.db.NewSelect().
		TableExpr("kb.agent_runs AS ar").
		ColumnExpr("ar.id AS run_id").
		ColumnExpr("ass.share_link_id AS share_link_id").
		ColumnExpr("ass.last_activity_at AS last_activity_at").
		Join("JOIN kb.agent_share_sessions AS ass ON ass.acp_session_id = ar.acp_session_id").
		Where("ar.status = ?", string(RunStatusPaused)).
		Scan(ctx, &rows)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	out := make([]shareReapCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, shareReapCandidate{
			RunID:        row.RunID,
			ShareLinkID:  row.ShareLinkID,
			LastActivity: row.LastActivity,
		})
	}
	return out, nil
}

// ListPendingQuestionsForACPSession returns pending questions whose run belongs
// to the given ACP session (share session linkage).
func (r *Repository) ListPendingQuestionsForACPSession(ctx context.Context, acpSessionID string) ([]*AgentQuestion, error) {
	var questions []*AgentQuestion
	err := r.db.NewSelect().
		Model(&questions).
		Join("JOIN kb.agent_runs AS ar ON ar.id = aq.run_id").
		Where("ar.acp_session_id = ?", acpSessionID).
		Where("aq.status = ?", QuestionStatusPending).
		Order("aq.created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return questions, nil
}

// CountSessionToolApprovals counts decided tool-approval rows for a share
// session (approval cap enforcement).
func (r *Repository) CountSessionToolApprovals(ctx context.Context, shareLinkID, acpSessionID string) (int, error) {
	var count int
	err := r.db.NewSelect().
		TableExpr("kb.agent_tool_approvals AS ata").
		ColumnExpr("COUNT(*)").
		Join("JOIN kb.agent_runs AS ar ON ar.id = ata.run_id").
		Where("ata.share_link_id = ?", shareLinkID).
		Where("ar.acp_session_id = ?", acpSessionID).
		Where("ata.decision != ?", "pending").
		Scan(ctx, &count)
	if err != nil {
		return 0, apperror.ErrDatabase.WithInternal(err)
	}
	return count, nil
}
