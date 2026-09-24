package apitoken

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/encryption"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	// TokenPrefix for Emergent API tokens
	TokenPrefix = "emt_"
	// TokenRandomBytes is the number of random bytes in a token
	TokenRandomBytes = 32
)

// Service handles business logic for API tokens
type Service struct {
	db   bun.IDB
	repo *Repository
	enc  *encryption.Service
	log  *slog.Logger
}

// NewService creates a new API token service
func NewService(db bun.IDB, repo *Repository, enc *encryption.Service, log *slog.Logger) *Service {
	return &Service{
		db:   db,
		repo: repo,
		enc:  enc,
		log:  log.With(logger.Scope("apitoken.svc")),
	}
}

// generateToken creates a new API token
// Format: emt_<32-byte-hex> = 4 + 64 = 68 characters
func generateToken() (string, error) {
	bytes := make([]byte, TokenRandomBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return TokenPrefix + hex.EncodeToString(bytes), nil
}

// hashToken creates a SHA-256 hash of a token
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// getTokenPrefix extracts the first 12 characters of a token
func getTokenPrefix(token string) string {
	if len(token) < 12 {
		return token
	}
	return token[:12]
}

// viewerReadOnlyScopes lists the only scopes a project_viewer may include in a token.
var viewerReadOnlyScopes = map[string]bool{
	"data:read":     true,
	"schema:read":   true,
	"agents:read":   true,
	"projects:read": true,
}

// errAdminAllScopeDenied is returned when a caller attempts to grant admin:all
// without org-admin or superadmin privileges.
var errAdminAllScopeDenied = apperror.New(403, "admin-all-scope-denied",
	"admin:all scope requires org admin or superadmin privileges")

// scopesContainAdminAll reports whether scopes includes the admin:all scope.
func scopesContainAdminAll(scopes []string) bool {
	for _, sc := range scopes {
		if sc == "admin:all" {
			return true
		}
	}
	return false
}

// checkAdminAllGrant rejects admin:all unless the caller is a superadmin or org admin.
func (s *Service) checkAdminAllGrant(ctx context.Context, userID string, scopes []string) error {
	if !scopesContainAdminAll(scopes) {
		return nil
	}
	allowed, err := s.repo.CanGrantAdminAll(ctx, userID)
	if err != nil {
		return err
	}
	if !allowed {
		return errAdminAllScopeDenied
	}
	return nil
}

// agentCallScope is the reserved marker scope minted only by the internal
// per-agent MCP share lifecycle. It mirrors mcp.AgentCallScope but is declared
// here to avoid importing domain/mcp (which imports this package).
const agentCallScope = "mcp:agent-call"

// shareAgentChatScope is the reserved marker scope minted only by the internal
// public agent-share link lifecycle (domain/agents share service). It is
// deliberately absent from user-facing token creation.
const shareAgentChatScope = "share:agent-chat"

// deviceAPIScope is the reserved marker scope minted only by the internal
// scoped per-device credential lifecycle (Service.CreateDeviceToken). It is
// deliberately absent from user-facing token creation. Its presence marks a
// token as device-class so the gateway (surface allowlist) and the server
// (exact-set ceiling + surface guard) can constrain it.
const deviceAPIScope = "device:api"

// deviceAPIScopes is the hardcoded scope set minted on every device credential.
// It is the authoritative ceiling: the reserved device:api marker plus the
// read-only agents/data families, strictly below project_viewer. No caller may
// attach or widen this set — CreateDeviceToken hardcodes it and the server
// enforces the exact set at validation time (rejectDeviceTokenOutsideSurface).
var deviceAPIScopes = []string{deviceAPIScope, "agents:read", "data:read"}

// webhookTriggerScope is the reserved marker scope minted only by the internal
// gateway→memory webhook trigger credential lifecycle (Service.CreateWebhookTriggerToken).
// It is deliberately absent from user-facing token creation. Its presence marks
// a token as webhook-trigger-class so the server (exact-set ceiling + surface
// guard) can constrain it to the trigger route and its query loopback.
const webhookTriggerScope = "webhook:trigger"

// webhookTriggerScopes is the hardcoded scope set minted on every webhook
// trigger credential. It is the authoritative ceiling: the reserved
// webhook:trigger marker plus agents:read/agents:write (the trigger route's own
// agents:write gate) and the data:read read family the review agent's in-process
// loopback needs (search-knowledge → /query, which is chat:use, and search via
// data:read). No caller may attach or widen this set — CreateWebhookTriggerToken
// hardcodes it and the server enforces the exact set at validation time.
var webhookTriggerScopes = []string{webhookTriggerScope, "agents:read", "agents:write", "data:read"}

// shareChatScopes is the scope set minted on a public agent-share link key: only
// the share:agent-chat marker, and nothing else. It must never carry a
// project-scoped scope (e.g. projects:read) — project/org context is resolved
// server-side from the binding, and a leaked share key must not be able to list
// the owner's projects or members.
var shareChatScopes = []string{shareAgentChatScope}

// Create creates a user-facing API token. It rejects the reserved agent-share
// marker scope; the internal per-agent share mint path uses
// CreateAgentShareToken instead.
func (s *Service) Create(ctx context.Context, projectID, userID, name string, scopes []string) (*CreateApiTokenResponseDTO, error) {
	return s.create(ctx, projectID, &userID, name, scopes, false, false, false, false, nil)
}

// CreateAgentShareToken mints a token that may carry the reserved
// mcp:agent-call marker. It must only be called by the per-agent MCP share
// lifecycle (domain/mcp/agent_mcp_share.go); all user-facing paths call Create.
func (s *Service) CreateAgentShareToken(ctx context.Context, projectID, userID, name string, scopes []string) (*CreateApiTokenResponseDTO, error) {
	return s.create(ctx, projectID, &userID, name, scopes, true, false, false, false, nil)
}

// CreateAgentShareTokenWithExpiry is CreateAgentShareToken with an optional
// token expiry. It backs the per-endpoint labeled keys, whose optional
// expiresAt maps straight onto core.api_tokens.expires_at.
func (s *Service) CreateAgentShareTokenWithExpiry(ctx context.Context, projectID, userID, name string, scopes []string, expiresAt *time.Time) (*CreateApiTokenResponseDTO, error) {
	return s.create(ctx, projectID, &userID, name, scopes, true, false, false, false, expiresAt)
}

// CreateAgentChatShareToken mints a public agent-share link key carrying the
// reserved share:agent-chat marker. The token is project-scoped but has
// user_id = NULL so it can never resolve as the owner for user-scoped
// endpoints. It must only be called by the agent-share link lifecycle
// (domain/agents); all user-facing paths call Create.
func (s *Service) CreateAgentChatShareToken(ctx context.Context, projectID, name string, expiresAt *time.Time) (*CreateApiTokenResponseDTO, error) {
	return s.create(ctx, projectID, nil, name, shareChatScopes, false, true, false, false, expiresAt)
}

// CreateDeviceToken mints a scoped per-device credential carrying the reserved
// device:api marker and the hardcoded read-only device scope set. The token is
// project-scoped with user_id = NULL (a device is not a user); name and
// expiresAt carry the device's self-reported identity and the operator-configured
// lifetime. The scope set is hardcoded — there is no scopes parameter — so no
// caller can widen it. It must only be called by the device-provisioning
// lifecycle (the gateway's session-authenticated setup flow); all user-facing
// paths call Create.
func (s *Service) CreateDeviceToken(ctx context.Context, projectID, name string, expiresAt *time.Time) (*CreateApiTokenResponseDTO, error) {
	return s.create(ctx, projectID, nil, name, deviceAPIScopes, false, false, true, false, expiresAt)
}

// CreateWebhookTriggerToken mints a scoped per-integration webhook trigger
// credential carrying the reserved webhook:trigger marker and the hardcoded
// webhook ceiling. The token is project-scoped with user_id = NULL (an
// integration is not a user); name carries the integration identity and
// expiresAt the operator-configured lifetime. The scope set is hardcoded — there
// is no scopes parameter — so no caller can widen it. It must only be called by
// the webhook-trigger credential lifecycle (operator/admin mint); all
// user-facing paths call Create and reject the marker.
func (s *Service) CreateWebhookTriggerToken(ctx context.Context, projectID, name string, expiresAt *time.Time) (*CreateApiTokenResponseDTO, error) {
	return s.create(ctx, projectID, nil, name, webhookTriggerScopes, false, false, false, true, expiresAt)
}

// rejectReservedScopes returns an error when scopes carries a reserved marker
// scope that the caller is not allowed to mint. allowAgentCall gates
// mcp:agent-call; allowShareChat gates share:agent-chat; allowDeviceAPI gates
// device:api; allowWebhookTrigger gates webhook:trigger.
func rejectReservedScopes(scopes []string, allowAgentCall, allowShareChat, allowDeviceAPI, allowWebhookTrigger bool) error {
	for _, scope := range scopes {
		if scope == agentCallScope && !allowAgentCall {
			return apperror.NewBadRequest("scope " + agentCallScope + " is reserved for agent MCP shares")
		}
		if scope == shareAgentChatScope && !allowShareChat {
			return apperror.NewBadRequest("scope " + shareAgentChatScope + " is reserved for public agent-share links")
		}
		if scope == deviceAPIScope && !allowDeviceAPI {
			return apperror.NewBadRequest("scope " + deviceAPIScope + " is reserved for scoped device credentials")
		}
		if scope == webhookTriggerScope && !allowWebhookTrigger {
			return apperror.NewBadRequest("scope " + webhookTriggerScope + " is reserved for webhook trigger integrations")
		}
	}
	return nil
}

// create is the shared implementation behind Create, CreateAgentShareToken,
// CreateAgentChatShareToken, CreateDeviceToken and CreateWebhookTriggerToken.
// allowAgentCallScope gates the reserved mcp:agent-call marker; allowShareChatScope
// gates share:agent-chat; allowDeviceAPIScope gates device:api; allowWebhookTrigger
// gates webhook:trigger; expiresAt, when non-nil, is persisted as the token's
// expiry. userID is nil for non-user tokens (share keys, device/webhook
// credentials): such tokens get user_id = NULL and skip the user-scoped checks.
func (s *Service) create(ctx context.Context, projectID string, userID *string, name string, scopes []string, allowAgentCallScope, allowShareChatScope, allowDeviceAPIScope, allowWebhookTrigger bool, expiresAt *time.Time) (*CreateApiTokenResponseDTO, error) {
	if err := rejectReservedScopes(scopes, allowAgentCallScope, allowShareChatScope, allowDeviceAPIScope, allowWebhookTrigger); err != nil {
		return nil, err
	}
	// Validate scopes
	for _, scope := range scopes {
		valid := false
		for _, validScope := range ValidApiTokenScopes {
			if scope == validScope {
				valid = true
				break
			}
		}
		if !valid {
			return nil, apperror.ErrBadRequest.WithMessage("invalid scope: " + scope)
		}
	}

	uid := ""
	if userID != nil {
		uid = *userID
	}

	// admin:all requires org admin or superadmin privileges
	if err := s.checkAdminAllGrant(ctx, uid, scopes); err != nil {
		return nil, err
	}

	// Viewers may only create read-only tokens
	if uid != "" && projectID != "" {
		role, err := s.repo.GetUserProjectRole(ctx, projectID, uid)
		if err != nil {
			return nil, err
		}
		if role == "project_viewer" {
			for _, scope := range scopes {
				if !viewerReadOnlyScopes[scope] {
					return nil, apperror.New(403, "viewer-write-scope-denied",
						"project_viewer may only request read-only scopes (data:read, schema:read, agents:read, projects:read)")
				}
			}
		}
	}

	// Check for duplicate name (matches DB unique index on user_id + name).
	// Skipped for non-user tokens (user_id IS NULL has no name uniqueness).
	if userID != nil {
		existing, err := s.repo.FindByUserAndProjectAndName(ctx, uid, projectID, name)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return nil, apperror.New(409, "token_name_exists", "A token named \""+name+"\" already exists for this project")
		}
	}

	// Generate token
	rawToken, err := generateToken()
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}

	// Encrypt the raw token for later retrieval
	var tokenEncrypted *string
	if s.enc != nil && s.enc.IsConfigured() {
		encrypted, encErr := s.enc.EncryptJSON(ctx, rawToken)
		if encErr != nil {
			s.log.Warn("failed to encrypt token for storage, token will not be retrievable later",
				slog.String("error", encErr.Error()))
		} else {
			tokenEncrypted = &encrypted
		}
	}

	// Create token record
	token := &ApiToken{
		ProjectID:      &projectID,
		UserID:         userID,
		Name:           name,
		TokenHash:      hashToken(rawToken),
		TokenPrefix:    getTokenPrefix(rawToken),
		TokenEncrypted: tokenEncrypted,
		Scopes:         scopes,
		ExpiresAt:      expiresAt,
	}

	if err := s.repo.Create(ctx, token); err != nil {
		return nil, err
	}

	s.log.Info("created API token",
		slog.String("name", name),
		slog.String("tokenPrefix", token.TokenPrefix),
		slog.String("projectID", projectID))

	return &CreateApiTokenResponseDTO{
		ApiTokenDTO: token.ToDTO(),
		Token:       rawToken, // Only returned at creation time
	}, nil
}

