// Package adk provides Google ADK-Go integration for agent workflows.
package adk

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"cloud.google.com/go/auth/credentials"
	"go.uber.org/fx"
	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// Module provides the ADK ModelFactory as an fx module
var Module = fx.Module("adk",
	fx.Provide(provideModelFactory),
)

// modelFactoryParams allows optional injection of a CredentialResolver,
// ModelWrapper, and ModelResolver via fx.
type modelFactoryParams struct {
	fx.In

	Cfg           *config.Config
	Log           *slog.Logger
	Resolver      CredentialResolver `optional:"true"`
	Wrapper       ModelWrapper       `optional:"true"`
	ModelResolver ModelResolver      `optional:"true"`
}

// provideModelFactory creates a ModelFactory from the main config, with an
// optional CredentialResolver, ModelWrapper, and ModelResolver injected by
// domain/provider.Module and domain/modelconfig.Module.
func provideModelFactory(p modelFactoryParams) *ModelFactory {
	return NewModelFactory(&p.Cfg.LLM, p.Log, p.Resolver, p.Wrapper, p.ModelResolver)
}

// ModelFactory creates ADK-compatible LLM models from configuration.
type ModelFactory struct {
	cfg           *config.LLMConfig
	log           *slog.Logger
	resolver      CredentialResolver // optional; nil → env-var-only mode
	wrapper       ModelWrapper       // optional; nil → no usage tracking
	modelResolver ModelResolver      // optional; nil → env default only
}

// NewModelFactory creates a new ModelFactory with the given configuration.
// resolver may be nil for env-var-only setups (tests, local dev without DB creds).
// wrapper may be nil; when provided it wraps every created model with usage tracking.
// modelResolver may be nil; when provided it resolves the effective model name per project.
func NewModelFactory(cfg *config.LLMConfig, log *slog.Logger, resolver CredentialResolver, wrapper ModelWrapper, modelResolver ModelResolver) *ModelFactory {
	return &ModelFactory{
		cfg:           cfg,
		log:           log,
		resolver:      resolver,
		wrapper:       wrapper,
		modelResolver: modelResolver,
	}
}

// CreateModel creates an ADK-compatible LLM model.
//
// Model resolution order:
//  1. ModelResolver.ResolveGenerativeModelByID — project → org DB chain.
//     If the resolved model is empty (no DB config) an error is returned.
//  2. If no ModelResolver is wired (tests / env-var-only mode), falls back to
//     the first configured env-var model (DEEPSEEK_MODEL → OPENAI_MODEL →
//     VERTEX_AI_MODEL). If none are set, returns ErrNoModelConfigured.
//
// The resolved model name must include a provider prefix (e.g. "deepseek/deepseek-v4-flash").
func (f *ModelFactory) CreateModel(ctx context.Context) (model.LLM, error) {
	if f.modelResolver != nil {
		projectID := ProjectIDFromContext(ctx)
		if projectID == "" {
			return nil, fmt.Errorf("no project ID in context — cannot resolve generative model")
		}
		resolved, source, err := f.modelResolver.ResolveGenerativeModelByID(ctx, projectID)
		if err != nil {
			return nil, fmt.Errorf("model resolver error for project %s: %w", projectID, err)
		}
		if resolved == "" {
			return nil, fmt.Errorf("no generative model configured for project %s — set a model via the project model-config API", projectID)
		}
		f.log.Debug("resolved generative model",
			slog.String("model", resolved),
			slog.String("source", source),
			slog.String("projectID", projectID),
		)
		return f.CreateModelWithName(ctx, resolved)
	}

	// No DB resolver — fall back to env vars (used in tests and env-var-only mode).
	envModel := f.ModelName()
	if envModel == "" {
		return nil, fmt.Errorf("no generative model configured: set DEEPSEEK_MODEL, OPENAI_MODEL, or VERTEX_AI_MODEL")
	}
	return f.CreateModelWithName(ctx, envModel)
}

