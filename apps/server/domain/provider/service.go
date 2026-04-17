package provider

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// ResolvedCredential holds the decrypted credential material and metadata
// needed to instantiate an LLM client for a specific request context.
type ResolvedCredential struct {
	// Provider type (google or google-vertex)
	Provider ProviderType

	// Source describes where the credential was resolved from
	Source CredentialSource

	// APIKey is set for google (decrypted)
	APIKey string

	// Vertex AI fields (set for google-vertex)
	ServiceAccountJSON string
	GCPProject         string
	Location           string

	// OpenAI-compatible fields (set for openai-compatible)
	IsOpenAICompatible bool
	BaseURL            string

	// Selected models (may come from org selection, project override, or env config)
	EmbeddingModel  string
	GenerativeModel string
}

// CredentialSource describes where a resolved credential originated.
type CredentialSource string

const (
	SourceProject      CredentialSource = "project"
	SourceOrganization CredentialSource = "organization"
	SourceEnvironment  CredentialSource = "environment"
)

// Static fallback model names used when SyncModels fails or when no model was
// explicitly selected by the caller.
const (
	staticFallbackGenerativeModel = "gemini-3.1-flash-lite-preview"
	staticFallbackEmbeddingModel  = "gemini-embedding-2-preview"
)

// CredentialService resolves LLM credentials following the hierarchy:
// Project config → Organization config → hard error (no env-var fallback).
type CredentialService struct {
	repo      *Repository
	registry  *Registry
	catalog   *ModelCatalogService
	encryptor *crypto.Encryptor // nil if encryption key not configured
	cfg       *config.Config
	log       *slog.Logger
}

// NewCredentialService creates a new CredentialService.
func NewCredentialService(
	repo *Repository,
	registry *Registry,
	catalog *ModelCatalogService,
	cfg *config.Config,
	log *slog.Logger,
) *CredentialService {
	s := &CredentialService{
		repo:     repo,
		registry: registry,
		catalog:  catalog,
		cfg:      cfg,
		log:      log.With(logger.Scope("provider.credential")),
	}

	if cfg.LLMProvider.IsEncryptionConfigured() {
		enc, err := crypto.NewEncryptor(cfg.LLMProvider.EncryptionKey)
		if err != nil {
			s.log.Error("failed to initialize credential encryptor", logger.Error(err))
		} else {
			s.encryptor = enc
		}
	}

	return s
}

// Resolve determines the effective credentials for the given provider by
// evaluating the context (project ID, org ID) against the resolution hierarchy.
//
// Resolution order:
//  1. If projectID present → look up project config; if found, decrypt+return.
//  2. Derive orgID (from context or DB lookup); if org config found, decrypt+return.
//  3. If project or org context present but no config found → hard error.
//  4. If neither present → return nil, nil (env-var callers handle this).
func (s *CredentialService) Resolve(ctx context.Context, provider ProviderType) (*ResolvedCredential, error) {
	if !s.registry.IsSupported(provider) {
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}

	projectID := auth.ProjectIDFromContext(ctx)
	orgID := auth.OrgIDFromContext(ctx)

	// --- 1. Project-level config ---
	if projectID != "" {
		cfg, err := s.repo.GetProjectProviderConfig(ctx, projectID, provider)
		if err != nil {
			return nil, fmt.Errorf("failed to get project provider config: %w", err)
		}
		if cfg != nil {
			return s.decryptProjectConfig(cfg)
		}

		// Project present but no project config — resolve orgID and try org.
		if orgID == "" {
			resolvedOrgID, err := s.repo.GetOrgIDForProject(ctx, projectID)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve org for project %s: %w", projectID, err)
			}
			orgID = resolvedOrgID
		}
	}

	// --- 2. Org-level config ---
	if orgID != "" {
		cfg, err := s.repo.GetOrgProviderConfig(ctx, orgID, provider)
		if err != nil {
			return nil, fmt.Errorf("failed to get org provider config: %w", err)
		}
		if cfg != nil {
			return s.decryptOrgConfig(cfg)
		}

		// Org present but no config → hard error (no silent env-var fallback).
		return nil, fmt.Errorf("no %s provider config found for organization %s — run `emergent provider configure` to set credentials", provider, orgID)
	}

	// --- 3. Neither project nor org in context → caller handles env-var fallback ---
	return nil, nil
}

