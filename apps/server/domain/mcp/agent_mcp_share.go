package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// ============================================================================
// Constants
// ============================================================================

// AgentCallScope is the marker scope minted on per-agent MCP share credentials.
// It is deliberately absent from normal project tokens. The project MCP
// transports (handler.go, streamable_http_handler.go, sse_handler.go) reject any
// credential carrying it, and the per-agent endpoint requires it. This keeps a
// share key usable only against its own agent.
const AgentCallScope = "mcp:agent-call"

// agentShareScopes is the scope set a per-agent MCP share token needs. It grants
// the legacy read-only MCP set (so the bound agent's internal loopback tool
// calls for search/graph/schema work) plus the marker scope that makes the
// credential valid only at the per-agent endpoint. No broad write scopes.
var agentShareScopes = append(append([]string(nil), readOnlyMCPScopes...), AgentCallScope)

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
// Model
// ============================================================================

// AgentMCPShare binds one core.api_tokens credential to exactly one project
// agent, exposing that agent as a single-tool MCP server.
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

// ============================================================================
// Store interface
// ============================================================================

// agentMCPShareStore persists AgentMCPShare rows.
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
// DTOs
// ============================================================================

// CreateAgentMCPShareRequest is the request body for POST .../mcp-share.
type CreateAgentMCPShareRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

