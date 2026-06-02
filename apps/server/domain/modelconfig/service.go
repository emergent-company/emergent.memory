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
// (e.g. "deepseek/deepseek-v4-flash", "google/gemini-embedding-001").
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

// --- Resolution ---

// ResolveGenerativeModel returns the effective generative model name for a project.
//
// Chain: project config only.
// Returns ("", ModelSourceNone, nil) when no config is set — callers must treat
// an empty model name as "not configured" and surface an error to the user.
func (s *Service) ResolveGenerativeModel(ctx context.Context, projectID uuid.UUID) (model string, source ModelSource, err error) {
	projCfg, err := s.store.GetProjectModelConfig(ctx, projectID)
	if err != nil {
		return "", "", err
	}
	if projCfg != nil && projCfg.GenerativeModel != "" {
		return projCfg.GenerativeModel, ModelSourceProject, nil
	}
	return "", ModelSourceNone, nil
}

// ResolveEmbeddingModel returns the effective embedding model name for a project.
//
// Chain: project config only.
// Returns ("", ModelSourceNone, nil) when no config is set.
func (s *Service) ResolveEmbeddingModel(ctx context.Context, projectID uuid.UUID) (model string, source ModelSource, err error) {
	projCfg, err := s.store.GetProjectModelConfig(ctx, projectID)
	if err != nil {
		return "", "", err
	}
	if projCfg != nil && projCfg.EmbeddingModel != "" {
		return projCfg.EmbeddingModel, ModelSourceProject, nil
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

func toModelConfigResponse(genModel, embModel string, createdAt, updatedAt time.Time) *ModelConfigResponse {
	return &ModelConfigResponse{
		GenerativeModel: genModel,
		EmbeddingModel:  embModel,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}
}
