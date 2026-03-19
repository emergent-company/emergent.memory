// Package adk provides Google ADK-Go integration for agent workflows.
package adk

import (
	"context"
	"fmt"
	"log/slog"

	"go.uber.org/fx"
	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent/internal/config"
)

// Module provides the ADK ModelFactory as an fx module
var Module = fx.Module("adk",
	fx.Provide(provideModelFactory),
)

// modelFactoryParams holds optional fx dependencies for ModelFactory.
// The CredentialResolver is optional; when not provided (e.g. in tests or
// when provider.Module is not wired), ModelFactory falls back to env-based config.
type modelFactoryParams struct {
	fx.In

	Cfg      *config.Config
	Log      *slog.Logger
	Resolver CredentialResolver `optional:"true"`
}

// CredentialResolver resolves LLM credentials from context.
// It is implemented by domain/provider.CredentialService and is defined here
// as an interface to avoid an import cycle (adk → provider).
type CredentialResolver interface {
	// ResolveForProvider returns the effective credentials for the given provider
	// by evaluating the request context (project ID, org ID) against the hierarchy.
	// Returns nil credentials and an error if no credentials are found.
	ResolveAny(ctx context.Context) (*ResolvedCredential, error)
}

// ResolvedCredential mirrors provider.ResolvedCredential but lives in the adk
// package to avoid the import cycle. It carries exactly the fields needed to
// build a genai.ClientConfig.
type ResolvedCredential struct {
	// IsGoogleAI is true when the credential is a Google AI API key.
	IsGoogleAI bool

	// APIKey is the Google AI API key (non-empty when IsGoogleAI is true).
	APIKey string

	// IsVertexAI is true when the credential is a Vertex AI service account.
	IsVertexAI bool

	// GCPProject is the GCP project ID (non-empty when IsVertexAI is true).
	GCPProject string
	// Location is the GCP region (non-empty when IsVertexAI is true).
	Location string
	// ServiceAccountJSON is the raw JSON key file (may be empty for default creds).
	ServiceAccountJSON string

	// GenerativeModel overrides the default model for this request (may be empty).
	GenerativeModel string
}

// provideModelFactory creates a ModelFactory from the main config and optional
// CredentialResolver. When provider.Module is loaded, the resolver is injected
// automatically via fx optional dependency injection.
func provideModelFactory(p modelFactoryParams) *ModelFactory {
	f := NewModelFactory(&p.Cfg.LLM, p.Log)
	if p.Resolver != nil {
		return f.WithResolver(p.Resolver)
	}
	return f
}

// ModelFactory creates ADK-compatible LLM models from configuration.
//
// It supports two modes:
//  1. Env-based (legacy): credentials are read from cfg.LLMConfig at startup.
//     This is the default when no CredentialResolver is injected.
//  2. Context-evaluating (multi-tenant): credentials are resolved per-request
//     from the execution context via the injected CredentialResolver.
//     The resolver is optional to preserve backward compatibility with tests
//     that construct ModelFactory without a provider service.
type ModelFactory struct {
	cfg      *config.LLMConfig
	resolver CredentialResolver // nil in env-only mode
	log      *slog.Logger
}

// NewModelFactory creates a new ModelFactory using only environment-based config.
// This constructor is used in tests and legacy code paths.
func NewModelFactory(cfg *config.LLMConfig, log *slog.Logger) *ModelFactory {
	return &ModelFactory{
		cfg: cfg,
		log: log,
	}
}

// WithResolver returns a new ModelFactory that resolves credentials from context
// via the provided CredentialResolver, falling back to env-based config when
// the resolver returns no credentials.
func (f *ModelFactory) WithResolver(resolver CredentialResolver) *ModelFactory {
	return &ModelFactory{
		cfg:      f.cfg,
		resolver: resolver,
		log:      f.log,
	}
}

// CreateModel creates an ADK-compatible Gemini model.
//
// Resolution order (when a resolver is configured):
//  1. Context-resolved credentials (project/org hierarchy)
//  2. Environment variables (GOOGLE_API_KEY / GCP_PROJECT_ID + VERTEX_AI_LOCATION)
//
// If no resolver is configured, only env-based credentials are used.
func (f *ModelFactory) CreateModel(ctx context.Context) (model.LLM, error) {
	return f.CreateModelWithName(ctx, f.defaultModelName(ctx))
}

