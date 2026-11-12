package embeddings

import (
	"context"
	"errors"
)

// Embedding errors. Embeddings are resolved per request from the project's
// configured provider — there is no environment-configured fallback — so a
// request that cannot resolve a project provider fails closed with one of these
// sentinels instead of silently using a deployment-wide credential.
var (
	// ErrNoProjectContext reports that the request carried no project, so there
	// is no project provider to resolve. Callers outside a project (global or
	// org-scoped skills) cannot generate embeddings.
	ErrNoProjectContext = errors.New("no project in context for embedding resolution")

	// ErrEmbeddingModelNotSet reports that the project has no embedding model
	// configured. The message is deliberately the historical phrasing
	// "no embedding model configured": extraction workers classify a permanent
	// (non-retryable) embedding failure by that substring, and a missing model is
	// a configuration error, not a transient network/quota failure.
	ErrEmbeddingModelNotSet = errors.New("no embedding model configured")
)

// ResolvedEmbeddingCredential holds the decrypted credential material needed to
// instantiate an embeddings client for a specific request context.
// This type is defined in pkg/embeddings (not domain/provider) to avoid an import cycle.
type ResolvedEmbeddingCredential struct {
	IsGoogleAI         bool
	APIKey             string
	IsVertexAI         bool
	GCPProject         string
	Location           string
	ServiceAccountJSON string // set for Vertex AI; SA key JSON
	EmbeddingModel     string
	// BaseURL is the HTTP endpoint override for OpenAI-compatible providers (LiteLLM, etc.).
	// When set alongside APIKey, the OpenAI-compatible client is used instead of Google AI.
	BaseURL string
	// Auth is the vendor's declared credential-injection style (e.g. "bearer",
	// "api-key"). Empty defaults to bearer. Azure OpenAI embeddings require
	// "api-key"; OpenAI-compatible vendors use bearer.
	Auth string
	// Extra carries vendor-specific request config (e.g. Azure "api_version",
	// applied as a query parameter by the OpenAI-compatible client). May be nil.
	Extra map[string]string
	// ExtraHeaders are additional request headers derived from vendor extra
	// fields (e.g. a secondary secret header). May be nil.
	ExtraHeaders map[string]string
	// Source describes where the credential was resolved from (project/organization/environment).
	// Informational only; used for logging and tracing.
	Source string
	// ProviderSlug is the provider instance id the credential came from (e.g.
	// "google", "google-vertex", "openai"). Informational: used in error and log
	// messages so an operator can tell which project provider failed.
	ProviderSlug string
}

// EmbeddingResolver resolves embedding credentials for the current request context.
// Implemented by domain/provider.EmbeddingCredentialAdapter to avoid an import cycle:
// pkg/embeddings cannot import domain/provider, so the adapter satisfies this interface
// and is injected via fx.
type EmbeddingResolver interface {
	ResolveEmbedding(ctx context.Context) (*ResolvedEmbeddingCredential, error)
}
