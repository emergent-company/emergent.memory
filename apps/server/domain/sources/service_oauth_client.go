package sources

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// Keys within the encrypted org OAuth client blob.
const (
	oauthClientIDKey     = "client_id"
	oauthClientSecretKey = "client_secret"
)

// OAuthClientConfigDTO is the redacted, API-facing shape of an org OAuth client
// config. It never carries the client secret (write-only); the client id is not
// a secret and is returned so the UI can show which app is configured.
type OAuthClientConfigDTO struct {
	ProviderType string    `json:"providerType"`
	ClientID     string    `json:"clientId"`
	Scopes       []string  `json:"scopes,omitempty"`
	Strategy     string    `json:"strategy,omitempty"`
	Configured   bool      `json:"configured"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	// Config carries provider-specific, non-secret extras (e.g. the Google
	// Picker developer key/app id). It never carries secret material.
	Config map[string]any `json:"config,omitempty"`
}

// SetOAuthClientRequest is the write shape for an org OAuth client config. The
// client secret is write-only: on an update an empty ClientSecret preserves the
// stored secret.
type SetOAuthClientRequest struct {
	ClientID     string         `json:"clientId"`
	ClientSecret string         `json:"clientSecret"`
	Scopes       []string       `json:"scopes,omitempty"`
	Strategy     string         `json:"strategy,omitempty"`
	Config       map[string]any `json:"config,omitempty"`
}

// ListOrgOAuthClients returns the org's configured OAuth clients (redacted). It
// requires org-admin authority.
func (s *Service) ListOrgOAuthClients(ctx context.Context, orgID uuid.UUID, userID string) ([]*OAuthClientConfigDTO, error) {
	if err := s.requireOrgAdmin(ctx, orgID, userID); err != nil {
		return nil, err
	}
	rows, err := s.orgStore().ListOAuthClientConfigs(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]*OAuthClientConfigDTO, 0, len(rows))
	for _, row := range rows {
		dto, err := s.toOAuthClientDTO(row)
		if err != nil {
			return nil, err
		}
		out = append(out, dto)
	}
	return out, nil
}

// GetOrgOAuthClient returns the org's client config for a provider (redacted).
// An unconfigured provider is not an error: it returns a DTO with
// Configured=false so the settings UI can render an empty form.
func (s *Service) GetOrgOAuthClient(ctx context.Context, orgID uuid.UUID, userID, providerType string) (*OAuthClientConfigDTO, error) {
	if err := s.requireOrgAdmin(ctx, orgID, userID); err != nil {
		return nil, err
	}
	if _, err := s.oauthProvider(providerType); err != nil {
		return nil, err
	}
	row, err := s.orgStore().GetOAuthClientConfig(ctx, orgID, providerType)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return &OAuthClientConfigDTO{ProviderType: providerType, Configured: false}, nil
		}
		return nil, err
	}
	return s.toOAuthClientDTO(row)
}

// UpsertOrgOAuthClient creates or replaces the org's client config for a provider.
// The client secret is encrypted at rest and never returned. On an update, an
// empty secret preserves the stored one.
func (s *Service) UpsertOrgOAuthClient(ctx context.Context, orgID uuid.UUID, userID, providerType string, req *SetOAuthClientRequest) (*OAuthClientConfigDTO, error) {
	if err := s.requireOrgAdmin(ctx, orgID, userID); err != nil {
		return nil, err
	}
	if _, err := s.oauthProvider(providerType); err != nil {
		return nil, err
	}
	clientID := strings.TrimSpace(req.ClientID)
	if clientID == "" {
		return nil, apperror.NewValidation("clientId is required")
	}

	secret := req.ClientSecret
	existing, err := s.orgStore().GetOAuthClientConfig(ctx, orgID, providerType)
	switch {
	case err == nil:
		if secret == "" {
			// Preserve the stored secret (write-only update).
			creds, derr := s.decryptCredentials(existing.EncryptedClient)
			if derr != nil {
				return nil, derr
			}
			secret, _ = creds[oauthClientSecretKey].(string)
		}
	case errors.Is(err, apperror.ErrNotFound):
		// New config: a secret is mandatory.
	default:
		return nil, err
	}
	if secret == "" {
		return nil, apperror.NewValidation("clientSecret is required")
	}

	blob, err := s.encryptCredentials(Credentials{
		oauthClientIDKey:     clientID,
		oauthClientSecretKey: secret,
	})
	if err != nil {
		return nil, err
	}

	saved, err := s.orgStore().UpsertOAuthClientConfig(ctx, &OAuthClientConfig{
		OrganizationID:  orgID,
		Provider:        providerType,
		EncryptedClient: blob,
		Scopes:          req.Scopes,
		Strategy:        strings.TrimSpace(req.Strategy),
		Config:          req.Config,
	})
	if err != nil {
		return nil, err
	}
	return s.toOAuthClientDTO(saved)
}

// DeleteOrgOAuthClient removes the org's client config for a provider. A
// missing row resolves to not-found.
func (s *Service) DeleteOrgOAuthClient(ctx context.Context, orgID uuid.UUID, userID, providerType string) error {
	if err := s.requireOrgAdmin(ctx, orgID, userID); err != nil {
		return err
	}
	return s.orgStore().DeleteOAuthClientConfig(ctx, orgID, providerType)
}

// ---- resolution + helpers ----

// resolveOAuthClient resolves the OAuth client for a provider for a project:
// org config → deployment-default env → actionable error (design D3).
func (s *Service) resolveOAuthClient(ctx context.Context, projectID uuid.UUID, providerType string) (OAuthClient, error) {
	if s.orgOAuth != nil {
		orgID, err := s.orgOAuth.ResolveOrgID(ctx, projectID)
		switch {
		case err == nil:
			row, gerr := s.orgOAuth.GetOAuthClientConfig(ctx, orgID, providerType)
			if gerr == nil {
				client, derr := s.decryptOAuthClientConfig(row)
				if derr != nil {
					return OAuthClient{}, derr
				}
				return s.applyOrgStrategy(providerType, client, row.Strategy), nil
			}
			if !errors.Is(gerr, apperror.ErrNotFound) {
				return OAuthClient{}, gerr
			}
			// not configured for this org → fall through to the deployment default
		case errors.Is(err, apperror.ErrNotFound):
			// no resolvable org → fall through to the deployment default
		default:
			return OAuthClient{}, err
		}
	}
	if c, ok := s.oauthClients[providerType]; ok && c.ClientID != "" && c.ClientSecret != "" {
		return c, nil
	}
	return OAuthClient{}, apperror.NewValidation(
		"no OAuth client configured for provider " + providerType +
			" (configure an org OAuth client or set SOURCES_OAUTH_GOOGLE_CLIENT_ID/SECRET)")
}

// applyOrgStrategy folds an org-selected scope strategy (design D6) into the
// resolved client: explicit scopes win, otherwise the provider maps the named
// strategy to its scope set. A provider without strategy support (or an unknown
// strategy) is left unchanged, falling back to its declared default.
func (s *Service) applyOrgStrategy(providerType string, client OAuthClient, strategy string) OAuthClient {
	if len(client.Scopes) > 0 || strings.TrimSpace(strategy) == "" {
		return client
	}
	p, ok := s.providers.Get(providerType)
	if !ok {
		return client
	}
	ss, ok := p.(ScopeStrategies)
	if !ok {
		return client
	}
	if scopes, ok := ss.ScopesForStrategy(strategy); ok {
		client.Scopes = scopes
	}
	return client
}

// requireOrgAdmin enforces org-admin authority, failing closed. An empty user
// yields unauthorized; a non-admin yields forbidden. The decision is the shared
// app-side org-administration check.
func (s *Service) requireOrgAdmin(ctx context.Context, orgID uuid.UUID, userID string) error {
	if strings.TrimSpace(userID) == "" {
		return apperror.ErrUnauthorized
	}
	if s.orgOAuth == nil {
		return apperror.ErrForbidden
	}
	ok, err := s.orgOAuth.IsOrgAdmin(ctx, orgID.String(), userID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.ErrForbidden
	}
	return nil
}

// orgStore returns the org-client store, or the repository when none was
// injected.
func (s *Service) orgStore() OrgOAuthClientStore {
	if s.orgOAuth != nil {
		return s.orgOAuth
	}
	return s.repo
}

// oauthProvider resolves a registered provider and requires it to support OAuth
// connect, so an org cannot configure a client for a provider that has no
// authorize flow.
func (s *Service) oauthProvider(providerType string) (SourceProvider, error) {
	p, ok := s.providers.Get(providerType)
	if !ok {
		return nil, apperror.NewValidation("unsupported providerType: " + providerType)
	}
	if _, ok := p.OAuth(); !ok {
		return nil, apperror.NewValidation("provider does not support OAuth connect: " + providerType)
	}
	return p, nil
}

// decryptOAuthClientConfig decrypts an org client row into the resolved client.
func (s *Service) decryptOAuthClientConfig(row *OAuthClientConfig) (OAuthClient, error) {
	creds, err := s.decryptCredentials(row.EncryptedClient)
	if err != nil {
		return OAuthClient{}, err
	}
	id, _ := creds[oauthClientIDKey].(string)
	secret, _ := creds[oauthClientSecretKey].(string)
	if strings.TrimSpace(id) == "" || secret == "" {
		return OAuthClient{}, apperror.NewInternal("org OAuth client config is incomplete", nil)
	}
	return OAuthClient{ClientID: id, ClientSecret: secret, Scopes: row.Scopes}, nil
}

// toOAuthClientDTO maps a stored row to the redacted API shape.
func (s *Service) toOAuthClientDTO(row *OAuthClientConfig) (*OAuthClientConfigDTO, error) {
	client, err := s.decryptOAuthClientConfig(row)
	if err != nil {
		return nil, err
	}
	return &OAuthClientConfigDTO{
		ProviderType: row.Provider,
		ClientID:     client.ClientID,
		Scopes:       row.Scopes,
		Strategy:     row.Strategy,
		Configured:   true,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		Config:       row.Config,
	}, nil
}
