package modelconfig

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/modelref"
)

// Service resolves and manages model configuration.
// It is also used by pkg/adk to determine the effective generative model
// when no per-agent override is present.
type Service struct {
	store modelConfigStore
	log   *slog.Logger
	// resolver supplies the project's provider-credential generative default
	// (optional). When set, generative resolution falls back to it when a
	// project has no explicit model config — mirroring the executor's default.
	resolver generativeDefaultResolver
	// embeddingResolver supplies the project's provider-credential embedding
	// default (optional). When set, embedding resolution falls back to it when a
	// project has no explicit embedding model — mirroring the fallback the
	// EmbeddingResolverAdapter applies when generating embeddings.
	embeddingResolver embeddingDefaultResolver
	// providerValidator checks that a model reference's provider segment names
	// a supported dialect or an existing project provider instance slug
	// (optional). It validates the provider only, never the credential.
	providerValidator modelProviderValidator
}

// modelConfigStore is the persistence seam Service needs. Implemented by
// *Store; extracted so resolution behavior is unit-testable without a DB.
type modelConfigStore interface {
	GetProjectModelConfig(ctx context.Context, projectID uuid.UUID) (*ProjectModelConfig, error)
	UpsertProjectModelConfig(ctx context.Context, cfg *ProjectModelConfig) error
	DeleteProjectModelConfig(ctx context.Context, projectID uuid.UUID) error
}

// generativeDefaultResolver returns a prefixed "provider/model" name for the
// project's first provider credential that carries a generative model, or ""
// when none does. Implemented by provider.CredentialService.
type generativeDefaultResolver interface {
	DefaultGenerativeModel(ctx context.Context, projectID string) (string, error)
}

// embeddingDefaultResolver returns a prefixed "provider/model" name for the
// project's first provider credential that carries an embedding model, or ""
// when none does. Implemented by provider.CredentialService.
type embeddingDefaultResolver interface {
	DefaultEmbeddingModel(ctx context.Context, projectID string) (string, error)
}

// modelProviderValidator reports whether a model reference's provider segment
// names a supported provider dialect or an existing project provider instance
// slug. It deliberately does not require a usable credential: a model can be
// selected before its API key is configured, but a typo is rejected at write
// time instead of failing per call at resolution time. Implemented by
// provider.CredentialService.
type modelProviderValidator interface {
	ModelProviderKnown(ctx context.Context, projectID, providerName string) (bool, error)
}

// NewService creates a new model config Service.
func NewService(store modelConfigStore, log *slog.Logger) *Service {
	return &Service{store: store, log: log}
}

// WithGenerativeDefaultResolver wires the provider-credential fallback used by
// ResolveGenerativeModel when no project model config is set. Nil-safe: without
// it, generative resolution stops at the project config (pre-fallback behavior).
func (s *Service) WithGenerativeDefaultResolver(r generativeDefaultResolver) *Service {
	s.resolver = r
	return s
}

// WithEmbeddingDefaultResolver wires the provider-credential fallback used by
// ResolveEmbeddingModel when no project embedding model is set. Nil-safe:
// without it, embedding resolution stops at the project config.
func (s *Service) WithEmbeddingDefaultResolver(r embeddingDefaultResolver) *Service {
	s.embeddingResolver = r
	return s
}

// WithProviderValidator wires the provider check applied to non-empty model
// references at write time. Nil-safe: without it, only the "provider/model"
// shape is validated.
func (s *Service) WithProviderValidator(v modelProviderValidator) *Service {
	s.providerValidator = v
	return s
}

// validateModelName returns an error if the model name is not a valid
// structured reference ("provider/model"), parsed at this single input boundary
// via modelref.Parse. Empty is allowed (means "not set").
func validateModelName(field, name string) error {
	if name == "" {
		return nil // empty is allowed (means "not set")
	}
	if _, err := modelref.Parse(name); err != nil {
		return apperror.NewBadRequest(fmt.Sprintf("%s %q must be in 'provider/model' form (e.g. \"deepseek/deepseek-v4-flash\", \"google/gemini-2.5-flash\", \"google-vertex/gemini-2.5-flash\")", field, name))
	}
	return nil
}

