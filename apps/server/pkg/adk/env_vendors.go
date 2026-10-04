// Package adk provides Google ADK-Go integration for agent workflows.
package adk

import (
	"context"
	"fmt"
	"os"
	"strings"

	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"
)

// Wire-protocol tags used by the env-var fallback. They mirror the
// domain/provider.Protocol* vocabulary as plain strings so pkg/adk can reference
// them without importing domain/provider (which would create an import cycle).
const (
	protocolOpenAIChat        = "openai-chat"
	protocolAnthropicMessages = "anthropic-messages"
	protocolGoogleGenAI       = "google-genai"
)

// Legacy env defaults preserved for back-compat. These are the defaults the
// four hardcoded providers shipped with before the env path was generalized
// onto the vendor registry; new vendors take their default endpoint from the
// registry (EnvVendor.DefaultBaseURL) instead.
const (
	legacyDeepSeekBaseURL = "https://api.deepseek.com/v1"
	legacyOpenAIBaseURL   = "https://api.openai.com/v1"
)

// EnvPrefix converts a provider dialect into its environment-variable prefix.
// The scheme is deterministic and registry-agnostic: the dialect is upper-cased
// and every '-' becomes '_'. Examples:
//
//	"openai"       -> "OPENAI"
//	"deepseek"     -> "DEEPSEEK"
//	"azure-openai" -> "AZURE_OPENAI"
//	"google-vertex"-> "GOOGLE_VERTEX"
//	"openrouter"   -> "OPENROUTER"
//
// A vendor configured from env therefore reads <PREFIX>_API_KEY,
// <PREFIX>_BASE_URL, and <PREFIX>_MODEL.
func EnvPrefix(provider string) string {
	return strings.ToUpper(strings.ReplaceAll(provider, "-", "_"))
}

// EnvVendor carries the registry-derived metadata pkg/adk needs to build a
// model from environment variables for one vendor. It is produced by
// domain/provider (the only package that holds the vendor Registry) and handed
// to ModelFactory through EnvVendorResolver, avoiding an import cycle.
type EnvVendor struct {
	// Prefix is the UPPER_SNAKE env prefix for the vendor (see EnvPrefix).
	Prefix string
	// DefaultBaseURL is the vendor's generative default endpoint. May be empty
	// for vendors that have no fixed endpoint (generic proxies); in that case
	// <PREFIX>_BASE_URL is required.
	DefaultBaseURL string
	// Protocol is the vendor's *default* wire protocol:
	// "openai-chat", "openai-responses", "anthropic-messages", or "google-genai".
	// Host-based Responses promotion is applied later, against the resolved
	// base URL, using ResponsesHosts.
	Protocol string
	// ResponsesHosts are the hostnames whose resolved vendor base URL must use
	// the Responses protocol instead of the default openai-chat protocol. It is
	// copied verbatim from the ProviderDefinition so the env path selects the
	// protocol with exactly the same host rule the DB path uses
	// (ProviderDefinition.ProtocolForBaseURL).
	ResponsesHosts []string
	// Auth is the credential-injection style ("bearer", "api-key", "x-api-key",
	// "x-goog-api-key", "none"). Empty defaults to bearer.
	Auth string
}

// EnvVendorResolver resolves a registered vendor to its env metadata. It is
// implemented by domain/provider.EnvVendorAdapter and injected optionally into
// ModelFactory via fx. When nil, only the four legacy providers can be built
// from env (their defaults are hardcoded here for back-compat).
type EnvVendorResolver interface {
	// EnvVendor returns the env metadata for a vendor dialect. The boolean is
	// false when the vendor is not registered.
	EnvVendor(provider string) (EnvVendor, bool)
	// EnvVendorOrder returns registered vendor dialects in display order so the
	// env fallback can pick a configured model deterministically.
	EnvVendorOrder() []string
}

