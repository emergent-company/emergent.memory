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
		return apperror.NewDatabase("Database operation failed", err)
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
		return nil, apperror.NewDatabase("Database operation failed", err)
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
		return nil, apperror.NewDatabase("Database operation failed", err)
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
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	return link, nil
}

// UpdateShareLink updates the label, config, and expires_at of a non-revoked link.
func (r *Repository) UpdateShareLink(ctx context.Context, linkID, projectID, label string, config *ShareLinkConfig, expiresAt *time.Time) (bool, error) {
	now := time.Now()
	res, err := r.db.NewUpdate().
		Model((*AgentShareLink)(nil)).
		Set("label = ?", label).
		Set("config = ?", config).
		Set("expires_at = ?", expiresAt).
		Set("updated_at = ?", now).
		Where("id = ?", linkID).
		Where("project_id = ?", projectID).
		Where("revoked_at IS NULL").
		Exec(ctx)
	if err != nil {
		if pgutils.IsUniqueViolation(err) {
			return false, apperror.New(409, "share_link_exists", "A share link with this label already exists for this agent")
		}
		return false, apperror.NewDatabase("Database operation failed", err)
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
		return false, apperror.NewDatabase("Database operation failed", err)
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
		return apperror.NewDatabase("Database operation failed", err)
	}
	return nil
}

// CreateShareSessionIfUnderCap inserts a share session atomically under an
// advisory lock, enforcing the max-active-sessions cap per (link, end user).
// Returns created=false (no error) when the cap is already reached, so two
// concurrent first-session requests cannot both create a session past the cap.
func (r *Repository) CreateShareSessionIfUnderCap(ctx context.Context, s *AgentShareSession, maxActive int) (bool, error) {
	created := false
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewRaw("SELECT pg_advisory_xact_lock(hashtext(?))", s.ShareLinkID+":"+s.EndUserRef).Exec(ctx); err != nil {
			return apperror.NewDatabase("Database operation failed", err)
		}
		if maxActive > 0 {
			var count int
			err := tx.NewSelect().
				TableExpr("kb.agent_share_sessions").
				ColumnExpr("COUNT(*)").
				Where("share_link_id = ?", s.ShareLinkID).
				Where("end_user_ref = ?", s.EndUserRef).
				Where("is_archived = false").
				Scan(ctx, &count)
			if err != nil {
				return apperror.NewDatabase("Database operation failed", err)
			}
			if count >= maxActive {
				return nil
			}
		}
		if _, err := tx.NewInsert().Model(s).Exec(ctx); err != nil {
			return apperror.NewDatabase("Database operation failed", err)
		}
		created = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return created, nil
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
		return nil, apperror.NewDatabase("Database operation failed", err)
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
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	return sessions, nil
}

// shareSessionProjectRow is a share session joined to its share link + agent
// definition, for owner-facing project-scoped listing. AgentShareSession has no
// AgentDefinitionID/agent-name columns, so this row carries them alongside the
// session's own fields.
type shareSessionProjectRow struct {
	ID                string     `bun:"id"`
	ShareLinkID       string     `bun:"share_link_id"`
	ACPSessionID      string     `bun:"acp_session_id"`
	EndUserRef        string     `bun:"end_user_ref"`
	Title             *string    `bun:"title"`
	LastActivityAt    *time.Time `bun:"last_activity_at"`
	IsArchived        bool       `bun:"is_archived"`
	CreatedAt         time.Time  `bun:"created_at"`
	AgentDefinitionID string     `bun:"agent_definition_id"`
	AgentName         string     `bun:"agent_name"`
}

// shareSessionProjectQuery builds the shared select (all share-session columns
// plus the joined link's agent_definition_id and the agent definition's name)
// used by the owner-facing project-scoped session queries.
func shareSessionProjectQuery(q *bun.SelectQuery) *bun.SelectQuery {
	return q.
		TableExpr("kb.agent_share_sessions AS ass").
		ColumnExpr("ass.id AS id").
		ColumnExpr("ass.share_link_id AS share_link_id").
		ColumnExpr("ass.acp_session_id AS acp_session_id").
		ColumnExpr("ass.end_user_ref AS end_user_ref").
		ColumnExpr("ass.title AS title").
		ColumnExpr("ass.last_activity_at AS last_activity_at").
		ColumnExpr("ass.is_archived AS is_archived").
		ColumnExpr("ass.created_at AS created_at").
		ColumnExpr("asl.agent_definition_id AS agent_definition_id").
		ColumnExpr("ad.name AS agent_name").
		Join("JOIN kb.agent_share_links AS asl ON asl.id = ass.share_link_id").
		Join("JOIN kb.agent_definitions AS ad ON ad.id = asl.agent_definition_id")
}

