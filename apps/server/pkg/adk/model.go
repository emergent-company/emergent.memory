// Package adk provides Google ADK-Go integration for agent workflows.
package adk

import (
	"context"
	"fmt"
	"log/slog"

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

// modelFactoryParams allows optional injection of a CredentialResolver and
// ModelWrapper via fx.
type modelFactoryParams struct {
	fx.In

	Cfg      *config.Config
	Log      *slog.Logger
	Resolver CredentialResolver `optional:"true"`
	Wrapper  ModelWrapper       `optional:"true"`
}

// provideModelFactory creates a ModelFactory from the main config, with an
// optional CredentialResolver and ModelWrapper injected by domain/provider.Module.
func provideModelFactory(p modelFactoryParams) *ModelFactory {
	return NewModelFactory(&p.Cfg.LLM, p.Log, p.Resolver, p.Wrapper)
}

// ModelFactory creates ADK-compatible LLM models from configuration.
type ModelFactory struct {
	cfg      *config.LLMConfig
	log      *slog.Logger
	resolver CredentialResolver // optional; nil → env-var-only mode
	wrapper  ModelWrapper       // optional; nil → no usage tracking
}

// NewModelFactory creates a new ModelFactory with the given configuration.
// resolver may be nil for env-var-only setups (tests, local dev without DB creds).
// wrapper may be nil; when provided it wraps every created model with usage tracking.
func NewModelFactory(cfg *config.LLMConfig, log *slog.Logger, resolver CredentialResolver, wrapper ModelWrapper) *ModelFactory {
	return &ModelFactory{
		cfg:      cfg,
		log:      log,
		resolver: resolver,
		wrapper:  wrapper,
	}
}

// CreateModel creates an ADK-compatible Gemini model for Vertex AI.
//
// The model uses Vertex AI backend with the configured GCP project and location.
// If the configuration is missing required fields, an error is returned.
func (f *ModelFactory) CreateModel(ctx context.Context) (model.LLM, error) {
	return f.CreateModelWithName(ctx, f.cfg.Model)
}

// CreateModelWithName creates an ADK-compatible Gemini model with a specific model name.
//
// This allows overriding the default model for specific use cases (e.g., using
// a different model for extraction vs verification).
//
// Credential resolution order:
//  1. If a CredentialResolver is configured, resolve per-request credentials from
//     the DB hierarchy (project → org → env). This is the production path when
//     domain/provider.Module is registered.
//  2. Fall back to static env-var config (GCP_PROJECT_ID+VERTEX_AI_LOCATION or
//     GOOGLE_API_KEY). Used in tests and env-var-only setups.
//
// If a ModelWrapper is configured the returned LLM is wrapped for usage tracking.
func (f *ModelFactory) CreateModelWithName(ctx context.Context, modelName string) (model.LLM, error) {
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}

	// --- 1. DB credential resolution (project/org hierarchy) ---
	if f.resolver != nil {
		cred, err := f.resolver.ResolveAny(ctx)
		if err != nil {
			f.log.Warn("credential resolver returned error, falling back to env config",
				slog.String("error", err.Error()),
			)
		} else if cred != nil {
			// Prefer the DB-stored model if available; fall back to caller's modelName,
			// then to the static config model.
			resolvedModel := cred.GenerativeModel
			if resolvedModel == "" {
				resolvedModel = modelName
			}
			if resolvedModel == "" {
				resolvedModel = f.cfg.Model
			}

			if cred.IsVertexAI {
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
			}

			if cred.IsGoogleAI && cred.APIKey != "" {
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

	// --- 2. Static env-var fallback ---
	// Try Vertex AI first (production), then fall back to Google AI API key (standalone/dev)
	if f.cfg.UseVertexAI() {
		clientCfg := &genai.ClientConfig{
			Backend:  genai.BackendVertexAI,
			Project:  f.cfg.GCPProjectID,
			Location: f.cfg.VertexAILocation,
		}
		f.log.Debug("creating ADK Gemini model via Vertex AI (env config)",
			slog.String("model", modelName),
			slog.String("project", f.cfg.GCPProjectID),
			slog.String("location", f.cfg.VertexAILocation),
		)

		llm, err := gemini.NewModel(ctx, modelName, clientCfg)
		if err == nil {
			return f.wrapModel(llm, "google-vertex"), nil
		}

		// If Vertex AI fails and we have an API key, fall back
		if f.cfg.GoogleAPIKey != "" {
			f.log.Warn("Vertex AI model creation failed, falling back to Google AI API key",
				slog.String("error", err.Error()),
			)
		} else {
			return nil, fmt.Errorf("failed to create Gemini model: %w", err)
		}
	}

	if f.cfg.GoogleAPIKey != "" {
		clientCfg := &genai.ClientConfig{
			Backend: genai.BackendGeminiAPI,
			APIKey:  f.cfg.GoogleAPIKey,
		}
		f.log.Debug("creating ADK Gemini model via Google AI (env config)",
			slog.String("model", modelName),
		)

		llm, err := gemini.NewModel(ctx, modelName, clientCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create Gemini model via Google AI: %w", err)
		}
		return f.wrapModel(llm, "google"), nil
	}

	return nil, fmt.Errorf("no LLM credentials configured: set GCP_PROJECT_ID+VERTEX_AI_LOCATION for Vertex AI, or GOOGLE_API_KEY for Google AI")
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

// ModelName returns the configured default model name.
func (f *ModelFactory) ModelName() string {
	return f.cfg.Model
}

// Helper function for pointer values
func ptrFloat32(v float32) *float32 {
	return &v
}