// decryptOrgConfig decrypts an org-level provider config.
func (s *CredentialService) decryptOrgConfig(cfg *OrgProviderConfig) (*ResolvedCredential, error) {
	if s.encryptor == nil {
		return nil, fmt.Errorf("credential encryption not configured (LLM_ENCRYPTION_KEY missing)")
	}

	plaintext, err := s.encryptor.Decrypt(cfg.EncryptedCredential, cfg.EncryptionNonce)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt org credential: %w", err)
	}

	resolved := &ResolvedCredential{
		Provider:        cfg.Provider,
		Source:          SourceOrganization,
		GCPProject:      cfg.GCPProject,
		Location:        cfg.Location,
		BaseURL:         cfg.BaseURL,
		GenerativeModel: cfg.GenerativeModel,
		EmbeddingModel:  cfg.EmbeddingModel,
	}
	switch cfg.Provider {
	case ProviderGoogleAI:
		resolved.APIKey = string(plaintext)
	case ProviderVertexAI:
		resolved.ServiceAccountJSON = string(plaintext)
	case ProviderOpenAICompatible:
		resolved.IsOpenAICompatible = true
		resolved.BaseURL = cfg.BaseURL
		resolved.APIKey = string(plaintext) // plaintext is the api_key (may be empty)
	}
	return resolved, nil
}

// decryptProjectConfig decrypts a project-level provider config.
func (s *CredentialService) decryptProjectConfig(cfg *ProjectProviderConfig) (*ResolvedCredential, error) {
	if s.encryptor == nil {
		return nil, fmt.Errorf("credential encryption not configured (LLM_ENCRYPTION_KEY missing)")
	}

	plaintext, err := s.encryptor.Decrypt(cfg.EncryptedCredential, cfg.EncryptionNonce)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt project credential: %w", err)
	}

	resolved := &ResolvedCredential{
		Provider:        cfg.Provider,
		Source:          SourceProject,
		GCPProject:      cfg.GCPProject,
		Location:        cfg.Location,
		BaseURL:         cfg.BaseURL,
		GenerativeModel: cfg.GenerativeModel,
		EmbeddingModel:  cfg.EmbeddingModel,
	}
	switch cfg.Provider {
	case ProviderGoogleAI:
		resolved.APIKey = string(plaintext)
	case ProviderVertexAI:
		resolved.ServiceAccountJSON = string(plaintext)
	case ProviderOpenAICompatible:
		resolved.IsOpenAICompatible = true
		resolved.BaseURL = cfg.BaseURL
		resolved.APIKey = string(plaintext)
	}
	return resolved, nil
}

// EncryptCredential encrypts a plaintext credential for storage.
func (s *CredentialService) EncryptCredential(plaintext []byte) (ciphertext, nonce []byte, err error) {
	if s.encryptor == nil {
		return nil, nil, fmt.Errorf("credential encryption not configured (LLM_ENCRYPTION_KEY missing)")
	}
	return s.encryptor.Encrypt(plaintext)
}

func (s *CredentialService) ResolveFor(ctx context.Context, provider string) (*ResolvedCredential, error) {
	return s.Resolve(ctx, ProviderType(provider))
}

// ResolveAny attempts to resolve the best available credential for the request
// context without requiring the caller to specify a provider type.
//
// Tries providers in order: Vertex AI first, then Google AI.
// Returns nil, nil when no credentials are available (neither project nor org
// context present). Returns an error when credentials were found but could not
// be resolved (e.g. decryption failure, DB error).
//
// This method satisfies the adk.CredentialResolver interface.
func (s *CredentialService) ResolveAny(ctx context.Context) (*ResolvedCredential, error) {
	var lastErr error
	for _, provider := range []ProviderType{ProviderOpenAICompatible, ProviderVertexAI, ProviderGoogleAI} {
		cred, err := s.Resolve(ctx, provider)
		if err != nil {
			s.log.Debug("provider resolution failed, trying next",
				slog.String("provider", string(provider)),
				slog.String("error", err.Error()),
			)
			lastErr = err
			continue
		}
		if cred != nil {
			return cred, nil
		}
	}
	// If every provider returned an error (credential found but resolution
	// failed — e.g. decryption error, DB error), propagate the last error
	// so the caller can surface a meaningful message instead of "no provider".
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, nil
}

