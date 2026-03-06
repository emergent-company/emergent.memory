// Package embeddings provides embedding generation functionality.
package embeddings

import (
	"context"
)

// EmbeddingDimension is the default embedding dimension (768 for gemini-embedding-001 with MRL)
const EmbeddingDimension = 768

// Client provides embedding generation functionality
type Client interface {
	// EmbedQuery generates an embedding vector for the given query text
	EmbedQuery(ctx context.Context, query string) ([]float32, error)

	// EmbedDocuments generates embedding vectors for the given documents
	EmbedDocuments(ctx context.Context, documents []string) ([][]float32, error)
}

// NoopClient is a no-op implementation that returns nil embeddings
// Used when embeddings are disabled
type NoopClient struct{}

// NewNoopClient creates a new NoopClient
func NewNoopClient() *NoopClient {
	return &NoopClient{}
}

// EmbedQuery returns nil, nil (no embedding available)
func (c *NoopClient) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return nil, nil
}

// EmbedDocuments returns nil, nil (no embeddings available)
func (c *NoopClient) EmbedDocuments(ctx context.Context, documents []string) ([][]float32, error) {
	return nil, nil
}