// validateProviderRef rejects a non-empty reference whose provider segment is
// neither a supported dialect nor an existing project provider instance slug.
// Credentials are not checked here: selecting a model before configuring its
// API key stays valid, while a typo fails at the write boundary instead of per
// embedding/generation call (#690 item 3).
func (s *Service) validateProviderRef(ctx context.Context, projectID uuid.UUID, field, name, providerName string) error {
	if providerName == "" || s.providerValidator == nil {
		return nil
	}
	known, err := s.providerValidator.ModelProviderKnown(ctx, projectID.String(), providerName)
	if err != nil {
		return fmt.Errorf("%s %q: failed to validate provider %q: %w", field, name, providerName, err)
	}
	if !known {
		return apperror.NewBadRequest(fmt.Sprintf("%s %q names unknown provider %q — use a supported provider dialect (e.g. \"google\", \"openai\") or an existing project provider instance slug (configure one with 'memory provider configure-project <provider> --name <slug>')", field, name, providerName))
	}
	return nil
}

// --- Project model config ---

// GetProjectModelConfig returns the stored project model config, or nil if not set.
func (s *Service) GetProjectModelConfig(ctx context.Context, projectID uuid.UUID) (*ModelConfigResponse, error) {
	cfg, err := s.store.GetProjectModelConfig(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, nil
	}
	return toModelConfigResponse(cfg.GenerativeModel, cfg.EmbeddingModel, cfg.GenerativeProviderSlug, cfg.EmbeddingProviderSlug, cfg.CreatedAt, cfg.UpdatedAt), nil
}

