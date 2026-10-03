package provider

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/modelref"
)

// ResolvedCredential holds the decrypted credential material and metadata
// needed to instantiate an LLM client for a specific request context.
type ResolvedCredential struct {
	// Provider is the provider dialect (wire/auth behavior).
	Provider ProviderType

	// Slug is the provider instance the credential was resolved from. It may
	// differ from Provider when several instances share one dialect.
	Slug ProviderSlug

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

	// Protocol is the wire protocol the vendor speaks ("openai-chat",
	// "google-genai", "anthropic-messages"). Populated from the registry
	// definition at resolution time.
	Protocol Protocol

	// Auth is the credential-injection style (bearer, api-key, x-api-key,
	// x-goog-api-key, none, signed). Populated from the registry definition.
	Auth AuthStyle

	// Extra carries vendor-specific configuration keyed by field name
	// (e.g. Azure "api_version").
	Extra map[string]string

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
		Slug:            cfg.Slug,
		Source:          SourceProject,
		GCPProject:      cfg.GCPProject,
		Location:        cfg.Location,
		BaseURL:         cfg.BaseURL,
		GenerativeModel: modelref.StripRoutingPrefix(cfg.GenerativeModel, isDialectName),
		EmbeddingModel:  modelref.StripRoutingPrefix(cfg.EmbeddingModel, isDialectName),
	}

	// Route the decrypted blob to the field the vendor's definition expects:
	// service-account vendors hold a service-account JSON document, every other
	// credential-bearing vendor holds an API key. Derived from the definition's
	// credential fields — no per-vendor switch — so a newly registered vendor's
	// credential is routed without touching this code.
	if definitionUsesServiceAccount(definitionFor(cfg.Provider)) {
		resolved.ServiceAccountJSON = string(plaintext)
	} else {
		resolved.APIKey = string(plaintext)
	}

	s.applyDefinition(resolved)
	return resolved, nil
}

// applyDefinition populates Protocol/Auth from the registry definition and
// fills a default BaseURL (generative first, then embedding) when none was
// configured. It is nil-safe: an unknown provider or an absent registry leaves
// the credential with only the fields populated by the legacy per-provider
// switch.
func (s *CredentialService) applyDefinition(resolved *ResolvedCredential) {
	if resolved == nil {
		return
	}
	def := definitionFor(resolved.Provider)
	if def == nil {
		return
	}
	resolved.Protocol = def.Protocol
	resolved.Auth = def.Auth
	if resolved.BaseURL == "" {
		if u := def.DefaultBaseURLs[ModelTypeGenerative]; u != "" {
			resolved.BaseURL = u
		} else if u := def.DefaultBaseURLs[ModelTypeEmbedding]; u != "" {
			resolved.BaseURL = u
		}
	}
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

// ResolveBySlug resolves credentials for a specific provider instance (slug).
// Returns nil, nil when no project context is present, and an error when the
// project has no instance with that slug.
func (s *CredentialService) ResolveBySlug(ctx context.Context, slug ProviderSlug) (*ResolvedCredential, error) {
	projectID := auth.ProjectIDFromContext(ctx)
	if projectID == "" {
		return nil, nil // no project context — env-var callers handle this
	}
	cfg, err := s.repo.GetProjectProviderConfigBySlug(ctx, projectID, slug)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, fmt.Errorf("no %s provider config found for project %s — configure it with `memory provider configure-project`", slug, projectID)
	}
	return s.decryptProjectConfig(cfg)
}

