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
	// Provider is the canonical provider name: "google", "google-vertex", "openai", "deepseek".
	// Used for dispatching model creation and recording usage events.
	Provider string
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
	// projectID must be a valid UUID string; returns env default when not found.
	ResolveGenerativeModelByID(ctx context.Context, projectID string) (model string, source string, err error)
}

// Implemented by domain/provider.UsageTrackerAdapter to avoid an import cycle:
// pkg/adk cannot import domain/provider, so the adapter satisfies this interface
// and is injected optionally via fx.
//
// The provider parameter is one of "google", "google-vertex", "openai", "deepseek"
// (the string values of domain/provider.ProviderType). It is passed as a plain
// string to avoid exporting domain types through this package.
type ModelWrapper interface {
	WrapModel(inner adkmodel.LLM, provider string) adkmodel.LLM
}
