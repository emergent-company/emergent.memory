package embeddings

import "context"

// ResolvedEmbeddingCredential holds the decrypted credential material needed to
// instantiate an embeddings client for a specific request context.
// This type is defined in pkg/embeddings (not domain/provider) to avoid an import cycle.
type ResolvedEmbeddingCredential struct {
	IsGoogleAI     bool
	APIKey         string
	IsVertexAI     bool
	GCPProject     string
	Location       string
	EmbeddingModel string
}

// EmbeddingResolver resolves embedding credentials for the current request context.
// Implemented by domain/provider.EmbeddingCredentialAdapter to avoid an import cycle:
// pkg/embeddings cannot import domain/provider, so the adapter satisfies this interface
// and is injected via fx.
type EmbeddingResolver interface {
	ResolveEmbedding(ctx context.Context) (*ResolvedEmbeddingCredential, error)
}
