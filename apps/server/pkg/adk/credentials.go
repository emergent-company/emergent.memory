package adk

import (
	"context"

	adkmodel "google.golang.org/adk/model"
)

// ResolvedCredential holds the decrypted credential material needed to
// instantiate an LLM client for a specific request context.
// This type is defined in pkg/adk (not domain/provider) to avoid an import cycle.
type ResolvedCredential struct {
	IsGoogleAI         bool
	APIKey             string
	IsVertexAI         bool
	GCPProject         string
	Location           string
	ServiceAccountJSON string
	GenerativeModel    string
	// BaseURL is the HTTP endpoint for OpenAI-protocol providers (openai, deepseek).
	BaseURL string
	// Provider is the canonical provider dialect: "google", "google-vertex", "openai", "deepseek".
	// Used for dispatching model creation and recording usage events.
	Provider string
	// Slug is the provider instance slug the credential was resolved from. It
	// may differ from Provider when several instances share one dialect.
	Slug string
	// Source describes where the credential was resolved from (project/organization/environment).
	// Informational only; used for logging and tracing.
	Source string
}

// CredentialResolver resolves LLM credentials for the current request context.
// Implemented by domain/provider.ADKCredentialAdapter to avoid an import cycle:
// pkg/adk cannot import domain/provider, so the adapter satisfies this interface
// and is injected via fx.
type CredentialResolver interface {
	ResolveAny(ctx context.Context) (*ResolvedCredential, error)
	ResolveFor(ctx context.Context, provider string) (*ResolvedCredential, error)
	// ResolveBySlug resolves credentials for a specific provider instance.
	// ResolveFor keeps its dialect semantics; this is the slug-addressed sibling.
	ResolveBySlug(ctx context.Context, slug string) (*ResolvedCredential, error)
}

// ModelLimitResolver looks up token limits for the active LLM model.
// Implemented by domain/provider.ModelLimitAdapter to avoid an import cycle.
type ModelLimitResolver interface {
	// GetInputLimit returns the max_input_tokens for the active model in the
	// current request context (project → org → env hierarchy). Returns 0 if
	// unknown; callers should treat 0 as "no limit".
	GetInputLimit(ctx context.Context) (int, error)
}

// ModelResolver resolves the effective generative model name for a project.
// Implemented by domain/modelconfig.Service via an adapter to avoid import cycles.
// When injected into ModelFactory it takes precedence over the env-var default.
type ModelResolver interface {
	// ResolveGenerativeModel returns the effective model name and source
	// for the given project UUID string.
	// projectID must be a valid UUID string; resolves project config →
	// provider-credential generative model, returns empty when neither is set
	// (env defaults are never consulted on the wired path).
	ResolveGenerativeModelByID(ctx context.Context, projectID string) (model string, source string, err error)
}

// Implemented by domain/provider.UsageTrackerAdapter to avoid an import cycle:
// pkg/adk cannot import domain/provider, so the adapter satisfies this interface
// and is injected optionally via fx.
//
// The slug and dialect parameters identify the provider instance and dialect
// (e.g. slug "azure-openai", dialect "openai") as plain strings to avoid
// exporting domain types through this package.
type ModelWrapper interface {
	WrapModel(inner adkmodel.LLM, slug, dialect string) adkmodel.LLM
}

// TestLLMChecker reports whether a project has deterministic test-LLM mode
// enabled. When true, ModelFactory returns a canned model that never calls a
// real provider. Implemented by domain/agents.TestLLMFlagChecker and injected
// optionally via fx.
type TestLLMChecker interface {
	IsTestLLM(ctx context.Context, projectID string) bool
}
