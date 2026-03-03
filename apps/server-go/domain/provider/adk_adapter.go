package provider

import (
	"context"

	"github.com/emergent-company/emergent/pkg/adk"
)

// ADKCredentialAdapter wraps CredentialService to satisfy the adk.CredentialResolver
// interface. It converts domain/provider.ResolvedCredential to adk.ResolvedCredential,
// which avoids an import cycle (pkg/adk cannot import domain/provider).
type ADKCredentialAdapter struct {
	svc *CredentialService
}

// NewADKCredentialAdapter creates a new ADKCredentialAdapter.
func NewADKCredentialAdapter(svc *CredentialService) *ADKCredentialAdapter {
	return &ADKCredentialAdapter{svc: svc}
}

// ResolveAny satisfies adk.CredentialResolver.
func (a *ADKCredentialAdapter) ResolveAny(ctx context.Context) (*adk.ResolvedCredential, error) {
	cred, err := a.svc.ResolveAny(ctx)
	if err != nil {
		return nil, err
	}
	if cred == nil {
		return nil, nil
	}
	return toADKCredential(cred), nil
}

// toADKCredential converts a domain ResolvedCredential to the adk-package type.
func toADKCredential(c *ResolvedCredential) *adk.ResolvedCredential {
	return &adk.ResolvedCredential{
		IsGoogleAI:         c.Provider == ProviderGoogleAI,
		APIKey:             c.APIKey,
		IsVertexAI:         c.Provider == ProviderVertexAI,
		GCPProject:         c.GCPProject,
		Location:           c.Location,
		ServiceAccountJSON: c.ServiceAccountJSON,
		GenerativeModel:    c.GenerativeModel,
		Source:             string(c.Source),
	}
}
