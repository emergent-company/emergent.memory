package apitoken

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/encryption"
	"github.com/emergent/emergent-core/pkg/logger"
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

	// Check for duplicate name
	existing, err := s.repo.FindByProjectAndName(ctx, projectID, name)
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
		ProjectID:      projectID,
		UserID:         userID,
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
