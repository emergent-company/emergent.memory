package provider

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cloud.google.com/go/auth/credentials"
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
		// Use stored service account credentials when available so we don't
		// rely on ambient ADC (which may not be present or may not have the
		// required scopes for the stored GCP project).
		if cred.ServiceAccountJSON != "" {
			creds, err := credentials.NewCredentialsFromJSON(
				credentials.ServiceAccount,
				[]byte(cred.ServiceAccountJSON),
				&credentials.DetectOptions{
					Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"},
				},
			)
			if err != nil {
				return nil, fmt.Errorf("failed to parse service account credentials: %w", err)
			}
			clientCfg.Credentials = creds
		}

	default:
		return nil, fmt.Errorf("unsupported provider for model listing: %s", provider)
	}

	client, err := genai.NewClient(fetchCtx, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}

	var models []ProviderSupportedModel
	var total int

	for m, err := range client.Models.All(fetchCtx) {
		if err != nil {
			return nil, fmt.Errorf("failed to list models: %w", err)
		}
		total++

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

	s.log.Info("model catalog fetch complete",
		slog.String("provider", string(provider)),
		slog.Int("total_from_api", total),
		slog.Int("classified", len(models)),
	)

	if len(models) == 0 {
		return nil, fmt.Errorf("API returned no usable models")
	}

	return models, nil
}

// classifyModel determines if a genai.Model is an embedding or generative model.
//
// It first checks SupportedActions (populated by the Google AI backend).
// For Vertex AI publisher models the SDK does not map supportedGenerationMethods
// into SupportedActions, so we fall back to name-based heuristics when the
// field is empty.
func classifyModel(m *genai.Model) ModelType {
	if m == nil {
		return ""
	}

	// Action-based classification (works for Google AI / Gemini API responses).
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

	// Name-based fallback for Vertex AI publisher models where the SDK omits
	// SupportedActions in the listModelsResponseFromVertex converter.
	name := strings.ToLower(m.Name)
	if strings.Contains(name, "embedding") || strings.Contains(name, "text-embedding") {
		return ModelTypeEmbedding
	}
	if strings.Contains(name, "gemini") || strings.Contains(name, "gemma") ||
		strings.Contains(name, "llama") || strings.Contains(name, "claude") ||
		strings.Contains(name, "mistral") || strings.Contains(name, "codestral") ||
		strings.Contains(name, "jamba") || strings.Contains(name, "command") {
		return ModelTypeGenerative
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
