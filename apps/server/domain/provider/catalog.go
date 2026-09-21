package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/auth/credentials"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/pkg/embeddings/openai"
	"github.com/emergent-company/emergent.memory/pkg/embeddings/vertex"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// modelCatalogRepo is the subset of Repository the model catalog service needs.
// It is declared as an interface (satisfied by *Repository) so tests can
// substitute an in-memory fake without a database.
type modelCatalogRepo interface {
	UpsertSupportedModels(ctx context.Context, models []ProviderSupportedModel) error
	DeleteSupportedModelsNotIn(ctx context.Context, provider ProviderType, modelNames []string) error
	ListSupportedModels(ctx context.Context, provider ProviderType, modelType *ModelType) ([]ProviderSupportedModel, error)
	ListAllSupportedModels(ctx context.Context, modelType *ModelType) ([]ProviderSupportedModel, error)
}

// ModelCatalogService fetches and caches available models from provider APIs.
type ModelCatalogService struct {
	repo modelCatalogRepo
	log  *slog.Logger
}

// NewModelCatalogService creates a new ModelCatalogService.
func NewModelCatalogService(repo *Repository, log *slog.Logger) *ModelCatalogService {
	return &ModelCatalogService{
		repo: repo,
		log:  log.With(logger.Scope("provider.catalog")),
	}
}

// SyncModels resolves the model catalog for the provider using the given
// credentials and persists it to the provider_supported_models cache.
//
// Persistence is: upsert the resolved rows, then prune stale rows for the
// provider — but only when the resolved set came from a complete, live catalog
// fetch. When an OpenAI-compatible /v1/models fetch fails and a configured-model
// fallback is used instead, the resolved set is a partial snapshot, so the sync
// upserts only and never prunes: pruning a partial snapshot would delete rows
// belonging to other configurations of the same provider.
func (s *ModelCatalogService) SyncModels(ctx context.Context, provider ProviderType, cred *ResolvedCredential) error {
	models, pruneOK, err := s.ResolveModels(ctx, provider, cred)
	if err != nil {
		return err
	}
	if len(models) == 0 {
		return fmt.Errorf("no models available for provider %s", provider)
	}

	if err := s.repo.UpsertSupportedModels(ctx, models); err != nil {
		return err
	}

	if !pruneOK {
		// Fallback snapshot: never prune. Stale-but-complete is strictly
		// better than wiping the catalog down to the fallback rows.
		return nil
	}

	// Remove any stale rows for this provider that were not returned by the
	// current sync. This handles: model renames, retired models, and stale
	// static-fallback rows left over from a previous failed API call.
	names := modelNames(models)
	if err := s.repo.DeleteSupportedModelsNotIn(ctx, provider, names); err != nil {
		// Non-fatal: stale rows are cosmetic, don't fail the whole sync.
		s.log.Warn("failed to delete stale models after sync",
			logger.Error(err),
			slog.String("provider", string(provider)),
		)
	}

	return nil
}

// ResolveModels fetches the model catalog from the provider API using the given
// credentials without persisting anything.
//
// It returns the resolved rows plus a pruneOK flag: pruneOK is true only when
// the rows are a complete, authoritative view of the provider's catalog, so
// callers may safely delete cached rows not present in the set. When an
// OpenAI-compatible /v1/models fetch fails the function falls back to the
// statically configured models and returns pruneOK=false — callers must then
// upsert only, never prune, because the fallback snapshot is partial.
//
// The callers that combine multiple resolved sets (e.g. the periodic resync of
// every OpenAI-compatible config) must union all configs before pruning against
// the union, since the catalog table is keyed globally by (provider, model_name)
// with no per-config dimension.
func (s *ModelCatalogService) ResolveModels(ctx context.Context, provider ProviderType, cred *ResolvedCredential) (models []ProviderSupportedModel, pruneOK bool, err error) {
	pruneOK = true

	switch provider {
	case ProviderOpenAI:
		// OpenAI-compatible (incl. LiteLLM proxies): list the full model set
		// exposed by GET {base_url}/models. Fall back to the configured model(s)
		// when the proxy's model list is unreachable, so the user's selection is
		// never lost. The fallback is a partial snapshot of this single config,
		// so it must never drive a prune.
		fetched, fetchErr := s.fetchOpenAICompatibleModels(ctx, provider, cred)
		if fetchErr != nil {
			s.log.Warn("openai-compatible: /v1/models fetch failed, storing configured models only", logger.Error(fetchErr))
			fetched = s.configuredOpenAIModels(provider, cred)
			pruneOK = false
		}
		models = fetched

	case ProviderDeepSeek:
		// DeepSeek: use static model list (no live catalog fetch — the fixed
		// endpoint lists only deepseek-chat/deepseek-reasoner and would prune
		// the alfred-specific aliases). The static list is authoritative, so
		// pruning is safe.
		models = staticModels(provider)

	default:
		fetched, fetchErr := s.fetchModelsFromAPI(ctx, provider, cred)
		if fetchErr != nil {
			return nil, false, fmt.Errorf("failed to fetch model catalog from %s API: %w", provider, fetchErr)
		}
		models = fetched
	}

	if len(models) == 0 {
		return nil, false, fmt.Errorf("no models available for provider %s", provider)
	}

	return models, pruneOK, nil
}