// EnvVendorConfig is the resolved environment configuration for one vendor.
// It is the single value the generic env resolver produces; construction of the
// concrete model is a separate step so the resolution rules are unit-testable.
type EnvVendorConfig struct {
	Provider   string
	Prefix     string
	APIKey     string
	BaseURL    string
	Model      string // routed form as read from <PREFIX>_MODEL (may include a provider prefix)
	Protocol   string
	Auth       string
	IsGoogleAI bool
	IsVertexAI bool
	GCPProject string
	Location   string
}

// ResolveEnvVendor resolves the env-var configuration for a provider dialect.
//
// The four legacy providers keep their historical env vars and defaults
// (DEEPSEEK_API_KEY/DEEPSEEK_MODEL, OPENAI_API_KEY/OPENAI_BASE_URL/OPENAI_MODEL,
// GOOGLE_API_KEY, and GCP_PROJECT_ID + VERTEX_AI_LOCATION). Every other
// registered vendor is resolved generically from <PREFIX>_API_KEY,
// <PREFIX>_BASE_URL, and <PREFIX>_MODEL using the deterministic EnvPrefix
// scheme. An unregistered vendor with no registry metadata returns an error.
//
// A missing API key is NOT an error at resolve time: it is reported by the
// caller so a vendor with no key is simply not enabled (no accidental enable).
func (f *ModelFactory) ResolveEnvVendor(provider, bareModel string) (*EnvVendorConfig, error) {
	prefix := EnvPrefix(provider)

	// Registry metadata is optional. It supplies the default endpoint, protocol,
	// and auth style for non-legacy vendors and confirms registration.
	var vendor EnvVendor
	hasVendor := false
	if f.envVendors != nil {
		vendor, hasVendor = f.envVendors.EnvVendor(provider)
	}

	cfg := &EnvVendorConfig{
		Provider: provider,
		Prefix:   prefix,
		Model:    bareModel,
	}

	switch provider {
	case "deepseek":
		cfg.APIKey = f.cfg.DeepSeekAPIKey
		cfg.BaseURL = legacyDeepSeekBaseURL
		if cfg.Model == "" {
			cfg.Model = stripEnvModelPrefix(f.cfg.DeepSeekModel, provider, true)
		}
		cfg.Protocol = protocolOpenAIChat
		cfg.Auth = string(AuthBearer)
		return cfg, nil

	case "openai":
		cfg.APIKey = f.cfg.OpenAIAPIKey
		cfg.BaseURL = f.cfg.OpenAIBaseURL
		if cfg.BaseURL == "" {
			cfg.BaseURL = legacyOpenAIBaseURL
		}
		if cfg.Model == "" {
			cfg.Model = stripEnvModelPrefix(f.cfg.OpenAIModel, provider, true)
		}
		// Host-aware: first-party api.openai.com selects Responses; relays and
		// proxies keep Chat Completions. Same rule the registry applies.
		cfg.Protocol = SelectOpenAIProtocol(cfg.BaseURL)
		cfg.Auth = string(AuthBearer)
		return cfg, nil

	case "google":
		cfg.APIKey = f.cfg.GoogleAPIKey
		cfg.IsGoogleAI = true
		cfg.Protocol = protocolGoogleGenAI
		if hasVendor {
			cfg.BaseURL = vendor.DefaultBaseURL
		}
		return cfg, nil

	case "google-vertex":
		cfg.IsVertexAI = true
		cfg.GCPProject = f.cfg.GCPProjectID
		cfg.Location = f.cfg.VertexAILocation
		cfg.Protocol = protocolGoogleGenAI
		return cfg, nil
	}

	// --- Generic registered vendor ---
	if !hasVendor {
		return nil, fmt.Errorf("unsupported provider %q: not registered and not one of the legacy env providers (deepseek, openai, google, google-vertex)", provider)
	}

	getenv := f.lookupEnv()
	// Trim surrounding whitespace: a key or URL that is only whitespace is not a
	// usable credential and must not enable the vendor.
	cfg.APIKey = strings.TrimSpace(getenv(prefix + "_API_KEY"))
	cfg.BaseURL = strings.TrimSpace(getenv(prefix + "_BASE_URL"))
	if cfg.BaseURL == "" {
		cfg.BaseURL = vendor.DefaultBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = stripEnvModelPrefix(getenv(prefix+"_MODEL"), provider, false)
	}
	cfg.Protocol = vendor.Protocol
	cfg.Auth = vendor.Auth

	// A vendor whose default protocol is openai-chat may be promoted to the
	// Responses protocol only when its resolved base URL host is declared in the
	// definition's ResponsesHosts (e.g. a vendor pointed at api.openai.com). This
	// is the exact per-definition rule the DB path applies
	// (ProviderDefinition.ProtocolForBaseURL), so env and DB never diverge: a
	// non-OpenAI openai-chat vendor keeps Chat Completions even when its base URL
	// is overridden to a first-party OpenAI host.
	if cfg.Protocol == protocolOpenAIChat && HostMatchesAny(cfg.BaseURL, vendor.ResponsesHosts) {
		cfg.Protocol = protocolOpenAIResponses
	}
	return cfg, nil
}