// UpsertOrgConfig saves provider credentials+models for an organization.
//
// Flow:
//  1. Assert caller owns org.
//  2. Encrypt credential.
//  3. Live-test the credential (5 s timeout).
//  4. SyncModels (15 s timeout; non-fatal — logs warning on failure).
//  5. Auto-select top generative/embedding model if not in req.
//  6. Upsert row.
func (s *CredentialService) UpsertOrgConfig(ctx context.Context, orgID string, provider ProviderType, req UpsertProviderConfigRequest) (*ProviderConfigResponse, error) {
	if err := assertCallerOwnsOrg(ctx, orgID); err != nil {
		return nil, err
	}

	plaintext, err := s.extractPlaintext(provider, req)
	if err != nil {
		return nil, err
	}

	ciphertext, nonce, err := s.EncryptCredential(plaintext)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt credential: %w", err)
	}

	// Build a temporary resolved cred for live-test and sync.
	tempCred := s.buildTempResolvedCred(provider, req)

	// Sync model catalog (15s timeout). Non-fatal: a sync failure (e.g. missing
	// DB column, API timeout) should not block credential storage. The catalog
	// will be populated on the next startup sync or manual retry.
	catalogSynced := true
	syncCtx, syncCancel := context.WithTimeout(ctx, 15*time.Second)
	defer syncCancel()
	if err := s.catalog.SyncModels(syncCtx, provider, tempCred); err != nil {
		s.log.Warn("model catalog sync failed during configure; continuing without catalog update",
			logger.Error(err), slog.String("provider", string(provider)))
		catalogSynced = false
	}

	// Live test using a model from the freshly synced catalog.
	// OpenAI-compatible endpoints (especially large local models) may be slow
	// to produce a first token, so we allow a longer timeout for them.
	testTimeout := 15 * time.Second
	if provider == ProviderOpenAICompatible {
		testTimeout = 60 * time.Second
	}
	testCtx, testCancel := context.WithTimeout(ctx, testTimeout)
	defer testCancel()
	if _, _, err := s.catalog.TestGenerate(testCtx, provider, tempCred); err != nil {
		return nil, fmt.Errorf("generative model test failed: %w", err)
	}
	if _, err := s.catalog.TestEmbed(testCtx, provider, tempCred); err != nil {
		return nil, fmt.Errorf("embedding model test failed: %w", err)
	}

	// Auto-select models if not explicitly provided.
	generativeModel := req.GenerativeModel
	embeddingModel := req.EmbeddingModel
	if generativeModel == "" || embeddingModel == "" {
		genType := ModelTypeGenerative
		embType := ModelTypeEmbedding
		genModels, _ := s.repo.ListSupportedModels(ctx, provider, &genType)
		embModels, _ := s.repo.ListSupportedModels(ctx, provider, &embType)
		if generativeModel == "" {
			generativeModel = s.pickBestGenerativeModel(genModels)
		}
		if embeddingModel == "" {
			embeddingModel = s.pickBestEmbeddingModel(embModels)
		}
	}

	// Validate explicitly-provided model names against the synced catalog
	// (only meaningful when catalog sync succeeded).
	if catalogSynced {
		if req.GenerativeModel != "" {
			if err := s.validateModelInCatalog(ctx, provider, req.GenerativeModel, ModelTypeGenerative); err != nil {
				return nil, err
			}
		}
		if req.EmbeddingModel != "" {
			if err := s.validateModelInCatalog(ctx, provider, req.EmbeddingModel, ModelTypeEmbedding); err != nil {
				return nil, err
			}
		}
	}

	cfg := &OrgProviderConfig{
		OrgID:               orgID,
		Provider:            provider,
		EncryptedCredential: ciphertext,
		EncryptionNonce:     nonce,
		GCPProject:          req.GCPProject,
		Location:            req.Location,
		BaseURL:             req.BaseURL,
		GenerativeModel:     generativeModel,
		EmbeddingModel:      embeddingModel,
	}

	if err := s.repo.UpsertOrgProviderConfig(ctx, cfg); err != nil {
		return nil, err
	}

	return &ProviderConfigResponse{
		ID:              cfg.ID,
		Provider:        cfg.Provider,
		GCPProject:      cfg.GCPProject,
		Location:        cfg.Location,
		BaseURL:         cfg.BaseURL,
		GenerativeModel: cfg.GenerativeModel,
		EmbeddingModel:  cfg.EmbeddingModel,
		CreatedAt:       cfg.CreatedAt,
		UpdatedAt:       cfg.UpdatedAt,
	}, nil
}