// ResolveByRef resolves credentials for a structured model reference. It is
// slug-first: the reference's provider is looked up as an instance slug, and if
// no such instance exists but the value names a dialect, it falls back to that
// dialect's default instance (legacy compatibility).
func (s *CredentialService) ResolveByRef(ctx context.Context, ref modelref.Ref) (*ResolvedCredential, error) {
	if ref.Provider == "" {
		return nil, fmt.Errorf("model reference has no provider")
	}
	projectID := auth.ProjectIDFromContext(ctx)
	if projectID == "" {
		return nil, nil // no project context — env-var callers handle this
	}
	cfg, err := s.repo.GetProjectProviderConfigBySlug(ctx, projectID, ProviderSlug(ref.Provider))
	if err != nil {
		return nil, err
	}
	if cfg != nil {
		return s.decryptProjectConfig(cfg)
	}
	if isDialectName(ref.Provider) {
		return s.Resolve(ctx, ProviderType(ref.Provider))
	}
	return nil, fmt.Errorf("no provider instance %q configured for project %s", ref.Provider, projectID)
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

// DefaultGenerativeModel returns a prefixed "provider/model" name for the
// given project's first provider credential (DeepSeek → OpenAI → VertexAI →
// GoogleAI) that carries a generative model, or "" when none does. The project
// is passed explicitly (not read from the request context) so callers can
// resolve for a project that may differ from the session's active one.
//
// This is the canonical home of the executor's provider-config fallback
// (pkg/adk CreateModel): name building is identical (prefix the bare model
// with its routing provider), so the model reported via
// modelconfig.ResolveGenerativeModel matches what a run would use.
func (s *CredentialService) DefaultGenerativeModel(ctx context.Context, projectID string) (string, error) {
	providerOrder := []ProviderType{ProviderDeepSeek, ProviderOpenAI, ProviderVertexAI, ProviderGoogleAI}
	if projectID == "" {
		return "", nil
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
		if cred != nil && cred.GenerativeModel != "" {
			return prefixedGenerativeModelName(cred.Slug, cred.GenerativeModel), nil
		}
	}
	return "", nil
}

// prefixedGenerativeModelName prefixes the routing provider instance (slug)
// onto a generative model name, producing the routed "slug/model" form the
// executor expects.
//
// cred.GenerativeModel is already bare (decryptProjectConfig runs it through
// modelref.StripRoutingPrefix, which leaves multi-segment Vertex resource paths
// like "publishers/google/models/gemini-2.5-flash" untouched), so an extra
// split here would corrupt those into "google-vertex/google/models/...".
// Routing through the idempotent modelref.StripRoutingPrefix keeps the edge
// safe without changing the bare-model result.
func prefixedGenerativeModelName(slug ProviderSlug, gen string) string {
	return string(slug) + "/" + modelref.StripRoutingPrefix(gen, isDialectName)
}

// prefixedEmbeddingModelName prefixes the routing provider instance (slug) onto
// an embedding model name, producing the routed "slug/model" form modelconfig
// resolution expects. Mirror of prefixedGenerativeModelName, kept separate so
// the two resolution chains stay independently named.
func prefixedEmbeddingModelName(slug ProviderSlug, emb string) string {
	return string(slug) + "/" + modelref.StripRoutingPrefix(emb, isDialectName)
}

// embeddingProviderOrder lists providers in preference order for embedding
// resolution. Google AI and Vertex AI come first (native embedding support),
// then OpenAI (embedding via the OpenAI API). DeepSeek is last — it has no
// embedding API, so its configs never carry an EmbeddingModel.
var embeddingProviderOrder = []ProviderType{
	ProviderGoogleAI,
	ProviderVertexAI,
	ProviderOpenAI,
	ProviderDeepSeek,
}

// pickEmbeddingConfig selects the first project provider config (from cfgs,
// keyed by provider) that carries a non-empty embedding model, in
// embeddingProviderOrder. Returns nil when no provider has an embedding model.
func pickEmbeddingConfig(cfgs map[ProviderType]*ProjectProviderConfig) *ProjectProviderConfig {
	for _, p := range embeddingProviderOrder {
		if cfg := cfgs[p]; cfg != nil && cfg.EmbeddingModel != "" {
			return cfg
		}
	}
	return nil
}

// ResolveAnyEmbedding resolves the best available embedding credential for the
// request context. Unlike ResolveAny (which prefers DeepSeek/OpenAI for
// generative work), it skips providers without an embedding model, so it never
// short-circuits on a generative-only provider (DeepSeek/OpenAI) when a Google
// or Vertex embedding provider is also configured.
// Returns nil, nil when no project context is present or no provider has an
// embedding model configured.
func (s *CredentialService) ResolveAnyEmbedding(ctx context.Context) (*ResolvedCredential, error) {
	projectID := auth.ProjectIDFromContext(ctx)
	if projectID == "" {
		return nil, nil // no project context — env-var callers handle this
	}

	cfgs := make(map[ProviderType]*ProjectProviderConfig, len(embeddingProviderOrder))
	for _, p := range embeddingProviderOrder {
		cfg, err := s.repo.GetProjectProviderConfig(ctx, projectID, p)
		if err != nil {
			s.log.Debug("embedding credential lookup failed, trying next",
				slog.String("provider", string(p)),
				slog.String("error", err.Error()),
			)
			continue
		}
		cfgs[p] = cfg
	}

	cfg := pickEmbeddingConfig(cfgs)
	if cfg == nil {
		return nil, nil
	}
	return s.decryptProjectConfig(cfg)
}

// DefaultEmbeddingModel returns a prefixed "provider/model" name for the given
// project's first provider credential (Google AI → Vertex AI → OpenAI → DeepSeek)
// that carries an embedding model, or "" when none does. It mirrors
// DefaultGenerativeModel for embedding: the project is passed explicitly (not
// read from the request context) so callers can resolve for a project that may
// differ from the session's active one.
//
// Unlike DefaultGenerativeModel it never decrypts credentials — EmbeddingModel
// is stored in plaintext on the provider config — so it is a cheap, safe way for
// modelconfig.ResolveEmbeddingModel to report the same provider-credential
// fallback the EmbeddingResolverAdapter applies when no project_model_config is
// set. Keeping this consistent with ResolveAnyEmbedding's embeddingProviderOrder
// ensures the reported model matches what an actual embedding call would use.
func (s *CredentialService) DefaultEmbeddingModel(ctx context.Context, projectID string) (string, error) {
	if projectID == "" {
		return "", nil
	}
	for _, p := range embeddingProviderOrder {
		cfg, err := s.repo.GetProjectProviderConfig(ctx, projectID, p)
		if err != nil || cfg == nil {
			continue
		}
		if cfg.EmbeddingModel != "" {
			return prefixedEmbeddingModelName(cfg.Slug, cfg.EmbeddingModel), nil
		}
	}
	return "", nil
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
			Slug:            cfg.Slug,
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
	if err := assertCallerOwnsOrg(ctx, s.repo, orgID); err != nil {
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
			Slug:            cfg.Slug,
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

	slug, err := resolveSlug(req.Slug, provider)
	if err != nil {
		return nil, err
	}

	// When the caller omits credential fields (e.g. only updating models or
	// baseURL), reuse the previously stored credential instead of requiring
	// the API key / service account on every upsert. Model selections are
	// reused too, so re-saving credentials/baseURL never silently re-auto-selects
	// a different model from the (possibly larger) synced catalog.
	if s.needsStoredCredential(provider, req) || req.GenerativeModel == "" || req.EmbeddingModel == "" {
		existing, err := s.repo.GetProjectProviderConfigBySlug(ctx, projectID, slug)
		if err != nil {
			return nil, fmt.Errorf("failed to get existing provider config: %w", err)
		}
		if s.needsStoredCredential(provider, req) {
			if err := s.reuseStoredCredential(provider, existing, &req); err != nil {
				return nil, err
			}
		}
		s.reuseStoredModels(existing, &req)
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
		testCred := *tempCred
		if _, _, err := s.catalog.TestGenerate(testCtx, provider, &testCred); err != nil {
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
	// (only meaningful when catalog sync succeeded). The catalog stores bare
	// model names — SyncModels runs against the stripped tempCred, so any
	// routing prefix (e.g. "deepseek/deepseek-v4-flash" via a LiteLLM proxy)
	// must be stripped here too or validation fails with "model not found".
	if catalogSynced {
		if bare := modelref.StripRoutingPrefix(req.GenerativeModel, isDialectName); bare != "" {
			if err := s.validateModelInCatalog(ctx, provider, bare, ModelTypeGenerative); err != nil {
				return nil, err
			}
		}
		if bare := modelref.StripRoutingPrefix(req.EmbeddingModel, isDialectName); bare != "" {
			if err := s.validateModelInCatalog(ctx, provider, bare, ModelTypeEmbedding); err != nil {
				return nil, err
			}
		}
	}

	cfg := &ProjectProviderConfig{
		ProjectID:           projectID,
		Provider:            provider,
		Slug:                slug,
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
		Slug:            cfg.Slug,
		GCPProject:      cfg.GCPProject,
		Location:        cfg.Location,
		BaseURL:         cfg.BaseURL,
		GenerativeModel: cfg.GenerativeModel,
		EmbeddingModel:  cfg.EmbeddingModel,
		CreatedAt:       cfg.CreatedAt,
		UpdatedAt:       cfg.UpdatedAt,
	}, nil
}

// GetProjectConfig retrieves the public-safe metadata for a project's provider
// instance. The argument is resolved as an instance slug first, then as a
// dialect (the dialect's default instance) for legacy callers.
func (s *CredentialService) GetProjectConfig(ctx context.Context, projectID string, slugOrDialect ProviderType) (*ProviderConfigResponse, error) {
	if err := s.assertCallerOwnsProject(ctx, projectID); err != nil {
		return nil, err
	}
	cfg, err := s.repo.GetProjectProviderConfigBySlug(ctx, projectID, ProviderSlug(slugOrDialect))
	if err != nil {
		return nil, err
	}
	if cfg == nil && isDialectName(string(slugOrDialect)) {
		cfg, err = s.repo.GetProjectProviderConfig(ctx, projectID, slugOrDialect)
		if err != nil {
			return nil, err
		}
	}
	if cfg == nil {
		return nil, nil
	}
	return &ProviderConfigResponse{
		ID:              cfg.ID,
		Provider:        cfg.Provider,
		Slug:            cfg.Slug,
		GCPProject:      cfg.GCPProject,
		Location:        cfg.Location,
		BaseURL:         cfg.BaseURL,
		GenerativeModel: cfg.GenerativeModel,
		EmbeddingModel:  cfg.EmbeddingModel,
		CreatedAt:       cfg.CreatedAt,
		UpdatedAt:       cfg.UpdatedAt,
	}, nil
}

// DeleteProjectConfig removes a project's provider instance. The argument is
// resolved as an instance slug first, then as a dialect (default instance).
func (s *CredentialService) DeleteProjectConfig(ctx context.Context, projectID string, slugOrDialect ProviderType) error {
	if err := s.assertCallerOwnsProject(ctx, projectID); err != nil {
		return err
	}
	if err := s.repo.DeleteProjectProviderConfigBySlug(ctx, projectID, ProviderSlug(slugOrDialect)); err != nil {
		return err
	}
	if isDialectName(string(slugOrDialect)) {
		return s.repo.DeleteProjectProviderConfig(ctx, projectID, slugOrDialect)
	}
	return nil
}

// --- provider instance slugs ---

const maxProviderSlugLen = 63

var providerSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// isDialectName reports whether s names a supported provider dialect (or an
// accepted alias). The set is built once from the embedded vendor registry.
func isDialectName(s string) bool {
	return dialectNames[s]
}

// dialectNames is the set of recognized provider dialect IDs plus accepted
// aliases, built from Builtins() at package init.
var dialectNames = buildDialectNames()

func buildDialectNames() map[string]bool {
	names := make(map[string]bool, len(Builtins())+1)
	for _, d := range Builtins() {
		names[string(d.Type)] = true
	}
	// "gemini" is an accepted alias for the "google" dialect.
	names["gemini"] = true
	return names
}

// validateProviderSlug validates a user-supplied instance slug. A slug must not
// equal a dialect other than the instance's own dialect, which would shadow the
// legacy dialect fallback.
func validateProviderSlug(slug ProviderSlug, dialect ProviderDialect) error {
	s := string(slug)
	if s == "" {
		return apperror.NewBadRequest("provider slug must not be empty")
	}
	if len(s) > maxProviderSlugLen {
		return apperror.NewBadRequest(fmt.Sprintf("provider slug %q is too long (max %d characters)", s, maxProviderSlugLen))
	}
	if !providerSlugRe.MatchString(s) {
		return apperror.NewBadRequest(fmt.Sprintf("invalid provider slug %q: must match [a-z0-9][a-z0-9-]*", s))
	}
	if isDialectName(s) && ProviderDialect(s) != dialect {
		return apperror.NewBadRequest(fmt.Sprintf("provider slug %q is a reserved dialect name", s))
	}
	return nil
}

// resolveSlug returns the target instance slug for a save: an explicit,
// validated slug, or the dialect's default slug when none is given. A save
// without a slug therefore updates the dialect's default instance in place
// rather than creating a duplicate (D9).
func resolveSlug(requested string, dialect ProviderDialect) (ProviderSlug, error) {
	if strings.TrimSpace(requested) == "" {
		return ProviderSlug(dialect), nil
	}
	slug := ProviderSlug(strings.TrimSpace(requested))
	if err := validateProviderSlug(slug, dialect); err != nil {
		return "", err
	}
	return slug, nil
}

// --- helpers ---

// needsStoredCredential reports whether the request is missing credential
// fields that must be filled from the previously stored config.
func (s *CredentialService) needsStoredCredential(provider ProviderType, req UpsertProviderConfigRequest) bool {
	def := definitionFor(provider)
	if definitionUsesServiceAccount(def) {
		return req.ServiceAccountJSON == "" || req.GCPProject == "" || req.Location == ""
	}
	if def == nil {
		return false
	}
	if def.Auth == AuthNone {
		return false
	}
	// bearer, api-key, x-api-key, x-goog-api-key, and signed (non-vertex)
	// vendors all use a single API-key credential.
	return req.APIKey == ""
}

// reuseStoredCredential fills credential fields omitted by the caller from the
// previously stored config, so callers can update models or baseURL without
// resending the API key or service account on every request.
func (s *CredentialService) reuseStoredCredential(provider ProviderType, existing *ProjectProviderConfig, req *UpsertProviderConfigRequest) error {
	if existing == nil || len(existing.EncryptedCredential) == 0 {
		return nil // nothing stored to reuse — extractPlaintext reports what's missing
	}
	if s.encryptor == nil {
		return fmt.Errorf("credential encryption not configured (LLM_ENCRYPTION_KEY missing)")
	}

	needsPlaintext := false
	isVertex := definitionUsesServiceAccount(definitionFor(provider))
	if isVertex {
		needsPlaintext = req.ServiceAccountJSON == ""
	} else if def := definitionFor(provider); def != nil && def.Auth != AuthNone {
		needsPlaintext = req.APIKey == ""
	}

	if needsPlaintext {
		plaintext, err := s.encryptor.Decrypt(existing.EncryptedCredential, existing.EncryptionNonce)
		if err != nil {
			return fmt.Errorf("failed to decrypt existing credential: %w", err)
		}
		if isVertex {
			req.ServiceAccountJSON = string(plaintext)
		} else {
			req.APIKey = string(plaintext)
		}
	}

	// Vertex GCPProject/Location are stored as plaintext columns — reuse when omitted.
	if isVertex {
		if req.GCPProject == "" {
			req.GCPProject = existing.GCPProject
		}
		if req.Location == "" {
			req.Location = existing.Location
		}
	}
	return nil
}

// reuseStoredModels restores the previously stored model selections when the
// request omits them. The settings UI sends only credentials + base_url, so
// without this the upsert would re-auto-select a model from the synced catalog
// and clobber the user's prior choice.
func (s *CredentialService) reuseStoredModels(existing *ProjectProviderConfig, req *UpsertProviderConfigRequest) {
	if existing == nil {
		return
	}
	if req.GenerativeModel == "" {
		req.GenerativeModel = existing.GenerativeModel
	}
	if req.EmbeddingModel == "" {
		req.EmbeddingModel = existing.EmbeddingModel
	}
}

// extractPlaintext returns the credential bytes to encrypt from the request.
func (s *CredentialService) extractPlaintext(provider ProviderType, req UpsertProviderConfigRequest) ([]byte, error) {
	def := definitionFor(provider)
	if definitionUsesServiceAccount(def) {
		if req.ServiceAccountJSON == "" {
			return nil, apperror.NewBadRequest(fmt.Sprintf("serviceAccountJson is required for %s", provider))
		}
		if req.GCPProject == "" {
			return nil, apperror.NewBadRequest(fmt.Sprintf("gcpProject is required for %s", provider))
		}
		if req.Location == "" {
			return nil, apperror.NewBadRequest(fmt.Sprintf("location is required for %s", provider))
		}
		return []byte(req.ServiceAccountJSON), nil
	}

	if def == nil {
		return nil, apperror.NewBadRequest(fmt.Sprintf("unsupported provider: %s", provider))
	}
	if def.Auth == AuthNone {
		return nil, nil // no credential required
	}
	if req.APIKey == "" {
		return nil, apperror.NewBadRequest(fmt.Sprintf("apiKey is required for %s", provider))
	}
	return []byte(req.APIKey), nil
}

// buildTempResolvedCred constructs a plaintext ResolvedCredential for testing/syncing.
func (s *CredentialService) buildTempResolvedCred(provider ProviderType, req UpsertProviderConfigRequest) *ResolvedCredential {
	cred := &ResolvedCredential{
		Provider:        provider,
		BaseURL:         req.BaseURL,
		GCPProject:      req.GCPProject,
		Location:        req.Location,
		GenerativeModel: modelref.StripRoutingPrefix(req.GenerativeModel, isDialectName),
		EmbeddingModel:  modelref.StripRoutingPrefix(req.EmbeddingModel, isDialectName),
	}

	// Mirror decryptProjectConfig: route the configured credential to the field
	// the vendor's definition expects, derived from the definition (not a
	// per-vendor switch) so new vendors' credentials survive the configure,
	// catalog-sync, and test-connection paths.
	if definitionUsesServiceAccount(definitionFor(provider)) {
		cred.ServiceAccountJSON = req.ServiceAccountJSON
	} else {
		cred.APIKey = req.APIKey
	}

	s.applyDefinition(cred)
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