// GetUserProjectRole returns the role of a user in a project ("" if not a member).
func (s *Service) GetUserProjectRole(ctx context.Context, projectID, userID string) (string, error) {
	return s.repo.GetUserProjectRole(ctx, projectID, userID)
}

// ListByProject returns all tokens for a project, marking the caller's own
// tokens via OwnedByCaller.
func (s *Service) ListByProject(ctx context.Context, projectID, callerUserID string) (*ApiTokenListResponseDTO, error) {
	tokens, err := s.repo.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	dtos := make([]ApiTokenDTO, len(tokens))
	for i, t := range tokens {
		dtos[i] = t.ToDTO()
		dtos[i].OwnedByCaller = t.UserID != nil && *t.UserID == callerUserID
	}

	return &ApiTokenListResponseDTO{
		Tokens: dtos,
		Total:  len(dtos),
	}, nil
}

// GetByID returns a token by ID, including the decrypted token value if available
func (s *Service) GetByID(ctx context.Context, tokenID, projectID string) (*GetApiTokenResponseDTO, error) {
	token, err := s.repo.GetByID(ctx, tokenID, projectID)
	if err != nil {
		return nil, err
	}
	if token == nil {
		return nil, nil
	}

	dto := &GetApiTokenResponseDTO{
		ApiTokenDTO: token.ToDTO(),
	}

	// Decrypt the token if available
	if token.TokenEncrypted != nil && *token.TokenEncrypted != "" && s.enc != nil && s.enc.IsConfigured() {
		decrypted, decErr := s.enc.Decrypt(ctx, *token.TokenEncrypted)
		if decErr != nil {
			s.log.Warn("failed to decrypt stored token",
				slog.String("tokenID", tokenID),
				slog.String("error", decErr.Error()))
		} else if val, ok := decrypted["value"]; ok {
			if tokenStr, ok := val.(string); ok {
				dto.Token = tokenStr
			}
		}
	}

	return dto, nil
}