// GetOrgConfig retrieves the public-safe metadata for an org's provider config.
func (s *CredentialService) GetOrgConfig(ctx context.Context, orgID string, provider ProviderType) (*ProviderConfigResponse, error) {
	if err := assertCallerOwnsOrg(ctx, orgID); err != nil {
		return nil, err
	}
	cfg, err := s.repo.GetOrgProviderConfig(ctx, orgID, provider)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, nil
	}
	return &ProviderConfigResponse{
		ID:              cfg.ID,
		Provider:        cfg.Provider,
		GCPProject:      cfg.GCPProject,
		Location:        cfg.Location,
		BaseURL:         cfg.BaseURL,
		GenerativeModel: cfg.GenerativeModel,
		EmbeddingModel:  cfg.EmbeddingModel,
		CreatedAt:       cfg.CreatedAt,
		UpdatedAt:       cfg.UpdatedAt,
	}, nil
}

// DeleteOrgConfig removes an organization's provider config.
func (s *CredentialService) DeleteOrgConfig(ctx context.Context, orgID string, provider ProviderType) error {
	if err := assertCallerOwnsOrg(ctx, orgID); err != nil {
		return err
	}
	return s.repo.DeleteOrgProviderConfig(ctx, orgID, provider)
}

// ListOrgConfigs returns all org provider configs (metadata only).
func (s *CredentialService) ListOrgConfigs(ctx context.Context, orgID string) ([]ProviderConfigResponse, error) {
	if err := assertCallerOwnsOrg(ctx, orgID); err != nil {
		return nil, err
	}
	cfgs, err := s.repo.ListOrgProviderConfigs(ctx, orgID)
	if err != nil {
		return nil, err
	}
	resp := make([]ProviderConfigResponse, len(cfgs))
	for i, cfg := range cfgs {
		resp[i] = ProviderConfigResponse{
			ID:              cfg.ID,
			Provider:        cfg.Provider,
			GCPProject:      cfg.GCPProject,
			Location:        cfg.Location,
			BaseURL:         cfg.BaseURL,
			GenerativeModel: cfg.GenerativeModel,
			EmbeddingModel:  cfg.EmbeddingModel,
			CreatedAt:       cfg.CreatedAt,
			UpdatedAt:       cfg.UpdatedAt,
		}
	}
	return resp, nil
}

// ListProjectConfigsByOrg returns all project-level provider configs for
// projects belonging to the given organization (metadata only).
func (s *CredentialService) ListProjectConfigsByOrg(ctx context.Context, orgID string) ([]ProjectProviderConfigResponse, error) {
	if err := assertCallerOwnsOrg(ctx, orgID); err != nil {
		return nil, err
	}
	cfgs, err := s.repo.ListProjectProviderConfigsByOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	resp := make([]ProjectProviderConfigResponse, len(cfgs))
	for i, cfg := range cfgs {
		resp[i] = ProjectProviderConfigResponse{
			ID:              cfg.ID,
			ProjectID:       cfg.ProjectID,
			Provider:        cfg.Provider,
			GCPProject:      cfg.GCPProject,
			Location:        cfg.Location,
			BaseURL:         cfg.BaseURL,
			GenerativeModel: cfg.GenerativeModel,
			EmbeddingModel:  cfg.EmbeddingModel,
			CreatedAt:       cfg.CreatedAt,
			UpdatedAt:       cfg.UpdatedAt,
		}
	}
	return resp, nil
}

