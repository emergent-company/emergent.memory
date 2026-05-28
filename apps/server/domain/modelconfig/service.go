package modelconfig

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// Service resolves and manages model configuration.
// It is also used by pkg/adk to determine the effective generative model
// when no per-agent override is present.
type Service struct {
	store *Store
	cfg   *config.Config
	log   *slog.Logger
}

// NewService creates a new model config Service.
func NewService(store *Store, cfg *config.Config, log *slog.Logger) *Service {
	return &Service{store: store, cfg: cfg, log: log}
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
	return toModelConfigResponse(cfg.GenerativeModel, cfg.EmbeddingModel, cfg.CreatedAt, cfg.UpdatedAt), nil
}

// UpsertProjectModelConfig sets the explicit default models for a project.
func (s *Service) UpsertProjectModelConfig(ctx context.Context, projectID uuid.UUID, req UpsertModelConfigRequest) (*ModelConfigResponse, error) {
	now := time.Now()
	cfg := &ProjectModelConfig{
		ProjectID:       projectID,
		GenerativeModel: req.GenerativeModel,
		EmbeddingModel:  req.EmbeddingModel,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.store.UpsertProjectModelConfig(ctx, cfg); err != nil {
		return nil, err
	}
	s.log.Info("project model config upserted",
		slog.String("projectID", projectID.String()),
		slog.String("generativeModel", req.GenerativeModel),
		slog.String("embeddingModel", req.EmbeddingModel),
	)
	return toModelConfigResponse(req.GenerativeModel, req.EmbeddingModel, cfg.CreatedAt, cfg.UpdatedAt), nil
}

// DeleteProjectModelConfig clears the project's explicit model config.
// After deletion, org config or env defaults will be used.
func (s *Service) DeleteProjectModelConfig(ctx context.Context, projectID uuid.UUID) error {
	return s.store.DeleteProjectModelConfig(ctx, projectID)
}

// --- Org model config ---

// GetOrgModelConfig returns the stored org model config, or nil if not set.
func (s *Service) GetOrgModelConfig(ctx context.Context, orgID uuid.UUID) (*ModelConfigResponse, error) {
	cfg, err := s.store.GetOrgModelConfig(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, nil
	}
	return toModelConfigResponse(cfg.GenerativeModel, cfg.EmbeddingModel, cfg.CreatedAt, cfg.UpdatedAt), nil
}

// UpsertOrgModelConfig sets the explicit default models for an org.
func (s *Service) UpsertOrgModelConfig(ctx context.Context, orgID uuid.UUID, req UpsertModelConfigRequest) (*ModelConfigResponse, error) {
	now := time.Now()
	cfg := &OrgModelConfig{
		OrgID:           orgID,
		GenerativeModel: req.GenerativeModel,
		EmbeddingModel:  req.EmbeddingModel,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.store.UpsertOrgModelConfig(ctx, cfg); err != nil {
		return nil, err
	}
	s.log.Info("org model config upserted",
		slog.String("orgID", orgID.String()),
		slog.String("generativeModel", req.GenerativeModel),
		slog.String("embeddingModel", req.EmbeddingModel),
	)
	return toModelConfigResponse(req.GenerativeModel, req.EmbeddingModel, cfg.CreatedAt, cfg.UpdatedAt), nil
}

// DeleteOrgModelConfig clears the org's explicit model config.
func (s *Service) DeleteOrgModelConfig(ctx context.Context, orgID uuid.UUID) error {
	return s.store.DeleteOrgModelConfig(ctx, orgID)
}

// --- Resolution ---

// ResolveGenerativeModel returns the effective generative model name for a project
// and the source it was resolved from.
//
// Chain: project config → org config → env default (VERTEX_AI_MODEL / LLM_MODEL).
func (s *Service) ResolveGenerativeModel(ctx context.Context, projectID uuid.UUID) (model string, source ModelSource, err error) {
	// 1. Project config.
	projCfg, err := s.store.GetProjectModelConfig(ctx, projectID)
	if err != nil {
		return "", "", err
	}
	if projCfg != nil && projCfg.GenerativeModel != "" {
		return projCfg.GenerativeModel, ModelSourceProject, nil
	}

	// 2. Org config.
	orgID, err := s.store.GetProjectOrgID(ctx, projectID)
	if err != nil {
		// Non-fatal: fall through to env default with a warning.
		s.log.Warn("could not resolve org for project, falling back to env default",
			slog.String("projectID", projectID.String()),
			slog.String("error", err.Error()),
		)
	} else {
		orgCfg, err := s.store.GetOrgModelConfig(ctx, orgID)
		if err == nil && orgCfg != nil && orgCfg.GenerativeModel != "" {
			return orgCfg.GenerativeModel, ModelSourceOrg, nil
		}
	}

	// 3. Env default.
	envModel := s.envGenerativeModel()
	return envModel, ModelSourceEnv, nil
}

// ResolveEmbeddingModel returns the effective embedding model name for a project
// and the source it was resolved from.
//
// Chain: project config → org config → env default (EMBEDDING_MODEL).
func (s *Service) ResolveEmbeddingModel(ctx context.Context, projectID uuid.UUID) (model string, source ModelSource, err error) {
	// 1. Project config.
	projCfg, err := s.store.GetProjectModelConfig(ctx, projectID)
	if err != nil {
		return "", "", err
	}
	if projCfg != nil && projCfg.EmbeddingModel != "" {
		return projCfg.EmbeddingModel, ModelSourceProject, nil
	}

	// 2. Org config.
	orgID, err := s.store.GetProjectOrgID(ctx, projectID)
	if err != nil {
		s.log.Warn("could not resolve org for project, falling back to env default",
			slog.String("projectID", projectID.String()),
			slog.String("error", err.Error()),
		)
	} else {
		orgCfg, err := s.store.GetOrgModelConfig(ctx, orgID)
		if err == nil && orgCfg != nil && orgCfg.EmbeddingModel != "" {
			return orgCfg.EmbeddingModel, ModelSourceOrg, nil
		}
	}

	// 3. Env default.
	return s.cfg.Embeddings.Model, ModelSourceEnv, nil
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

// envGenerativeModel returns the server-level default generative model name.
// Priority: DEEPSEEK_MODEL → OPENAI_MODEL → VERTEX_AI_MODEL.
func (s *Service) envGenerativeModel() string {
	if s.cfg.LLM.DeepSeekAPIKey != "" && s.cfg.LLM.DeepSeekModel != "" {
		return s.cfg.LLM.DeepSeekModel
	}
	if s.cfg.LLM.OpenAIAPIKey != "" && s.cfg.LLM.OpenAIModel != "" {
		return s.cfg.LLM.OpenAIModel
	}
	return s.cfg.LLM.Model
}

// --- Helpers ---

func toModelConfigResponse(genModel, embModel string, createdAt, updatedAt time.Time) *ModelConfigResponse {
	return &ModelConfigResponse{
		GenerativeModel: genModel,
		EmbeddingModel:  embModel,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}
}
