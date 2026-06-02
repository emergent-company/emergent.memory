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
	// Provider type
	Provider ProviderType

	// Source describes where the credential was resolved from
	Source CredentialSource

	// APIKey is set for google, openai, deepseek (decrypted)
	APIKey string

	// Vertex AI fields (set for google-vertex)
	ServiceAccountJSON string
	GCPProject         string
	Location           string

	// BaseURL is the HTTP endpoint for OpenAI-protocol providers (openai, deepseek)
	BaseURL string

	// Selected models (may come from org selection, project override, or env config)
	EmbeddingModel  string
	GenerativeModel string
}

// CredentialSource describes where a resolved credential originated.
type CredentialSource string

const (
	SourceProject     CredentialSource = "project"
	SourceEnvironment CredentialSource = "environment"
)

// CredentialService resolves LLM credentials from project-level config.
// All configuration is per-project — there is no org-level fallback.
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
// looking up the project-level config only. No org fallback.
//
// Returns nil, nil when no project context is present (env-var callers).
// Returns an error when the project has no config for this provider.
func (s *CredentialService) Resolve(ctx context.Context, provider ProviderType) (*ResolvedCredential, error) {
	if !s.registry.IsSupported(provider) {
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}

	projectID := auth.ProjectIDFromContext(ctx)
	if projectID == "" {
		return nil, nil // no project context — env-var callers handle this
	}

	cfg, err := s.repo.GetProjectProviderConfig(ctx, projectID, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to get project provider config: %w", err)
	}
	if cfg != nil {
		return s.decryptProjectConfig(cfg)
	}

	return nil, fmt.Errorf("no %s provider config found for project %s — run 'memory provider configure-project %s' to set credentials", provider, projectID, provider)
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
	case ProviderOpenAI:
		resolved.BaseURL = cfg.BaseURL
		if resolved.BaseURL == "" {
			resolved.BaseURL = "https://api.openai.com/v1"
		}
		resolved.APIKey = string(plaintext)
	case ProviderDeepSeek:
		resolved.BaseURL = "https://api.deepseek.com/v1"
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
// context. Tries project-level configs in order: DeepSeek → OpenAI → VertexAI → GoogleAI.
// Returns nil, nil when no project context is present.
// This method satisfies the adk.CredentialResolver interface.
func (s *CredentialService) ResolveAny(ctx context.Context) (*ResolvedCredential, error) {
	providerOrder := []ProviderType{ProviderDeepSeek, ProviderOpenAI, ProviderVertexAI, ProviderGoogleAI}

	projectID := auth.ProjectIDFromContext(ctx)
	if projectID == "" {
		return nil, nil // no project context
	}

	for _, p := range providerOrder {
		cfg, err := s.repo.GetProjectProviderConfig(ctx, projectID, p)
		if err != nil || cfg == nil {
			continue
		}
		cred, err := s.decryptProjectConfig(cfg)
		if err != nil {
			s.log.Debug("project credential decryption failed, trying next",
				slog.String("provider", string(p)),
				slog.String("error", err.Error()),
			)
			continue
		}
		if cred != nil {
			return cred, nil
		}
	}
	return nil, nil
}

// UpsertOrgConfig is deprecated. Org-level provider config is no longer supported.
// Use UpsertProjectConfig instead.
func (s *CredentialService) UpsertOrgConfig(ctx context.Context, orgID string, provider ProviderType, req UpsertProviderConfigRequest) (*ProviderConfigResponse, error) {
	return nil, apperror.NewBadRequest("org-level provider config is deprecated; use project-level config via `memory provider configure-project`")
}

// GetOrgConfig is deprecated.
func (s *CredentialService) GetOrgConfig(_ context.Context, _ string, _ ProviderType) (*ProviderConfigResponse, error) {
	return nil, apperror.NewBadRequest("org-level provider config is deprecated")
}

// DeleteOrgConfig is deprecated.
func (s *CredentialService) DeleteOrgConfig(_ context.Context, _ string, _ ProviderType) error {
	return apperror.NewBadRequest("org-level provider config is deprecated")
}

// ListOrgConfigs is deprecated.
func (s *CredentialService) ListOrgConfigs(_ context.Context, _ string) ([]ProviderConfigResponse, error) {
	return nil, apperror.NewBadRequest("org-level provider config is deprecated")
}

// ListProjectConfigs returns all provider configs for a specific project (metadata only).
func (s *CredentialService) ListProjectConfigs(ctx context.Context, projectID string) ([]ProjectProviderConfigResponse, error) {
	if err := s.assertCallerOwnsProject(ctx, projectID); err != nil {
		return nil, err
	}
	cfgs, err := s.repo.ListProjectProviderConfigs(ctx, projectID)
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

	// OpenAI and DeepSeek endpoints may be slow to produce a first token.
	testTimeout2 := 15 * time.Second
	if provider == ProviderOpenAI || provider == ProviderDeepSeek {
		testTimeout2 = 60 * time.Second
	}
	testCtx, testCancel := context.WithTimeout(ctx, testTimeout2)
	defer testCancel()
	// Only test generative when a generative model is explicitly requested.
	// Embedding-only configs (e.g. Google gemini-embedding-*) must not be forced
	// through a generative test — the API key may have no generative scope.
	if req.GenerativeModel != "" || req.EmbeddingModel == "" {
		if _, _, err := s.catalog.TestGenerate(testCtx, provider, tempCred); err != nil {
			return nil, apperror.NewBadRequest(fmt.Sprintf("generative model test failed: %s", err.Error()))
		}
	}
	// DeepSeek and OpenAI providers have no embedding API — skip the embed test.
	noEmbedProvider2 := provider == ProviderDeepSeek || provider == ProviderOpenAI
	if noEmbedProvider2 {
		s.log.Warn(fmt.Sprintf("%s provider configured without embeddings — configure a separate embedding provider for document indexing", provider))
	} else if req.EmbeddingModel != "" || req.GenerativeModel == "" {
		if _, err := s.catalog.TestEmbed(testCtx, provider, tempCred); err != nil {
			return nil, apperror.NewBadRequest(fmt.Sprintf("embedding model test failed: %s", err.Error()))
		}
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
		if embeddingModel == "" && !noEmbedProvider2 {
			embeddingModel = s.pickBestEmbeddingModel(embModels)
		}
	}

	// Require an explicit generative model when catalog auto-selection yields nothing.
	// Providers like DeepSeek must have a model set explicitly — no silent Google fallback.
	if generativeModel == "" {
		return nil, apperror.NewBadRequest(fmt.Sprintf(
			"generativeModel is required for provider %s — catalog is empty and no model was specified", provider,
		))
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
	case ProviderOpenAI:
		if req.APIKey == "" {
			return nil, apperror.NewBadRequest("apiKey is required for openai")
		}
		// BaseURL is optional; defaults to https://api.openai.com/v1
		return []byte(req.APIKey), nil
	case ProviderDeepSeek:
		if req.APIKey == "" {
			return nil, apperror.NewBadRequest("apiKey is required for deepseek")
		}
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
	case ProviderOpenAI:
		cred.BaseURL = req.BaseURL
		if cred.BaseURL == "" {
			cred.BaseURL = "https://api.openai.com/v1"
		}
		cred.APIKey = req.APIKey
	case ProviderDeepSeek:
		cred.BaseURL = "https://api.deepseek.com/v1"
		cred.APIKey = req.APIKey
	}
	return cred
}

// pickBestGenerativeModel selects the preferred generative model from the catalog.
// Returns an empty string if the catalog is empty — callers must error if a model is required.
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
	return ""
}

// pickBestEmbeddingModel selects the preferred embedding model from the catalog.
// Returns an empty string if the catalog is empty — callers must error if a model is required.
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
	return ""
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
