package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// ============================================================================
// Constants
// ============================================================================

// AgentCallScope is the marker scope minted on per-agent MCP endpoint
// credentials. It is deliberately absent from normal project tokens. The project
// MCP transports (handler.go, streamable_http_handler.go, sse_handler.go) reject
// any credential carrying it, and the per-agent endpoint requires it. This keeps
// an endpoint key usable only against its own agent.
const AgentCallScope = "mcp:agent-call"

// agentShareScopes is the scope set minted on a per-agent MCP endpoint key: the
// marker scope plus projects:read, and nothing else.
//
// Loopback authorization: the bound agent's internal tool calls run through
// ToolPool.CallTool -> mcp.Service.ExecuteTool, which executes built-in tools
// directly and never consults the credential's scopes. handleToolsCall's
// per-tool RequiredScope check is a transport-layer guard and is bypassed on
// that path. The read-only MCP scopes previously granted here (data:read,
// schema:read, agents:read, chat:use) were therefore not needed for internal
// tool execution, yet they are valid REST scopes: a leaked endpoint key could
// use them to read project data or use chat project-wide, contradicting the
// "exactly one call_agent tool" promise. They are deliberately dropped.
// projects:read is retained as the narrowest project-context scope (and is what
// share tokens are expected to carry); the marker keeps the credential usable
// only at the per-agent endpoint.
var agentShareScopes = []string{AgentCallScope, "projects:read"}

// hasAgentCallScope reports whether scopes contains the per-agent marker scope.
func hasAgentCallScope(scopes []string) bool {
	for _, s := range scopes {
		if s == AgentCallScope {
			return true
		}
	}
	return false
}

// Per-agent run budget defaults. Kept below common MCP client tools/call
// timeouts (~60s) so a synchronous call returns rather than hanging.
const (
	agentShareRunMaxSteps = 12
	agentShareRunTimeout  = 60 * time.Second
)

// ============================================================================
// Legacy model + store (deprecated)
// ============================================================================

