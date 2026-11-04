package mcp

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// ============================================================================
// DTOs
// ============================================================================

// AgentMCPEndpointDTO is the non-secret representation of an agent's MCP
// endpoint.
type AgentMCPEndpointDTO struct {
	ID        string     `json:"id"`
	ProjectID string     `json:"projectId"`
	AgentID   string     `json:"agentId"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
	MCPURL    string     `json:"mcpUrl"`
}

// AgentMCPKeyDTO is the non-secret representation of one endpoint key. It never
// carries the raw token value.
type AgentMCPKeyDTO struct {
	ID         string     `json:"id"`
	EndpointID string     `json:"endpointId"`
	AgentID    string     `json:"agentId"`
	Label      string     `json:"label"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
}

// AgentMCPKeyListResponse is the response for the list-keys endpoint.
type AgentMCPKeyListResponse struct {
	Keys  []AgentMCPKeyDTO `json:"keys"`
	Total int              `json:"total"`
}

// CreateAgentMCPKeyRequest is the request body for POST .../keys. Label is
// required; ExpiresAt optionally maps onto the bound token's expires_at.
type CreateAgentMCPKeyRequest struct {
	Label     string     `json:"label"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// CreateAgentMCPKeyResponse is returned by create and rotate. It includes the
// raw token value exactly once.
type CreateAgentMCPKeyResponse struct {
	AgentMCPKeyDTO
	Token  string `json:"token"`
	MCPURL string `json:"mcpUrl"`
}

// ============================================================================
// Store accessors
// ============================================================================

func (s *Service) agentEndpointStore() agentMCPEndpointStore {
	if s.agentEndpoints != nil {
		return s.agentEndpoints
	}
	if s.db != nil {
		return newAgentMCPEndpointStore(s.db)
	}
	return nil
}

func (s *Service) agentKeyStore() agentMCPKeyStore {
	if s.agentKeys != nil {
		return s.agentKeys
	}
	if s.db != nil {
		return newAgentMCPKeyStore(s.db)
	}
	return nil
}

// ============================================================================
// Pure helpers
// ============================================================================

// agentMCPKeyTokenNamePrefix prefixes every endpoint-key token name.
const agentMCPKeyTokenNamePrefix = "Agent MCP Key: "

// agentMCPKeyTokenName builds a stable token name for a key. core.api_tokens.name
// is varchar(255), so the name is truncated (by runes) to fit the prefix.
func agentMCPKeyTokenName(label string) string {
	maxRunes := 255 - len(agentMCPKeyTokenNamePrefix)
	runes := []rune(label)
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	return agentMCPKeyTokenNamePrefix + string(runes)
}

// agentMCPKeyTokenNameDisambiguated appends the agent id to keep the name unique
// when a label on a different endpoint would otherwise collide with an existing
// core.api_tokens name (names are unique per user+project among active tokens).
// The label is truncated first so the suffix survives the varchar(255) limit.
func agentMCPKeyTokenNameDisambiguated(label, agentID string) string {
	suffix := " (" + agentID + ")"
	maxLabel := 255 - len(agentMCPKeyTokenNamePrefix) - len([]rune(suffix))
	runes := []rune(label)
	if maxLabel < 0 {
		maxLabel = 0
	}
	if len(runes) > maxLabel {
		runes = runes[:maxLabel]
	}
	return agentMCPKeyTokenNamePrefix + string(runes) + suffix
}

// isTokenNameExists reports whether the apitoken failure is the project-unique
// name conflict.
func isTokenNameExists(err error) bool {
	var appErr *apperror.Error
	return errors.As(err, &appErr) && appErr.Code == "token_name_exists"
}

// agentMCPEndpointURL builds the per-agent MCP endpoint URL from a base URL.
func agentMCPEndpointURL(baseURL, agentID string) string {
	path := "/api/mcp/agents/" + agentID
	if baseURL == "" {
		return path
	}
	return strings.TrimRight(baseURL, "/") + path
}

// normalizeAgentMCPKeyLabel trims and validates a key label.
func normalizeAgentMCPKeyLabel(label string) (string, error) {
	trimmed := strings.TrimSpace(label)
	if trimmed == "" {
		return "", apperror.NewValidation("label is required")
	}
	if len(trimmed) > 255 {
		return "", apperror.NewValidation("label must be at most 255 characters")
	}
	return trimmed, nil
}

// agentMCPEndpointStatus derives the endpoint status from its lifecycle.
func agentMCPEndpointStatus(revokedAt *time.Time) string {
	if revokedAt != nil {
		return "revoked"
	}
	return "active"
}

func (ep *AgentMCPEndpoint) toDTO(baseURL string) AgentMCPEndpointDTO {
	return AgentMCPEndpointDTO{
		ID:        ep.ID,
		ProjectID: ep.ProjectID,
		AgentID:   ep.AgentID,
		Status:    agentMCPEndpointStatus(ep.RevokedAt),
		CreatedAt: ep.CreatedAt,
		UpdatedAt: ep.UpdatedAt,
		RevokedAt: ep.RevokedAt,
		MCPURL:    agentMCPEndpointURL(baseURL, ep.AgentID),
	}
}

// agentMCPKeyDTO renders a freshly created/repointed key. TokenLifecycle
// timestamps are passed explicitly because a new row carries no joined token
// fields.
func agentMCPKeyDTO(key *AgentMCPKey, agentID string, tokenExpiresAt *time.Time, now time.Time) AgentMCPKeyDTO {
	return AgentMCPKeyDTO{
		ID:         key.ID,
		EndpointID: key.EndpointID,
		AgentID:    agentID,
		Label:      key.Label,
		Status:     shareInstanceStatus(key.RevokedAt, key.TokenRevokedAt, tokenExpiresAt, now),
		CreatedAt:  key.CreatedAt,
		UpdatedAt:  key.UpdatedAt,
		LastUsedAt: key.TokenLastUsedAt,
		ExpiresAt:  tokenExpiresAt,
	}
}

// agentMCPKeyDetailDTO renders a listed key from the joined read model.
func agentMCPKeyDetailDTO(key *AgentMCPKeyDetail, now time.Time) AgentMCPKeyDTO {
	return AgentMCPKeyDTO{
		ID:         key.ID,
		EndpointID: key.EndpointID,
		AgentID:    key.EndpointAgentID,
		Label:      key.Label,
		Status:     shareInstanceStatus(key.RevokedAt, key.TokenRevokedAt, key.TokenExpiresAt, now),
		CreatedAt:  key.CreatedAt,
		UpdatedAt:  key.UpdatedAt,
		LastUsedAt: key.TokenLastUsedAt,
		ExpiresAt:  key.TokenExpiresAt,
	}
}

// ============================================================================
// Authorization
// ============================================================================

// AuthorizeAgentEndpoint authorizes a request to the per-agent MCP endpoint. It
// resolves the ACTIVE key bound to the presented credential, then the endpoint
// that key belongs to, and rejects:
//   - an unknown/unbound credential (403 "credential not bound")
//   - a key whose endpoint is missing or revoked (403 "credential not bound")
//   - a credential bound to a different agent than the URL names (403)
//   - a revoked or expired token (403)
//   - a disabled (or missing) bound agent (403), failing fast before any run.
//
// The returned key is the credential identity used to scope sessions; the
// endpoint carries the authorization target (agent).
func (s *Service) AuthorizeAgentEndpoint(ctx context.Context, apiTokenID, agentID string) (*AgentMCPEndpoint, *AgentMCPKey, error) {
	if strings.TrimSpace(apiTokenID) == "" {
		return nil, nil, apperror.NewForbidden("credential not bound")
	}
	keyStore := s.agentKeyStore()
	if keyStore == nil {
		return nil, nil, apperror.NewInternal("agent MCP key storage unavailable", nil)
	}
	key, err := keyStore.GetActiveKeyByTokenID(ctx, apiTokenID)
	if err != nil {
		return nil, nil, err
	}
	if key == nil {
		return nil, nil, apperror.NewForbidden("credential not bound")
	}

	endpointStore := s.agentEndpointStore()
	if endpointStore == nil {
		return nil, nil, apperror.NewInternal("agent MCP endpoint storage unavailable", nil)
	}
	endpoint, err := endpointStore.GetEndpointByID(ctx, key.EndpointID)
	if err != nil {
		return nil, nil, err
	}
	if endpoint == nil || endpoint.RevokedAt != nil {
		return nil, nil, apperror.NewForbidden("credential not bound")
	}
	if endpoint.AgentID != agentID {
		return nil, nil, apperror.NewForbidden("credential is bound to a different agent")
	}

	now := time.Now().UTC()
	if key.TokenRevokedAt != nil {
		return nil, nil, apperror.NewForbidden("credential is revoked")
	}
	if key.TokenExpiresAt != nil && !key.TokenExpiresAt.After(now) {
		return nil, nil, apperror.NewForbidden("credential is expired")
	}

	agent, err := s.resolveProjectAgent(ctx, endpoint.ProjectID, endpoint.AgentID)
	if err != nil {
		return nil, nil, err
	}
	if agent == nil {
		return nil, nil, apperror.NewForbidden("agent is unavailable")
	}
	if !agent.Enabled {
		return nil, nil, apperror.NewForbidden("agent is disabled")
	}
	return endpoint, key, nil
}

// ============================================================================
// Endpoint lifecycle
// ============================================================================

// resolveEndpointAgent resolves the agent reference named by an endpoint route
// (a runtime agent ID or an agent-definition ID) and maps failures to the
// endpoint surface's error shape.
func (s *Service) resolveEndpointAgent(ctx context.Context, projectID, agentID string) (*AgentRef, error) {
	trimmed := strings.TrimSpace(agentID)
	if _, err := uuid.Parse(trimmed); err != nil {
		return nil, apperror.NewValidation("invalid agent id: " + agentID)
	}
	agent, err := s.resolveAgentShareTarget(ctx, projectID, trimmed)
	if err != nil {
		if errors.Is(err, errDefinitionHasNoRuntimeAgent) {
			return nil, apperror.NewValidation("agent " + trimmed + " has no runtime agent in this project yet")
		}
		return nil, err
	}
	if agent == nil {
		return nil, apperror.NewNotFound("Agent", trimmed)
	}
	return agent, nil
}

// CreateAgentEndpoint establishes the single active endpoint for an agent. It
// returns 409 when an active endpoint already exists.
func (s *Service) CreateAgentEndpoint(ctx context.Context, projectID, userID, baseURL, agentID string) (*AgentMCPEndpointDTO, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	store := s.agentEndpointStore()
	if store == nil {
		return nil, apperror.NewInternal("agent MCP endpoint storage unavailable", nil)
	}
	agent, err := s.resolveEndpointAgent(ctx, projectID, agentID)
	if err != nil {
		return nil, err
	}
	existing, err := store.GetActiveEndpointByAgentID(ctx, projectID, agent.ID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, apperror.New(409, "agent_mcp_endpoint_exists", "An active MCP endpoint already exists for this agent")
	}

	now := time.Now().UTC()
	endpoint := &AgentMCPEndpoint{
		ID:        uuid.NewString(),
		ProjectID: projectID,
		AgentID:   agent.ID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.CreateEndpoint(ctx, endpoint); err != nil {
		return nil, err
	}
	dto := endpoint.toDTO(baseURL)
	return &dto, nil
}

// GetAgentEndpoint returns the agent's active endpoint, or a not-found error
// when none exists.
func (s *Service) GetAgentEndpoint(ctx context.Context, projectID, userID, baseURL, agentID string) (*AgentMCPEndpointDTO, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	store := s.agentEndpointStore()
	if store == nil {
		return nil, apperror.NewInternal("agent MCP endpoint storage unavailable", nil)
	}
	agent, err := s.resolveEndpointAgent(ctx, projectID, agentID)
	if err != nil {
		var appErr *apperror.Error
		if errors.As(err, &appErr) && appErr.HTTPStatus == 422 {
			// A definition with no runtime agent has no endpoint: not found.
			return nil, apperror.NewNotFound("Agent MCP endpoint", agentID)
		}
		return nil, err
	}
	endpoint, err := store.GetActiveEndpointByAgentID(ctx, projectID, agent.ID)
	if err != nil {
		return nil, err
	}
	if endpoint == nil {
		return nil, apperror.NewNotFound("Agent MCP endpoint", agent.ID)
	}
	dto := endpoint.toDTO(baseURL)
	return &dto, nil
}

// RevokeAgentEndpoint revokes an endpoint and its remaining active keys,
// invalidating their tokens. It is idempotent and project-scoped.
func (s *Service) RevokeAgentEndpoint(ctx context.Context, projectID, userID, endpointID string) error {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return err
	}
	store := s.agentEndpointStore()
	if store == nil {
		return apperror.NewInternal("agent MCP endpoint storage unavailable", nil)
	}
	endpoint, err := store.GetEndpointByID(ctx, endpointID)
	if err != nil {
		return err
	}
	if endpoint == nil || endpoint.ProjectID != projectID {
		return apperror.NewNotFound("Agent MCP endpoint", endpointID)
	}
	if endpoint.RevokedAt != nil {
		return nil
	}
	now := time.Now().UTC()
	if err := store.RevokeEndpoint(ctx, endpointID, now); err != nil {
		return err
	}
	return s.revokeEndpointKeys(ctx, projectID, userID, endpointID, now)
}

// revokeEndpointKeys best-effort revokes every active key on an endpoint and
// its bound token. The endpoint is already revoked, so a failure here cannot
// re-enable it.
func (s *Service) revokeEndpointKeys(ctx context.Context, projectID, userID, endpointID string, now time.Time) error {
	keyStore := s.agentKeyStore()
	if keyStore == nil {
		return nil
	}
	keys, err := keyStore.ListKeysByEndpoint(ctx, endpointID)
	if err != nil {
		return err
	}
	tokenSvc := s.shareTokenSvc()
	for _, key := range keys {
		if key.RevokedAt != nil {
			continue
		}
		if tokenSvc != nil {
			if rerr := tokenSvc.Revoke(ctx, key.TokenID, projectID, userID); rerr != nil && !isTokenAlreadyRevoked(rerr) {
				return rerr
			}
		}
		if rerr := keyStore.RevokeKey(ctx, key.ID, now); rerr != nil {
			return rerr
		}
	}
	return nil
}

// ============================================================================
// Key lifecycle
// ============================================================================

// CreateAgentKey mints a labeled credential bound to an endpoint. The raw token
// is returned exactly once. The label is unique among the endpoint's active
// keys, case-insensitively.
func (s *Service) CreateAgentKey(ctx context.Context, projectID, userID, baseURL, endpointID string, req CreateAgentMCPKeyRequest) (*CreateAgentMCPKeyResponse, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	endpointStore := s.agentEndpointStore()
	keyStore := s.agentKeyStore()
	tokenSvc := s.shareTokenSvc()
	if endpointStore == nil || keyStore == nil || tokenSvc == nil {
		return nil, apperror.NewInternal("agent MCP key storage unavailable", nil)
	}
	label, err := normalizeAgentMCPKeyLabel(req.Label)
	if err != nil {
		return nil, err
	}
	endpoint, err := endpointStore.GetEndpointByID(ctx, endpointID)
	if err != nil {
		return nil, err
	}
	if endpoint == nil || endpoint.ProjectID != projectID {
		return nil, apperror.NewNotFound("Agent MCP endpoint", endpointID)
	}
	if endpoint.RevokedAt != nil {
		return nil, apperror.New(409, "agent_mcp_endpoint_revoked", "Agent MCP endpoint is revoked")
	}

	existing, err := keyStore.ListKeysByEndpoint(ctx, endpointID)
	if err != nil {
		return nil, err
	}
	for _, key := range existing {
		if key.RevokedAt == nil && strings.EqualFold(key.Label, label) {
			return nil, apperror.New(409, "agent_mcp_key_label_exists", "An active key with this label already exists for this endpoint")
		}
	}

	tokenName := agentMCPKeyTokenName(label)
	token, err := tokenSvc.CreateAgentShareTokenWithExpiry(
		ctx, projectID, userID, tokenName, append([]string(nil), agentShareScopes...), req.ExpiresAt,
	)
	if err != nil && isTokenNameExists(err) {
		// The same label may exist on another endpoint of the project; token
		// names are project-unique, so disambiguate rather than reject.
		tokenName = agentMCPKeyTokenNameDisambiguated(label, endpoint.AgentID)
		token, err = tokenSvc.CreateAgentShareTokenWithExpiry(
			ctx, projectID, userID, tokenName, append([]string(nil), agentShareScopes...), req.ExpiresAt,
		)
	}
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	key := &AgentMCPKey{
		ID:         uuid.NewString(),
		EndpointID: endpointID,
		TokenID:    token.ID,
		Label:      label,
		CreatedBy:  &userID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := keyStore.CreateKey(ctx, key); err != nil {
		// Best-effort cleanup so we do not strand an untracked credential.
		_ = tokenSvc.Revoke(ctx, token.ID, projectID, userID)
		return nil, err
	}
	return &CreateAgentMCPKeyResponse{
		AgentMCPKeyDTO: agentMCPKeyDTO(key, endpoint.AgentID, token.ExpiresAt, now),
		Token:          token.Token,
		MCPURL:         agentMCPEndpointURL(baseURL, endpoint.AgentID),
	}, nil
}

// ListAgentKeys returns an endpoint's keys (active and revoked) without
// secrets. It is project-scoped.
func (s *Service) ListAgentKeys(ctx context.Context, projectID, userID, endpointID string) (*AgentMCPKeyListResponse, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	endpointStore := s.agentEndpointStore()
	keyStore := s.agentKeyStore()
	if endpointStore == nil || keyStore == nil {
		return nil, apperror.NewInternal("agent MCP key storage unavailable", nil)
	}
	endpoint, err := endpointStore.GetEndpointByID(ctx, endpointID)
	if err != nil {
		return nil, err
	}
	if endpoint == nil || endpoint.ProjectID != projectID {
		return nil, apperror.NewNotFound("Agent MCP endpoint", endpointID)
	}
	keys, err := keyStore.ListKeysByEndpoint(ctx, endpointID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]AgentMCPKeyDTO, 0, len(keys))
	for _, key := range keys {
		out = append(out, agentMCPKeyDetailDTO(key, now))
	}
	return &AgentMCPKeyListResponse{Keys: out, Total: len(out)}, nil
}

// RevokeAgentKey revokes one key without touching its endpoint or sibling keys.
// It is idempotent and project-scoped.
func (s *Service) RevokeAgentKey(ctx context.Context, projectID, userID, keyID string) error {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return err
	}
	keyStore := s.agentKeyStore()
	if keyStore == nil {
		return apperror.NewInternal("agent MCP key storage unavailable", nil)
	}
	key, err := keyStore.GetKeyByID(ctx, keyID)
	if err != nil {
		return err
	}
	if key == nil || key.EndpointProjectID != projectID {
		return apperror.NewNotFound("Agent MCP key", keyID)
	}
	if key.RevokedAt != nil {
		return nil
	}
	if tokenSvc := s.shareTokenSvc(); tokenSvc != nil {
		if err := tokenSvc.Revoke(ctx, key.TokenID, projectID, userID); err != nil && !isTokenAlreadyRevoked(err) {
			return err
		}
	}
	return keyStore.RevokeKey(ctx, keyID, time.Now().UTC())
}

// RotateAgentKey issues a replacement token for the same key and endpoint,
// invalidating the previous token while preserving the key identity so existing
// sessions remain continuable.
func (s *Service) RotateAgentKey(ctx context.Context, projectID, userID, baseURL, keyID string) (*CreateAgentMCPKeyResponse, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	keyStore := s.agentKeyStore()
	tokenSvc := s.shareTokenSvc()
	if keyStore == nil || tokenSvc == nil {
		return nil, apperror.NewInternal("agent MCP key storage unavailable", nil)
	}
	key, err := keyStore.GetKeyByID(ctx, keyID)
	if err != nil {
		return nil, err
	}
	if key == nil || key.EndpointProjectID != projectID {
		return nil, apperror.NewNotFound("Agent MCP key", keyID)
	}
	if key.RevokedAt != nil {
		return nil, apperror.New(409, "agent_mcp_key_revoked", "Agent MCP key is revoked and cannot be rotated")
	}

	now := time.Now().UTC()
	updatedInTx := false
	newToken, err := tokenSvc.RegenerateWith(ctx, key.TokenID, projectID, userID, func(ctx context.Context, tx bun.Tx, newID string) error {
		updatedInTx = true
		_, uerr := tx.NewUpdate().
			Model((*AgentMCPKey)(nil)).
			Set("token_id = ?", newID).
			Set("updated_at = ?", now).
			Where("id = ?", key.ID).
			Where("endpoint_id = ?", key.EndpointID).
			Exec(ctx)
		return uerr
	})
	if err != nil {
		return nil, err
	}
	if !updatedInTx {
		// Test seam / non-transactional token service: repoint via the store and
		// compensate by revoking the replacement if the update fails.
		if uerr := keyStore.SetKeyToken(ctx, key.ID, newToken.ID, now); uerr != nil {
			_ = tokenSvc.Revoke(ctx, newToken.ID, projectID, userID)
			return nil, uerr
		}
	}

	dto := AgentMCPKeyDTO{
		ID:         key.ID,
		EndpointID: key.EndpointID,
		AgentID:    key.EndpointAgentID,
		Label:      key.Label,
		Status:     agentMCPEndpointStatus(nil),
		CreatedAt:  key.CreatedAt,
		UpdatedAt:  now,
		ExpiresAt:  newToken.ExpiresAt,
	}
	return &CreateAgentMCPKeyResponse{
		AgentMCPKeyDTO: dto,
		Token:          newToken.Token,
		MCPURL:         agentMCPEndpointURL(baseURL, key.EndpointAgentID),
	}, nil
}
