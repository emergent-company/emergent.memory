package modelconfig

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Service resolves and manages model configuration.
// It is also used by pkg/adk to determine the effective generative model
// when no per-agent override is present.
type Service struct {
	store *Store
	log   *slog.Logger
}

// NewService creates a new model config Service.
func NewService(store *Store, log *slog.Logger) *Service {
	return &Service{store: store, log: log}
}

// validateModelName returns an error if the model name does not include a
// provider prefix (e.g. "deepseek/deepseek-v4-flash", "google/gemini-2.5-flash").
func validateModelName(field, name string) error {
	if name == "" {
		return nil // empty is allowed (means "not set")
	}
	if !strings.Contains(name, "/") {
		return fmt.Errorf("%s %q must include a provider prefix (e.g. \"deepseek/deepseek-v4-flash\", \"google/gemini-2.5-flash\", \"google-vertex/gemini-2.5-flash\")", field, name)
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
	return toModelConfigResponse(cfg.GenerativeModel, cfg.EmbeddingModel, cfg.CreatedAt, cfg.UpdatedAt), nil
}

// UpsertProjectModelConfig sets the explicit default models for a project.
// Both generativeModel and embeddingModel must include a provider prefix
// (e.g. "deepseek/deepseek-v4-flash", "google/gemini-embedding-2-preview").
func (s *Service) UpsertProjectModelConfig(ctx context.Context, projectID uuid.UUID, req UpsertModelConfigRequest) (*ModelConfigResponse, error) {
	if err := validateModelName("generativeModel", req.GenerativeModel); err != nil {
		return nil, err
	}
	if err := validateModelName("embeddingModel", req.EmbeddingModel); err != nil {
		return nil, err
	}
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
// Both generativeModel and embeddingModel must include a provider prefix.
func (s *Service) UpsertOrgModelConfig(ctx context.Context, orgID uuid.UUID, req UpsertModelConfigRequest) (*ModelConfigResponse, error) {
	if err := validateModelName("generativeModel", req.GenerativeModel); err != nil {
		return nil, err
	}
	if err := validateModelName("embeddingModel", req.EmbeddingModel); err != nil {
		return nil, err
	}
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
// Chain: project config → org config.
// Returns ("", ModelSourceProject, nil) when no config is set at any level —
// callers must treat an empty model name as "not configured" and surface an
// appropriate error to the user.
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
		s.log.Warn("could not resolve org for project",
			slog.String("projectID", projectID.String()),
			slog.String("error", err.Error()),
		)
	} else {
		orgCfg, err := s.store.GetOrgModelConfig(ctx, orgID)
		if err == nil && orgCfg != nil && orgCfg.GenerativeModel != "" {
			return orgCfg.GenerativeModel, ModelSourceOrg, nil
		}
	}

	// No config found at any level.
	return "", ModelSourceNone, nil
}

// ResolveEmbeddingModel returns the effective embedding model name for a project
// and the source it was resolved from.
//
// Chain: project config → org config.
// Returns ("", ModelSourceNone, nil) when no config is set at any level.
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
		s.log.Warn("could not resolve org for project",
			slog.String("projectID", projectID.String()),
			slog.String("error", err.Error()),
		)
	} else {
		orgCfg, err := s.store.GetOrgModelConfig(ctx, orgID)
		if err == nil && orgCfg != nil && orgCfg.EmbeddingModel != "" {
			return orgCfg.EmbeddingModel, ModelSourceOrg, nil
		}
	}

	// No config found at any level.
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

func toModelConfigResponse(genModel, embModel string, createdAt, updatedAt time.Time) *ModelConfigResponse {
	return &ModelConfigResponse{
		GenerativeModel: genModel,
		EmbeddingModel:  embModel,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}
}