// Revoke revokes a token
func (s *Service) Revoke(ctx context.Context, tokenID, projectID, userID string) error {
	// Check if token exists
	token, err := s.repo.GetByID(ctx, tokenID, projectID)
	if err != nil {
		return err
	}
	if token == nil {
		return apperror.ErrNotFound.WithMessage("Token not found")
	}

	// Check if already revoked
	if token.RevokedAt != nil {
		return apperror.New(409, "token_already_revoked", "Token is already revoked")
	}

	// Revoke
	revoked, err := s.repo.Revoke(ctx, tokenID, projectID)
	if err != nil {
		return err
	}
	if !revoked {
		return apperror.ErrNotFound.WithMessage("Token not found")
	}

	s.log.Info("revoked API token",
		slog.String("name", token.Name),
		slog.String("tokenPrefix", token.TokenPrefix),
		slog.String("userID", userID))

	return nil
}

// CreateAccountToken creates a new account-level (non-project-bound) API token
func (s *Service) CreateAccountToken(ctx context.Context, userID, name string, scopes []string) (*CreateApiTokenResponseDTO, error) {
	if err := rejectReservedScopes(scopes, false, false, false, false); err != nil {
		return nil, err
	}
	// Validate scopes
	for _, scope := range scopes {
		valid := false
		for _, validScope := range ValidApiTokenScopes {
			if scope == validScope {
				valid = true
				break
			}
		}
		if !valid {
			return nil, apperror.ErrBadRequest.WithMessage("invalid scope: " + scope)
		}
	}

	// admin:all requires org admin or superadmin privileges
	if err := s.checkAdminAllGrant(ctx, userID, scopes); err != nil {
		return nil, err
	}

	// Check for duplicate name (among active account tokens for this user)
	existing, err := s.repo.FindByUserAndName(ctx, userID, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, apperror.New(409, "token_name_exists", "An account token named \""+name+"\" already exists")
	}

	// Generate token
	rawToken, err := generateToken()
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}

	// Encrypt the raw token for later retrieval
	var tokenEncrypted *string
	if s.enc != nil && s.enc.IsConfigured() {
		encrypted, encErr := s.enc.EncryptJSON(ctx, rawToken)
		if encErr != nil {
			s.log.Warn("failed to encrypt token for storage, token will not be retrievable later",
				slog.String("error", encErr.Error()))
		} else {
			tokenEncrypted = &encrypted
		}
	}

	// Create token record with project_id = NULL
	token := &ApiToken{
		ProjectID:      nil, // account-level: no project binding
		UserID:         &userID,
		Name:           name,
		TokenHash:      hashToken(rawToken),
		TokenPrefix:    getTokenPrefix(rawToken),
		TokenEncrypted: tokenEncrypted,
		Scopes:         scopes,
	}

	if err := s.repo.CreateAccountToken(ctx, token); err != nil {
		return nil, err
	}

	s.log.Info("created account API token",
		slog.String("name", name),
		slog.String("tokenPrefix", token.TokenPrefix),
		slog.String("userID", userID))

	return &CreateApiTokenResponseDTO{
		ApiTokenDTO: token.ToDTO(),
		Token:       rawToken,
	}, nil
}

