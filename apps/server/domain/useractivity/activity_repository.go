package useractivity

import (
	"context"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// ListOrgActivity returns the org's members with their activity aggregates over
// the window starting at since.
func (r *Repository) ListOrgActivity(ctx context.Context, orgID string, since time.Time) ([]MemberActivityRow, error) {
	q := `
SELECT up.id AS user_id,
       COALESCE(ue.email, '') AS email,
       up.display_name,
       up.first_name,
       up.last_name,
       om.role AS role,
       om.created_at AS joined_at,
       MAX(a.last_seen_at)   AS last_seen_at,
       MAX(a.last_action_at) AS last_action_at,
       COALESCE(SUM(a.request_count), 0) AS request_count,
       COALESCE(SUM(a.action_count), 0)  AS action_count,
       COUNT(DISTINCT a.day)             AS active_days
FROM kb.organization_memberships AS om
JOIN core.user_profiles AS up ON up.id = om.user_id
LEFT JOIN LATERAL (
    SELECT email FROM core.user_emails
    WHERE user_id = up.id
    ORDER BY verified DESC, created_at ASC
    LIMIT 1
) AS ue ON true
LEFT JOIN kb.user_activity_daily AS a
       ON a.user_id = om.user_id
      AND a.organization_id = om.organization_id
      AND a.day >= ?::date
WHERE om.organization_id = ?::uuid
GROUP BY up.id, ue.email, up.display_name, up.first_name, up.last_name, om.role, om.created_at
ORDER BY last_seen_at DESC NULLS LAST, up.id
`
	rows := []MemberActivityRow{}
	if err := r.db.NewRaw(q, since, orgID).Scan(ctx, &rows); err != nil {
		r.log.Error("failed to list org activity", logger.Error(err))
		return nil, apperror.NewDatabase("activity operation failed", err)
	}
	return rows, nil
}

// ListProjectActivity returns the project's members with their activity
// aggregates over the window starting at since.
func (r *Repository) ListProjectActivity(ctx context.Context, projectID string, since time.Time) ([]MemberActivityRow, error) {
	q := `
SELECT up.id AS user_id,
       COALESCE(ue.email, '') AS email,
       up.display_name,
       up.first_name,
       up.last_name,
       pm.role AS role,
       pm.created_at AS joined_at,
       MAX(a.last_seen_at)   AS last_seen_at,
       MAX(a.last_action_at) AS last_action_at,
       COALESCE(SUM(a.request_count), 0) AS request_count,
       COALESCE(SUM(a.action_count), 0)  AS action_count,
       COUNT(DISTINCT a.day)             AS active_days
FROM kb.project_memberships AS pm
JOIN core.user_profiles AS up ON up.id = pm.user_id
LEFT JOIN LATERAL (
    SELECT email FROM core.user_emails
    WHERE user_id = up.id
    ORDER BY verified DESC, created_at ASC
    LIMIT 1
) AS ue ON true
LEFT JOIN kb.user_activity_daily AS a
       ON a.user_id = pm.user_id
      AND a.project_id = pm.project_id
      AND a.day >= ?::date
WHERE pm.project_id = ?::uuid
GROUP BY up.id, ue.email, up.display_name, up.first_name, up.last_name, pm.role, pm.created_at
ORDER BY last_seen_at DESC NULLS LAST, up.id
`
	rows := []MemberActivityRow{}
	if err := r.db.NewRaw(q, since, projectID).Scan(ctx, &rows); err != nil {
		r.log.Error("failed to list project activity", logger.Error(err))
		return nil, apperror.NewDatabase("activity operation failed", err)
	}
	return rows, nil
}

// LastSeenByUser returns the newest last_seen_at per member for a project. Used
// to enrich the plain member list without paying for the full window aggregate.
func (r *Repository) LastSeenByUser(ctx context.Context, projectID string) (map[string]time.Time, error) {
	type row struct {
		UserID     string    `bun:"user_id"`
		LastSeenAt time.Time `bun:"last_seen_at"`
	}
	rows := []row{}
	err := r.db.NewRaw(`
		SELECT user_id, MAX(last_seen_at) AS last_seen_at
		FROM kb.user_activity_daily
		WHERE project_id = ?::uuid AND last_seen_at IS NOT NULL
		GROUP BY user_id
	`, projectID).Scan(ctx, &rows)
	if err != nil {
		return nil, apperror.NewDatabase("activity operation failed", err)
	}
	out := make(map[string]time.Time, len(rows))
	for _, rw := range rows {
		out[rw.UserID] = rw.LastSeenAt
	}
	return out, nil
}

// MyActivity aggregates the authenticated user's own rows over the window.
func (r *Repository) MyActivity(ctx context.Context, userID string, since time.Time) (*MemberActivityRow, error) {
	q := `
SELECT MAX(last_seen_at)   AS last_seen_at,
       MAX(last_action_at) AS last_action_at,
       COALESCE(SUM(request_count), 0) AS request_count,
       COALESCE(SUM(action_count), 0)  AS action_count,
       COUNT(DISTINCT day)             AS active_days
FROM kb.user_activity_daily
WHERE user_id = ?::uuid AND day >= ?::date
`
	rows := []MemberActivityRow{}
	if err := r.db.NewRaw(q, userID, since).Scan(ctx, &rows); err != nil {
		r.log.Error("failed to read own activity", logger.Error(err))
		return nil, apperror.NewDatabase("activity operation failed", err)
	}
	if len(rows) == 0 {
		return &MemberActivityRow{UserID: userID}, nil
	}
	return &rows[0], nil
}

// Rollup derives action_count for the window from existing user-attributed
// sources and upserts it into the daily buckets. Reruns are idempotent: the
// daily value moves monotonically with GREATEST(existing, recomputed) rather
// than being overwritten, so the shrinking lookback window cannot decay an
// older day's count as it slides out of range.
func (r *Repository) Rollup(ctx context.Context, since time.Time) error {
	const q = `
INSERT INTO kb.user_activity_daily AS a
    (id, day, user_id, organization_id, project_id, action_count, last_action_at)
SELECT md5(concat(
           d.day::text, '|', d.user_id::text, '|',
           coalesce(d.organization_id::text, ''), '|',
           coalesce(d.project_id::text, '')
       ))::uuid,
       d.day, d.user_id, d.organization_id, d.project_id,
       SUM(d.actions), MAX(d.last_at)
FROM (
    SELECT pj.created_at::date AS day,
           pj.actor_id AS user_id,
           p.organization_id AS organization_id,
           pj.project_id AS project_id,
           count(*) AS actions,
           max(pj.created_at) AS last_at
    FROM kb.project_journal AS pj
    JOIN kb.projects AS p ON p.id = pj.project_id
    WHERE pj.actor_type = 'user'
      AND pj.actor_id IS NOT NULL
      AND pj.created_at >= ?
    GROUP BY 1, 2, 3, 4

    UNION ALL

    SELECT e.created_at::date, e.user_id, e.org_id, e.project_id,
           count(*), max(e.created_at)
    FROM kb.llm_usage_events AS e
    WHERE e.user_id IS NOT NULL AND e.created_at >= ?
    GROUP BY 1, 2, 3, 4

    UNION ALL

    SELECT c.updated_at::date, c.owner_user_id, p.organization_id, c.project_id,
           count(*), max(c.updated_at)
    FROM kb.chat_conversations AS c
    JOIN kb.projects AS p ON p.id = c.project_id
    WHERE c.owner_user_id IS NOT NULL AND c.updated_at >= ?
    GROUP BY 1, 2, 3, 4
) AS d
GROUP BY 1, 2, 3, 4, 5
ON CONFLICT (id) DO UPDATE
SET action_count   = GREATEST(a.action_count, EXCLUDED.action_count),
    last_action_at = GREATEST(a.last_action_at, EXCLUDED.last_action_at),
    updated_at     = now()`

	if _, err := r.db.NewRaw(q, since, since, since).Exec(ctx); err != nil {
		r.log.Error("failed to roll up member activity", logger.Error(err))
		return apperror.NewDatabase("activity operation failed", err)
	}
	return nil
}

// BackfillSeen seeds last_seen_at from API-token usage, the only historical
// signal that carries a per-user timestamp before this feature existed.
// Request counts are not invented: the backfill records the presence of a
// timestamp only.
func (r *Repository) BackfillSeen(ctx context.Context, since time.Time) error {
	const q = `
INSERT INTO kb.user_activity_daily AS a
    (id, day, user_id, organization_id, project_id, last_seen_at)
SELECT md5(concat(
           d.day::text, '|', d.user_id::text, '|',
           coalesce(d.organization_id::text, ''), '|',
           coalesce(d.project_id::text, '')
       ))::uuid,
       d.day, d.user_id, d.organization_id, d.project_id, d.last_at
FROM (
    SELECT t.last_used_at::date AS day,
           t.user_id,
           p.organization_id,
           t.project_id,
           max(t.last_used_at) AS last_at
    FROM core.api_tokens AS t
    JOIN kb.projects AS p ON p.id = t.project_id
    WHERE t.last_used_at IS NOT NULL
      AND t.project_id IS NOT NULL
      AND t.last_used_at >= ?
    GROUP BY 1, 2, 3, 4
) AS d
ON CONFLICT (id) DO UPDATE
SET last_seen_at = GREATEST(a.last_seen_at, EXCLUDED.last_seen_at),
    updated_at   = now()`

	if _, err := r.db.NewRaw(q, since).Exec(ctx); err != nil {
		r.log.Error("failed to backfill member last-seen", logger.Error(err))
		return apperror.NewDatabase("activity operation failed", err)
	}
	return nil
}

// Prune drops daily buckets older than before. The table is aggregate, so
// retention is a plain delete; presence is never persisted.
func (r *Repository) Prune(ctx context.Context, before time.Time) error {
	if _, err := r.db.NewRaw(
		`DELETE FROM kb.user_activity_daily WHERE day < ?::date`, before,
	).Exec(ctx); err != nil {
		r.log.Error("failed to prune member activity", logger.Error(err))
		return apperror.NewDatabase("activity operation failed", err)
	}
	return nil
}

// HasActivity reports whether any bucket exists, so the startup backfill runs
// exactly once on a fresh deployment.
func (r *Repository) HasActivity(ctx context.Context) (bool, error) {
	var exists bool
	if err := r.db.NewRaw(
		`SELECT EXISTS(SELECT 1 FROM kb.user_activity_daily)`,
	).Scan(ctx, &exists); err != nil {
		return false, apperror.NewDatabase("activity operation failed", err)
	}
	return exists, nil
}