// modelNames extracts the bare model names from a set of catalog rows, in the
// same order, for use with DeleteSupportedModelsNotIn.
func modelNames(models []ProviderSupportedModel) []string {
	names := make([]string, len(models))
	for i, m := range models {
		names[i] = m.ModelName
	}
	return names
}

// ListModels returns the cached supported models for a provider,
// optionally filtered by model type (embedding or generative).
func (s *ModelCatalogService) ListModels(ctx context.Context, provider ProviderType, modelType *ModelType) ([]ProviderSupportedModel, error) {
	return s.repo.ListSupportedModels(ctx, provider, modelType)
}

// ListAllModels returns the cached model catalog across all providers,
// optionally filtered by model type.
func (s *ModelCatalogService) ListAllModels(ctx context.Context, modelType *ModelType) ([]ProviderSupportedModel, error) {
	return s.repo.ListAllSupportedModels(ctx, modelType)
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

// fetchOpenAICompatibleModels lists every model exposed by an OpenAI-compatible
// endpoint (including LiteLLM proxies) via GET {base_url}/models. Each entry is
// classified generative/embedding by name heuristic and carries context-window
// limits when the server advertises them (llama.cpp context_length or vLLM
// max_model_len). The configured generative + embedding models are always
// appended (deduped), so the catalog never loses the user's selected models.
func (s *ModelCatalogService) fetchOpenAICompatibleModels(ctx context.Context, provider ProviderType, cred *ResolvedCredential) ([]ProviderSupportedModel, error) {
	if cred.BaseURL == "" {
		return nil, fmt.Errorf("openai-compatible provider requires base_url")
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	url := strings.TrimSuffix(cred.BaseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build /v1/models request: %w", err)
	}
	if cred.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cred.APIKey)
	}

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("/v1/models request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("/v1/models returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var body struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"` // llama.cpp field
			MaxModelLen   int    `json:"max_model_len"`  // vLLM field
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("failed to decode /v1/models response: %w", err)
	}

	models := make([]ProviderSupportedModel, 0, len(body.Data)+2)
	seen := map[string]bool{}
	add := func(name string, mt ModelType, ctxLen int) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		in, out := openAICompatibleDefaultInputTokens, openAICompatibleDefaultOutputTokens
		if ctxLen > 0 {
			in = ctxLen
			out = max(openAICompatibleDefaultOutputTokens, ctxLen/2)
		}
		models = append(models, ProviderSupportedModel{
			Provider:        provider,
			ModelName:       name,
			ModelType:       mt,
			DisplayName:     displayNameForOpenAICompatible(name),
			MaxInputTokens:  &in,
			MaxOutputTokens: &out,
		})
	}

	for _, m := range body.Data {
		mt := ModelTypeGenerative
		if nameLooksEmbedding(m.ID) {
			mt = ModelTypeEmbedding
		}
		ctxLen := m.ContextLength
		if ctxLen == 0 {
			ctxLen = m.MaxModelLen
		}
		add(m.ID, mt, ctxLen)
	}

	// Ensure the configured models are present even if the proxy omits them.
	add(cred.GenerativeModel, ModelTypeGenerative, 0)
	add(cred.EmbeddingModel, ModelTypeEmbedding, 0)

	if len(models) == 0 {
		return nil, fmt.Errorf("no models returned by /v1/models")
	}

	s.log.Info("openai-compatible: fetched model catalog",
		slog.Int("models", len(models)),
	)
	return models, nil
}

// configuredOpenAIModels builds a fallback catalog from the explicitly
// configured generative/embedding models (used when the /v1/models list is
// unreachable). It dedupes by model name.
func (s *ModelCatalogService) configuredOpenAIModels(provider ProviderType, cred *ResolvedCredential) []ProviderSupportedModel {
	var models []ProviderSupportedModel
	seen := map[string]bool{}
	add := func(name string, mt ModelType) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		models = append(models, ProviderSupportedModel{
			Provider:    provider,
			ModelName:   name,
			ModelType:   mt,
			DisplayName: displayNameForOpenAICompatible(name),
		})
	}
	add(cred.GenerativeModel, ModelTypeGenerative)
	add(cred.EmbeddingModel, ModelTypeEmbedding)
	return models
}