// ListAccountTokens returns all account-level tokens for a user
func (s *Service) ListAccountTokens(ctx context.Context, userID string) (*ApiTokenListResponseDTO, error) {
	tokens, err := s.repo.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	dtos := make([]ApiTokenDTO, len(tokens))
	for i, t := range tokens {
		dtos[i] = t.ToDTO()
	}

	return &ApiTokenListResponseDTO{
		Tokens: dtos,
		Total:  len(dtos),
	}, nil
}

// GetAccountToken returns an account-level token by ID, owned by the user
func (s *Service) GetAccountToken(ctx context.Context, tokenID, userID string) (*GetApiTokenResponseDTO, error) {
	token, err := s.repo.GetByIDAndUser(ctx, tokenID, userID)
	if err != nil {
		return nil, err
	}
	if token == nil {
		return nil, nil
	}

	dto := &GetApiTokenResponseDTO{
		ApiTokenDTO: token.ToDTO(),
	}

	// Decrypt the token if available
	if token.TokenEncrypted != nil && *token.TokenEncrypted != "" && s.enc != nil && s.enc.IsConfigured() {
		decrypted, decErr := s.enc.Decrypt(ctx, *token.TokenEncrypted)
		if decErr != nil {
			s.log.Warn("failed to decrypt stored token",
				slog.String("tokenID", tokenID),
				slog.String("error", decErr.Error()))
		} else if val, ok := decrypted["value"]; ok {
			if tokenStr, ok := val.(string); ok {
				dto.Token = tokenStr
			}
		}
	}

	return dto, nil
}