// createEnvModel builds a model.LLM from the resolved env configuration. The
// switch is protocol-driven, so every registered vendor shares one creation
// path and a new registry entry needs no change here.
func (f *ModelFactory) createEnvModel(ctx context.Context, provider, bareModel string) (model.LLM, error) {
	cfg, err := f.ResolveEnvVendor(provider, bareModel)
	if err != nil {
		return nil, err
	}

	if cfg.IsVertexAI {
		clientCfg := &genai.ClientConfig{
			Backend:  genai.BackendVertexAI,
			Project:  cfg.GCPProject,
			Location: cfg.Location,
		}
		f.log.Debug("creating ADK Gemini model via Vertex AI (env config)",
			"model", cfg.Model,
			"project", cfg.GCPProject,
			"location", cfg.Location,
		)
		llm, err := gemini.NewModel(ctx, cfg.Model, clientCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create Gemini model: %w", err)
		}
		return f.wrapModel(llm, provider, provider), nil
	}

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("%s_API_KEY is not set", cfg.Prefix)
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("model name is required: set %s_MODEL (e.g. %s/<model>)", cfg.Prefix, provider)
	}

	switch cfg.Protocol {
	case protocolOpenAIResponses:
		f.log.Debug("creating ADK model via OpenAI Responses endpoint (env config)",
			"provider", provider,
			"model", cfg.Model,
			"baseURL", cfg.BaseURL,
		)
		return f.wrapModel(NewOpenAIResponsesModel(cfg.BaseURL, cfg.APIKey, cfg.Model, AuthStyle(cfg.Auth)), provider, provider), nil

	case protocolAnthropicMessages:
		f.log.Debug("creating ADK model via Anthropic Messages endpoint (env config)",
			"provider", provider,
			"model", cfg.Model,
			"baseURL", cfg.BaseURL,
		)
		return f.wrapModel(NewAnthropicModel(cfg.BaseURL, cfg.APIKey, cfg.Model), provider, provider), nil

	case protocolGoogleGenAI:
		if !cfg.IsGoogleAI {
			return nil, fmt.Errorf("provider %q requires Google AI credentials (GOOGLE_API_KEY)", provider)
		}
		clientCfg := &genai.ClientConfig{
			Backend: genai.BackendGeminiAPI,
			APIKey:  cfg.APIKey,
		}
		f.log.Debug("creating ADK Gemini model via Google AI (env config)",
			"provider", provider,
			"model", cfg.Model,
		)
		llm, err := gemini.NewModel(ctx, cfg.Model, clientCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create Gemini model via Google AI: %w", err)
		}
		return f.wrapModel(llm, provider, provider), nil

	default: // openai-chat and any empty protocol
		if cfg.BaseURL == "" {
			return nil, fmt.Errorf("%s_BASE_URL is required for provider %q: no default endpoint is registered", cfg.Prefix, provider)
		}
		f.log.Debug("creating ADK model via OpenAI-compatible endpoint (env config)",
			"provider", provider,
			"model", cfg.Model,
			"baseURL", cfg.BaseURL,
			"auth", cfg.Auth,
		)
		return f.wrapModel(NewOpenAICompatibleModelWithAuth(cfg.BaseURL, cfg.APIKey, cfg.Model, AuthStyle(cfg.Auth), nil, nil), provider, provider), nil
	}
}