// CreateModelWithName creates an ADK-compatible Gemini model with a specific model name.
//
// modelName MUST include a provider prefix (e.g. "deepseek/deepseek-v4-flash",
// "openai/gpt-4o", "google/gemini-2.5-flash", "google-vertex/gemini-2.5-flash").
// Bare model names without a slash are rejected.
//
// Credential resolution order:
//  1. If a CredentialResolver is configured, resolve per-request credentials from
//     the DB hierarchy (project → org → env). This is the production path when
//     domain/provider.Module is registered.
//  2. Fall back to per-provider env vars:
//     DeepSeek: DEEPSEEK_API_KEY
//     OpenAI:   OPENAI_API_KEY (+ optional OPENAI_BASE_URL)
//     Google:   GOOGLE_API_KEY
//     Vertex:   GCP_PROJECT_ID + VERTEX_AI_LOCATION
//
// If a ModelWrapper is configured the returned LLM is wrapped for usage tracking.
func (f *ModelFactory) CreateModelWithName(ctx context.Context, modelName string) (model.LLM, error) {
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}

	// Require provider prefix: "provider/model-name"
	providerHint, bareModel, hasPrefix := strings.Cut(modelName, "/")
	if !hasPrefix {
		return nil, fmt.Errorf("model name %q must include a provider prefix (e.g. deepseek/deepseek-v4-flash, openai/gpt-4o, google/gemini-2.5-flash)", modelName)
	}

	// --- 1. DB credential resolution (project/org hierarchy) ---
	if f.resolver != nil {
		var cred *ResolvedCredential
		var err error
		cred, err = f.resolver.ResolveFor(ctx, providerHint)
		if err != nil {
			// Only fall through to env-var config when env vars are actually
			// configured. Otherwise the credential error would be masked.
			hasEnvFallback := f.cfg.UseVertexAI() || f.cfg.GoogleAPIKey != "" ||
				f.cfg.DeepSeekAPIKey != "" || f.cfg.OpenAIAPIKey != ""
			if hasEnvFallback {
				f.log.Warn("credential resolver returned error, falling back to env config",
					slog.String("error", err.Error()),
				)
			} else {
				return nil, fmt.Errorf("LLM credential resolution failed: %w", err)
			}
		} else if cred != nil {
			// Explicit provider+model from caller — always honour bareModel.
			// Fall back to DB-stored model if caller somehow passed empty bare portion.
			resolvedModel := bareModel
			if resolvedModel == "" {
				resolvedModel = cred.GenerativeModel
			}
			if resolvedModel == "" {
				return nil, fmt.Errorf("no model configured: model name has no model portion and provider credential has no generative model stored")
			}

			switch cred.Provider {
			case "openai", "deepseek":
				f.log.Debug("creating ADK model via OpenAI-protocol endpoint (DB cred)",
					slog.String("model", resolvedModel),
					slog.String("baseURL", cred.BaseURL),
					slog.String("provider", cred.Provider),
					slog.String("source", cred.Source),
				)
				llm := NewOpenAICompatibleModel(cred.BaseURL, cred.APIKey, resolvedModel)
				return f.wrapModel(llm, cred.Provider), nil
			case "google-vertex":
				clientCfg := &genai.ClientConfig{
					Backend:  genai.BackendVertexAI,
					Project:  cred.GCPProject,
					Location: cred.Location,
				}
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
				f.log.Debug("creating ADK Gemini model via Vertex AI (DB cred)",
					slog.String("model", resolvedModel),
					slog.String("project", cred.GCPProject),
					slog.String("location", cred.Location),
					slog.String("source", cred.Source),
				)
				llm, err := gemini.NewModel(ctx, resolvedModel, clientCfg)
				if err != nil {
					return nil, fmt.Errorf("failed to create Gemini model via Vertex AI (DB cred): %w", err)
				}
				return f.wrapModel(llm, "google-vertex"), nil
			case "google":
				clientCfg := &genai.ClientConfig{
					Backend: genai.BackendGeminiAPI,
					APIKey:  cred.APIKey,
				}
				f.log.Debug("creating ADK Gemini model via Google AI (DB cred)",
					slog.String("model", resolvedModel),
					slog.String("source", cred.Source),
				)
				llm, err := gemini.NewModel(ctx, resolvedModel, clientCfg)
				if err != nil {
					return nil, fmt.Errorf("failed to create Gemini model via Google AI (DB cred): %w", err)
				}
				return f.wrapModel(llm, "google"), nil
			}
		}
		// cred == nil means no DB credential found — fall through to env vars
	}

	// --- 2. Per-provider env-var fallback ---
	switch providerHint {
	case "deepseek":
		return f.createDeepSeekEnv(bareModel)
	case "openai":
		return f.createOpenAIEnv(bareModel)
	case "google-vertex":
		return f.createVertexAIEnv(ctx, bareModel)
	case "google":
		return f.createGoogleAIEnv(ctx, bareModel)
	default:
		return nil, fmt.Errorf("unsupported provider %q in model name %q — supported providers: deepseek, openai, google, google-vertex", providerHint, modelName)
	}
}

// createDeepSeekEnv creates a model from DEEPSEEK_API_KEY env var.
func (f *ModelFactory) createDeepSeekEnv(bareModel string) (model.LLM, error) {
	if f.cfg.DeepSeekAPIKey == "" {
		return nil, fmt.Errorf("DEEPSEEK_API_KEY is not set")
	}
	if bareModel == "" {
		// Strip prefix from DeepSeekModel env var if it was set with provider prefix
		m := f.cfg.DeepSeekModel
		if _, stripped, ok := strings.Cut(m, "/"); ok {
			m = stripped
		}
		bareModel = m
	}
	if bareModel == "" {
		return nil, fmt.Errorf("model name is required: set DEEPSEEK_MODEL (e.g. deepseek/deepseek-v4-flash)")
	}
	f.log.Debug("creating ADK model via DeepSeek (env config)", slog.String("model", bareModel))
	llm := NewOpenAICompatibleModel("https://api.deepseek.com/v1", f.cfg.DeepSeekAPIKey, bareModel)
	return f.wrapModel(llm, "deepseek"), nil
}