// AgentMCPShareDTO is the non-secret representation of an agent share.
type AgentMCPShareDTO struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"projectId"`
	AgentID     string     `json:"agentId"`
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
}

// AgentMCPShareListResponse is the response for the list endpoints.
type AgentMCPShareListResponse struct {
	Shares []AgentMCPShareDTO `json:"shares"`
	Total  int                `json:"total"`
}

// CreateAgentMCPShareResponse is returned by create/rotate and includes the raw
// token value exactly once.
type CreateAgentMCPShareResponse struct {
	AgentMCPShareDTO
	Token  string `json:"token"`
	MCPURL string `json:"mcpUrl"`
}

// ============================================================================
// Pure helpers
// ============================================================================

// normalizeAgentShareName trims and validates a share name.
func normalizeAgentShareName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", apperror.ErrValidation.WithMessage("name is required")
	}
	if len(trimmed) > 255 {
		return "", apperror.ErrValidation.WithMessage("name must be at most 255 characters")
	}
	return trimmed, nil
}

// defaultAgentShareName builds a name identifying the agent and the date.
func defaultAgentShareName(agentName string) string {
	name := strings.TrimSpace(agentName)
	if name == "" {
		name = "agent"
	}
	return fmt.Sprintf("%s — %s", name, time.Now().UTC().Format("2006-01-02"))
}

// agentMCPEndpointURL builds the per-agent MCP endpoint URL from a base URL.
func agentMCPEndpointURL(baseURL, agentID string) string {
	path := "/api/mcp/agents/" + agentID
	if baseURL == "" {
		return path
	}
	return strings.TrimRight(baseURL, "/") + path
}

// agentShareStatus derives active/revoked/expired from the bound token and the
// share lifecycle. Uses the same semantics as share instances.
func agentShareStatus(shareRevokedAt, tokenRevokedAt, tokenExpiresAt *time.Time, now time.Time) string {
	return shareInstanceStatus(shareRevokedAt, tokenRevokedAt, tokenExpiresAt, now)
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
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return rows, nil
}

func (r *bunAgentMCPShareStore) ListByAgent(ctx context.Context, projectID, agentID string) ([]*AgentMCPShare, error) {
	var rows []*AgentMCPShare
	err := r.db.NewRaw(agentMCPShareSelect+` WHERE ams.project_id = ? AND ams.agent_id = ? ORDER BY ams.created_at DESC`, projectID, agentID).Scan(ctx, &rows)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
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
		return nil, apperror.ErrDatabase.WithInternal(err)
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
		return nil, apperror.ErrDatabase.WithInternal(err)
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
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return row, nil
}

func (r *bunAgentMCPShareStore) Create(ctx context.Context, share *AgentMCPShare) error {
	_, err := r.db.NewInsert().Model(share).Exec(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return apperror.New(409, "agent_mcp_share_name_exists", "An agent MCP share with this name already exists")
		}
		return apperror.ErrDatabase.WithInternal(err)
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
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// ============================================================================
// Service accessors + lifecycle
// ============================================================================

func (s *Service) agentShareStore() agentMCPShareStore {
	if s.agentShares != nil {
		return s.agentShares
	}
	if s.db != nil {
		return newAgentMCPShareStore(s.db)
	}
	return nil
}

// agentShareTokenNamePrefix prefixes every per-agent share token name.
const agentShareTokenNamePrefix = "Agent MCP Share: "

// agentShareTokenName builds a stable token name for a share. core.api_tokens.name
// is varchar(255), so the name is truncated (by runes) to fit the prefix.
func agentShareTokenName(name string) string {
	maxRunes := 255 - len(agentShareTokenNamePrefix)
	runes := []rune(name)
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	return agentShareTokenNamePrefix + string(runes)
}

// CreateAgentShare validates the request, mints a minimal-scope project token,
// and persists the binding.
func (s *Service) CreateAgentShare(ctx context.Context, projectID, userID, baseURL, agentID string, req CreateAgentMCPShareRequest) (*CreateAgentMCPShareResponse, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	store := s.agentShareStore()
	tokenSvc := s.shareTokenSvc()
	if store == nil || tokenSvc == nil {
		return nil, apperror.ErrInternal.WithMessage("agent share storage unavailable")
	}

	agentID = strings.TrimSpace(agentID)
	if _, err := uuid.Parse(agentID); err != nil {
		return nil, apperror.ErrValidation.WithMessage("invalid agent id: " + agentID)
	}
	agent, err := s.resolveProjectAgent(ctx, projectID, agentID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, apperror.ErrNotFound.WithMessage("Agent not found in project")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = defaultAgentShareName(agent.Name)
	} else {
		normalized, nerr := normalizeAgentShareName(name)
		if nerr != nil {
			return nil, nerr
		}
		name = normalized
	}
	if len(name) > 255 {
		return nil, apperror.ErrValidation.WithMessage("name must be at most 255 characters")
	}
	existing, err := store.FindByName(ctx, projectID, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, apperror.New(409, "agent_mcp_share_name_exists", "An agent MCP share named \""+name+"\" already exists for this project")
	}

	token, err := tokenSvc.Create(ctx, projectID, userID, agentShareTokenName(name), append([]string(nil), agentShareScopes...))
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	share := &AgentMCPShare{
		ID:          uuid.NewString(),
		ProjectID:   projectID,
		AgentID:     agentID,
		TokenID:     token.ID,
		Name:        name,
		Description: req.Description,
		CreatedBy:   &userID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Create(ctx, share); err != nil {
		// Best-effort cleanup so we do not strand an untracked credential.
		_ = tokenSvc.Revoke(ctx, token.ID, projectID, userID)
		return nil, err
	}

	return &CreateAgentMCPShareResponse{
		AgentMCPShareDTO: share.toDTO(now),
		Token:            token.Token,
		MCPURL:           agentMCPEndpointURL(baseURL, agentID),
	}, nil
}

// resolveProjectAgent returns a project agent reference by ID, or nil when the
// agent does not exist in the project. Errors propagate storage failures.
func (s *Service) resolveProjectAgent(ctx context.Context, projectID, agentID string) (*AgentRef, error) {
	dir := s.agentDirectorySvc()
	if dir == nil {
		return nil, apperror.ErrInternal.WithMessage("agent directory unavailable")
	}
	return dir.FindProjectAgentByID(ctx, projectID, agentID)
}

// ListAgentShares returns all shares for one agent within a project.
func (s *Service) ListAgentShares(ctx context.Context, projectID, userID, agentID string) (*AgentMCPShareListResponse, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	store := s.agentShareStore()
	if store == nil {
		return nil, apperror.ErrInternal.WithMessage("agent share storage unavailable")
	}
	shares, err := store.ListByAgent(ctx, projectID, agentID)
	if err != nil {
		return nil, err
	}
	return agentShareListResponse(shares), nil
}

// ListProjectAgentShares returns all agent shares in a project.
func (s *Service) ListProjectAgentShares(ctx context.Context, projectID, userID string) (*AgentMCPShareListResponse, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	store := s.agentShareStore()
	if store == nil {
		return nil, apperror.ErrInternal.WithMessage("agent share storage unavailable")
	}
	shares, err := store.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return agentShareListResponse(shares), nil
}

// RevokeAgentShare revokes the bound token and marks the share revoked.
// It is idempotent.
func (s *Service) RevokeAgentShare(ctx context.Context, projectID, userID, id string) error {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return err
	}
	store := s.agentShareStore()
	tokenSvc := s.shareTokenSvc()
	if store == nil || tokenSvc == nil {
		return apperror.ErrInternal.WithMessage("agent share storage unavailable")
	}
	share, err := store.GetByID(ctx, projectID, id)
	if err != nil {
		return err
	}
	if share == nil {
		return apperror.ErrNotFound.WithMessage("Agent MCP share not found")
	}
	if share.RevokedAt != nil {
		return nil
	}
	if err := tokenSvc.Revoke(ctx, share.TokenID, projectID, userID); err != nil && !isTokenAlreadyRevoked(err) {
		return err
	}
	now := time.Now().UTC()
	share.RevokedAt = &now
	share.UpdatedAt = now
	return store.Update(ctx, share)
}

// RotateAgentShare issues a replacement token with the same scopes/binding,
// invalidates the old token, and keeps the agent binding intact.
func (s *Service) RotateAgentShare(ctx context.Context, projectID, userID, id, baseURL string) (*CreateAgentMCPShareResponse, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	store := s.agentShareStore()
	tokenSvc := s.shareTokenSvc()
	if store == nil || tokenSvc == nil {
		return nil, apperror.ErrInternal.WithMessage("agent share storage unavailable")
	}
	share, err := store.GetByID(ctx, projectID, id)
	if err != nil {
		return nil, err
	}
	if share == nil {
		return nil, apperror.ErrNotFound.WithMessage("Agent MCP share not found")
	}
	if share.RevokedAt != nil {
		return nil, apperror.New(409, "agent_mcp_share_revoked", "Agent MCP share is revoked and cannot be rotated")
	}

	// Atomic: revoke old token, insert replacement, and repoint the share's
	// token_id in the same transaction.
	rotateUpdatedInTx := false
	newToken, err := tokenSvc.RegenerateWith(ctx, share.TokenID, projectID, userID, func(ctx context.Context, tx bun.Tx, newID string) error {
		rotateUpdatedInTx = true
		share.TokenID = newID
		share.UpdatedAt = time.Now().UTC()
		_, uerr := tx.NewUpdate().
			Model((*AgentMCPShare)(nil)).
			Set("token_id = ?", newID).
			Set("updated_at = ?", share.UpdatedAt).
			Where("id = ?", share.ID).
			Where("project_id = ?", projectID).
			Exec(ctx)
		return uerr
	})
	if err != nil {
		return nil, err
	}
	if !rotateUpdatedInTx {
		// Test seam / non-transactional token service: update via the store and
		// compensate by revoking the replacement if the update fails.
		share.TokenID = newToken.ID
		share.UpdatedAt = time.Now().UTC()
		if uerr := store.Update(ctx, share); uerr != nil {
			_ = tokenSvc.Revoke(ctx, newToken.ID, projectID, userID)
			return nil, uerr
		}
	}
	return &CreateAgentMCPShareResponse{
		AgentMCPShareDTO: share.toDTO(share.UpdatedAt),
		Token:            newToken.Token,
		MCPURL:           agentMCPEndpointURL(baseURL, share.AgentID),
	}, nil
}

// ResolveAgentShareByToken returns the active share bound to an API token ID,
// or nil when the token is not bound to any active share.
func (s *Service) ResolveAgentShareByToken(ctx context.Context, apiTokenID string) *AgentMCPShare {
	if apiTokenID == "" {
		return nil
	}
	store := s.agentShareStore()
	if store == nil {
		return nil
	}
	share, err := store.GetByTokenID(ctx, apiTokenID)
	if err != nil || share == nil || share.RevokedAt != nil {
		return nil
	}
	return share
}

// AuthorizeAgentShare resolves the share bound to apiTokenID and verifies it
// targets agentID and is still active. It returns the share when authorized, or
// a non-nil error describing the rejection.
func (s *Service) AuthorizeAgentShare(ctx context.Context, apiTokenID, agentID string) (*AgentMCPShare, error) {
	share := s.ResolveAgentShareByToken(ctx, apiTokenID)
	if share == nil {
		return nil, apperror.ErrForbidden.WithMessage("credential is not bound to an agent MCP share")
	}
	if share.AgentID != agentID {
		return nil, apperror.ErrForbidden.WithMessage("credential is bound to a different agent")
	}
	now := time.Now().UTC()
	if share.TokenRevokedAt != nil {
		return nil, apperror.ErrForbidden.WithMessage("share credential is revoked")
	}
	if share.TokenExpiresAt != nil && !share.TokenExpiresAt.After(now) {
		return nil, apperror.ErrForbidden.WithMessage("share credential is expired")
	}
	return share, nil
}

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

// ============================================================================
// DTO helpers
// ============================================================================

func (share *AgentMCPShare) toDTO(now time.Time) AgentMCPShareDTO {
	return AgentMCPShareDTO{
		ID:          share.ID,
		ProjectID:   share.ProjectID,
		AgentID:     share.AgentID,
		Name:        share.Name,
		Description: share.Description,
		Status:      agentShareStatus(share.RevokedAt, share.TokenRevokedAt, share.TokenExpiresAt, now),
		CreatedAt:   share.CreatedAt,
		UpdatedAt:   share.UpdatedAt,
		LastUsedAt:  share.TokenLastUsedAt,
	}
}

func agentShareListResponse(shares []*AgentMCPShare) *AgentMCPShareListResponse {
	now := time.Now().UTC()
	out := make([]AgentMCPShareDTO, 0, len(shares))
	for _, share := range shares {
		out = append(out, share.toDTO(now))
	}
	return &AgentMCPShareListResponse{Shares: out, Total: len(out)}
}