// stripEnvModelPrefix removes a leading routing-provider prefix from an env
// model value. Legacy providers (naive=true) strip any first segment, preserving
// the exact behaviour the hardcoded DeepSeek/OpenAI creators had. Registry
// vendors (naive=false) strip only a prefix that names the vendor itself, so a
// relay's namespaced model id ("openai/gpt-4o" behind OpenRouter) is preserved.
func stripEnvModelPrefix(model, provider string, naive bool) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	if naive {
		// Legacy providers historically accepted a routed "<provider>/<model>"
		// value in DEEPSEEK_MODEL/OPENAI_MODEL and stripped the first segment.
		// modelref:allow — this split parses the untrusted environment-variable
		// input boundary, not a routed model reference reconstructed from a
		// trusted internal string, so the canonical modelref.Parse (strict) does
		// not apply here.
		if _, stripped, ok := strings.Cut(model, "/"); ok {
			return stripped
		}
		return model
	}
	if rest, ok := strings.CutPrefix(model, provider+"/"); ok {
		return rest
	}
	return model
}

// lookupEnv returns the env getter for the factory: the injected test seam when
// set, otherwise os.Getenv.
func (f *ModelFactory) lookupEnv() func(string) string {
	if f.envLookup != nil {
		return f.envLookup
	}
	return os.Getenv
}

// hasEnvFallback reports whether env configuration can serve the given provider.
// It is used to decide whether a credential-resolver error should fall through
// to env instead of masking the error. For a registered vendor this is true
// only when the vendor has a usable credential (API key, or GCP project +
// location for Vertex); for an unknown provider it preserves the legacy
// any-legacy-env fallthrough.
func (f *ModelFactory) hasEnvFallback(provider string) bool {
	cfg, err := f.ResolveEnvVendor(provider, "")
	if err != nil {
		return f.cfg.UseVertexAI() || f.cfg.GoogleAPIKey != "" ||
			f.cfg.DeepSeekAPIKey != "" || f.cfg.OpenAIAPIKey != ""
	}
	if cfg.IsVertexAI {
		return cfg.GCPProject != "" && cfg.Location != ""
	}
	return cfg.APIKey != ""
}

// genericEnvModelName returns the first registered non-legacy vendor that has
// both an env API key and an env model configured, in registry display order.
// The model is returned in routed "provider/model" form so it can be passed
// straight to CreateModelWithName. Returns "" when none is configured.
func (f *ModelFactory) genericEnvModelName() string {
	if f.envVendors == nil {
		return ""
	}
	getenv := f.lookupEnv()
	for _, provider := range f.envVendors.EnvVendorOrder() {
		switch provider {
		case "deepseek", "openai", "google", "google-vertex":
			continue // handled by the legacy config fields with historical priority
		}
		prefix := EnvPrefix(provider)
		if strings.TrimSpace(getenv(prefix+"_API_KEY")) == "" {
			continue
		}
		// The value is scoped to this vendor (its own key was read), so strip a
		// leading "<provider>/" if present and always re-prefix it. A foreign
		// namespace such as OpenRouter's "openai/gpt-4o" belongs to this vendor,
		// not to the "openai" dialect, and must not hijack routing.
		model := stripEnvModelPrefix(getenv(prefix+"_MODEL"), provider, false)
		if model == "" {
			continue
		}
		return provider + "/" + model
	}
	return ""
}
