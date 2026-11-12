// Package adk provides Google ADK-Go integration for agent workflows.
package adk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"cloud.google.com/go/auth/credentials"
	"go.uber.org/fx"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/modelref"
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
	TestLLM       TestLLMChecker     `optional:"true"`
	EnvVendors    EnvVendorResolver  `optional:"true"`
}

// provideModelFactory creates a ModelFactory from the main config, with an
// optional CredentialResolver, ModelWrapper, and ModelResolver injected by
// domain/provider.Module and domain/modelconfig.Module.
func provideModelFactory(p modelFactoryParams) *ModelFactory {
	f := NewModelFactory(&p.Cfg.LLM, p.Log, p.Resolver, p.Wrapper, p.ModelResolver)
	if p.TestLLM != nil {
		f.WithTestLLMChecker(p.TestLLM)
	}
	if p.EnvVendors != nil {
		f.WithEnvVendorResolver(p.EnvVendors)
	}
	return f
}

// ModelFactory creates ADK-compatible LLM models from configuration.
type ModelFactory struct {
	cfg           *config.LLMConfig
	log           *slog.Logger
	resolver      CredentialResolver  // optional; nil → env-var-only mode
	wrapper       ModelWrapper        // optional; nil → no usage tracking
	modelResolver ModelResolver       // optional; nil → env default only
	testLLM       TestLLMChecker      // optional; nil → no test-LLM mode
	testLLMResp   TestLLMResponse     // optional; zero → the fixed default answer
	envVendors    EnvVendorResolver   // optional; nil → legacy env providers only
	envLookup     func(string) string // optional test seam; nil → os.Getenv
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

// WithTestLLMChecker sets the optional test-LLM flag checker. When set and the
// project has test-LLM mode enabled, CreateModel/CreateModelWithName return a
// canned model instead of a real provider-backed model.
func (f *ModelFactory) WithTestLLMChecker(c TestLLMChecker) {
	f.testLLM = c
}

// WithTestLLMResponse replaces the deterministic test model's fixed answer with
// the given scripted response. It exists so an integration test can carry an
// ```a2ui block (streamed as partial chunks, then as the final response) through
// the real pipeline without a provider. An unset response keeps the fixed
// default text.
func (f *ModelFactory) WithTestLLMResponse(resp TestLLMResponse) {
	f.testLLMResp = resp
}

// WithEnvVendorResolver sets the optional registry-backed vendor resolver used
// by the generic env-var fallback. Without it only the four legacy providers
// can be built from env.
func (f *ModelFactory) WithEnvVendorResolver(r EnvVendorResolver) {
	f.envVendors = r
}

// WithEnvLookup overrides the environment getter used by the generic env-var
// fallback. It exists for tests; production uses os.Getenv.
func (f *ModelFactory) WithEnvLookup(lookup func(string) string) {
	f.envLookup = lookup
}

// maybeTestLLM returns a deterministic test model when the project in ctx has
// test-LLM mode enabled, or nil otherwise.
func (f *ModelFactory) maybeTestLLM(ctx context.Context) model.LLM {
	if f == nil || f.testLLM == nil {
		return nil
	}
	pid := ProjectIDFromContext(ctx)
	if pid == "" {
		return nil
	}
	if f.testLLM.IsTestLLM(ctx, pid) {
		return newTestModel("test-llm", f.testLLMResp)
	}
	return nil
}

// ErrNetworkDisabled is returned by CreateModel and CreateModelWithName when the
// LLM_NETWORK_DISABLED kill switch is on. It is a hard error: while network
// access is disabled, no provider-backed model is constructed from either env
// vars or the DB credential hierarchy.
var ErrNetworkDisabled = errors.New("LLM network access is disabled by LLM_NETWORK_DISABLED")

// networkDisabled reports whether the LLM_NETWORK_DISABLED kill switch is on.
func (f *ModelFactory) networkDisabled() bool {
	return f != nil && f.cfg != nil && f.cfg.NetworkDisabled
}

// CreateModel creates an ADK-compatible LLM model.
//
// Model resolution order:
//  1. ModelResolver.ResolveGenerativeModelByID — project config → provider
//     credential generative model. DB precedence is absolute: when it resolves
//     a model it wins and env is never consulted.
//  2. When the DB hierarchy yields nothing, fall back to env-var configuration:
//     the legacy vars (DEEPSEEK_MODEL → OPENAI_MODEL → VERTEX_AI_MODEL) and then
//     the generic registry vendor vars (<PREFIX>_MODEL). If none are set,
//     returns a "no generative model configured" error.
//  3. If no ModelResolver is wired at all (tests / env-var-only mode), uses the
//     same env-var fallback directly.
//
// The resolved model name must include a provider prefix (e.g. "deepseek/deepseek-v4-flash").
//
// The LLM_NETWORK_DISABLED kill switch takes precedence over both paths: when
// on, CreateModel returns ErrNetworkDisabled without building a model. The
// deterministic test-LLM model (a no-network canned model) still takes
// precedence over the kill switch.
func (f *ModelFactory) CreateModel(ctx context.Context) (model.LLM, error) {
	if f == nil {
		return nil, fmt.Errorf("ModelFactory is nil — no LLM provider configured")
	}
	if llm := f.maybeTestLLM(ctx); llm != nil {
		return llm, nil
	}
	if f.networkDisabled() {
		return nil, ErrNetworkDisabled
	}
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
			// Defensive copy of the provider-config fallback now canonical in
			// modelconfig.Service.ResolveGenerativeModel (via
			// CredentialService.DefaultGenerativeModel). Kept for graph
			// variants where the resolver is wired without that fallback
			// (e.g. tests); behavior must stay identical to it.
			if f.resolver != nil {
				if cred, _ := f.resolver.ResolveAny(ctx); cred != nil && cred.GenerativeModel != "" {
					slug := cred.Slug
					if slug == "" {
						slug = cred.Provider
					}
					// cred.GenerativeModel is already bare: the provider adapter
					// strips the routing prefix on resolution
					// (modelref.StripRoutingPrefix), so the bare name may
					// itself contain slashes (Vertex resource paths) and must
					// not be re-split here.
					name := string(slug) + "/" + cred.GenerativeModel
					f.log.Debug("resolved generative model from provider config fallback",
						slog.String("model", name),
						slog.String("slug", slug),
						slog.String("provider", cred.Provider),
						slog.String("source", cred.Source),
						slog.String("projectID", projectID),
					)
					return f.CreateModelWithName(ctx, name)
				}
			}
			// No DB project/provider model. Fall back to env-var configuration
			// (legacy vars plus generic registry vendors) before erroring, so
			// env-only self-host chat keeps working even though the model
			// resolver module is always wired in production. DB precedence is
			// preserved: this runs only when the DB hierarchy yields nothing.
			if envModel := f.ModelName(); envModel != "" {
				f.log.Debug("resolved generative model from env fallback",
					slog.String("model", envModel),
					slog.String("projectID", projectID),
				)
				return f.CreateModelWithName(ctx, envModel)
			}
			return nil, fmt.Errorf("no generative model configured for project %s — set a model via 'memory projects set-models --generative provider/model' or 'memory provider configure-project <provider> --generative-model <model>'", projectID)
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
		return nil, fmt.Errorf("no generative model configured: set DEEPSEEK_MODEL, OPENAI_MODEL, VERTEX_AI_MODEL, or <PROVIDER>_MODEL (e.g. ANTHROPIC_MODEL)")
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
//     the DB hierarchy (project → env). This is the production path when
//     domain/provider.Module is registered. Org-level provider config is
//     deprecated, so there is no org tier.
//  2. Fall back to per-provider env vars. The four legacy providers keep their
//     historical variables:
//     DeepSeek: DEEPSEEK_API_KEY
//     OpenAI:   OPENAI_API_KEY (+ optional OPENAI_BASE_URL)
//     Google:   GOOGLE_API_KEY
//     Vertex:   GCP_PROJECT_ID + VERTEX_AI_LOCATION
//     Every other registered vendor resolves `<PREFIX>_API_KEY`,
//     `<PREFIX>_BASE_URL`, and `<PREFIX>_MODEL` via the registry-driven
//     resolver (see EnvPrefix).
//
// If a ModelWrapper is configured the returned LLM is wrapped for usage tracking.
//
// The LLM_NETWORK_DISABLED kill switch is a hard gate: when on,
// CreateModelWithName returns ErrNetworkDisabled without resolving credentials
// or building a model. The deterministic test-LLM model is exempt (no network).
func (f *ModelFactory) CreateModelWithName(ctx context.Context, modelName string) (model.LLM, error) {
	if llm := f.maybeTestLLM(ctx); llm != nil {
		return llm, nil
	}
	if f.networkDisabled() {
		return nil, ErrNetworkDisabled
	}
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}

	// Require provider prefix: "provider/model-name". Parsed via the canonical
	// modelref.Parse (strict, split-on-first-slash) so provider identity is not
	// reconstructed here with an ad-hoc string split.
	ref, err := modelref.Parse(modelName)
	if err != nil {
		return nil, fmt.Errorf("model name %q must include a provider prefix (e.g. deepseek/deepseek-v4-flash, openai/gpt-4o, google/gemini-2.5-flash)", modelName)
	}
	providerHint, bareModel := ref.Provider, ref.Model

	// --- 1. DB credential resolution (project only) ---
	if f.resolver != nil {
		var cred *ResolvedCredential
		var err error
		cred, err = f.resolver.ResolveFor(ctx, providerHint)
		if err != nil {
			// Only fall through to env-var config when env vars are actually
			// configured. Otherwise the credential error would be masked.
			if f.hasEnvFallback(providerHint) {
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

			// Resolve the effective protocol, preferring the explicit field and
			// falling back to legacy flags/dialect for compatibility with
			// callers that still populate only the deprecated fields.
			protocol := cred.Protocol
			if protocol == "" {
				switch {
				case cred.IsGoogleAI || cred.Provider == "google",
					cred.IsVertexAI || cred.Provider == "google-vertex":
					protocol = "google-genai"
				case cred.Provider == "anthropic":
					protocol = "anthropic-messages"
				default:
					// Host-aware: first-party api.openai.com selects
					// openai-responses; relays/proxies keep openai-chat.
					protocol = SelectOpenAIProtocol(cred.BaseURL)
				}
			}

			switch protocol {
			case protocolOpenAIResponses:
				f.log.Debug("creating ADK model via OpenAI Responses endpoint (DB cred)",
					slog.String("model", resolvedModel),
					slog.String("baseURL", cred.BaseURL),
					slog.String("provider", cred.Provider),
					slog.String("protocol", protocol),
					slog.String("auth", cred.Auth),
					slog.String("source", cred.Source),
				)
				llm := NewOpenAIResponsesModel(cred.BaseURL, cred.APIKey, resolvedModel, AuthStyle(cred.Auth))
				return f.wrapModel(llm, cred.Slug, cred.Provider), nil
			case "openai-chat":
				f.log.Debug("creating ADK model via OpenAI-protocol endpoint (DB cred)",
					slog.String("model", resolvedModel),
					slog.String("baseURL", cred.BaseURL),
					slog.String("provider", cred.Provider),
					slog.String("protocol", protocol),
					slog.String("auth", cred.Auth),
					slog.String("source", cred.Source),
				)
				llm := NewOpenAICompatibleModelWithCompat(cred.BaseURL, cred.APIKey, resolvedModel, AuthStyle(cred.Auth), cred.Compat, cred.Extra, cred.ExtraHeaders)
				return f.wrapModel(llm, cred.Slug, cred.Provider), nil
			case "anthropic-messages":
				f.log.Debug("creating ADK model via Anthropic Messages endpoint (DB cred)",
					slog.String("model", resolvedModel),
					slog.String("baseURL", cred.BaseURL),
					slog.String("provider", cred.Provider),
					slog.String("source", cred.Source),
				)
				llm := NewAnthropicModel(cred.BaseURL, cred.APIKey, resolvedModel)
				return f.wrapModel(llm, cred.Slug, cred.Provider), nil
			case "google-genai":
				if cred.IsVertexAI || cred.Provider == "google-vertex" {
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
					return f.wrapModel(llm, "google-vertex", "google-vertex"), nil
				}
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
				return f.wrapModel(llm, "google", "google"), nil
			default:
				return nil, fmt.Errorf("unsupported protocol %q for provider %q", protocol, cred.Provider)
			}
		}
		// cred == nil means no DB credential found — fall through to env vars
	}

	// --- 2. Env-var fallback (registry-driven; legacy providers preserved) ---
	return f.createEnvModel(ctx, providerHint, bareModel)
}

// wrapModel applies the optional ModelWrapper to the given LLM.
// If no wrapper is configured the model is returned unchanged. When no instance
// slug is known the dialect is used as the slug (the default instance).
func (f *ModelFactory) wrapModel(llm model.LLM, slug, dialect string) model.LLM {
	if f.wrapper == nil {
		return llm
	}
	if slug == "" {
		slug = dialect
	}
	return f.wrapper.WrapModel(llm, slug, dialect)
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
// The four legacy providers keep the exact semantics of LLMConfig.IsEnabled();
// a registered non-legacy vendor also enables the factory once it has both an
// env API key and an env model (never on a key alone). LLM_NETWORK_DISABLED is
// a hard kill switch: when set, no provider — legacy or generic — is enabled.
func (f *ModelFactory) IsEnabled() bool {
	if f.cfg.NetworkDisabled {
		return false
	}
	return f.cfg.IsEnabled() || f.genericEnvModelName() != ""
}

// ModelName returns the first configured model name across all env-var providers.
// Priority: DEEPSEEK_MODEL → OPENAI_MODEL → VERTEX_AI_MODEL → registered
// vendor env models in registry order. Returns "" if no provider is configured
// via env vars. Used primarily in tests and env-var-only mode; production
// resolves models via DB.
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
	return f.genericEnvModelName()
}

// Helper function for pointer values
func ptrFloat32(v float32) *float32 {
	return &v
}
