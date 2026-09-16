package mcp

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// ============================================================================
// Model
// ============================================================================

// AgentMCPEndpoint is the agent-owned MCP endpoint. There is at most one active
// endpoint per agent (partial unique index), and the row's lifetime follows the
// agent (kb.agents hard-deletes, ON DELETE CASCADE). Credentials live on the
// per-endpoint keys in AgentMCPKey.
type AgentMCPEndpoint struct {
	bun.BaseModel `bun:"table:core.agent_mcp_endpoints,alias:ame"`

	ID        string     `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	ProjectID string     `bun:"project_id,type:uuid,notnull"`
	AgentID   string     `bun:"agent_id,type:uuid,notnull"`
	CreatedAt time.Time  `bun:"created_at,notnull,default:now()"`
	UpdatedAt time.Time  `bun:"updated_at,notnull,default:now()"`
	RevokedAt *time.Time `bun:"revoked_at"`
}

// ============================================================================
// Store interface
// ============================================================================

// agentMCPEndpointStore persists AgentMCPEndpoint rows.
type agentMCPEndpointStore interface {
	// GetEndpointByID returns the endpoint by primary key, including revoked
	// rows. Callers that need an active endpoint must check RevokedAt (or use
	// GetActiveEndpointByAgentID).
	GetEndpointByID(ctx context.Context, id string) (*AgentMCPEndpoint, error)
	// GetActiveEndpointByAgentID returns the single active endpoint for an agent
	// within a project, or nil when none exists.
	GetActiveEndpointByAgentID(ctx context.Context, projectID, agentID string) (*AgentMCPEndpoint, error)
	CreateEndpoint(ctx context.Context, endpoint *AgentMCPEndpoint) error
	// RevokeEndpoint sets revoked_at on an active endpoint. It is idempotent:
	// an already-revoked or unknown endpoint is a no-op.
	RevokeEndpoint(ctx context.Context, id string, revokedAt time.Time) error
	// TouchEndpoint bumps updated_at, recording endpoint activity.
	TouchEndpoint(ctx context.Context, id string, at time.Time) error
}

// ============================================================================
// Bun store implementation
// ============================================================================

type bunAgentMCPEndpointStore struct {
	db bun.IDB
}

// The store is not yet injected into Service (that wiring lands with the auth
// refactor); assert conformance and keep the injection seam referenced.
var (
	_ agentMCPEndpointStore = (*bunAgentMCPEndpointStore)(nil)
	_                       = agentMCPEndpointOrNil
)

func newAgentMCPEndpointStore(db bun.IDB) *bunAgentMCPEndpointStore {
	return &bunAgentMCPEndpointStore{db: db}
}

// agentMCPEndpointOrNil avoids installing a store whose underlying DB is nil.
func agentMCPEndpointOrNil(db bun.IDB) agentMCPEndpointStore {
	if db == nil {
		return nil
	}
	return newAgentMCPEndpointStore(db)
}

func (r *bunAgentMCPEndpointStore) GetEndpointByID(ctx context.Context, id string) (*AgentMCPEndpoint, error) {
	row := new(AgentMCPEndpoint)
	err := r.db.NewSelect().
		Model(row).
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return row, nil
}

func (r *bunAgentMCPEndpointStore) GetActiveEndpointByAgentID(ctx context.Context, projectID, agentID string) (*AgentMCPEndpoint, error) {
	row := new(AgentMCPEndpoint)
	err := r.db.NewSelect().
		Model(row).
		Where("project_id = ?", projectID).
		Where("agent_id = ?", agentID).
		Where("revoked_at IS NULL").
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return row, nil
}

func (r *bunAgentMCPEndpointStore) CreateEndpoint(ctx context.Context, endpoint *AgentMCPEndpoint) error {
	now := time.Now().UTC()
	if endpoint.CreatedAt.IsZero() {
		endpoint.CreatedAt = now
	}
	if endpoint.UpdatedAt.IsZero() {
		endpoint.UpdatedAt = now
	}
	_, err := r.db.NewInsert().Model(endpoint).Exec(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return apperror.New(409, "agent_mcp_endpoint_exists", "An active MCP endpoint already exists for this agent")
		}
		return apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return nil
}

func (r *bunAgentMCPEndpointStore) RevokeEndpoint(ctx context.Context, id string, revokedAt time.Time) error {
	_, err := r.db.NewUpdate().
		Model((*AgentMCPEndpoint)(nil)).
		Set("revoked_at = ?", revokedAt).
		Set("updated_at = ?", revokedAt).
		Where("id = ?", id).
		Where("revoked_at IS NULL").
		Exec(ctx)
	if err != nil {
		return apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return nil
}

func (r *bunAgentMCPEndpointStore) TouchEndpoint(ctx context.Context, id string, at time.Time) error {
	_, err := r.db.NewUpdate().
		Model((*AgentMCPEndpoint)(nil)).
		Set("updated_at = ?", at).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return nil
}