// CreateEphemeral mints a short-lived project-scoped emt_* token for use inside
// sandbox containers. The token is bound to the given project and expires after
// ttl from now. If userID is non-empty the token acts on behalf of that user
// (enabling org-level queries such as listing all projects). The caller must
// call RevokeEphemeral when the sandbox is torn down to ensure early revocation.
//
// Returns (tokenID, rawToken, error). rawToken must be injected into the container
// as MEMORY_ACCOUNT_API_KEY; it is never stored in plaintext.
func (s *Service) CreateEphemeral(ctx context.Context, projectID, orgID, userID string, ttl time.Duration) (tokenID, rawToken string, err error) {
	// Generate a new emt_* token
	raw, genErr := generateToken()
	if genErr != nil {
		return "", "", apperror.ErrInternal.WithInternal(genErr)
	}

	// Ephemeral tokens are minted on behalf of the calling user. Storing the real
	// user ID allows the token to perform org-level queries (e.g. list all projects)
	// just like a regular API token. ProjectID is intentionally left nil so the
	// token is not restricted to a single project — the Python script may need to
	// operate across multiple projects. Security is enforced through scopes and the
	// user's own org membership.
	expiresAt := time.Now().Add(ttl)
	var uid *string
	if userID != "" {
		uid = &userID
	}
	token := &ApiToken{
		ProjectID:   nil, // org-level (not project-restricted) for cross-project operations
		UserID:      uid,
		Name:        fmt.Sprintf("ephemeral-sandbox-%d", time.Now().UnixMilli()),
		TokenHash:   hashToken(raw),
		TokenPrefix: getTokenPrefix(raw),
		Scopes: []string{
			// Coarse-grained (legacy) — kept for backwards compat with route middleware
			"data:read", "data:write", "schema:read", "schema:write",
			"agents:read", "agents:write", "projects:read", "projects:write",
			// Fine-grained MCP scopes — agent runners need full graph + schema + branches
			"graph:read", "graph:write",
			"schema:migrate",
			"branches:read", "branches:write",
			"search",
			"journal:read", "journal:write",
			"skills:read", "skills:write",
			"documents:read", "documents:write",
			"admin",
		},
		ExpiresAt: &expiresAt,
	}

	if err := s.repo.Create(ctx, token); err != nil {
		return "", "", fmt.Errorf("CreateEphemeral: %w", err)
	}

	s.log.Info("created ephemeral sandbox token",
		slog.String("token_id", token.ID),
		slog.String("project_id", projectID),
		slog.Time("expires_at", expiresAt),
	)

	return token.ID, raw, nil
}

