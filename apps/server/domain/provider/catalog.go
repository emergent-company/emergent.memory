package provider

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cloud.google.com/go/auth/credentials"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/pkg/embeddings/vertex"
	"github.com/emergent-company/emergent.memory/pkg/logger"
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
		return fmt.Errorf("failed to fetch model catalog from %s API: %w", provider, err)
	}

	if len(models) == 0 {
		return fmt.Errorf("no models available for provider %s", provider)
	}

	if err := s.repo.UpsertSupportedModels(ctx, models); err != nil {
		return err
	}

	// Remove any stale rows for this provider that were not returned by the
	// current sync. This handles: model renames, retired models, and stale
	// static-fallback rows left over from a previous failed API call.
	names := make([]string, len(models))
	for i, m := range models {
		names[i] = m.ModelName
	}
	if err := s.repo.DeleteSupportedModelsNotIn(ctx, provider, names); err != nil {
		// Non-fatal: stale rows are cosmetic, don't fail the whole sync.
		s.log.Warn("failed to delete stale models after sync",
			logger.Error(err),
			slog.String("provider", string(provider)),
		)
	}

	return nil
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

	clientCfg, err := buildClientConfig(provider, cred)
	if err != nil {
		return nil, err
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

// staticModels returns a hardcoded list of well-known models for a provider.
// It is used as a fallback when the provider API is unavailable during startup,
// and by tests that do not require a live API connection.
//
// Only generative and embedding models that are confirmed to be stable and
// widely available are included. The list is intentionally conservative.
func staticModels(p ProviderType) []ProviderSupportedModel {
	type entry struct {
		name        string
		displayName string
		modelType   ModelType
	}

	// These models are provider-agnostic; both Google AI and Vertex AI support them.
	known := []entry{
		{"gemini-1.5-flash", "Gemini 1.5 Flash", ModelTypeGenerative},
		{"gemini-1.5-flash-8b", "Gemini 1.5 Flash 8B", ModelTypeGenerative},
		{"gemini-1.5-pro", "Gemini 1.5 Pro", ModelTypeGenerative},
		{"gemini-2.0-flash", "Gemini 2.0 Flash", ModelTypeGenerative},
		{"gemini-2.5-flash", "Gemini 2.5 Flash", ModelTypeGenerative},
		{"gemini-2.5-pro", "Gemini 2.5 Pro", ModelTypeGenerative},
		{"gemini-3.1-flash-lite-preview", "Gemini 3.1 Flash Lite Preview", ModelTypeGenerative},
		{"gemini-3.1-flash", "Gemini 3.1 Flash", ModelTypeGenerative},
		{"gemini-3.1-pro", "Gemini 3.1 Pro", ModelTypeGenerative},
		{"text-embedding-004", "Text Embedding 004", ModelTypeEmbedding},
		{"gemini-embedding-001", "Gemini Embedding 001", ModelTypeEmbedding},
		{"gemini-embedding-2-preview", "Gemini Embedding 2 Preview", ModelTypeEmbedding},
	}

	models := make([]ProviderSupportedModel, 0, len(known))
	for _, e := range known {
		models = append(models, ProviderSupportedModel{
			Provider:    p,
			ModelName:   e.name,
			ModelType:   e.modelType,
			DisplayName: e.displayName,
		})
	}
	return models
}

// normalizeModelName strips path prefixes that the API sometimes returns,
// producing a short canonical model name suitable for storage and display.
//
// Examples of inputs handled:
//   - "models/gemini-2.0-flash"                              → "gemini-2.0-flash"
//   - "publishers/google/models/gemini-2.0-flash"            → "gemini-2.0-flash"
//   - "locations/us-central1/publishers/google/models/gemma" → "gemma"
func normalizeModelName(name string) string {
	// Strip leading location segment: locations/<loc>/publishers/...
	if idx := strings.Index(name, "/publishers/"); idx != -1 {
		name = name[idx+1:] // keep "publishers/..."
	}
	// Strip publishers/<org>/models/ prefix
	if idx := strings.Index(name, "/models/"); idx != -1 {
		name = name[idx+8:] // skip "/models/"
	}
	// Strip bare "models/" prefix (Google AI backend)
	name = strings.TrimPrefix(name, "models/")
	return name
}

// TestGenerate sends a single "say hello" generate call to verify credentials
// work end-to-end. It uses the configured generative model from the resolved
// credential when available, otherwise falls back to a cheap flash model from
// the catalog. Returns the model name used and the LLM's reply text.
func (s *ModelCatalogService) TestGenerate(ctx context.Context, provider ProviderType, cred *ResolvedCredential) (model, reply string, err error) {
	// Use the configured generative model so the test validates exactly what
	// the user will use in practice.
	if cred.GenerativeModel != "" {
		model = cred.GenerativeModel
	} else {
		// No model configured — fall back to catalog selection.
		genType := ModelTypeGenerative
		models, listErr := s.repo.ListSupportedModels(ctx, provider, &genType)
		if listErr != nil || len(models) == 0 {
			return "", "", fmt.Errorf("no models in catalog for provider %s (sync models before testing)", provider)
		}
		model = s.pickCheapTestModel(models)
	}

	clientCfg, err := buildClientConfig(provider, cred)
	if err != nil {
		return "", "", err
	}

	client, err := genai.NewClient(ctx, clientCfg)
	if err != nil {
		return "", "", fmt.Errorf("failed to create genai client: %w", err)
	}

	resp, err := client.Models.GenerateContent(ctx, model, genai.Text("Say hello in one sentence."), nil)
	if err != nil {
		return "", "", fmt.Errorf("generate call failed: %w", err)
	}

	reply = resp.Text()
	return model, reply, nil
}

// TestEmbed sends a single embed call to verify embedding credentials and model
// work end-to-end. It uses the configured embedding model from the resolved
// credential when available, otherwise falls back to a known default.
// Returns the model name used.
func (s *ModelCatalogService) TestEmbed(ctx context.Context, provider ProviderType, cred *ResolvedCredential) (model string, err error) {
	// Use the configured embedding model or the default.
	model = cred.EmbeddingModel
	if model == "" {
		embType := ModelTypeEmbedding
		models, listErr := s.repo.ListSupportedModels(ctx, provider, &embType)
		if listErr != nil || len(models) == 0 {
			model = staticFallbackEmbeddingModel
		} else {
			model = models[0].ModelName
		}
	}

	switch provider {
	case ProviderVertexAI:
		if cred.GCPProject == "" || cred.Location == "" {
			return "", fmt.Errorf("GCP project and location required for Vertex AI embedding test")
		}
		opts := []vertex.ClientOption{}
		if cred.ServiceAccountJSON != "" {
			opts = append(opts, vertex.WithCredentialsJSON([]byte(cred.ServiceAccountJSON)))
		}
		client, clientErr := vertex.NewClient(ctx, vertex.Config{
			ProjectID: cred.GCPProject,
			Location:  cred.Location,
			Model:     model,
		}, opts...)
		if clientErr != nil {
			return "", fmt.Errorf("embedding model test failed: %w", clientErr)
		}
		vec, embedErr := client.EmbedQuery(ctx, "test")
		if embedErr != nil {
			return "", fmt.Errorf("embedding model test failed: %w", embedErr)
		}
		if len(vec) == 0 {
			return "", fmt.Errorf("embedding model test failed: empty vector returned")
		}

	case ProviderGoogleAI:
		clientCfg, cfgErr := buildClientConfig(provider, cred)
		if cfgErr != nil {
			return "", fmt.Errorf("embedding model test failed: %w", cfgErr)
		}
		client, clientErr := genai.NewClient(ctx, clientCfg)
		if clientErr != nil {
			return "", fmt.Errorf("embedding model test failed: %w", clientErr)
		}
		result, embedErr := client.Models.EmbedContent(ctx, model, genai.Text("test"), nil)
		if embedErr != nil {
			return "", fmt.Errorf("embedding model test failed: %w", embedErr)
		}
		if result == nil || len(result.Embeddings) == 0 || len(result.Embeddings[0].Values) == 0 {
			return "", fmt.Errorf("embedding model test failed: empty vector returned")
		}

	default:
		return "", fmt.Errorf("unsupported provider for embedding test: %s", provider)
	}

	return model, nil
}

// pickCheapTestModel selects a cheap, fast model from the catalog for testing
// when no configured model is available. Prefers flash variants.
func (s *ModelCatalogService) pickCheapTestModel(models []ProviderSupportedModel) string {
	best := models[0].ModelName
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
		name := m.ModelName
		if strings.Contains(name, "flash") &&
			!strings.Contains(name, "image") &&
			!strings.Contains(name, "tts") &&
			!strings.Contains(name, "audio") {
			return name
		}
	}
	return best
}

// buildClientConfig constructs a genai.ClientConfig from resolved credentials.
func buildClientConfig(provider ProviderType, cred *ResolvedCredential) (*genai.ClientConfig, error) {
	switch provider {
	case ProviderGoogleAI:
		if cred.APIKey == "" {
			return nil, fmt.Errorf("API key required for Google AI")
		}
		return &genai.ClientConfig{
			Backend: genai.BackendGeminiAPI,
			APIKey:  cred.APIKey,
		}, nil

	case ProviderVertexAI:
		if cred.GCPProject == "" || cred.Location == "" {
			return nil, fmt.Errorf("GCP project and location required for Vertex AI")
		}
		cfg := &genai.ClientConfig{
			Backend:  genai.BackendVertexAI,
			Project:  cred.GCPProject,
			Location: cred.Location,
		}
		if cred.ServiceAccountJSON != "" {
			c, err := credentials.NewCredentialsFromJSON(
				credentials.ServiceAccount,
				[]byte(cred.ServiceAccountJSON),
				&credentials.DetectOptions{
					Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"},
				},
			)
			if err != nil {
				return nil, fmt.Errorf("failed to parse service account credentials: %w", err)
			}
			cfg.Credentials = c
		}
		return cfg, nil

	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
}