// AgentMCPShare binds one core.api_tokens credential to exactly one project
// agent, exposing that agent as a single-tool MCP server.
//
// Deprecated: superseded by AgentMCPEndpoint + AgentMCPKey
// (core.agent_mcp_endpoints / core.agent_mcp_keys). The table is retained for
// the lane-1 backfill and is dropped in a later migration once no reader
// remains; this model/store are kept only for that migration's tests.
type AgentMCPShare struct {
	bun.BaseModel `bun:"table:core.agent_mcp_shares,alias:ams"`

	ID          string     `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	ProjectID   string     `bun:"project_id,type:uuid,notnull"`
	AgentID     string     `bun:"agent_id,type:uuid,notnull"`
	TokenID     string     `bun:"token_id,type:uuid,notnull"`
	Name        string     `bun:"name,notnull"`
	Description *string    `bun:"description"`
	CreatedBy   *string    `bun:"created_by,type:uuid"`
	CreatedAt   time.Time  `bun:"created_at,notnull,default:now()"`
	UpdatedAt   time.Time  `bun:"updated_at,notnull,default:now()"`
	RevokedAt   *time.Time `bun:"revoked_at"`

	// Transient fields populated by read queries that join core.api_tokens.
	TokenLastUsedAt *time.Time `bun:"token_last_used_at,scanonly"`
	TokenRevokedAt  *time.Time `bun:"token_revoked_at,scanonly"`
	TokenExpiresAt  *time.Time `bun:"token_expires_at,scanonly"`
}

// agentMCPShareStore persists legacy AgentMCPShare rows.
//
// Deprecated: superseded by agentMCPEndpointStore + agentMCPKeyStore. Retained
// for the lane-1 backfill tests until the table is dropped.
type agentMCPShareStore interface {
	ListByProject(ctx context.Context, projectID string) ([]*AgentMCPShare, error)
	ListByAgent(ctx context.Context, projectID, agentID string) ([]*AgentMCPShare, error)
	GetByID(ctx context.Context, projectID, id string) (*AgentMCPShare, error)
	GetByTokenID(ctx context.Context, tokenID string) (*AgentMCPShare, error)
	FindByName(ctx context.Context, projectID, name string) (*AgentMCPShare, error)
	Create(ctx context.Context, share *AgentMCPShare) error
	Update(ctx context.Context, share *AgentMCPShare) error
}

// ============================================================================
// Bun store implementation
// ============================================================================

type bunAgentMCPShareStore struct {
	db bun.IDB
}

func newAgentMCPShareStore(db bun.IDB) *bunAgentMCPShareStore {
	return &bunAgentMCPShareStore{db: db}
}

// agentMCPShareOrNil avoids installing a store whose underlying DB is nil.
func agentMCPShareOrNil(db bun.IDB) agentMCPShareStore {
	if db == nil {
		return nil
	}
	return newAgentMCPShareStore(db)
}

// Assert conformance; the legacy store is retained for the backfill tests.
var _ agentMCPShareStore = (*bunAgentMCPShareStore)(nil)

const agentMCPShareSelect = `SELECT ams.id, ams.project_id, ams.agent_id, ams.token_id,
	ams.name, ams.description, ams.created_by,
	ams.created_at, ams.updated_at, ams.revoked_at,
	at.last_used_at AS token_last_used_at,
	at.revoked_at AS token_revoked_at,
	at.expires_at AS token_expires_at
FROM core.agent_mcp_shares ams
LEFT JOIN core.api_tokens at ON at.id = ams.token_id`

func (r *bunAgentMCPShareStore) ListByProject(ctx context.Context, projectID string) ([]*AgentMCPShare, error) {
	var rows []*AgentMCPShare
	err := r.db.NewRaw(agentMCPShareSelect+` WHERE ams.project_id = ? ORDER BY ams.created_at DESC`, projectID).Scan(ctx, &rows)
	if err != nil {
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return rows, nil
}

func (r *bunAgentMCPShareStore) ListByAgent(ctx context.Context, projectID, agentID string) ([]*AgentMCPShare, error) {
	var rows []*AgentMCPShare
	err := r.db.NewRaw(agentMCPShareSelect+` WHERE ams.project_id = ? AND ams.agent_id = ? ORDER BY ams.created_at DESC`, projectID, agentID).Scan(ctx, &rows)
	if err != nil {
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return rows, nil
}

func (r *bunAgentMCPShareStore) GetByID(ctx context.Context, projectID, id string) (*AgentMCPShare, error) {
	row := new(AgentMCPShare)
	err := r.db.NewRaw(agentMCPShareSelect+` WHERE ams.project_id = ? AND ams.id = ?`, projectID, id).Scan(ctx, row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return row, nil
}

func (r *bunAgentMCPShareStore) GetByTokenID(ctx context.Context, tokenID string) (*AgentMCPShare, error) {
	row := new(AgentMCPShare)
	err := r.db.NewRaw(agentMCPShareSelect+` WHERE ams.token_id = ? AND ams.revoked_at IS NULL`, tokenID).Scan(ctx, row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return row, nil
}

func (r *bunAgentMCPShareStore) FindByName(ctx context.Context, projectID, name string) (*AgentMCPShare, error) {
	row := new(AgentMCPShare)
	err := r.db.NewRaw(agentMCPShareSelect+` WHERE ams.project_id = ? AND lower(ams.name) = lower(?) AND ams.revoked_at IS NULL`, projectID, name).Scan(ctx, row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return row, nil
}

func (r *bunAgentMCPShareStore) Create(ctx context.Context, share *AgentMCPShare) error {
	_, err := r.db.NewInsert().Model(share).Exec(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return apperror.New(409, "agent_mcp_share_name_exists", "An agent MCP share with this name already exists")
		}
		return apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return nil
}

func (r *bunAgentMCPShareStore) Update(ctx context.Context, share *AgentMCPShare) error {
	_, err := r.db.NewUpdate().
		Model((*AgentMCPShare)(nil)).
		Set("name = ?", share.Name).
		Set("description = ?", share.Description).
		Set("token_id = ?", share.TokenID).
		Set("updated_at = ?", share.UpdatedAt).
		Set("revoked_at = ?", share.RevokedAt).
		Where("id = ?", share.ID).
		Where("project_id = ?", share.ProjectID).
		Exec(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return apperror.New(409, "agent_mcp_share_name_exists", "An agent MCP share with this name already exists")
		}
		return apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return nil
}

// ============================================================================
// Agent resolution
// ============================================================================

// errDefinitionHasNoRuntimeAgent marks an agent definition that exists in the
// project but has no runtime agent yet. It is distinguishable from "unknown
// agent" so write paths can return 422 (definition exists, not usable yet)
// while unknown IDs keep returning 404.
var errDefinitionHasNoRuntimeAgent = errors.New("agent definition has no runtime agent in this project yet")

// resolveProjectAgent returns a project runtime agent reference by ID, or nil
// when the agent does not exist in the project. Errors propagate storage
// failures.
func (s *Service) resolveProjectAgent(ctx context.Context, projectID, agentID string) (*AgentRef, error) {
	dir := s.agentDirectorySvc()
	if dir == nil {
		return nil, apperror.NewInternal("agent directory unavailable", nil)
	}
	return dir.FindProjectAgentByID(ctx, projectID, agentID)
}

// resolveAgentShareTarget resolves an incoming agent reference for the
// per-agent endpoint surface. It accepts either a runtime agent ID or an
// agent-definition ID; for a definition ID it returns the definition's primary
// runtime agent (FK-linked first, then oldest, then id).
//
// It returns (nil, nil) when the ID is neither a project runtime agent nor a
// project agent definition, and (nil, errDefinitionHasNoRuntimeAgent) when the
// ID is a known definition that resolves to no runtime agent yet.
func (s *Service) resolveAgentShareTarget(ctx context.Context, projectID, agentID string) (*AgentRef, error) {
	dir := s.agentDirectorySvc()
	if dir == nil {
		return nil, apperror.NewInternal("agent directory unavailable", nil)
	}
	agent, err := dir.FindProjectAgentByID(ctx, projectID, agentID)
	if err != nil || agent != nil {
		return agent, err
	}
	isDef, err := dir.AgentDefinitionExists(ctx, projectID, agentID)
	if err != nil {
		return nil, err
	}
	if !isDef {
		return nil, nil
	}
	refs, err := dir.FindAgentRefsByDefinitionID(ctx, projectID, agentID)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, errDefinitionHasNoRuntimeAgent
	}
	ref := refs[0]
	return &ref, nil
}

// ============================================================================
// call_agent execution (byte-compatible with the pre-refactor endpoint)
// ============================================================================

// CallAgentOnce runs the bound agent synchronously and maps the outcome to a
// structured MCP tool result. It never returns a transport error: run
// failures/pauses/budget exhaustion are tool results with isError=true.
func (s *Service) CallAgentOnce(ctx context.Context, projectID, agentID, message string) *ToolResult {
	if s.agentToolHandler == nil {
		return agentRunErrorResult(&AgentRunError{Kind: AgentRunErrorUnavailable, Message: "agent execution is unavailable"})
	}
	budget := AgentRunBudget{MaxSteps: agentShareRunMaxSteps, Timeout: agentShareRunTimeout}
	reply, _, err := s.agentToolHandler.RunAgentOnce(ctx, projectID, agentID, message, budget)
	if err != nil {
		return agentRunErrorResult(err)
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: reply}}}
}

// agentRunErrorResult converts a RunAgentOnce failure into a structured,
// isError tool result. Unknown errors are treated as run failures.
func agentRunErrorResult(err error) *ToolResult {
	runErr := new(AgentRunError)
	if !errors.As(err, &runErr) {
		runErr = &AgentRunError{Kind: AgentRunErrorFailed, Message: err.Error()}
	}
	payload := map[string]any{"error": runErr.Message}
	if runErr.Kind != "" {
		payload["kind"] = string(runErr.Kind)
	}
	if runErr.Question != "" {
		payload["question"] = runErr.Question
	}
	if runErr.RunID != "" {
		payload["runId"] = runErr.RunID
	}
	text, mErr := json.Marshal(payload)
	if mErr != nil {
		text = []byte(fmt.Sprintf(`{"error": %q}`, runErr.Message))
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(text)}}, IsError: true}
}