// displayNameForOpenAICompatible strips a LiteLLM "vendor/model" prefix for a
// friendlier display name while keeping the full ID as the stored model name.
func displayNameForOpenAICompatible(name string) string {
	if i := strings.Index(name, "/"); i != -1 {
		return name[i+1:]
	}
	return name
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

	if p == ProviderDeepSeek {
		known = []entry{
			{"deepseek-v4-flash", "DeepSeek V4 Flash", ModelTypeGenerative},
			{"deepseek-v4-pro", "DeepSeek V4 Pro", ModelTypeGenerative},
			{"deepseek-chat", "DeepSeek Chat", ModelTypeGenerative},
			{"deepseek-reasoner", "DeepSeek Reasoner", ModelTypeGenerative},
		}
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

	// If the resolved model is actually an embedding model, test it via the
	// embeddings endpoint instead of generateContent. Providers like Vertex AI
	// reject generateContent for embedding models with a misleading error
	// ("text-embedding-004 ... is not supported for generateContent"), causing
	// false failures when an embedding model ends up as the tested model.
	if s.modelIsEmbedding(ctx, provider, model) {
		embModel, embErr := s.embedContentForModel(ctx, provider, cred, model)
		if embErr != nil {
			return "", "", fmt.Errorf("embedding model test failed: %w", embErr)
		}
		return embModel, "(ok)", nil
	}

	reply, err = s.generateContentForModel(ctx, provider, cred, model)
	if err != nil {
		return "", "", err
	}
	return model, reply, nil
}

// TestGenerateForModel runs the generate test against an explicit model name,
// bypassing the configured-model resolution in TestGenerate. It preserves the
// same embedding-model auto-detection: an embedding model is exercised through
// the embed endpoint rather than generateContent. Returns the LLM's reply text.
func (s *ModelCatalogService) TestGenerateForModel(ctx context.Context, provider ProviderType, cred *ResolvedCredential, model string) (reply string, err error) {
	if s.modelIsEmbedding(ctx, provider, model) {
		_, embErr := s.embedContentForModel(ctx, provider, cred, model)
		if embErr != nil {
			return "", fmt.Errorf("embedding model test failed: %w", embErr)
		}
		return "(ok)", nil
	}
	return s.generateContentForModel(ctx, provider, cred, model)
}

// generateContentForModel runs the raw generate call against an explicit model
// name. OpenAI-compatible and DeepSeek use a direct HTTP call; Google/Vertex use
// the genai client. Returns the LLM's reply text.
func (s *ModelCatalogService) generateContentForModel(ctx context.Context, provider ProviderType, cred *ResolvedCredential, model string) (reply string, err error) {
	// OpenAI-compatible and DeepSeek: use direct HTTP call instead of genai client.
	if provider == ProviderOpenAI || provider == ProviderDeepSeek {
		if cred.BaseURL == "" {
			return "", fmt.Errorf("openai-compatible provider requires base_url")
		}
		reqBody := map[string]interface{}{
			"model": model,
			"messages": []map[string]string{
				{"role": "user", "content": "Say hello in one sentence."},
			},
			"max_tokens": 64,
		}
		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return "", fmt.Errorf("failed to marshal request: %w", err)
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			strings.TrimSuffix(cred.BaseURL, "/")+"/chat/completions",
			bytes.NewReader(bodyBytes))
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if cred.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+cred.APIKey)
		}
		httpClient := &http.Client{Timeout: 30 * time.Second}
		resp, err := httpClient.Do(httpReq)
		if err != nil {
			return "", fmt.Errorf("openai-compatible generate call failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("openai-compatible generate call returned %d: %s", resp.StatusCode, string(body))
		}
		var result struct {
			Choices []struct {
				Message struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return "", fmt.Errorf("failed to decode openai-compatible response: %w", err)
		}
		if len(result.Choices) == 0 {
			return "", fmt.Errorf("openai-compatible response had no choices")
		}
		reply := result.Choices[0].Message.Content
		if reply == "" {
			reply = result.Choices[0].Message.ReasoningContent
		}
		if reply == "" {
			reply = "(ok)" // model responded with no text content but no error
		}
		return reply, nil
	}

	clientCfg, err := buildClientConfig(provider, cred)
	if err != nil {
		return "", err
	}

	client, err := genai.NewClient(ctx, clientCfg)
	if err != nil {
		return "", fmt.Errorf("failed to create genai client: %w", err)
	}

	resp, err := client.Models.GenerateContent(ctx, model, genai.Text("Say hello in one sentence."), nil)
	if err != nil {
		return "", fmt.Errorf("generate call failed: %w", err)
	}

	return resp.Text(), nil
}