// UpsertProjectModelConfig sets the explicit default models for a project.
// Each non-empty field must be a structured reference (e.g.
// "deepseek/deepseek-v4-flash", "google/gemini-embedding-001") whose provider
// segment names a supported dialect or an existing project provider instance;
// the provider segment and the bare model are stored as distinct values.
//
// The update is partial: a field the caller does not supply keeps its stored
// value, so `set-models --generative X` no longer wipes the embedding model.
// Clearing is the DELETE endpoint, which removes the whole row.
func (s *Service) UpsertProjectModelConfig(ctx context.Context, projectID uuid.UUID, req UpsertModelConfigRequest) (*ModelConfigResponse, error) {
	if err := validateModelName("generativeModel", req.GenerativeModel); err != nil {
		return nil, err
	}
	if err := validateModelName("embeddingModel", req.EmbeddingModel); err != nil {
		return nil, err
	}
	genRef, _ := modelref.Parse(req.GenerativeModel)
	embRef, _ := modelref.Parse(req.EmbeddingModel)
	if err := s.validateProviderRef(ctx, projectID, "generativeModel", req.GenerativeModel, genRef.Provider); err != nil {
		return nil, err
	}
	if err := s.validateProviderRef(ctx, projectID, "embeddingModel", req.EmbeddingModel, embRef.Provider); err != nil {
		return nil, err
	}

	// Load the existing row so an unsupplied field can be preserved. A missing
	// row is fine: both fields then stay empty.
	existing, err := s.store.GetProjectModelConfig(ctx, projectID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	cfg := &ProjectModelConfig{
		ProjectID: projectID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if req.GenerativeModel != "" {
		cfg.GenerativeModel = genRef.Model
		cfg.GenerativeProviderSlug = genRef.Provider
	} else if existing != nil {
		cfg.GenerativeModel = existing.GenerativeModel
		cfg.GenerativeProviderSlug = existing.GenerativeProviderSlug
	}
	if req.EmbeddingModel != "" {
		cfg.EmbeddingModel = embRef.Model
		cfg.EmbeddingProviderSlug = embRef.Provider
	} else if existing != nil {
		cfg.EmbeddingModel = existing.EmbeddingModel
		cfg.EmbeddingProviderSlug = existing.EmbeddingProviderSlug
	}
	if err := s.store.UpsertProjectModelConfig(ctx, cfg); err != nil {
		return nil, err
	}
	s.log.Info("project model config upserted",
		slog.String("projectID", projectID.String()),
		slog.String("generativeModel", cfg.GenerativeModel),
		slog.String("generativeProviderSlug", cfg.GenerativeProviderSlug),
		slog.String("embeddingModel", cfg.EmbeddingModel),
		slog.String("embeddingProviderSlug", cfg.EmbeddingProviderSlug),
	)
	return toModelConfigResponse(cfg.GenerativeModel, cfg.EmbeddingModel, cfg.GenerativeProviderSlug, cfg.EmbeddingProviderSlug, cfg.CreatedAt, cfg.UpdatedAt), nil
}

// DeleteProjectModelConfig clears the project's explicit model config.
func (s *Service) DeleteProjectModelConfig(ctx context.Context, projectID uuid.UUID) error {
	return s.store.DeleteProjectModelConfig(ctx, projectID)
}

// --- Resolution ---

// ResolveGenerativeModel returns the effective generative model name for a project.
//
// Chain: project model config → provider-credential generative model (when a
// resolver is wired) → none. The provider-credential fallback mirrors the
// executor's default (pkg/adk CreateModel), so the reported model matches what
// a run would actually use. Env-var models are NOT consulted: production runs
// always go through the resolver, so env defaults are a test-only path.
// Returns ("", ModelSourceNone, nil) when nothing resolves — callers must
// treat an empty model name as "not configured" and surface an error.
func (s *Service) ResolveGenerativeModel(ctx context.Context, projectID uuid.UUID) (model string, source ModelSource, err error) {
	projCfg, err := s.store.GetProjectModelConfig(ctx, projectID)
	if err != nil {
		return "", "", err
	}
	if projCfg != nil && projCfg.GenerativeModel != "" {
		// Stored structured: bare model + instance slug. Reconstruct the routed
		// "slug/model" form for the string-based adk boundary (the executor's
		// CreateModelWithName parses it once at the edge).
		return routedModelName(projCfg.GenerativeModel, projCfg.GenerativeProviderSlug), ModelSourceProject, nil
	}
	if s.resolver != nil {
		if m, rerr := s.resolver.DefaultGenerativeModel(ctx, projectID.String()); rerr == nil && m != "" {
			return m, ModelSourceProvider, nil
		}
	}
	return "", ModelSourceNone, nil
}

// ResolveEmbeddingModel returns the effective embedding model name for a project.
//
// Chain: project model config → provider-credential embedding model (when a
// resolver is wired) → none. The provider-credential fallback mirrors the
// EmbeddingResolverAdapter (pkg/embeddings) so the reported model matches what
// an embedding call would actually use — a project configured only via
// 'memory provider configure-project <provider> --embedding-model <model>' still
// generates vectors, so it must not be reported as unconfigured.
// Returns ("", ModelSourceNone, nil) when nothing resolves — callers must treat
// an empty model name as "not configured".
func (s *Service) ResolveEmbeddingModel(ctx context.Context, projectID uuid.UUID) (model string, source ModelSource, err error) {
	projCfg, err := s.store.GetProjectModelConfig(ctx, projectID)
	if err != nil {
		return "", "", err
	}
	if projCfg != nil && projCfg.EmbeddingModel != "" {
		return routedModelName(projCfg.EmbeddingModel, projCfg.EmbeddingProviderSlug), ModelSourceProject, nil
	}
	if s.embeddingResolver != nil {
		if m, rerr := s.embeddingResolver.DefaultEmbeddingModel(ctx, projectID.String()); rerr == nil && m != "" {
			return m, ModelSourceProvider, nil
		}
	}
	return "", ModelSourceNone, nil
}

// ResolveEffectiveModels returns the full effective model config for a project.
func (s *Service) ResolveEffectiveModels(ctx context.Context, projectID uuid.UUID) (*EffectiveModelConfig, error) {
	genModel, genSource, err := s.ResolveGenerativeModel(ctx, projectID)
	if err != nil {
		return nil, err
	}
	embModel, embSource, err := s.ResolveEmbeddingModel(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return &EffectiveModelConfig{
		GenerativeModel:       genModel,
		GenerativeModelSource: genSource,
		EmbeddingModel:        embModel,
		EmbeddingModelSource:  embSource,
	}, nil
}

// --- Helpers ---

// toModelConfigResponse builds the API response. The GenerativeModel/
// EmbeddingModel fields carry the routed "slug/model" display form (reconstructed
// from the stored bare model + slug); the slug fields expose the structured
// identity.
func toModelConfigResponse(genModel, embModel, genSlug, embSlug string, createdAt, updatedAt time.Time) *ModelConfigResponse {
	return &ModelConfigResponse{
		GenerativeModel:        routedModelName(genModel, genSlug),
		EmbeddingModel:         routedModelName(embModel, embSlug),
		GenerativeProviderSlug: genSlug,
		EmbeddingProviderSlug:  embSlug,
		CreatedAt:              createdAt,
		UpdatedAt:              updatedAt,
	}
}

// routedModelName reconstructs the "slug/model" display form from a stored bare
// model name and instance slug. An empty model yields ""; a missing slug (an
// unresolved, flagged row) yields the bare name.
func routedModelName(bare, slug string) string {
	if bare == "" {
		return ""
	}
	if slug == "" {
		return bare
	}
	return slug + "/" + bare
}