// RevokeEphemeral immediately revokes an ephemeral token by ID.
// Non-fatal: logs a warning on failure but does not return an error so teardown
// cannot be blocked.
func (s *Service) RevokeEphemeral(ctx context.Context, tokenID string) {
	if tokenID == "" {
		return
	}
	if _, err := s.repo.RevokeByID(ctx, tokenID); err != nil {
		s.log.Warn("failed to revoke ephemeral sandbox token",
			slog.String("token_id", tokenID),
			slog.String("error", err.Error()),
		)
	} else {
		s.log.Info("revoked ephemeral sandbox token", slog.String("token_id", tokenID))
	}
}

// UpdateScopes updates the scopes of a non-revoked project token.
func (s *Service) UpdateScopes(ctx context.Context, tokenID, projectID, userID string, scopes []string) (*ApiTokenDTO, error) {
	if err := rejectReservedScopes(scopes, false, false, false, false); err != nil {
		return nil, err
	}
	// Validate scopes
	for _, scope := range scopes {
		valid := false
		for _, validScope := range ValidApiTokenScopes {
			if scope == validScope {
				valid = true
				break
			}
		}
		if !valid {
			return nil, apperror.ErrBadRequest.WithMessage("invalid scope: " + scope)
		}
	}

	// admin:all requires org admin or superadmin privileges
	if err := s.checkAdminAllGrant(ctx, userID, scopes); err != nil {
		return nil, err
	}

	// Viewers may only set read-only scopes
	if userID != "" && projectID != "" {
		role, err := s.repo.GetUserProjectRole(ctx, projectID, userID)
		if err != nil {
			return nil, err
		}
		if role == "project_viewer" {
			for _, scope := range scopes {
				if !viewerReadOnlyScopes[scope] {
					return nil, apperror.New(403, "viewer-write-scope-denied",
						"project_viewer may only request read-only scopes (data:read, schema:read, agents:read, projects:read)")
				}
			}
		}
	}

	updated, err := s.repo.UpdateScopes(ctx, tokenID, projectID, scopes)
	if err != nil {
		return nil, err
	}
	if !updated {
		return nil, apperror.ErrNotFound.WithMessage("Token not found or already revoked")
	}

	token, err := s.repo.GetByID(ctx, tokenID, projectID)
	if err != nil {
		return nil, err
	}
	if token == nil {
		return nil, apperror.ErrNotFound.WithMessage("Token not found")
	}

	dto := token.ToDTO()
	s.log.Info("updated API token scopes",
		slog.String("tokenID", tokenID),
		slog.String("projectID", projectID))
	return &dto, nil
}