// UpsertProjectConfig saves provider credentials+models for a project.
// Same flow as UpsertOrgConfig (test + sync + auto-select + upsert).
func (s *CredentialService) UpsertProjectConfig(ctx context.Context, projectID string, provider ProviderType, req UpsertProviderConfigRequest) (*ProviderConfigResponse, error) {
	if err := s.assertCallerOwnsProject(ctx, projectID); err != nil {
		return nil, err
	}

	plaintext, err := s.extractPlaintext(provider, req)
	if err != nil {
		return nil, err
	}

	ciphertext, nonce, err := s.EncryptCredential(plaintext)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt credential: %w", err)
	}

	tempCred := s.buildTempResolvedCred(provider, req)

	// Sync model catalog (15s timeout). Non-fatal — same as UpsertOrgConfig.
	catalogSynced := true
	syncCtx, syncCancel := context.WithTimeout(ctx, 15*time.Second)
	defer syncCancel()
	if err := s.catalog.SyncModels(syncCtx, provider, tempCred); err != nil {
		s.log.Warn("model catalog sync failed during project configure; continuing without catalog update",
			logger.Error(err), slog.String("provider", string(provider)))
		catalogSynced = false
	}

	// Live test using a model from the freshly synced catalog.
	// OpenAI-compatible endpoints (especially large local models) may be slow
	// to produce a first token, so we allow a longer timeout for them.
	testTimeout2 := 15 * time.Second
	if provider == ProviderOpenAICompatible {
		testTimeout2 = 60 * time.Second
	}
	testCtx, testCancel := context.WithTimeout(ctx, testTimeout2)
	defer testCancel()
	if _, _, err := s.catalog.TestGenerate(testCtx, provider, tempCred); err != nil {
		return nil, fmt.Errorf("generative model test failed: %w", err)
	}
	if _, err := s.catalog.TestEmbed(testCtx, provider, tempCred); err != nil {
		return nil, fmt.Errorf("embedding model test failed: %w", err)
	}

	generativeModel := req.GenerativeModel
	embeddingModel := req.EmbeddingModel
	if generativeModel == "" || embeddingModel == "" {
		genType := ModelTypeGenerative
		embType := ModelTypeEmbedding
		genModels, _ := s.repo.ListSupportedModels(ctx, provider, &genType)
		embModels, _ := s.repo.ListSupportedModels(ctx, provider, &embType)
		if generativeModel == "" {
			generativeModel = s.pickBestGenerativeModel(genModels)
		}
		if embeddingModel == "" {
			embeddingModel = s.pickBestEmbeddingModel(embModels)
		}
	}

	// Validate explicitly-provided model names against the synced catalog
	// (only meaningful when catalog sync succeeded).
	if catalogSynced {
		if req.GenerativeModel != "" {
			if err := s.validateModelInCatalog(ctx, provider, req.GenerativeModel, ModelTypeGenerative); err != nil {
				return nil, err
			}
		}
		if req.EmbeddingModel != "" {
			if err := s.validateModelInCatalog(ctx, provider, req.EmbeddingModel, ModelTypeEmbedding); err != nil {
				return nil, err
			}
		}
	}

	cfg := &ProjectProviderConfig{
		ProjectID:           projectID,
		Provider:            provider,
		EncryptedCredential: ciphertext,
		EncryptionNonce:     nonce,
		GCPProject:          req.GCPProject,
		Location:            req.Location,
		BaseURL:             req.BaseURL,
		GenerativeModel:     generativeModel,
		EmbeddingModel:      embeddingModel,
	}

	if err := s.repo.UpsertProjectProviderConfig(ctx, cfg); err != nil {
		return nil, err
	}

	return &ProviderConfigResponse{
		ID:              cfg.ID,
		Provider:        cfg.Provider,
		GCPProject:      cfg.GCPProject,
		Location:        cfg.Location,
		BaseURL:         cfg.BaseURL,
		GenerativeModel: cfg.GenerativeModel,
		EmbeddingModel:  cfg.EmbeddingModel,
		CreatedAt:       cfg.CreatedAt,
		UpdatedAt:       cfg.UpdatedAt,
	}, nil
}

// GetProjectConfig retrieves the public-safe metadata for a project's provider config.
func (s *CredentialService) GetProjectConfig(ctx context.Context, projectID string, provider ProviderType) (*ProviderConfigResponse, error) {
	if err := s.assertCallerOwnsProject(ctx, projectID); err != nil {
		return nil, err
	}
	cfg, err := s.repo.GetProjectProviderConfig(ctx, projectID, provider)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, nil
	}
	return &ProviderConfigResponse{
		ID:              cfg.ID,
		Provider:        cfg.Provider,
		GCPProject:      cfg.GCPProject,
		Location:        cfg.Location,
		BaseURL:         cfg.BaseURL,
		GenerativeModel: cfg.GenerativeModel,
		EmbeddingModel:  cfg.EmbeddingModel,
		CreatedAt:       cfg.CreatedAt,
		UpdatedAt:       cfg.UpdatedAt,
	}, nil
}

// DeleteProjectConfig removes a project's provider config.
func (s *CredentialService) DeleteProjectConfig(ctx context.Context, projectID string, provider ProviderType) error {
	if err := s.assertCallerOwnsProject(ctx, projectID); err != nil {
		return err
	}
	return s.repo.DeleteProjectProviderConfig(ctx, projectID, provider)
}