// ListShareSessionsByProject lists a project's share sessions (across all of its
// share links), newest activity first.
func (r *Repository) ListShareSessionsByProject(ctx context.Context, projectID string) ([]shareSessionProjectRow, error) {
	var rows []shareSessionProjectRow
	err := shareSessionProjectQuery(r.db.NewSelect()).
		Where("asl.project_id = ?", projectID).
		OrderExpr("COALESCE(ass.last_activity_at, ass.created_at) DESC").
		Scan(ctx, &rows)
	if err != nil {
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	return rows, nil
}

// GetShareSessionByProject returns a share session scoped to a project via its
// link, or nil when the session does not exist or belongs to another project.
func (r *Repository) GetShareSessionByProject(ctx context.Context, sessionID, projectID string) (*shareSessionProjectRow, error) {
	row := new(shareSessionProjectRow)
	err := shareSessionProjectQuery(r.db.NewSelect()).
		Where("ass.id = ?", sessionID).
		Where("asl.project_id = ?", projectID).
		Scan(ctx, row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	return row, nil
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
		return 0, apperror.NewDatabase("Database operation failed", err)
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
		return false, apperror.NewDatabase("Database operation failed", err)
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
		return nil, apperror.NewDatabase("Database operation failed", err)
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
		return 0, apperror.NewDatabase("Database operation failed", err)
	}
	return count, nil
}

// CreateShareRunIfUnderLimit atomically reserves a concurrent-run slot and
// pre-creates the run (status working) under an advisory lock, so concurrent
// admissions for the same link serialize and cannot exceed maxConcurrent.
// Returns a 429 share_busy error when the limit is reached; otherwise the
// pre-created run (to pass as ExecuteRequest.PreCreatedRun).
func (r *Repository) CreateShareRunIfUnderLimit(ctx context.Context, linkID string, maxConcurrent int, opts CreateRunOptions) (*AgentRun, error) {
	var run *AgentRun
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewRaw("SELECT pg_advisory_xact_lock(hashtext(?))", linkID).Exec(ctx); err != nil {
			return apperror.NewDatabase("Database operation failed", err)
		}
		if maxConcurrent > 0 {
			var count int
			err := tx.NewSelect().
				TableExpr("kb.agent_runs AS ar").
				ColumnExpr("COUNT(*)").
				Join("JOIN kb.agent_share_sessions AS ass ON ass.acp_session_id = ar.acp_session_id").
				Where("ass.share_link_id = ?", linkID).
				Where("ar.status IN (?)", bun.In([]string{string(RunStatusRunning), string(RunStatusQueued)})).
				Scan(ctx, &count)
			if err != nil {
				return apperror.NewDatabase("Database operation failed", err)
			}
			if count >= maxConcurrent {
				return apperror.New(429, "share_busy", "share link is at its concurrent run limit")
			}
		}
		run = newAgentRun(opts)
		if _, err := tx.NewInsert().Model(run).Returning("*").Exec(ctx); err != nil {
			return apperror.NewDatabase("Database operation failed", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return run, nil
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
		return apperror.NewDatabase("Database operation failed", err)
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
		return nil, apperror.NewDatabase("Database operation failed", err)
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
		return apperror.NewDatabase("Database operation failed", err)
	}
	return nil
}

// ReserveShareBudget atomically reserves one message slot plus a per-turn
// token/cost allowance for a share turn, enforcing the rolling budget. It locks
// the (link, period) usage row FOR UPDATE so concurrent turns serialize instead
// of all passing a check-then-act gate. The token/cost allowance is reserved
// (added) here and reconciled to actuals after the run via IncrementShareUsage.
// Returns a 429 share_budget_exceeded error when the budget is exhausted (the
// transaction is rolled back and nothing is reserved).
func (r *Repository) ReserveShareBudget(ctx context.Context, linkID string, periodStart time.Time, maxMessages int, maxTokens int64, maxCostUSD float64, perTurnTokens int64, perTurnCostUSD float64) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Ensure the current period row exists so FOR UPDATE has a row to lock.
		if _, err := tx.NewRaw(`
			INSERT INTO kb.agent_share_usage (link_id, period_start, messages, tokens, cost_usd, updated_at)
			VALUES (?, ?, 0, 0, 0, NOW())
			ON CONFLICT (link_id, period_start) DO NOTHING
		`, linkID, periodStart).Exec(ctx); err != nil {
			return apperror.NewDatabase("Database operation failed", err)
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
			return apperror.NewDatabase("Database operation failed", err)
		}

		// Budget check against current settled totals + the reserved message slot
		// + the reserved per-turn token/cost allowance, so a link near its edge
		// is denied BEFORE the run spends.
		if maxMessages > 0 && int(messages)+1 > maxMessages {
			return apperror.New(429, "share_budget_exceeded", "share link message budget exceeded")
		}
		if maxTokens > 0 && tokens+perTurnTokens > maxTokens {
			return apperror.New(429, "share_budget_exceeded", "share link token budget exceeded")
		}
		if maxCostUSD > 0 && costUSD+perTurnCostUSD > maxCostUSD {
			return apperror.New(429, "share_budget_exceeded", "share link cost budget exceeded")
		}

		// Reserve one message slot + the per-turn token/cost allowance.
		if _, err := tx.NewRaw(`
			UPDATE kb.agent_share_usage
			SET messages = messages + 1,
			    tokens   = tokens + ?,
			    cost_usd = cost_usd + ?,
			    updated_at = NOW()
			WHERE link_id = ? AND period_start = ?
		`, perTurnTokens, perTurnCostUSD, linkID, periodStart).Exec(ctx); err != nil {
			return apperror.NewDatabase("Database operation failed", err)
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
		return nil, apperror.NewDatabase("Database operation failed", err)
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
		return nil, apperror.NewDatabase("Database operation failed", err)
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
		return nil, apperror.NewDatabase("Database operation failed", err)
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
		return nil, apperror.NewDatabase("Database operation failed", err)
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
		return 0, apperror.NewDatabase("Database operation failed", err)
	}
	return count, nil
}

// ReserveAndDecideShareApproval atomically reserves an approval slot and flips
// the pending approval to decided, under a per-session advisory lock so
// concurrent approvals cannot exceed maxApprovals. Returns decided=false when
// the cap is already reached (the pending row is left untouched).
func (r *Repository) ReserveAndDecideShareApproval(ctx context.Context, shareLinkID, acpSessionID, questionID, decision, message, decidedBy string, maxApprovals int) (bool, error) {
	decided := false
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewRaw("SELECT pg_advisory_xact_lock(hashtext(?))", shareLinkID+":"+acpSessionID).Exec(ctx); err != nil {
			return apperror.NewDatabase("Database operation failed", err)
		}
		if maxApprovals > 0 {
			var count int
			err := tx.NewSelect().
				TableExpr("kb.agent_tool_approvals AS ata").
				ColumnExpr("COUNT(*)").
				Join("JOIN kb.agent_runs AS ar ON ar.id = ata.run_id").
				Where("ata.share_link_id = ?", shareLinkID).
				Where("ar.acp_session_id = ?", acpSessionID).
				Where("ata.decision != ?", "pending").
				Scan(ctx, &count)
			if err != nil {
				return apperror.NewDatabase("Database operation failed", err)
			}
			if count >= maxApprovals {
				return nil
			}
		}
		res, err := tx.NewUpdate().
			TableExpr("kb.agent_tool_approvals").
			Set("decision = ?", decision).
			Set("message = ?", message).
			Set("decided_by = ?", decidedBy).
			Set("decided_at = ?", time.Now()).
			Where("question_id = ?", questionID).
			Where("decision = ?", "pending").
			Exec(ctx)
		if err != nil {
			return apperror.NewDatabase("Database operation failed", err)
		}
		n, _ := res.RowsAffected()
		decided = n > 0
		return nil
	})
	if err != nil {
		return false, err
	}
	return decided, nil
}