// UpdateAccountTokenScopes updates the scopes of a non-revoked account-level token.
func (s *Service) UpdateAccountTokenScopes(ctx context.Context, tokenID, userID string, scopes []string) (*ApiTokenDTO, error) {
	if err := rejectReservedScopes(scopes, false, false, false, false); err != nil {
		return nil, err
	}
	// Validate scopes
	for _, scope := range scopes {
		valid := false
		for _, validScope := range ValidApiTokenScopes {
			if scope == validScope {
				valid = true
				break
			}
		}
		if !valid {
			return nil, apperror.ErrBadRequest.WithMessage("invalid scope: " + scope)
		}
	}

	// admin:all requires org admin or superadmin privileges
	if err := s.checkAdminAllGrant(ctx, userID, scopes); err != nil {
		return nil, err
	}

	updated, err := s.repo.UpdateScopesByUser(ctx, tokenID, userID, scopes)
	if err != nil {
		return nil, err
	}
	if !updated {
		return nil, apperror.ErrNotFound.WithMessage("Token not found or already revoked")
	}

	token, err := s.repo.GetByIDAndUser(ctx, tokenID, userID)
	if err != nil {
		return nil, err
	}
	if token == nil {
		return nil, apperror.ErrNotFound.WithMessage("Token not found")
	}

	dto := token.ToDTO()
	s.log.Info("updated account token scopes",
		slog.String("tokenID", tokenID),
		slog.String("userID", userID))
	return &dto, nil
}

// Regenerate atomically revokes a project token and creates a new one with the same name and scopes.
// Returns the new token (with plaintext value). If the insert fails, the revoke is rolled back.
func (s *Service) Regenerate(ctx context.Context, tokenID, projectID, userID string) (*CreateApiTokenResponseDTO, error) {
	return s.regenerate(ctx, tokenID, projectID, userID, nil)
}

// RegenerateWith is Regenerate plus an after-hook invoked inside the same
// transaction after the replacement token row is inserted. The MCP share
// rotation uses it to update the instance's token_id atomically with the new
// token, so a failure cannot leave an orphan active token.
func (s *Service) RegenerateWith(ctx context.Context, tokenID, projectID, userID string, after func(ctx context.Context, tx bun.Tx, newTokenID string) error) (*CreateApiTokenResponseDTO, error) {
	return s.regenerate(ctx, tokenID, projectID, userID, after)
}

