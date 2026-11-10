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
	ProviderType     string    `json:"providerType"`
	ClientID         string    `json:"clientId"`
	RedirectURI      string    `json:"redirectUri"`
	JavascriptOrigin string    `json:"javascriptOrigin"`
	Scopes           []string  `json:"scopes,omitempty"`
	Strategy         string    `json:"strategy,omitempty"`
	Configured       bool      `json:"configured"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
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
		if _, ok := s.orgAppStore(); ok {
			vendor := s.appVendor(row)
			for _, p := range s.vendorProviders(vendor) {
				dto, derr := s.legacyDTO(ctx, row, p.Catalog().Type)
				if derr != nil {
					return nil, derr
				}
				out = append(out, dto)
			}
			continue
		}
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
	if st, ok := s.orgAppStore(); ok {
		vendor, _, _ := s.vendorForProvider(providerType)
		apps, lerr := st.ListOAuthClientConfigsByVendor(ctx, orgID, vendor)
		if lerr != nil {
			return nil, lerr
		}
		switch len(apps) {
		case 0:
			return &OAuthClientConfigDTO{ProviderType: providerType, Configured: false}, nil
		case 1:
			return s.legacyDTO(ctx, apps[0], providerType)
		default:
			return nil, ambiguousAppConflict(vendor, apps)
		}
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
	if st, ok := s.orgAppStore(); ok {
		vendor, vendorName, _ := s.vendorForProvider(providerType)
		apps, lerr := st.ListOAuthClientConfigsByVendor(ctx, orgID, vendor)
		if lerr != nil {
			return nil, lerr
		}
		cfg := OAuthProviderConfig{Scopes: req.Scopes, Strategy: strings.TrimSpace(req.Strategy)}
		switch len(apps) {
		case 0:
			dto, cerr := s.CreateOrgOAuthApp(ctx, orgID, userID, &CreateOAuthAppRequest{
				Name:            vendorName,
				Vendor:          vendor,
				ClientID:        req.ClientID,
				ClientSecret:    req.ClientSecret,
				ProviderConfigs: map[string]OAuthProviderConfig{providerType: cfg},
				Config:          req.Config,
			})
			if cerr != nil {
				return nil, cerr
			}
			return s.legacyFromAppDTO(dto, providerType), nil
		case 1:
			clientID := strings.TrimSpace(req.ClientID)
			dto, uerr := s.UpdateOrgOAuthApp(ctx, orgID, userID, apps[0].ID.String(), &UpdateOAuthAppRequest{
				ClientID:        &clientID,
				ClientSecret:    req.ClientSecret,
				ProviderConfigs: mergeProviderConfig(apps[0].ProviderConfigs, providerType, cfg),
				Config:          req.Config,
			})
			if uerr != nil {
				return nil, uerr
			}
			return s.legacyFromAppDTO(dto, providerType), nil
		default:
			return nil, ambiguousAppConflict(vendor, apps)
		}
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

	vendor, vendorName, _ := s.vendorForProvider(providerType)
	saved, err := s.orgStore().UpsertOAuthClientConfig(ctx, &OAuthClientConfig{
		OrganizationID:  orgID,
		Vendor:          vendor,
		Name:            vendorName,
		Provider:        providerType,
		EncryptedClient: blob,
		Scopes:          req.Scopes,
		Strategy:        strings.TrimSpace(req.Strategy),
		ProviderConfigs: map[string]OAuthProviderConfig{
			providerType: {Scopes: req.Scopes, Strategy: strings.TrimSpace(req.Strategy)},
		},
		Config: req.Config,
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
	if st, ok := s.orgAppStore(); ok {
		vendor, _, _ := s.vendorForProvider(providerType)
		apps, lerr := st.ListOAuthClientConfigsByVendor(ctx, orgID, vendor)
		if lerr != nil {
			return lerr
		}
		switch len(apps) {
		case 0:
			return apperror.ErrNotFound
		case 1:
			return s.DeleteOrgOAuthApp(ctx, orgID, userID, apps[0].ID.String())
		default:
			return ambiguousAppConflict(vendor, apps)
		}
	}
	return s.orgStore().DeleteOAuthClientConfig(ctx, orgID, providerType)
}

// ---- project-scoped client CRUD ----

// ListProjectOAuthClients returns the project's configured OAuth clients
// (redacted). It requires project-admin authority.
func (s *Service) ListProjectOAuthClients(ctx context.Context, projectID uuid.UUID, userID string) ([]*OAuthClientConfigDTO, error) {
	if err := s.requireProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	rows, err := s.projectStore().ListProjectOAuthClientConfigs(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]*OAuthClientConfigDTO, 0, len(rows))
	for _, row := range rows {
		if _, ok := s.projectAppStore(); ok {
			vendor := s.appVendor(row)
			for _, p := range s.vendorProviders(vendor) {
				dto, derr := s.legacyDTO(ctx, row, p.Catalog().Type)
				if derr != nil {
					return nil, derr
				}
				out = append(out, dto)
			}
			continue
		}
		dto, err := s.toOAuthClientDTO(row)
		if err != nil {
			return nil, err
		}
		out = append(out, dto)
	}
	return out, nil
}

// GetProjectOAuthClient returns the project's client config for a provider
// (redacted). An unconfigured provider is not an error: it returns a DTO with
// Configured=false so the settings UI can render an empty form.
func (s *Service) GetProjectOAuthClient(ctx context.Context, projectID uuid.UUID, userID, providerType string) (*OAuthClientConfigDTO, error) {
	if err := s.requireProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	if _, err := s.oauthProvider(providerType); err != nil {
		return nil, err
	}
	if st, ok := s.projectAppStore(); ok {
		vendor, _, _ := s.vendorForProvider(providerType)
		apps, lerr := st.ListProjectOAuthClientConfigsByVendor(ctx, projectID, vendor)
		if lerr != nil {
			return nil, lerr
		}
		switch len(apps) {
		case 0:
			return &OAuthClientConfigDTO{ProviderType: providerType, Configured: false}, nil
		case 1:
			return s.legacyDTO(ctx, apps[0], providerType)
		default:
			return nil, ambiguousAppConflict(vendor, apps)
		}
	}
	row, err := s.projectStore().GetProjectOAuthClientConfig(ctx, projectID, providerType)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return &OAuthClientConfigDTO{ProviderType: providerType, Configured: false}, nil
		}
		return nil, err
	}
	return s.toOAuthClientDTO(row)
}

// UpsertProjectOAuthClient creates or replaces the project's client config for a
// provider. The client secret is encrypted at rest and never returned. On an
// update, an empty secret preserves the stored one.
func (s *Service) UpsertProjectOAuthClient(ctx context.Context, projectID uuid.UUID, userID, providerType string, req *SetOAuthClientRequest) (*OAuthClientConfigDTO, error) {
	if err := s.requireProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	if _, err := s.oauthProvider(providerType); err != nil {
		return nil, err
	}
	if st, ok := s.projectAppStore(); ok {
		vendor, vendorName, _ := s.vendorForProvider(providerType)
		apps, lerr := st.ListProjectOAuthClientConfigsByVendor(ctx, projectID, vendor)
		if lerr != nil {
			return nil, lerr
		}
		cfg := OAuthProviderConfig{Scopes: req.Scopes, Strategy: strings.TrimSpace(req.Strategy)}
		switch len(apps) {
		case 0:
			dto, cerr := s.CreateProjectOAuthApp(ctx, projectID, userID, &CreateOAuthAppRequest{
				Name:            vendorName,
				Vendor:          vendor,
				ClientID:        req.ClientID,
				ClientSecret:    req.ClientSecret,
				ProviderConfigs: map[string]OAuthProviderConfig{providerType: cfg},
				Config:          req.Config,
			})
			if cerr != nil {
				return nil, cerr
			}
			return s.legacyFromAppDTO(dto, providerType), nil
		case 1:
			clientID := strings.TrimSpace(req.ClientID)
			dto, uerr := s.UpdateProjectOAuthApp(ctx, projectID, userID, apps[0].ID.String(), &UpdateOAuthAppRequest{
				ClientID:        &clientID,
				ClientSecret:    req.ClientSecret,
				ProviderConfigs: mergeProviderConfig(apps[0].ProviderConfigs, providerType, cfg),
				Config:          req.Config,
			})
			if uerr != nil {
				return nil, uerr
			}
			return s.legacyFromAppDTO(dto, providerType), nil
		default:
			return nil, ambiguousAppConflict(vendor, apps)
		}
	}
	clientID := strings.TrimSpace(req.ClientID)
	if clientID == "" {
		return nil, apperror.NewValidation("clientId is required")
	}

	secret := req.ClientSecret
	existing, err := s.projectStore().GetProjectOAuthClientConfig(ctx, projectID, providerType)
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

	// A project-scoped row still carries its owning organization_id (the column
	// is NOT NULL), so resolve it from the project.
	orgID, err := s.projectStore().ResolveOrgID(ctx, projectID)
	if err != nil {
		return nil, err
	}

	vendor, vendorName, _ := s.vendorForProvider(providerType)
	saved, err := s.projectStore().UpsertProjectOAuthClientConfig(ctx, &OAuthClientConfig{
		OrganizationID:  orgID,
		ProjectID:       &projectID,
		Vendor:          vendor,
		Name:            vendorName,
		Provider:        providerType,
		EncryptedClient: blob,
		Scopes:          req.Scopes,
		Strategy:        strings.TrimSpace(req.Strategy),
		ProviderConfigs: map[string]OAuthProviderConfig{
			providerType: {Scopes: req.Scopes, Strategy: strings.TrimSpace(req.Strategy)},
		},
		Config: req.Config,
	})
	if err != nil {
		return nil, err
	}
	return s.toOAuthClientDTO(saved)
}

// DeleteProjectOAuthClient removes the project's client config for a provider. A
// missing row resolves to not-found.
func (s *Service) DeleteProjectOAuthClient(ctx context.Context, projectID uuid.UUID, userID, providerType string) error {
	if err := s.requireProjectAdmin(ctx, projectID, userID); err != nil {
		return err
	}
	if st, ok := s.projectAppStore(); ok {
		vendor, _, _ := s.vendorForProvider(providerType)
		apps, lerr := st.ListProjectOAuthClientConfigsByVendor(ctx, projectID, vendor)
		if lerr != nil {
			return lerr
		}
		switch len(apps) {
		case 0:
			return apperror.ErrNotFound
		case 1:
			return s.DeleteProjectOAuthApp(ctx, projectID, userID, apps[0].ID.String())
		default:
			return ambiguousAppConflict(vendor, apps)
		}
	}
	return s.projectStore().DeleteProjectOAuthClientConfig(ctx, projectID, providerType)
}

// ---- resolution + helpers ----

// resolveOAuthClient resolves the OAuth client for a provider and project. When
// the store supports named apps, selection is: explicit app → sole project app
// for the vendor → sole org app for the vendor → deployment default → actionable
// error. Stores that only implement the legacy per-provider interface keep the
// old project → org → deployment path.
func (s *Service) resolveOAuthClient(ctx context.Context, projectID uuid.UUID, providerType string, appID *uuid.UUID) (OAuthClient, error) {
	_, hasProjectApps := s.projectAppStore()
	_, hasOrgApps := s.orgAppStore()
	if !hasProjectApps && !hasOrgApps {
		return s.resolveOAuthClientLegacy(ctx, projectID, providerType)
	}

	vendor, _, ok := s.vendorForProvider(providerType)
	if !ok {
		return OAuthClient{}, apperror.NewValidation("unsupported providerType: " + providerType)
	}

	forceDeployment := appID != nil && *appID == uuid.Nil
	if appID != nil && *appID != uuid.Nil {
		row, err := s.getVisibleApp(ctx, projectID, *appID)
		if err != nil {
			return OAuthClient{}, err
		}
		if s.appVendor(row) != vendor {
			return OAuthClient{}, apperror.NewValidation("OAuth app " + appID.String() + " belongs to vendor " + s.appVendor(row) + ", not " + vendor)
		}
		return s.clientFromApp(row, providerType, s.orgStrategyForVendor(ctx, projectID, vendor, providerType))
	}

	if pst, ok := s.projectAppStore(); ok && !forceDeployment {
		apps, err := pst.ListProjectOAuthClientConfigsByVendor(ctx, projectID, vendor)
		if err != nil {
			return OAuthClient{}, err
		}
		if len(apps) == 1 {
			return s.clientFromApp(apps[0], providerType, s.orgStrategyForVendor(ctx, projectID, vendor, providerType))
		}
		if len(apps) > 1 {
			return OAuthClient{}, ambiguousAppError(vendor, apps)
		}
	}

	if ost, ok := s.orgAppStore(); ok && !forceDeployment {
		orgID, err := s.orgStore().ResolveOrgID(ctx, projectID)
		switch {
		case err == nil:
			apps, lerr := ost.ListOAuthClientConfigsByVendor(ctx, orgID, vendor)
			if lerr != nil {
				return OAuthClient{}, lerr
			}
			if len(apps) == 1 {
				return s.clientFromApp(apps[0], providerType, "")
			}
			if len(apps) > 1 {
				return OAuthClient{}, ambiguousAppError(vendor, apps)
			}
		case !errors.Is(err, apperror.ErrNotFound):
			return OAuthClient{}, err
		}
	}

	if c, ok := s.oauthClients[providerType]; ok && c.ClientID != "" && c.ClientSecret != "" {
		return c, nil
	}
	return OAuthClient{}, s.missingOAuthClientError(providerType)
}

// ambiguousAppError tells the caller to choose one of several applicable apps.
func ambiguousAppError(vendor string, apps []*OAuthClientConfig) error {
	names := make([]string, 0, len(apps))
	for _, app := range apps {
		names = append(names, app.Name)
	}
	return apperror.NewValidation("multiple OAuth apps configured for " + vendor + " (" + strings.Join(names, ", ") + "): choose one by setting oauthAppId")
}

// getVisibleApp loads an app the project may use: a project app owned by the
// project, or an org app owned by the project's organization.
func (s *Service) getVisibleApp(ctx context.Context, projectID, appID uuid.UUID) (*OAuthClientConfig, error) {
	if pst, ok := s.projectAppStore(); ok {
		if row, err := pst.GetProjectOAuthClientConfigByID(ctx, projectID, appID); err == nil {
			return row, nil
		} else if !errors.Is(err, apperror.ErrNotFound) {
			return nil, err
		}
	}
	if ost, ok := s.orgAppStore(); ok {
		orgID, err := s.orgStore().ResolveOrgID(ctx, projectID)
		if err == nil {
			if row, gerr := ost.GetOAuthClientConfigByID(ctx, orgID, appID); gerr == nil {
				return row, nil
			} else if !errors.Is(gerr, apperror.ErrNotFound) {
				return nil, gerr
			}
		} else if !errors.Is(err, apperror.ErrNotFound) {
			return nil, err
		}
	}
	return nil, apperror.NewValidation("OAuth app not found in this project or its organization")
}

// orgStrategyForVendor returns the strategy the org app for a vendor applies to
// a provider, or "". Used when a project app has no explicit strategy.
func (s *Service) orgStrategyForVendor(ctx context.Context, projectID uuid.UUID, vendor, providerType string) string {
	ost, ok := s.orgAppStore()
	if !ok {
		return ""
	}
	orgID, err := s.orgStore().ResolveOrgID(ctx, projectID)
	if err != nil {
		return ""
	}
	apps, err := ost.ListOAuthClientConfigsByVendor(ctx, orgID, vendor)
	if err != nil || len(apps) != 1 {
		return ""
	}
	return appProviderConfig(apps[0], providerType).Strategy
}

// resolveOAuthClientLegacy is the pre-app resolution path, kept for stores that
// only implement the per-provider interface.
//
// Scope-strategy resolution order (design D6), per client:
//  1. explicit scopes (stored on the winning row) win outright;
//  2. an explicit strategy on the winning row wins;
//  3. an empty project-row strategy inherits the org row's strategy for the
//     same provider (only the strategy is inherited — project credentials win);
//  4. when neither supplies a strategy, the provider default applies.
func (s *Service) resolveOAuthClientLegacy(ctx context.Context, projectID uuid.UUID, providerType string) (OAuthClient, error) {
	// Project override wins; it is keyed by projectID directly (no org
	// resolution).
	if s.projectOAuth != nil {
		row, perr := s.projectOAuth.GetProjectOAuthClientConfig(ctx, projectID, providerType)
		switch {
		case perr == nil:
			client, derr := s.decryptOAuthClientConfig(row)
			if derr != nil {
				return OAuthClient{}, derr
			}
			// An empty project strategy (with no explicit scopes) inherits the
			// org strategy for the same provider before falling back to the
			// provider default.
			inherited := ""
			if strings.TrimSpace(row.Strategy) == "" && len(client.Scopes) == 0 {
				inherited = s.orgStrategyForProject(ctx, projectID, providerType)
			}
			return s.applyOrgStrategy(providerType, client, row.Strategy, inherited), nil
		case !errors.Is(perr, apperror.ErrNotFound):
			return OAuthClient{}, perr
		}
		// not configured for this project → fall through to the org config
	}

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
				return s.applyOrgStrategy(providerType, client, row.Strategy, ""), nil
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
	return OAuthClient{}, s.missingOAuthClientError(providerType)
}

// missingOAuthClientError builds the validation error for a provider with no
// OAuth client (neither org config nor deployment default). It always names the
// provider and directs the user to the org OAuth client setting in words; the
// deployment-env-var hint is appended only for providers that have one (the
// Google-backed providers google_drive and gmail), so a github user is never
// told to set Google env vars.
func (s *Service) missingOAuthClientError(providerType string) error {
	msg := "no OAuth client configured for provider " + providerType +
		" (configure one under Organization settings → Data source OAuth clients)"
	if hint := s.oauthCfg.DeploymentEnvVarHint(providerType); hint != "" {
		msg += " or set " + hint
	}
	return apperror.NewValidation(msg)
}

// applyOrgStrategy folds a scope strategy into the resolved client (design D6):
// explicit scopes win outright; otherwise the provider maps the named strategy to
// its scope set. An empty strategy falls through to inherited (the org row's
// strategy for a project-scoped client) before giving up; a provider without
// strategy support (or an unknown strategy) is left unchanged, falling back to
// its declared default.
func (s *Service) applyOrgStrategy(providerType string, client OAuthClient, strategy, inherited string) OAuthClient {
	if len(client.Scopes) > 0 {
		return client
	}
	strategy = strings.TrimSpace(strategy)
	if strategy == "" {
		strategy = strings.TrimSpace(inherited)
	}
	if strategy == "" {
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
		client.Strategy = strategy
	}
	return client
}

// orgStrategyForProject returns the org-level strategy for a provider, used when
// a project-scoped row has no explicit strategy and no explicit scopes so the
// empty project strategy inherits the org strategy (design D6). It resolves the
// project's org and reads the org's client config; any failure (no org, no org
// row) yields "" so the provider default applies.
func (s *Service) orgStrategyForProject(ctx context.Context, projectID uuid.UUID, providerType string) string {
	if s.orgOAuth == nil {
		return ""
	}
	orgID, err := s.orgOAuth.ResolveOrgID(ctx, projectID)
	if err != nil {
		return ""
	}
	row, err := s.orgOAuth.GetOAuthClientConfig(ctx, orgID, providerType)
	if err != nil {
		return ""
	}
	return row.Strategy
}

// effectiveOAuthStrategy reports the strategy a project's source effectively
// uses for a provider: the resolved client's explicit strategy, otherwise the
// strategy whose scopes the client carries, otherwise "" (the provider default
// applies). It never fails — a missing or unreadable OAuth client means the
// deployment default, which is what the authorize request would use too.
func (s *Service) effectiveOAuthStrategy(ctx context.Context, projectID uuid.UUID, providerType string, appID *uuid.UUID) string {
	client, err := s.resolveOAuthClient(ctx, projectID, providerType, appID)
	if err != nil {
		return ""
	}
	if strategy := strings.TrimSpace(client.Strategy); strategy != "" {
		return strategy
	}
	if len(client.Scopes) == 0 {
		return ""
	}
	p, ok := s.providers.Get(providerType)
	if !ok {
		return ""
	}
	ss, ok := p.(ScopeStrategies)
	if !ok {
		return ""
	}
	return ss.StrategyForScopes(client.Scopes)
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

// requireProjectAdmin enforces project-admin authority, failing closed. An empty
// user yields unauthorized; a non-admin yields forbidden. The decision is the
// shared app-side project-administration check; org-admin authority does not
// qualify.
func (s *Service) requireProjectAdmin(ctx context.Context, projectID uuid.UUID, userID string) error {
	if strings.TrimSpace(userID) == "" {
		return apperror.ErrUnauthorized
	}
	if s.projectOAuth == nil {
		return apperror.ErrForbidden
	}
	ok, err := s.projectOAuth.IsProjectAdmin(ctx, projectID.String(), userID)
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

// projectStore returns the project-client store, or the repository when none
// was injected.
func (s *Service) projectStore() ProjectOAuthClientStore {
	if s.projectOAuth != nil {
		return s.projectOAuth
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
	return OAuthClient{ClientID: id, ClientSecret: secret, Scopes: row.Scopes, Config: row.Config}, nil
}

// toOAuthClientDTO maps a stored row to the redacted API shape.
func (s *Service) toOAuthClientDTO(row *OAuthClientConfig) (*OAuthClientConfigDTO, error) {
	client, err := s.decryptOAuthClientConfig(row)
	if err != nil {
		return nil, err
	}
	return &OAuthClientConfigDTO{
		ProviderType:     row.Provider,
		ClientID:         client.ClientID,
		RedirectURI:      s.redirectURIOrEmpty(row.Provider),
		JavascriptOrigin: s.oauthJavascriptOrigin(),
		Scopes:           row.Scopes,
		Strategy:         row.Strategy,
		Configured:       true,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
		Config:           row.Config,
	}, nil
}
