package mcp

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// AgentMCPSessionStatusActive is the status a session starts in.
const AgentMCPSessionStatusActive = "active"

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
	SetSessionStatus(ctx context.Context, id, status string) error
	// TouchSession records one more turn in a single update: it increments
	// turn_count, adds steps to total_steps, and sets last_run_id/last_active_at.
	TouchSession(ctx context.Context, id string, steps int, lastRunID *string, at time.Time) error
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
