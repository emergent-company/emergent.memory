package sources

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/gdrive"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// driveClient is the narrow surface of the Drive transport used by connection
// testing. *gdrive.Client satisfies it.
type driveClient interface {
	TestConnection(ctx context.Context) error
}

// driveClientFactory constructs a Drive client from decrypted credentials.
// Injectable so tests can substitute a fake without any live Google call.
type driveClientFactory func(ctx context.Context, creds gdrive.Credentials) (driveClient, error)

// defaultDriveClientFactory builds a real Drive client from OAuth2 credentials.
func defaultDriveClientFactory(ctx context.Context, creds gdrive.Credentials) (driveClient, error) {
	return gdrive.New(ctx, creds)
}

// Service handles business logic for data sources.
type Service struct {
	repo     *Repository
	key      []byte // raw AES-256 key bytes (hex-decoded from config)
	newDrive driveClientFactory
	log      *slog.Logger
}

// NewService creates a new sources service. The credential-encryption key is
// derived from config.Config.LLMProvider.EncryptionKey — the same config-sourced
// key the provider domain uses — so provider credentials and Drive source
// credentials share one operator-managed secret (LLM_ENCRYPTION_KEY).
func NewService(repo *Repository, cfg *config.Config, log *slog.Logger) *Service {
	s := &Service{
		repo:     repo,
		newDrive: defaultDriveClientFactory,
		log:      log.With(logger.Scope("sources.svc")),
	}
	if cfg != nil && cfg.LLMProvider.IsEncryptionConfigured() {
		if key, err := hex.DecodeString(cfg.LLMProvider.EncryptionKey); err == nil {
			s.key = key
		} else {
			s.log.Error("invalid credential encryption key", logger.Error(err))
		}
	}
	return s
}

// ---- Provider catalog + schema ----

// ListProviders returns the available data-source providers. Today that is the
// single google_drive provider.
func (s *Service) ListProviders(ctx context.Context) []Provider {
	return []Provider{{
		Type:        ProviderTypeGoogleDrive,
		Name:        "Google Drive",
		Description: "Sync documents from a Google Drive folder into your knowledge base.",
		SourceType:  SourceTypeDrive,
		Available:   true,
	}}
}

// GetProviderSchema returns the config schema for a provider type.
func (s *Service) GetProviderSchema(ctx context.Context, providerType string) (*ProviderSchema, error) {
	if providerType != ProviderTypeGoogleDrive {
		return nil, apperror.ErrNotFound
	}
	return &ProviderSchema{
		Type: ProviderTypeGoogleDrive,
		Properties: map[string]any{
			"folder_id": map[string]any{
				"type":        "string",
				"description": "The Google Drive folder id to sync from.",
			},
			"shared_drive_id": map[string]any{
				"type":        "string",
				"description": "Optional shared-drive id when the folder lives in a shared drive.",
			},
			"mime_types": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional MIME-type allowlist; empty syncs all non-folder types.",
			},
			"recursive": map[string]any{
				"type":        "boolean",
				"description": "Recurse into subfolders when true.",
			},
			"client_id": map[string]any{
				"type":        "string",
				"description": "OAuth2 client id (write-only).",
			},
			"client_secret": map[string]any{
				"type":        "string",
				"description": "OAuth2 client secret (write-only).",
			},
			"refresh_token": map[string]any{
				"type":        "string",
				"description": "OAuth2 refresh token (write-only).",
			},
		},
		Required: []string{"folder_id"},
	}, nil
}

// ListSourceTypes returns document counts grouped by source type for a project.
func (s *Service) ListSourceTypes(ctx context.Context, projectID uuid.UUID) ([]SourceType, error) {
	return s.repo.ListSourceTypes(ctx, projectID)
}

// ---- Connection testing ----

// TestConfig tests an unsaved provider config, building a Drive client from the
// credentials carried in the request config. It never persists anything.
func (s *Service) TestConfig(ctx context.Context, req *TestConfigRequest) (*TestConnectionResponse, error) {
	if req.ProviderType != ProviderTypeGoogleDrive {
		return nil, apperror.NewValidation("providerType must be google_drive")
	}
	if err := validateConfig(req.Config); err != nil {
		return nil, err
	}
	creds := extractCredentials(req.Config)
	if creds.RefreshToken == "" {
		return nil, apperror.NewValidation("config.refresh_token is required")
	}
	return s.testCredentials(ctx, creds)
}

