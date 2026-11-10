package sources

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// Resolved credential scopes. An OAuth app is resolved project → organization →
// deployment default; "none" means no client is configured anywhere and a
// connect would fail with an actionable error.
const (
	CredentialScopeProject      = "project"
	CredentialScopeOrganization = "organization"
	CredentialScopeDeployment   = "deployment"
	CredentialScopeNone         = "none"
)

// OAuthCredentialSource describes one scope's OAuth app for a provider.
// ClientID is not a secret (it is embedded in every authorization URL), so it
// is safe to expose to project members. The client secret and provider-specific
// config extras are deliberately absent.
type OAuthCredentialSource struct {
	Configured bool       `json:"configured"`
	ClientID   string     `json:"clientId,omitempty"`
	UpdatedAt  *time.Time `json:"updatedAt,omitempty"`
}

// OAuthCredentialDescription is the member-readable credential card for one
// OAuth-capable provider: which scopes have an app, which one wins, and the
// non-secret client id of each.
type OAuthCredentialDescription struct {
	ProviderType  string                `json:"providerType"`
	Project       OAuthCredentialSource `json:"project"`
	Organization  OAuthCredentialSource `json:"organization"`
	Deployment    OAuthCredentialSource `json:"deployment"`
	ResolvedScope string                `json:"resolvedScope"`
}

// DescribeOAuthCredentials lists the OAuth apps available to a project, one
// entry per OAuth-capable provider, across all three scopes. It is intentionally
// member-readable (the credentials page is visible to every project member), so
// it returns only non-secret material: configured flags, client ids, and update
// timestamps. Secrets never leave the service.
//
// The resolved scope mirrors resolveOAuthClient's precedence: project override
// wins, then the organization-wide app, then the deployment default.
func (s *Service) DescribeOAuthCredentials(ctx context.Context, projectID uuid.UUID) ([]*OAuthCredentialDescription, error) {
	out := make([]*OAuthCredentialDescription, 0)
	for _, p := range s.providers.List() {
		if _, ok := p.OAuth(); !ok {
			continue
		}
		providerType := p.Catalog().Type
		desc := &OAuthCredentialDescription{ProviderType: providerType}
		vendor, _, _ := s.vendorForProvider(providerType)

		if pst, ok := s.projectAppStore(); ok {
			apps, lerr := pst.ListProjectOAuthClientConfigsByVendor(ctx, projectID, vendor)
			if lerr != nil {
				return nil, lerr
			}
			if len(apps) > 0 {
				if dto, derr := s.toOAuthAppDTO(ctx, apps[0], CredentialScopeProject); derr == nil {
					updated := apps[0].UpdatedAt
					desc.Project = OAuthCredentialSource{Configured: true, ClientID: dto.ClientID, UpdatedAt: &updated}
				}
			}
		} else if s.projectOAuth != nil || s.repo != nil {
			row, err := s.projectStore().GetProjectOAuthClientConfig(ctx, projectID, providerType)
			switch {
			case err == nil:
				client, derr := s.decryptOAuthClientConfig(row)
				if derr != nil {
					return nil, derr
				}
				updated := row.UpdatedAt
				desc.Project = OAuthCredentialSource{Configured: true, ClientID: client.ClientID, UpdatedAt: &updated}
			case !errors.Is(err, apperror.ErrNotFound):
				return nil, err
			}
		}

		if ost, ok := s.orgAppStore(); ok {
			orgID, orgErr := s.orgStore().ResolveOrgID(ctx, projectID)
			switch {
			case orgErr == nil:
				apps, lerr := ost.ListOAuthClientConfigsByVendor(ctx, orgID, vendor)
				if lerr != nil {
					return nil, lerr
				}
				if len(apps) > 0 {
					if dto, derr := s.toOAuthAppDTO(ctx, apps[0], CredentialScopeOrganization); derr == nil {
						updated := apps[0].UpdatedAt
						desc.Organization = OAuthCredentialSource{Configured: true, ClientID: dto.ClientID, UpdatedAt: &updated}
					}
				}
			case !errors.Is(orgErr, apperror.ErrNotFound):
				return nil, orgErr
			}
		} else if s.orgOAuth != nil || s.repo != nil {
			orgID, orgErr := s.orgStore().ResolveOrgID(ctx, projectID)
			switch {
			case orgErr == nil:
				row, gerr := s.orgStore().GetOAuthClientConfig(ctx, orgID, providerType)
				switch {
				case gerr == nil:
					client, derr := s.decryptOAuthClientConfig(row)
					if derr != nil {
						return nil, derr
					}
					updated := row.UpdatedAt
					desc.Organization = OAuthCredentialSource{Configured: true, ClientID: client.ClientID, UpdatedAt: &updated}
				case !errors.Is(gerr, apperror.ErrNotFound):
					return nil, gerr
				}
			case !errors.Is(orgErr, apperror.ErrNotFound):
				return nil, orgErr
			}
		}

		if c, ok := s.oauthClients[providerType]; ok && c.ClientID != "" && c.ClientSecret != "" {
			desc.Deployment = OAuthCredentialSource{Configured: true, ClientID: c.ClientID}
		}

		desc.ResolvedScope = resolveOAuthScope(desc.Project.Configured, desc.Organization.Configured, desc.Deployment.Configured)
		out = append(out, desc)
	}
	return out, nil
}

// resolveOAuthScope applies the project → organization → deployment precedence.
func resolveOAuthScope(project, organization, deployment bool) string {
	switch {
	case project:
		return CredentialScopeProject
	case organization:
		return CredentialScopeOrganization
	case deployment:
		return CredentialScopeDeployment
	default:
		return CredentialScopeNone
	}
}
