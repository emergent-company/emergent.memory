package provider

import (
	"context"

	"github.com/emergent-company/emergent.memory/pkg/embeddings"
)

// EmbeddingCredentialAdapter wraps CredentialService to satisfy the
// embeddings.EmbeddingResolver interface. It converts domain/provider.ResolvedCredential
// to embeddings.ResolvedEmbeddingCredential, avoiding an import cycle
// (pkg/embeddings cannot import domain/provider).
type EmbeddingCredentialAdapter struct {
	svc *CredentialService
}

// NewEmbeddingCredentialAdapter creates a new EmbeddingCredentialAdapter.
func NewEmbeddingCredentialAdapter(svc *CredentialService) *EmbeddingCredentialAdapter {
	return &EmbeddingCredentialAdapter{svc: svc}
}

// ResolveEmbedding satisfies embeddings.EmbeddingResolver.
func (a *EmbeddingCredentialAdapter) ResolveEmbedding(ctx context.Context) (*embeddings.ResolvedEmbeddingCredential, error) {
	cred, err := a.svc.ResolveAny(ctx)
	if err != nil {
		return nil, err
	}
	if cred == nil {
		return nil, nil
	}
	return toEmbeddingCredential(cred), nil
}

// toEmbeddingCredential converts a domain ResolvedCredential to the embeddings-package type.
func toEmbeddingCredential(c *ResolvedCredential) *embeddings.ResolvedEmbeddingCredential {
	return &embeddings.ResolvedEmbeddingCredential{
		IsGoogleAI:         c.Provider == ProviderGoogleAI,
		APIKey:             c.APIKey,
		IsVertexAI:         c.Provider == ProviderVertexAI,
		GCPProject:         c.GCPProject,
		Location:           c.Location,
		ServiceAccountJSON: c.ServiceAccountJSON,
		EmbeddingModel:     c.EmbeddingModel,
		Source:             string(c.Source),
	}
}
