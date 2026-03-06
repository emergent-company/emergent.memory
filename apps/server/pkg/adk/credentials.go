package adk

import "context"

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
}