func (s *Service) regenerate(ctx context.Context, tokenID, projectID, userID string, after func(context.Context, bun.Tx, string) error) (*CreateApiTokenResponseDTO, error) {
	// Fetch existing token to get name + scopes
	existing, err := s.repo.GetByID(ctx, tokenID, projectID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, apperror.ErrNotFound.WithMessage("Token not found")
	}
	if existing.RevokedAt != nil {
		return nil, apperror.New(409, "token_already_revoked", "Token is already revoked")
	}

	// Viewer scope restriction
	if userID != "" && projectID != "" {
		role, err := s.repo.GetUserProjectRole(ctx, projectID, userID)
		if err != nil {
			return nil, err
		}
		if role == "project_viewer" {
			for _, scope := range existing.Scopes {
				if !viewerReadOnlyScopes[scope] {
					return nil, apperror.New(403, "viewer-write-scope-denied",
						"project_viewer may only regenerate tokens with read-only scopes")
				}
			}
		}
	}

	// Generate new token material before the transaction
	rawToken, err := generateToken()
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}

	var tokenEncrypted *string
	if s.enc != nil && s.enc.IsConfigured() {
		encrypted, encErr := s.enc.EncryptJSON(ctx, rawToken)
		if encErr != nil {
			s.log.Warn("failed to encrypt regenerated token for storage",
				slog.String("error", encErr.Error()))
		} else {
			tokenEncrypted = &encrypted
		}
	}

	newToken := &ApiToken{
		ProjectID:      &projectID,
		UserID:         existing.UserID,
		Name:           existing.Name,
		TokenHash:      hashToken(rawToken),
		TokenPrefix:    getTokenPrefix(rawToken),
		TokenEncrypted: tokenEncrypted,
		Scopes:         existing.Scopes,
	}

	// Atomic: revoke old, insert new
	if err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().
			Model((*ApiToken)(nil)).
			Set("revoked_at = NOW()").
			Where("id = ?", tokenID).
			Where("project_id = ?", projectID).
			Where("revoked_at IS NULL").
			Exec(ctx)
		if err != nil {
			return apperror.ErrDatabase.WithInternal(err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return apperror.ErrDatabase.WithInternal(err)
		}
		if rows == 0 {
			return apperror.ErrNotFound.WithMessage("Token not found or already revoked")
		}

		if _, err := tx.NewInsert().Model(newToken).Exec(ctx); err != nil {
			return apperror.ErrDatabase.WithInternal(err)
		}
		if after != nil {
			if err := after(ctx, tx, newToken.ID); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	s.log.Info("regenerated project API token",
		slog.String("name", existing.Name),
		slog.String("oldID", tokenID),
		slog.String("newID", newToken.ID),
		slog.String("projectID", projectID))

	return &CreateApiTokenResponseDTO{
		ApiTokenDTO: newToken.ToDTO(),
		Token:       rawToken,
	}, nil
}

// RegenerateAccountToken atomically revokes an account-level token and creates a new one
// with the same name and scopes. Returns the new token (with plaintext value).
func (s *Service) RegenerateAccountToken(ctx context.Context, tokenID, userID string) (*CreateApiTokenResponseDTO, error) {
	// Fetch existing token
	existing, err := s.repo.GetByIDAndUser(ctx, tokenID, userID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		// Token not found as account-level — check if it's a project-scoped token owned by this user.
		// If so, delegate transparently so the caller doesn't need to know the project ID.
		projectToken, lookupErr := s.repo.GetByIDForUser(ctx, tokenID, userID)
		if lookupErr == nil && projectToken != nil && projectToken.ProjectID != nil {
			return s.Regenerate(ctx, tokenID, *projectToken.ProjectID, userID)
		}
		return nil, apperror.ErrNotFound.WithMessage("Token not found")
	}
	if existing.RevokedAt != nil {
		return nil, apperror.New(409, "token_already_revoked", "Token is already revoked")
	}

	// Generate new token material
	rawToken, err := generateToken()
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}

	var tokenEncrypted *string
	if s.enc != nil && s.enc.IsConfigured() {
		encrypted, encErr := s.enc.EncryptJSON(ctx, rawToken)
		if encErr != nil {
			s.log.Warn("failed to encrypt regenerated account token for storage",
				slog.String("error", encErr.Error()))
		} else {
			tokenEncrypted = &encrypted
		}
	}

	newToken := &ApiToken{
		ProjectID:      nil,
		UserID:         &userID,
		Name:           existing.Name,
		TokenHash:      hashToken(rawToken),
		TokenPrefix:    getTokenPrefix(rawToken),
		TokenEncrypted: tokenEncrypted,
		Scopes:         existing.Scopes,
	}

	// Atomic: revoke old, insert new
	if err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().
			Model((*ApiToken)(nil)).
			Set("revoked_at = NOW()").
			Where("id = ?", tokenID).
			Where("user_id = ?", userID).
			Where("project_id IS NULL").
			Where("revoked_at IS NULL").
			Exec(ctx)
		if err != nil {
			return apperror.ErrDatabase.WithInternal(err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return apperror.ErrDatabase.WithInternal(err)
		}
		if rows == 0 {
			return apperror.ErrNotFound.WithMessage("Token not found or already revoked")
		}

		if _, err := tx.NewInsert().Model(newToken).Exec(ctx); err != nil {
			return apperror.ErrDatabase.WithInternal(err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	s.log.Info("regenerated account API token",
		slog.String("name", existing.Name),
		slog.String("oldID", tokenID),
		slog.String("newID", newToken.ID),
		slog.String("userID", userID))

	return &CreateApiTokenResponseDTO{
		ApiTokenDTO: newToken.ToDTO(),
		Token:       rawToken,
	}, nil
}

// RevokeAccountToken revokes an account-level token owned by the user
func (s *Service) RevokeAccountToken(ctx context.Context, tokenID, userID string) error {
	// Check if token exists
	token, err := s.repo.GetByIDAndUser(ctx, tokenID, userID)
	if err != nil {
		return err
	}
	if token == nil {
		return apperror.ErrNotFound.WithMessage("Token not found")
	}

	// Check if already revoked
	if token.RevokedAt != nil {
		return apperror.New(409, "token_already_revoked", "Token is already revoked")
	}

	revoked, err := s.repo.RevokeByUser(ctx, tokenID, userID)
	if err != nil {
		return err
	}
	if !revoked {
		return apperror.ErrNotFound.WithMessage("Token not found")
	}

	s.log.Info("revoked account API token",
		slog.String("name", token.Name),
		slog.String("tokenPrefix", token.TokenPrefix),
		slog.String("userID", userID))

	return nil
}