// createOpenAIEnv creates a model from OPENAI_API_KEY env var.
func (f *ModelFactory) createOpenAIEnv(bareModel string) (model.LLM, error) {
	if f.cfg.OpenAIAPIKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is not set")
	}
	if bareModel == "" {
		m := f.cfg.OpenAIModel
		if _, stripped, ok := strings.Cut(m, "/"); ok {
			m = stripped
		}
		bareModel = m
	}
	if bareModel == "" {
		return nil, fmt.Errorf("model name is required: set OPENAI_MODEL (e.g. openai/gpt-4o)")
	}
	baseURL := f.cfg.OpenAIBaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	f.log.Debug("creating ADK model via OpenAI (env config)",
		slog.String("model", bareModel),
		slog.String("baseURL", baseURL),
	)
	llm := NewOpenAICompatibleModel(baseURL, f.cfg.OpenAIAPIKey, bareModel)
	return f.wrapModel(llm, "openai"), nil
}

// createVertexAIEnv creates a model from env-var Vertex AI config.
func (f *ModelFactory) createVertexAIEnv(ctx context.Context, bareModel string) (model.LLM, error) {
	clientCfg := &genai.ClientConfig{
		Backend:  genai.BackendVertexAI,
		Project:  f.cfg.GCPProjectID,
		Location: f.cfg.VertexAILocation,
	}
	f.log.Debug("creating ADK Gemini model via Vertex AI (env config)",
		slog.String("model", bareModel),
		slog.String("project", f.cfg.GCPProjectID),
		slog.String("location", f.cfg.VertexAILocation),
	)
	llm, err := gemini.NewModel(ctx, bareModel, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini model: %w", err)
	}
	return f.wrapModel(llm, "google-vertex"), nil
}

// createGoogleAIEnv creates a model from env-var Google AI API key config.
func (f *ModelFactory) createGoogleAIEnv(ctx context.Context, bareModel string) (model.LLM, error) {
	clientCfg := &genai.ClientConfig{
		Backend: genai.BackendGeminiAPI,
		APIKey:  f.cfg.GoogleAPIKey,
	}
	f.log.Debug("creating ADK Gemini model via Google AI (env config)",
		slog.String("model", bareModel),
	)
	llm, err := gemini.NewModel(ctx, bareModel, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini model via Google AI: %w", err)
	}
	return f.wrapModel(llm, "google"), nil
}

// wrapModel applies the optional ModelWrapper to the given LLM.
// If no wrapper is configured the model is returned unchanged.
func (f *ModelFactory) wrapModel(llm model.LLM, provider string) model.LLM {
	if f.wrapper == nil {
		return llm
	}
	return f.wrapper.WrapModel(llm, provider)
}

// DefaultGenerateConfig returns a default GenerateContentConfig for extraction tasks.
//
// The config uses low temperature for deterministic JSON extraction output.
func (f *ModelFactory) DefaultGenerateConfig() *genai.GenerateContentConfig {
	return &genai.GenerateContentConfig{
		Temperature:     ptrFloat32(float32(f.cfg.Temperature)),
		MaxOutputTokens: int32(f.cfg.MaxOutputTokens),
	}
}

// ExtractionGenerateConfig returns a GenerateContentConfig optimized for entity extraction.
//
// Uses temperature=0 for deterministic structured output.
func (f *ModelFactory) ExtractionGenerateConfig() *genai.GenerateContentConfig {
	return &genai.GenerateContentConfig{
		Temperature:     ptrFloat32(0.0),
		MaxOutputTokens: int32(f.cfg.MaxOutputTokens),
	}
}

// ExtractionGenerateConfigWithSchema returns a GenerateContentConfig with ResponseSchema
// for guaranteed structured JSON output. The schema constrains LLM output format.
//
// This is ~35% faster than embedding schema in the prompt and guarantees valid JSON.
func (f *ModelFactory) ExtractionGenerateConfigWithSchema(schema *genai.Schema) *genai.GenerateContentConfig {
	return &genai.GenerateContentConfig{
		Temperature:      ptrFloat32(0.0),
		MaxOutputTokens:  int32(f.cfg.MaxOutputTokens),
		ResponseMIMEType: "application/json",
		ResponseSchema:   schema,
	}
}

// IsEnabled returns true if the LLM configuration is valid for creating models.
func (f *ModelFactory) IsEnabled() bool {
	return f.cfg.IsEnabled()
}

// ModelName returns the first configured model name across all env-var providers.
// Priority: DEEPSEEK_MODEL → OPENAI_MODEL → VERTEX_AI_MODEL.
// Returns "" if no provider is configured via env vars.
// Used primarily in tests and env-var-only mode; production resolves models via DB.
func (f *ModelFactory) ModelName() string {
	if f.cfg.DeepSeekAPIKey != "" && f.cfg.DeepSeekModel != "" {
		return f.cfg.DeepSeekModel
	}
	if f.cfg.OpenAIAPIKey != "" && f.cfg.OpenAIModel != "" {
		return f.cfg.OpenAIModel
	}
	if f.cfg.Model != "" {
		return f.cfg.Model
	}
	return ""
}

// Helper function for pointer values
func ptrFloat32(v float32) *float32 {
	return &v
}