// TestConnection tests an existing source's connection by decrypting its stored
// credentials and performing a bounded Drive call.
func (s *Service) TestConnection(ctx context.Context, projectID, id uuid.UUID) (*TestConnectionResponse, error) {
	src, err := s.repo.GetByIDWithCredentials(ctx, id, projectID)
	if err != nil {
		return nil, err
	}
	creds, err := s.decryptCredentials(src.EncryptedCredentials)
	if err != nil {
		return &TestConnectionResponse{Success: false, Message: err.Error()}, nil
	}
	return s.testCredentials(ctx, creds)
}

func (s *Service) testCredentials(ctx context.Context, creds gdrive.Credentials) (*TestConnectionResponse, error) {
	client, err := s.newDrive(ctx, creds)
	if err != nil {
		return &TestConnectionResponse{Success: false, Message: "failed to initialize Drive client: " + err.Error()}, nil
	}
	if err := client.TestConnection(ctx); err != nil {
		return &TestConnectionResponse{Success: false, Message: err.Error()}, nil
	}
	return &TestConnectionResponse{Success: true, Message: "connection successful"}, nil
}

// ---- CRUD ----

// List returns all sources for a project.
func (s *Service) List(ctx context.Context, projectID uuid.UUID) ([]*Integration, error) {
	srcs, err := s.repo.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]*Integration, len(srcs))
	for i, src := range srcs {
		out[i] = toIntegration(src)
	}
	return out, nil
}

// Create validates the request, encrypts the supplied credentials, strips the
// credential keys from the persisted config, and stores the source.
func (s *Service) Create(ctx context.Context, projectID uuid.UUID, req *CreateIntegrationRequest) (*Integration, error) {
	if req.ProviderType != ProviderTypeGoogleDrive {
		return nil, apperror.NewValidation("providerType must be google_drive")
	}
	if err := validateConfig(req.Config); err != nil {
		return nil, err
	}
	creds := extractCredentials(req.Config)
	if creds.RefreshToken == "" {
		return nil, apperror.NewValidation("config.refresh_token is required")
	}
	blob, err := s.encryptCredentials(creds)
	if err != nil {
		return nil, err
	}

	now := s.now()
	src := &Source{
		ID:                   uuid.New(),
		ProjectID:            projectID,
		Provider:             ProviderTypeGoogleDrive,
		DisplayName:          req.Name,
		Config:               stripCredentialKeys(req.Config),
		EncryptedCredentials: blob,
		SyncPolicy:           "manual",
		SyncIntervalMinutes:  req.SyncIntervalMinutes,
		Cursor:               map[string]any{},
		Status:               "active",
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if req.SyncMode != nil && *req.SyncMode != "" {
		src.SyncPolicy = *req.SyncMode
	}

	created, err := s.repo.Create(ctx, src)
	if err != nil {
		return nil, err
	}
	return toIntegration(created), nil
}

// Get returns a single source scoped by project (cross-project → not-found).
func (s *Service) Get(ctx context.Context, projectID, id uuid.UUID) (*Integration, error) {
	src, err := s.repo.GetByID(ctx, id, projectID)
	if err != nil {
		return nil, err
	}
	return toIntegration(src), nil
}

// Update applies a partial update. When the request config carries credential
// keys, they are re-encrypted and replace the stored blob; otherwise the stored
// credentials are preserved.
func (s *Service) Update(ctx context.Context, projectID, id uuid.UUID, req *UpdateIntegrationRequest) (*Integration, error) {
	existing, err := s.repo.GetByIDWithCredentials(ctx, id, projectID)
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		existing.DisplayName = *req.Name
	}
	if req.SyncMode != nil && *req.SyncMode != "" {
		existing.SyncPolicy = *req.SyncMode
	}
	if req.SyncIntervalMinutes != nil {
		existing.SyncIntervalMinutes = req.SyncIntervalMinutes
	}
	if req.Enabled != nil {
		if *req.Enabled {
			existing.Status = SourceStatusActive
		} else {
			existing.Status = SourceStatusDisabled
		}
	}
	if req.Config != nil {
		if err := validateConfig(req.Config); err != nil {
			return nil, err
		}
		if hasCredentialKeys(req.Config) {
			creds := extractCredentials(req.Config)
			if creds.RefreshToken == "" {
				return nil, apperror.NewValidation("config.refresh_token is required")
			}
			blob, err := s.encryptCredentials(creds)
			if err != nil {
				return nil, err
			}
			existing.EncryptedCredentials = blob
		}
		existing.Config = stripCredentialKeys(req.Config)
	}

	updated, err := s.repo.Update(ctx, existing)
	if err != nil {
		return nil, err
	}
	return toIntegration(updated), nil
}