// --- helpers ---

// extractPlaintext returns the credential bytes to encrypt from the request.
func (s *CredentialService) extractPlaintext(provider ProviderType, req UpsertProviderConfigRequest) ([]byte, error) {
	switch provider {
	case ProviderGoogleAI:
		if req.APIKey == "" {
			return nil, apperror.NewBadRequest("apiKey is required for google")
		}
		return []byte(req.APIKey), nil
	case ProviderVertexAI:
		if req.ServiceAccountJSON == "" {
			return nil, apperror.NewBadRequest("serviceAccountJson is required for google-vertex")
		}
		if req.GCPProject == "" {
			return nil, apperror.NewBadRequest("gcpProject is required for google-vertex")
		}
		if req.Location == "" {
			return nil, apperror.NewBadRequest("location is required for google-vertex")
		}
		return []byte(req.ServiceAccountJSON), nil
	case ProviderOpenAICompatible:
		if req.BaseURL == "" {
			return nil, apperror.NewBadRequest("baseUrl is required for openai-compatible")
		}
		// APIKey is optional (keyless local servers); encrypt empty string if not provided
		return []byte(req.APIKey), nil
	default:
		return nil, apperror.NewBadRequest(fmt.Sprintf("unsupported provider: %s", provider))
	}
}

// buildTempResolvedCred constructs a plaintext ResolvedCredential for testing/syncing.
func (s *CredentialService) buildTempResolvedCred(provider ProviderType, req UpsertProviderConfigRequest) *ResolvedCredential {
	cred := &ResolvedCredential{
		Provider:        provider,
		GCPProject:      req.GCPProject,
		Location:        req.Location,
		GenerativeModel: req.GenerativeModel,
		EmbeddingModel:  req.EmbeddingModel,
	}
	switch provider {
	case ProviderGoogleAI:
		cred.APIKey = req.APIKey
	case ProviderVertexAI:
		cred.ServiceAccountJSON = req.ServiceAccountJSON
	case ProviderOpenAICompatible:
		cred.IsOpenAICompatible = true
		cred.BaseURL = req.BaseURL
		cred.APIKey = req.APIKey
	}
	return cred
}

// pickBestGenerativeModel selects the preferred generative model from the
// catalog, falling back to the static default if none is available.
// gemini-3.1-flash-lite-preview is the preferred default; gemini-2.5-flash
// is the secondary fallback for environments that haven't synced the new model yet.
func (s *CredentialService) pickBestGenerativeModel(models []ProviderSupportedModel) string {
	for _, m := range models {
		if m.ModelName == "gemini-3.1-flash-lite-preview" {
			return m.ModelName
		}
	}
	for _, m := range models {
		if m.ModelName == "gemini-2.5-flash" {
			return m.ModelName
		}
	}
	for _, m := range models {
		if len(m.ModelName) > 0 {
			return m.ModelName
		}
	}
	return staticFallbackGenerativeModel
}

// pickBestEmbeddingModel selects the preferred embedding model from the
// catalog, falling back to the static default if none is available.
func (s *CredentialService) pickBestEmbeddingModel(models []ProviderSupportedModel) string {
	for _, m := range models {
		if m.ModelName == "gemini-embedding-2-preview" {
			return m.ModelName
		}
	}
	for _, m := range models {
		if m.ModelName == "gemini-embedding-001" {
			return m.ModelName
		}
	}
	for _, m := range models {
		if len(m.ModelName) > 0 {
			return m.ModelName
		}
	}
	return staticFallbackEmbeddingModel
}

// validateModelInCatalog checks that a model name exists in the synced catalog.
// Returns an error if the model is not found, listing available models to help the caller.
func (s *CredentialService) validateModelInCatalog(ctx context.Context, provider ProviderType, modelName string, modelType ModelType) error {
	models, err := s.repo.ListSupportedModels(ctx, provider, &modelType)
	if err != nil {
		return fmt.Errorf("failed to look up %s models: %w", modelType, err)
	}
	for _, m := range models {
		if m.ModelName == modelName {
			return nil
		}
	}
	names := make([]string, len(models))
	for i, m := range models {
		names[i] = m.ModelName
	}
	return apperror.NewBadRequest(fmt.Sprintf("model %q not found in %s catalog for provider %s; available: %v", modelName, modelType, provider, names))
}
