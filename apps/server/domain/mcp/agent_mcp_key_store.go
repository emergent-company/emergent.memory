package mcp

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// ============================================================================
// Models
// ============================================================================

// AgentMCPKey binds one core.api_tokens credential to an agent MCP endpoint.
// token_id is UNIQUE, so a credential maps to exactly one key (and therefore one
// endpoint/agent). Rotation keeps the key id and swaps token_id; revocation sets
// revoked_at and releases the label.
type AgentMCPKey struct {
	bun.BaseModel `bun:"table:core.agent_mcp_keys,alias:amk"`

	ID         string     `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	EndpointID string     `bun:"endpoint_id,type:uuid,notnull"`
	TokenID    string     `bun:"token_id,type:uuid,notnull"`
	Label      string     `bun:"label,notnull"`
	CreatedBy  *string    `bun:"created_by,type:uuid"`
	CreatedAt  time.Time  `bun:"created_at,notnull,default:now()"`
	UpdatedAt  time.Time  `bun:"updated_at,notnull,default:now()"`
	RevokedAt  *time.Time `bun:"revoked_at"`

	// Transient fields populated by queries that join core.api_tokens.
	TokenLastUsedAt *time.Time `bun:"token_last_used_at,scanonly"`
	TokenRevokedAt  *time.Time `bun:"token_revoked_at,scanonly"`
	TokenExpiresAt  *time.Time `bun:"token_expires_at,scanonly"`
}

// AgentMCPKeyDetail is the read model for listing keys: the key row joined with
// its endpoint's identity and the bound token's lifecycle timestamps, so callers
// can render status/last-used without a second query.
type AgentMCPKeyDetail struct {
	bun.BaseModel `bun:"table:core.agent_mcp_keys,alias:amk"`

	ID         string     `bun:"id"`
	EndpointID string     `bun:"endpoint_id"`
	TokenID    string     `bun:"token_id"`
	Label      string     `bun:"label"`
	CreatedBy  *string    `bun:"created_by"`
	CreatedAt  time.Time  `bun:"created_at"`
	UpdatedAt  time.Time  `bun:"updated_at"`
	RevokedAt  *time.Time `bun:"revoked_at"`

	EndpointProjectID string     `bun:"endpoint_project_id,scanonly"`
	EndpointAgentID   string     `bun:"endpoint_agent_id,scanonly"`
	EndpointRevokedAt *time.Time `bun:"endpoint_revoked_at,scanonly"`

	TokenLastUsedAt *time.Time `bun:"token_last_used_at,scanonly"`
	TokenRevokedAt  *time.Time `bun:"token_revoked_at,scanonly"`
	TokenExpiresAt  *time.Time `bun:"token_expires_at,scanonly"`
}

// ============================================================================
// Store interface
// ============================================================================

// agentMCPKeyStore persists AgentMCPKey rows.
type agentMCPKeyStore interface {
	// GetActiveKeyByTokenID resolves the active key bound to a credential,
	// including the token's lifecycle timestamps. Returns nil when the token is
	// not bound to any active key.
	GetActiveKeyByTokenID(ctx context.Context, tokenID string) (*AgentMCPKey, error)
	// ListKeysByEndpoint returns every key for an endpoint (active and revoked)
	// with its endpoint identity and token timestamps, newest first.
	ListKeysByEndpoint(ctx context.Context, endpointID string) ([]*AgentMCPKeyDetail, error)
	CreateKey(ctx context.Context, key *AgentMCPKey) error
	// RevokeKey sets revoked_at on an active key. It is idempotent.
	RevokeKey(ctx context.Context, id string, revokedAt time.Time) error
	// SetKeyToken repoints the key at a replacement credential (rotation),
	// keeping the key id so sessions survive rotation.
	SetKeyToken(ctx context.Context, id, tokenID string, at time.Time) error
}

// ============================================================================
// Bun store implementation
// ============================================================================

type bunAgentMCPKeyStore struct {
	db bun.IDB
}

// The store is not yet injected into Service (that wiring lands with the auth
// refactor); assert conformance and keep the injection seam referenced.
var (
	_ agentMCPKeyStore = (*bunAgentMCPKeyStore)(nil)
	_                  = agentMCPKeyOrNil
)

func newAgentMCPKeyStore(db bun.IDB) *bunAgentMCPKeyStore {
	return &bunAgentMCPKeyStore{db: db}
}

// agentMCPKeyOrNil avoids installing a store whose underlying DB is nil.
func agentMCPKeyOrNil(db bun.IDB) agentMCPKeyStore {
	if db == nil {
		return nil
	}
	return newAgentMCPKeyStore(db)
}

const agentMCPKeySelect = `SELECT amk.id, amk.endpoint_id, amk.token_id, amk.label, amk.created_by,
	amk.created_at, amk.updated_at, amk.revoked_at,
	at.last_used_at AS token_last_used_at,
	at.revoked_at AS token_revoked_at,
	at.expires_at AS token_expires_at
FROM core.agent_mcp_keys amk
LEFT JOIN core.api_tokens at ON at.id = amk.token_id`

const agentMCPKeyDetailSelect = `SELECT amk.id, amk.endpoint_id, amk.token_id, amk.label, amk.created_by,
	amk.created_at, amk.updated_at, amk.revoked_at,
	ame.project_id AS endpoint_project_id,
	ame.agent_id AS endpoint_agent_id,
	ame.revoked_at AS endpoint_revoked_at,
	at.last_used_at AS token_last_used_at,
	at.revoked_at AS token_revoked_at,
	at.expires_at AS token_expires_at
FROM core.agent_mcp_keys amk
JOIN core.agent_mcp_endpoints ame ON ame.id = amk.endpoint_id
LEFT JOIN core.api_tokens at ON at.id = amk.token_id`

func (r *bunAgentMCPKeyStore) GetActiveKeyByTokenID(ctx context.Context, tokenID string) (*AgentMCPKey, error) {
	row := new(AgentMCPKey)
	err := r.db.NewRaw(agentMCPKeySelect+` WHERE amk.token_id = ? AND amk.revoked_at IS NULL`, tokenID).Scan(ctx, row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return row, nil
}

func (r *bunAgentMCPKeyStore) ListKeysByEndpoint(ctx context.Context, endpointID string) ([]*AgentMCPKeyDetail, error) {
	var rows []*AgentMCPKeyDetail
	err := r.db.NewRaw(agentMCPKeyDetailSelect+` WHERE amk.endpoint_id = ? ORDER BY amk.created_at DESC`, endpointID).Scan(ctx, &rows)
	if err != nil {
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return rows, nil
}

func (r *bunAgentMCPKeyStore) CreateKey(ctx context.Context, key *AgentMCPKey) error {
	now := time.Now().UTC()
	if key.CreatedAt.IsZero() {
		key.CreatedAt = now
	}
	if key.UpdatedAt.IsZero() {
		key.UpdatedAt = now
	}
	_, err := r.db.NewInsert().Model(key).Exec(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			if strings.Contains(err.Error(), "uq_agent_mcp_keys_endpoint_label") {
				return apperror.New(409, "agent_mcp_key_label_exists", "An active key with this label already exists for this endpoint")
			}
			return apperror.New(409, "agent_mcp_key_token_exists", "This credential is already bound to a key")
		}
		return apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return nil
}

func (r *bunAgentMCPKeyStore) RevokeKey(ctx context.Context, id string, revokedAt time.Time) error {
	_, err := r.db.NewUpdate().
		Model((*AgentMCPKey)(nil)).
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

func (r *bunAgentMCPKeyStore) SetKeyToken(ctx context.Context, id, tokenID string, at time.Time) error {
	_, err := r.db.NewUpdate().
		Model((*AgentMCPKey)(nil)).
		Set("token_id = ?", tokenID).
		Set("updated_at = ?", at).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return apperror.New(409, "agent_mcp_key_token_exists", "This credential is already bound to a key")
		}
		return apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return nil
}
