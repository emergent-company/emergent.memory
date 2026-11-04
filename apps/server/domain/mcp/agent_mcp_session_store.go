package mcp

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// AgentMCPSessionStatusActive is the status a session starts in and returns to
// after a successful or failed turn.
const AgentMCPSessionStatusActive = "active"

// Session lifecycle statuses.
const (
	// AgentMCPSessionStatusRunning marks a turn in flight. A second concurrent
	// turn is rejected while a session is running (unless the run looks stuck).
	AgentMCPSessionStatusRunning = "running"
	// AgentMCPSessionStatusInterrupted marks a turn canceled mid-run. An
	// interrupted session remains continuable.
	AgentMCPSessionStatusInterrupted = "interrupted"
	// AgentMCPSessionStatusExpired marks a session past its TTL. Expired
	// sessions are not continuable.
	AgentMCPSessionStatusExpired = "expired"
)

// ============================================================================
// Model
// ============================================================================

// AgentMCPSession stores session ownership/metadata for the agent MCP endpoint.
// Message history lives in the ADK session store under
// session:<projectID>:<sessionRef>; only counters/lifecycle are persisted here.
// Sessions are key-scoped (KeyID), never endpoint-scoped, so revoking one key
// does not strand another key's sessions.
type AgentMCPSession struct {
	bun.BaseModel `bun:"table:core.agent_mcp_sessions,alias:amsess"`

	ID           string     `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	EndpointID   string     `bun:"endpoint_id,type:uuid,notnull"`
	KeyID        string     `bun:"key_id,type:uuid,notnull"`
	SessionRef   string     `bun:"session_ref,notnull"`
	Status       string     `bun:"status,notnull,default:'active'"`
	TurnCount    int        `bun:"turn_count,notnull,default:0"`
	TotalSteps   int        `bun:"total_steps,notnull,default:0"`
	LastRunID    *string    `bun:"last_run_id,type:uuid"`
	CreatedAt    time.Time  `bun:"created_at,notnull,default:now()"`
	LastActiveAt time.Time  `bun:"last_active_at,notnull,default:now()"`
	ExpiresAt    *time.Time `bun:"expires_at"`
}

// AgentMCPSessionDetail is the admin read model for listing an endpoint's
// sessions: the session metadata joined with its owning key's label. It carries
// no message content and no credential material.
type AgentMCPSessionDetail struct {
	bun.BaseModel `bun:"table:core.agent_mcp_sessions,alias:amsess"`

	ID           string     `bun:"id"`
	EndpointID   string     `bun:"endpoint_id"`
	KeyID        string     `bun:"key_id"`
	SessionRef   string     `bun:"session_ref"`
	Status       string     `bun:"status"`
	TurnCount    int        `bun:"turn_count"`
	TotalSteps   int        `bun:"total_steps"`
	CreatedAt    time.Time  `bun:"created_at"`
	LastActiveAt time.Time  `bun:"last_active_at"`
	ExpiresAt    *time.Time `bun:"expires_at"`

	// KeyLabel is the owning key's label, populated by the join. A session whose
	// key row is missing still lists, with an empty label.
	KeyLabel string `bun:"key_label,scanonly"`
}

// ============================================================================
// Store interface
// ============================================================================

// agentMCPSessionStore persists AgentMCPSession rows.
type agentMCPSessionStore interface {
	CreateSession(ctx context.Context, session *AgentMCPSession) error
	// GetSessionByRef returns a session by its ref, or nil when unknown. The
	// returned row carries KeyID so callers can enforce key-scoped ownership.
	GetSessionByRef(ctx context.Context, sessionRef string) (*AgentMCPSession, error)
	// ListSessionsByKey returns a key's sessions, most recently active first.
	ListSessionsByKey(ctx context.Context, keyID string) ([]*AgentMCPSession, error)
	// ListSessionsByEndpoint returns every session for an endpoint, joined with
	// its owning key's label, most recently active first. An empty status lists
	// all lifecycle states; a non-empty status filters to that exact state.
	ListSessionsByEndpoint(ctx context.Context, endpointID, status string) ([]*AgentMCPSessionDetail, error)
	SetSessionStatus(ctx context.Context, id, status string) error
	// TouchSession records one more turn in a single update: it increments
	// turn_count, adds steps to total_steps, and sets last_run_id/last_active_at.
	TouchSession(ctx context.Context, id string, steps int, lastRunID *string, at time.Time) error
	// ClaimSession is the per-session optimistic CAS that serializes turns. It
	// flips a session to "running" only when it is active/interrupted, or when it
	// is already running but its last_active_at is older than staleBefore (a
	// crashed run that must be recoverable). It reports whether the claim won;
	// a false result means another turn holds the session and the caller must
	// return "session busy".
	ClaimSession(ctx context.Context, id string, at, staleBefore time.Time) (bool, error)
	// ReleaseSession records the terminal status of a claimed turn (active or
	// interrupted) and refreshes last_active_at.
	ReleaseSession(ctx context.Context, id, status string, at time.Time) error
	// MarkExpiredSessions marks every non-expired session whose expiry has passed
	// as "expired" and returns how many rows changed. It is the reaper's only
	// write and never touches sessions without an expiry.
	MarkExpiredSessions(ctx context.Context, now time.Time) (int, error)
}

// ============================================================================
// Bun store implementation
// ============================================================================

type bunAgentMCPSessionStore struct {
	db bun.IDB
}

// The store is not yet injected into Service (that wiring lands with the auth
// refactor); assert conformance and keep the injection seam referenced.
var (
	_ agentMCPSessionStore = (*bunAgentMCPSessionStore)(nil)
	_                      = agentMCPSessionOrNil
)

func newAgentMCPSessionStore(db bun.IDB) *bunAgentMCPSessionStore {
	return &bunAgentMCPSessionStore{db: db}
}

// agentMCPSessionOrNil avoids installing a store whose underlying DB is nil.
func agentMCPSessionOrNil(db bun.IDB) agentMCPSessionStore {
	if db == nil {
		return nil
	}
	return newAgentMCPSessionStore(db)
}

func (r *bunAgentMCPSessionStore) CreateSession(ctx context.Context, session *AgentMCPSession) error {
	now := time.Now().UTC()
	if session.Status == "" {
		session.Status = AgentMCPSessionStatusActive
	}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	if session.LastActiveAt.IsZero() {
		session.LastActiveAt = now
	}
	_, err := r.db.NewInsert().Model(session).Exec(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return apperror.New(409, "agent_mcp_session_ref_exists", "A session with this ref already exists")
		}
		return apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return nil
}

func (r *bunAgentMCPSessionStore) GetSessionByRef(ctx context.Context, sessionRef string) (*AgentMCPSession, error) {
	row := new(AgentMCPSession)
	err := r.db.NewSelect().
		Model(row).
		Where("session_ref = ?", sessionRef).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return row, nil
}

func (r *bunAgentMCPSessionStore) ListSessionsByKey(ctx context.Context, keyID string) ([]*AgentMCPSession, error) {
	var rows []*AgentMCPSession
	err := r.db.NewSelect().
		Model(&rows).
		Where("key_id = ?", keyID).
		Order("last_active_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return rows, nil
}

// agentMCPSessionDetailSelect joins a session with its owning key so the admin
// list can render the key's label without a second query.
const agentMCPSessionDetailSelect = `SELECT amsess.id, amsess.endpoint_id, amsess.key_id, amsess.session_ref,
	amsess.status, amsess.turn_count, amsess.total_steps, amsess.created_at, amsess.last_active_at, amsess.expires_at,
	amk.label AS key_label
FROM core.agent_mcp_sessions amsess
LEFT JOIN core.agent_mcp_keys amk ON amk.id = amsess.key_id`

func (r *bunAgentMCPSessionStore) ListSessionsByEndpoint(ctx context.Context, endpointID, status string) ([]*AgentMCPSessionDetail, error) {
	query := agentMCPSessionDetailSelect + ` WHERE amsess.endpoint_id = ?`
	args := []any{endpointID}
	if status != "" {
		query += ` AND amsess.status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY amsess.last_active_at DESC`

	var rows []*AgentMCPSessionDetail
	if err := r.db.NewRaw(query, args...).Scan(ctx, &rows); err != nil {
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return rows, nil
}

func (r *bunAgentMCPSessionStore) SetSessionStatus(ctx context.Context, id, status string) error {
	_, err := r.db.NewUpdate().
		Model((*AgentMCPSession)(nil)).
		Set("status = ?", status).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return nil
}

func (r *bunAgentMCPSessionStore) TouchSession(ctx context.Context, id string, steps int, lastRunID *string, at time.Time) error {
	_, err := r.db.NewUpdate().
		Model((*AgentMCPSession)(nil)).
		Set("turn_count = turn_count + 1").
		Set("total_steps = total_steps + ?", steps).
		Set("last_run_id = ?", lastRunID).
		Set("last_active_at = ?", at).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return nil
}

func (r *bunAgentMCPSessionStore) ClaimSession(ctx context.Context, id string, at, staleBefore time.Time) (bool, error) {
	res, err := r.db.NewUpdate().
		Model((*AgentMCPSession)(nil)).
		Set("status = ?", AgentMCPSessionStatusRunning).
		Set("last_active_at = ?", at).
		Where("id = ?", id).
		Where(
			"(status IN (?) OR (status = ? AND last_active_at < ?))",
			bun.In([]string{AgentMCPSessionStatusActive, AgentMCPSessionStatusInterrupted}),
			AgentMCPSessionStatusRunning,
			staleBefore,
		).
		Exec(ctx)
	if err != nil {
		return false, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return n > 0, nil
}

func (r *bunAgentMCPSessionStore) ReleaseSession(ctx context.Context, id, status string, at time.Time) error {
	_, err := r.db.NewUpdate().
		Model((*AgentMCPSession)(nil)).
		Set("status = ?", status).
		Set("last_active_at = ?", at).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return nil
}

func (r *bunAgentMCPSessionStore) MarkExpiredSessions(ctx context.Context, now time.Time) (int, error) {
	res, err := r.db.NewUpdate().
		Model((*AgentMCPSession)(nil)).
		Set("status = ?", AgentMCPSessionStatusExpired).
		Where("status <> ?", AgentMCPSessionStatusExpired).
		Where("expires_at IS NOT NULL").
		Where("expires_at <= ?", now).
		Exec(ctx)
	if err != nil {
		return 0, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return int(n), nil
}