// Delete removes a source scoped by project.
func (s *Service) Delete(ctx context.Context, projectID, id uuid.UUID) error {
	return s.repo.Delete(ctx, id, projectID)
}

// ---- Credential + config helpers ----

// encryptCredentials encrypts the credential struct into a single self-contained
// blob using the config-sourced key.
func (s *Service) encryptCredentials(creds gdrive.Credentials) ([]byte, error) {
	if len(s.key) == 0 {
		return nil, apperror.NewInternal("credential encryption is not configured (LLM_ENCRYPTION_KEY missing)", nil)
	}
	blob, err := gdrive.EncryptCredentials(s.key, creds)
	if err != nil {
		return nil, apperror.NewInternal("encrypt credentials", err)
	}
	return blob, nil
}

// decryptCredentials reverses encryptCredentials.
func (s *Service) decryptCredentials(blob []byte) (gdrive.Credentials, error) {
	if len(s.key) == 0 {
		return gdrive.Credentials{}, fmt.Errorf("credential encryption is not configured (LLM_ENCRYPTION_KEY missing)")
	}
	creds, err := gdrive.DecryptCredentials(s.key, blob)
	if err != nil {
		return gdrive.Credentials{}, fmt.Errorf("decrypt credentials: %w", err)
	}
	return creds, nil
}

func (s *Service) now() time.Time {
	return time.Now().UTC()
}

// credentialKeyNames are the config keys that carry OAuth2 credential material.
var credentialKeyNames = []string{"client_id", "client_secret", "refresh_token"}

// extractCredentials reads the OAuth2 credential fields out of a config map.
func extractCredentials(cfg map[string]any) gdrive.Credentials {
	var creds gdrive.Credentials
	creds.ClientID, _ = cfg["client_id"].(string)
	creds.ClientSecret, _ = cfg["client_secret"].(string)
	creds.RefreshToken, _ = cfg["refresh_token"].(string)
	return creds
}

// hasCredentialKeys reports whether any credential key is present in the config.
func hasCredentialKeys(cfg map[string]any) bool {
	for _, k := range credentialKeyNames {
		if _, ok := cfg[k]; ok {
			return true
		}
	}
	return false
}

// stripCredentialKeys returns a copy of cfg with the credential keys removed so
// no plaintext secret is ever stored in the config jsonb column.
func stripCredentialKeys(cfg map[string]any) map[string]any {
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		skip := false
		for _, ck := range credentialKeyNames {
			if k == ck {
				skip = true
				break
			}
		}
		if !skip {
			out[k] = v
		}
	}
	return out
}

// validateConfig enforces the google_drive config contract: folder_id required,
// mime_types (when present) an array of non-empty strings.
func validateConfig(cfg map[string]any) error {
	if folderID, _ := cfg["folder_id"].(string); strings.TrimSpace(folderID) == "" {
		return apperror.NewValidation("config.folder_id is required")
	}
	if mimeTypes, ok := cfg["mime_types"].([]any); ok {
		for _, m := range mimeTypes {
			if s, ok := m.(string); !ok || strings.TrimSpace(s) == "" {
				return apperror.NewValidation("config.mime_types must be an array of non-empty strings")
			}
		}
	} else if mimeTypes, ok := cfg["mime_types"].([]string); ok {
		for _, s := range mimeTypes {
			if strings.TrimSpace(s) == "" {
				return apperror.NewValidation("config.mime_types must be an array of non-empty strings")
			}
		}
	}
	return nil
}

// toIntegration maps a persisted source to the SDK integration shape. It never
// includes credential material.
func toIntegration(src *Source) *Integration {
	return &Integration{
		ID:                  src.ID.String(),
		ProjectID:           src.ProjectID.String(),
		Name:                src.DisplayName,
		ProviderType:        src.Provider,
		SourceType:          SourceTypeDrive,
		SyncMode:            src.SyncPolicy,
		SyncIntervalMinutes: src.SyncIntervalMinutes,
		LastSyncedAt:        src.LastSyncedAt,
		Status:              src.Status,
		ErrorMessage:        src.LastError,
		ErrorCount:          src.ErrorCount,
		CreatedAt:           src.CreatedAt,
		UpdatedAt:           src.UpdatedAt,
	}
}
