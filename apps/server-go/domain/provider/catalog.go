package provider

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/emergent-company/emergent/pkg/logger"
)

// ModelCatalogService fetches and caches available models from provider APIs.
type ModelCatalogService struct {
	repo *Repository
	log  *slog.Logger
}

// NewModelCatalogService creates a new ModelCatalogService.
func NewModelCatalogService(repo *Repository, log *slog.Logger) *ModelCatalogService {
	return &ModelCatalogService{
		repo: repo,
		log:  log.With(logger.Scope("provider.catalog")),
	}
}

// SyncModels fetches the model catalog from the provider API using the given
// credentials and persists them to the provider_supported_models cache.
// If the API call fails (timeout or non-auth error), it falls back to a
// static known-good model list.
func (s *ModelCatalogService) SyncModels(ctx context.Context, provider ProviderType, cred *ResolvedCredential) error {
	models, err := s.fetchModelsFromAPI(ctx, provider, cred)
	if err != nil {
		s.log.Warn("failed to fetch models from API, using static fallback",
			logger.Error(err),
			slog.String("provider", string(provider)),
		)
		models = staticModels(provider)
	}

	if len(models) == 0 {
		return fmt.Errorf("no models available for provider %s", provider)
	}

	return s.repo.UpsertSupportedModels(ctx, models)
}

// ListModels returns the cached supported models for a provider,
// optionally filtered by model type (embedding or generative).
func (s *ModelCatalogService) ListModels(ctx context.Context, provider ProviderType, modelType *ModelType) ([]ProviderSupportedModel, error) {
	return s.repo.ListSupportedModels(ctx, provider, modelType)
}

// fetchModelsFromAPI calls the provider API to list available models.
func (s *ModelCatalogService) fetchModelsFromAPI(ctx context.Context, provider ProviderType, cred *ResolvedCredential) ([]ProviderSupportedModel, error) {
	// Apply a timeout so we don't block credential setup
	fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var clientCfg *genai.ClientConfig

	switch provider {
	case ProviderGoogleAI:
		if cred.APIKey == "" {
			return nil, fmt.Errorf("API key required for Google AI model listing")
		}
		clientCfg = &genai.ClientConfig{
			Backend: genai.BackendGeminiAPI,
			APIKey:  cred.APIKey,
		}

	case ProviderVertexAI:
		if cred.GCPProject == "" || cred.Location == "" {
			return nil, fmt.Errorf("GCP project and location required for Vertex AI model listing")
		}
		clientCfg = &genai.ClientConfig{
			Backend:  genai.BackendVertexAI,
			Project:  cred.GCPProject,
			Location: cred.Location,
		}

	default:
		return nil, fmt.Errorf("unsupported provider for model listing: %s", provider)
	}

	client, err := genai.NewClient(fetchCtx, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}

	var models []ProviderSupportedModel

	for m, err := range client.Models.All(fetchCtx) {
		if err != nil {
			return nil, fmt.Errorf("failed to list models: %w", err)
		}

		mt := classifyModel(m)
		if mt == "" {
			continue // skip models we can't classify
		}

		models = append(models, ProviderSupportedModel{
			Provider:    provider,
			ModelName:   normalizeModelName(m.Name),
			ModelType:   mt,
			DisplayName: m.DisplayName,
		})
	}

	if len(models) == 0 {
		return nil, fmt.Errorf("API returned no usable models")
	}

	s.log.Info("fetched models from API",
		slog.String("provider", string(provider)),
		slog.Int("count", len(models)),
	)

	return models, nil
}

// classifyModel determines if a genai.Model is an embedding or generative model
// based on its supported generation methods.
func classifyModel(m *genai.Model) ModelType {
	if m == nil {
		return ""
	}

	for _, action := range m.SupportedActions {
		if action == "embedContent" || action == "batchEmbedContents" {
			return ModelTypeEmbedding
		}
	}

	for _, action := range m.SupportedActions {
		if action == "generateContent" || action == "streamGenerateContent" {
			return ModelTypeGenerative
		}
	}

	return ""
}

// normalizeModelName strips the "models/" prefix that the API sometimes returns.
func normalizeModelName(name string) string {
	return strings.TrimPrefix(name, "models/")
}

// staticModels returns a known-good list of models as a fallback when the
// provider API is unavailable. These are kept intentionally conservative.
func staticModels(provider ProviderType) []ProviderSupportedModel {
	// Both Google AI and Vertex AI serve the same Gemini model family
	base := []struct {
		name        string
		displayName string
		modelType   ModelType
	}{
		// Generative models
		{"gemini-2.0-flash", "Gemini 2.0 Flash", ModelTypeGenerative},
		{"gemini-2.0-flash-lite", "Gemini 2.0 Flash Lite", ModelTypeGenerative},
		{"gemini-2.5-flash-preview-05-20", "Gemini 2.5 Flash Preview", ModelTypeGenerative},
		{"gemini-2.5-pro-preview-05-06", "Gemini 2.5 Pro Preview", ModelTypeGenerative},
		// Embedding models
		{"gemini-embedding-001", "Gemini Embedding 001", ModelTypeEmbedding},
		{"text-embedding-004", "Text Embedding 004", ModelTypeEmbedding},
	}

	models := make([]ProviderSupportedModel, len(base))
	for i, m := range base {
		models[i] = ProviderSupportedModel{
			Provider:    provider,
			ModelName:   m.name,
			ModelType:   m.modelType,
			DisplayName: m.displayName,
		}
	}
	return models
}
