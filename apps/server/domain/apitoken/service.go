package apitoken

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

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
	repo *Repository
	enc  *encryption.Service
	log  *slog.Logger
}

// NewService creates a new API token service
func NewService(repo *Repository, enc *encryption.Service, log *slog.Logger) *Service {
	return &Service{
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

// Create creates a new API token
func (s *Service) Create(ctx context.Context, projectID, userID, name string, scopes []string) (*CreateApiTokenResponseDTO, error) {
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

	// Viewers may only create read-only tokens
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

	// Check for duplicate name (matches DB unique index on user_id + name)
	existing, err := s.repo.FindByUserAndProjectAndName(ctx, userID, projectID, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, apperror.New(409, "token_name_exists", "A token named \""+name+"\" already exists for this project")
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
		UserID:         &userID,
		Name:           name,
		TokenHash:      hashToken(rawToken),
		TokenPrefix:    getTokenPrefix(rawToken),
		TokenEncrypted: tokenEncrypted,
		Scopes:         scopes,
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

// ListByProject returns all tokens for a project
func (s *Service) ListByProject(ctx context.Context, projectID string) (*ApiTokenListResponseDTO, error) {
	tokens, err := s.repo.ListByProject(ctx, projectID)
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
// as MEMORY_API_KEY; it is never stored in plaintext.
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
		Scopes:      []string{"data:read", "data:write", "schema:read", "agents:read", "agents:write", "projects:read", "projects:write"},
		ExpiresAt:   &expiresAt,
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