// CreateModelWithName creates an ADK-compatible Gemini model with a specific model name.
//
// Backend selection (evaluated per-request):
//   - If context provides Vertex AI credentials → Vertex AI
//   - Else if context provides Google AI API key → Google AI
//   - Else if GCP_PROJECT_ID + VERTEX_AI_LOCATION env vars set → Vertex AI (env fallback)
//   - Else if GOOGLE_API_KEY env var set → Google AI (env fallback)
//   - Otherwise → actionable error
func (f *ModelFactory) CreateModelWithName(ctx context.Context, modelName string) (model.LLM, error) {
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}

	// Try context-resolved credentials first
	if f.resolver != nil {
		cred, err := f.resolver.ResolveAny(ctx)
		if err == nil && cred != nil {
			llm, err := f.createFromResolvedCredential(ctx, cred, modelName)
			if err != nil {
				// Resolved credentials exist but client creation failed — surface the error
				return nil, fmt.Errorf("failed to create model from resolved credentials: %w", err)
			}
			return llm, nil
		}
		// Log that resolver returned nothing and we're falling back to env
		if err != nil {
			f.log.Debug("credential resolver returned error, falling back to env-based credentials",
				slog.String("error", err.Error()),
			)
		}
	}

	// Fall back to environment variable credentials
	return f.createFromEnvConfig(ctx, modelName)
}

// createFromResolvedCredential builds a genai client from context-resolved credentials.
func (f *ModelFactory) createFromResolvedCredential(ctx context.Context, cred *ResolvedCredential, modelName string) (model.LLM, error) {
	// Honour per-request model override if set
	effectiveModel := modelName
	if cred.GenerativeModel != "" && effectiveModel == f.cfg.Model {
		// Only substitute the default model; explicit overrides take precedence
		effectiveModel = cred.GenerativeModel
	}

	if cred.IsVertexAI {
		clientCfg := &genai.ClientConfig{
			Backend:  genai.BackendVertexAI,
			Project:  cred.GCPProject,
			Location: cred.Location,
		}
		f.log.Debug("creating ADK Gemini model via Vertex AI (context-resolved)",
			slog.String("model", effectiveModel),
			slog.String("project", cred.GCPProject),
			slog.String("location", cred.Location),
		)
		return gemini.NewModel(ctx, effectiveModel, clientCfg)
	}

	if cred.IsGoogleAI && cred.APIKey != "" {
		clientCfg := &genai.ClientConfig{
			Backend: genai.BackendGeminiAPI,
			APIKey:  cred.APIKey,
		}
		f.log.Debug("creating ADK Gemini model via Google AI (context-resolved)",
			slog.String("model", effectiveModel),
		)
		return gemini.NewModel(ctx, effectiveModel, clientCfg)
	}

	return nil, fmt.Errorf("resolved credential has neither Vertex AI nor Google AI fields set")
}

// createFromEnvConfig builds a genai client from environment variable config.
func (f *ModelFactory) createFromEnvConfig(ctx context.Context, modelName string) (model.LLM, error) {
	// Try Vertex AI first (production), then fall back to Google AI API key (standalone/dev)
	if f.cfg.UseVertexAI() {
		clientCfg := &genai.ClientConfig{
			Backend:  genai.BackendVertexAI,
			Project:  f.cfg.GCPProjectID,
			Location: f.cfg.VertexAILocation,
		}
		f.log.Debug("creating ADK Gemini model via Vertex AI (env-based)",
			slog.String("model", modelName),
			slog.String("project", f.cfg.GCPProjectID),
			slog.String("location", f.cfg.VertexAILocation),
		)

		llm, err := gemini.NewModel(ctx, modelName, clientCfg)
		if err == nil {
			return llm, nil
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
		f.log.Debug("creating ADK Gemini model via Google AI (env-based)",
			slog.String("model", modelName),
		)

		llm, err := gemini.NewModel(ctx, modelName, clientCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create Gemini model via Google AI: %w", err)
		}
		return llm, nil
	}

	return nil, fmt.Errorf("no LLM credentials configured. " +
		"Run 'emergent provider set-key <api-key>' to configure Google AI for your organization, " +
		"or set GCP_PROJECT_ID+VERTEX_AI_LOCATION / GOOGLE_API_KEY environment variables as a server-level fallback")
}

// defaultModelName returns the effective default model name, considering any
// per-request model override from the credential resolver.
func (f *ModelFactory) defaultModelName(ctx context.Context) string {
	return f.cfg.Model
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
// In multi-tenant mode, this may be false even when orgs have credentials —
// use CreateModel and inspect the error for actionable messaging.
func (f *ModelFactory) IsEnabled() bool {
	if f.resolver != nil {
		// With a resolver we can potentially always create models
		return true
	}
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