// TestEmbed sends a single embed call to verify embedding credentials and model
// work end-to-end. It uses the configured embedding model from the resolved
// credential when available, otherwise falls back to a known default.
// Returns the model name used.
func (s *ModelCatalogService) TestEmbed(ctx context.Context, provider ProviderType, cred *ResolvedCredential) (model string, err error) {
	// Use the configured embedding model or pick from catalog. Error if none available.
	model = cred.EmbeddingModel
	if model == "" {
		embType := ModelTypeEmbedding
		models, listErr := s.repo.ListSupportedModels(ctx, provider, &embType)
		if listErr != nil || len(models) == 0 {
			return "", fmt.Errorf("no embedding model configured for provider %s — configure an embedding model explicitly", provider)
		}
		model = models[0].ModelName
	}
	return s.embedContentForModel(ctx, provider, cred, model)
}

// TestEmbedForModel runs the embed test against an explicit model name,
// bypassing the configured-model resolution in TestEmbed. It delegates to the
// same per-model embed path used by TestEmbed. Returns the verified model name.
func (s *ModelCatalogService) TestEmbedForModel(ctx context.Context, provider ProviderType, cred *ResolvedCredential, model string) (string, error) {
	return s.embedContentForModel(ctx, provider, cred, model)
}

// embedContentForModel sends a single embed call for a specific model name to
// verify credentials and model work end-to-end. It is shared by TestEmbed and
// by TestGenerate when the target model turns out to be an embedding model.
// Returns the model name used.
func (s *ModelCatalogService) embedContentForModel(ctx context.Context, provider ProviderType, cred *ResolvedCredential, model string) (string, error) {
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

	case ProviderOpenAI:
		// OpenAI-compatible providers (including LiteLLM proxies): use the
		// OpenAI-compatible embeddings client to verify the target embedding
		// model works end-to-end.
		if cred.BaseURL == "" {
			return "", fmt.Errorf("embedding model test failed: openai-compatible provider requires base_url")
		}
		if model == "" {
			return "", fmt.Errorf("embedding model test failed: openai-compatible provider requires an embedding model")
		}
		client, clientErr := openai.NewClient(openai.Config{
			APIKey:  cred.APIKey,
			BaseURL: cred.BaseURL,
			Model:   model,
		})
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
		return model, nil

	case ProviderDeepSeek:
		// DeepSeek has no embedding API.
		return "not supported", nil

	default:
		return "", fmt.Errorf("unsupported provider for embedding test: %s", provider)
	}

	return model, nil
}

// modelIsEmbedding reports whether the given model name is an embedding model.
// It consults the synced catalog (the authoritative classification) and falls
// back to a name-based heuristic when the catalog lookup fails or is empty
// (e.g. no sync has happened yet).
func (s *ModelCatalogService) modelIsEmbedding(ctx context.Context, provider ProviderType, model string) bool {
	if nameLooksEmbedding(model) {
		return true
	}
	embType := ModelTypeEmbedding
	models, err := s.repo.ListSupportedModels(ctx, provider, &embType)
	if err != nil {
		return false
	}
	for _, m := range models {
		if m.ModelName == model {
			return true
		}
	}
	return false
}

// nameLooksEmbedding applies a cheap name-based heuristic for detecting
// embedding models. Covers the common Google/Vertex and OpenAI families.
func nameLooksEmbedding(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "embedding") || strings.Contains(lower, "text-embed")
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

	case ProviderOpenAI:
		return nil, fmt.Errorf("openai-compatible provider does not use genai client — use the HTTP adapter directly")

	case ProviderDeepSeek:
		return nil, fmt.Errorf("deepseek provider does not use genai client — use the HTTP adapter directly")

	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
}

const (
	// openAICompatibleDefaultInputTokens is used when the server does not
	// advertise a context_length via /v1/models (e.g. non-llama.cpp servers,
	// unavailable server at config time). Conservative for large GGUF models.
	openAICompatibleDefaultInputTokens = 32_768
	// openAICompatibleDefaultOutputTokens is a safe default max-output budget.
	openAICompatibleDefaultOutputTokens = 8_192
)
