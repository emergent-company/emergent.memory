package sources

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// maxOAuthAppNameLength bounds the user-supplied app name before storage and
// render. It matches the org-name bound (domain/orgs.MaxOrgNameLength) so a name
// that fits that picker fits an OAuth app too.
const maxOAuthAppNameLength = 120

// OAuthAppDTO is the redacted API shape of a named, vendor-level OAuth app. The
// client secret is write-only and never appears here.
type OAuthAppDTO struct {
	ID               string                         `json:"id"`
	Scope            string                         `json:"scope"`
	Vendor           string                         `json:"vendor"`
	VendorName       string                         `json:"vendorName"`
	Name             string                         `json:"name"`
	ClientID         string                         `json:"clientId"`
	RedirectURIs     map[string]string              `json:"redirectUris,omitempty"`
	JavascriptOrigin string                         `json:"javascriptOrigin,omitempty"`
	ProviderConfigs  map[string]OAuthProviderConfig `json:"providerConfigs,omitempty"`
	Config           map[string]any                 `json:"config,omitempty"`
	Configured       bool                           `json:"configured"`
	UsedBy           int                            `json:"usedBy"`
	CreatedAt        time.Time                      `json:"createdAt"`
	UpdatedAt        time.Time                      `json:"updatedAt"`
	// legacyProviderType is set when this DTO came through the per-provider
	// compatibility API.
	legacyProviderType string
}

// CreateOAuthAppRequest is the write shape for a new named OAuth app.
type CreateOAuthAppRequest struct {
	Name            string                         `json:"name"`
	Vendor          string                         `json:"vendor"`
	ClientID        string                         `json:"clientId"`
	ClientSecret    string                         `json:"clientSecret"`
	ProviderConfigs map[string]OAuthProviderConfig `json:"providerConfigs,omitempty"`
	Config          map[string]any                 `json:"config,omitempty"`
}

// UpdateOAuthAppRequest is the partial-update shape. An empty client secret
// preserves the stored one.
type UpdateOAuthAppRequest struct {
	Name            *string                        `json:"name,omitempty"`
	ClientID        *string                        `json:"clientId,omitempty"`
	ClientSecret    string                         `json:"clientSecret,omitempty"`
	ProviderConfigs map[string]OAuthProviderConfig `json:"providerConfigs,omitempty"`
	Config          map[string]any                 `json:"config,omitempty"`
}

// ---- store accessors (optional app surface) ----

func (s *Service) orgAppStore() (OrgOAuthAppStore, bool) {
	if s.orgOAuth != nil {
		st, ok := s.orgOAuth.(OrgOAuthAppStore)
		return st, ok
	}
	if s.repo != nil {
		st, ok := any(s.repo).(OrgOAuthAppStore)
		return st, ok
	}
	return nil, false
}

func (s *Service) projectAppStore() (ProjectOAuthAppStore, bool) {
	if s.projectOAuth != nil {
		st, ok := s.projectOAuth.(ProjectOAuthAppStore)
		return st, ok
	}
	if s.repo != nil {
		st, ok := any(s.repo).(ProjectOAuthAppStore)
		return st, ok
	}
	return nil, false
}

// vendorForProvider returns the provider's vendor identity and display name.
func (s *Service) vendorForProvider(providerType string) (vendor, vendorName string, ok bool) {
	p, found := s.providers.Get(providerType)
	if !found {
		return "", "", false
	}
	c := p.Catalog()
	vendor = strings.TrimSpace(c.Vendor)
	if vendor == "" {
		vendor = c.Type
	}
	vendorName = strings.TrimSpace(c.VendorName)
	if vendorName == "" {
		vendorName = vendor
	}
	return vendor, vendorName, true
}

// vendorProviders returns every OAuth-capable provider of a vendor.
func (s *Service) vendorProviders(vendor string) []SourceProvider {
	out := make([]SourceProvider, 0)
	for _, p := range s.providers.List() {
		if _, ok := p.OAuth(); !ok {
			continue
		}
		c := p.Catalog()
		v := strings.TrimSpace(c.Vendor)
		if v == "" {
			v = c.Type
		}
		if v == vendor {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Catalog().Type < out[j].Catalog().Type })
	return out
}

// appVendor is the row's vendor, tolerating rows created before the migration
// (where only provider was set).
func (s *Service) appVendor(row *OAuthClientConfig) string {
	if v := strings.TrimSpace(row.Vendor); v != "" {
		return v
	}
	if v, _, ok := s.vendorForProvider(row.Provider); ok {
		return v
	}
	return strings.TrimSpace(row.Provider)
}

// appProviderConfig returns the per-provider scope configuration, falling back
// to the legacy scopes/strategy columns for a row created before the migration.
func appProviderConfig(row *OAuthClientConfig, providerType string) OAuthProviderConfig {
	if row.ProviderConfigs != nil {
		if cfg, ok := row.ProviderConfigs[providerType]; ok {
			return cfg
		}
	}
	if row.Provider == providerType || row.Provider == "" {
		return OAuthProviderConfig{Scopes: row.Scopes, Strategy: row.Strategy}
	}
	return OAuthProviderConfig{}
}

// ---- CRUD: organization scope ----

// ListOrgOAuthApps returns the org's named OAuth apps (secrets redacted). Org
// admin only.
func (s *Service) ListOrgOAuthApps(ctx context.Context, orgID uuid.UUID, userID string) ([]*OAuthAppDTO, error) {
	if err := s.requireOrgAdmin(ctx, orgID, userID); err != nil {
		return nil, err
	}
	rows, err := s.orgStore().ListOAuthClientConfigs(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return s.toOAuthAppDTOs(ctx, rows, CredentialScopeOrganization), nil
}

// GetOrgOAuthApp returns one org app by id. Org admin only.
func (s *Service) GetOrgOAuthApp(ctx context.Context, orgID uuid.UUID, userID, id string) (*OAuthAppDTO, error) {
	if err := s.requireOrgAdmin(ctx, orgID, userID); err != nil {
		return nil, err
	}
	appID, err := parseAppID(id)
	if err != nil {
		return nil, err
	}
	st, ok := s.orgAppStore()
	if !ok {
		return nil, apperror.NewInternal("load OAuth app: OAuth app store is not configured", nil)
	}
	row, err := st.GetOAuthClientConfigByID(ctx, orgID, appID)
	if err != nil {
		return nil, err
	}
	dto, err := s.toOAuthAppDTO(ctx, row, CredentialScopeOrganization)
	if err != nil {
		return nil, err
	}
	return dto, nil
}

// CreateOrgOAuthApp adds a named org app. Org admin only.
func (s *Service) CreateOrgOAuthApp(ctx context.Context, orgID uuid.UUID, userID string, req *CreateOAuthAppRequest) (*OAuthAppDTO, error) {
	if err := s.requireOrgAdmin(ctx, orgID, userID); err != nil {
		return nil, err
	}
	vendor, name, clientID, secret, configs, err := s.validateAppRequest(req.Vendor, req.Name, req.ClientID, req.ClientSecret, req.ProviderConfigs)
	if err != nil {
		return nil, err
	}
	if err := s.ensureAppNameFree(ctx, true, uuid.Nil, orgID, vendor, name, uuid.Nil); err != nil {
		return nil, err
	}
	blob, err := s.encryptCredentials(Credentials{oauthClientIDKey: clientID, oauthClientSecretKey: secret})
	if err != nil {
		return nil, err
	}
	st, ok := s.orgAppStore()
	if !ok {
		return nil, apperror.NewInternal("create OAuth app: OAuth app store is not configured", nil)
	}
	row, err := st.CreateOAuthClientConfig(ctx, &OAuthClientConfig{
		OrganizationID:  orgID,
		Vendor:          vendor,
		Name:            name,
		Provider:        vendor,
		EncryptedClient: blob,
		ProviderConfigs: configs,
		Config:          req.Config,
	})
	if err != nil {
		if duplicateError(err) {
			return nil, apperror.NewConflict("an OAuth app named " + name + " already exists for " + vendor)
		}
		return nil, err
	}
	return s.toOAuthAppDTO(ctx, row, CredentialScopeOrganization)
}

// UpdateOrgOAuthApp updates a named org app. Org admin only.
func (s *Service) UpdateOrgOAuthApp(ctx context.Context, orgID uuid.UUID, userID, id string, req *UpdateOAuthAppRequest) (*OAuthAppDTO, error) {
	if err := s.requireOrgAdmin(ctx, orgID, userID); err != nil {
		return nil, err
	}
	appID, err := parseAppID(id)
	if err != nil {
		return nil, err
	}
	st, ok := s.orgAppStore()
	if !ok {
		return nil, apperror.NewInternal("update OAuth app: OAuth app store is not configured", nil)
	}
	existing, err := st.GetOAuthClientConfigByID(ctx, orgID, appID)
	if err != nil {
		return nil, err
	}
	updated, err := s.applyAppUpdate(existing, req)
	if err != nil {
		return nil, err
	}
	if updated.Name != existing.Name {
		if err := s.ensureAppNameFree(ctx, true, uuid.Nil, orgID, s.appVendor(existing), updated.Name, existing.ID); err != nil {
			return nil, err
		}
	}
	saved, err := st.UpdateOAuthClientConfig(ctx, updated)
	if err != nil {
		if duplicateError(err) {
			return nil, apperror.NewConflict("an OAuth app named " + updated.Name + " already exists for " + s.appVendor(existing))
		}
		return nil, err
	}
	return s.toOAuthAppDTO(ctx, saved, CredentialScopeOrganization)
}

// DeleteOrgOAuthApp removes a named org app. It refuses while a source uses it.
func (s *Service) DeleteOrgOAuthApp(ctx context.Context, orgID uuid.UUID, userID, id string) error {
	if err := s.requireOrgAdmin(ctx, orgID, userID); err != nil {
		return err
	}
	appID, err := parseAppID(id)
	if err != nil {
		return err
	}
	st, ok := s.orgAppStore()
	if !ok {
		return apperror.NewInternal("delete OAuth app: OAuth app store is not configured", nil)
	}
	refs, err := st.SourcesUsingOAuthConfig(ctx, appID)
	if err != nil {
		return err
	}
	if len(refs) > 0 {
		return oauthAppInUseError(refs)
	}
	return st.DeleteOAuthClientConfigByID(ctx, orgID, appID)
}

// ---- CRUD: project scope ----

// ListProjectOAuthApps returns the project's named OAuth apps. Member-readable:
// the credentials page is visible to every project member.
func (s *Service) ListProjectOAuthApps(ctx context.Context, projectID uuid.UUID) ([]*OAuthAppDTO, error) {
	rows, err := s.projectStore().ListProjectOAuthClientConfigs(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := s.toOAuthAppDTOs(ctx, rows, CredentialScopeProject)
	// Organization apps are usable by every project in the org, so the member-safe
	// project list includes them (the route enforces project membership).
	if _, ok := s.orgAppStore(); ok {
		if orgID, oerr := s.orgStore().ResolveOrgID(ctx, projectID); oerr == nil {
			if orgRows, lerr := s.orgStore().ListOAuthClientConfigs(ctx, orgID); lerr == nil {
				out = append(out, s.toOAuthAppDTOs(ctx, orgRows, CredentialScopeOrganization)...)
			}
		}
	}
	return out, nil
}

// GetProjectOAuthApp returns one project app by id. Member-readable.
func (s *Service) GetProjectOAuthApp(ctx context.Context, projectID uuid.UUID, id string) (*OAuthAppDTO, error) {
	appID, err := parseAppID(id)
	if err != nil {
		return nil, err
	}
	st, ok := s.projectAppStore()
	if !ok {
		return nil, apperror.NewInternal("load project OAuth app: OAuth app store is not configured", nil)
	}
	row, err := st.GetProjectOAuthClientConfigByID(ctx, projectID, appID)
	if err != nil {
		return nil, err
	}
	return s.toOAuthAppDTO(ctx, row, CredentialScopeProject)
}

// CreateProjectOAuthApp adds a named project app. Project admin only.
func (s *Service) CreateProjectOAuthApp(ctx context.Context, projectID uuid.UUID, userID string, req *CreateOAuthAppRequest) (*OAuthAppDTO, error) {
	if err := s.requireProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	vendor, name, clientID, secret, configs, err := s.validateAppRequest(req.Vendor, req.Name, req.ClientID, req.ClientSecret, req.ProviderConfigs)
	if err != nil {
		return nil, err
	}
	if err := s.ensureAppNameFree(ctx, false, projectID, uuid.Nil, vendor, name, uuid.Nil); err != nil {
		return nil, err
	}
	blob, err := s.encryptCredentials(Credentials{oauthClientIDKey: clientID, oauthClientSecretKey: secret})
	if err != nil {
		return nil, err
	}
	orgID, err := s.projectStore().ResolveOrgID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	st, ok := s.projectAppStore()
	if !ok {
		return nil, apperror.NewInternal("create project OAuth app: OAuth app store is not configured", nil)
	}
	row, err := st.CreateProjectOAuthClientConfig(ctx, &OAuthClientConfig{
		OrganizationID:  orgID,
		ProjectID:       &projectID,
		Vendor:          vendor,
		Name:            name,
		Provider:        vendor,
		EncryptedClient: blob,
		ProviderConfigs: configs,
		Config:          req.Config,
	})
	if err != nil {
		if duplicateError(err) {
			return nil, apperror.NewConflict("an OAuth app named " + name + " already exists for " + vendor)
		}
		return nil, err
	}
	return s.toOAuthAppDTO(ctx, row, CredentialScopeProject)
}

// UpdateProjectOAuthApp updates a named project app. Project admin only.
func (s *Service) UpdateProjectOAuthApp(ctx context.Context, projectID uuid.UUID, userID, id string, req *UpdateOAuthAppRequest) (*OAuthAppDTO, error) {
	if err := s.requireProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	appID, err := parseAppID(id)
	if err != nil {
		return nil, err
	}
	st, ok := s.projectAppStore()
	if !ok {
		return nil, apperror.NewInternal("update project OAuth app: OAuth app store is not configured", nil)
	}
	existing, err := st.GetProjectOAuthClientConfigByID(ctx, projectID, appID)
	if err != nil {
		return nil, err
	}
	updated, err := s.applyAppUpdate(existing, req)
	if err != nil {
		return nil, err
	}
	if updated.Name != existing.Name {
		if err := s.ensureAppNameFree(ctx, false, projectID, uuid.Nil, s.appVendor(existing), updated.Name, existing.ID); err != nil {
			return nil, err
		}
	}
	saved, err := st.UpdateProjectOAuthClientConfig(ctx, updated)
	if err != nil {
		if duplicateError(err) {
			return nil, apperror.NewConflict("an OAuth app named " + updated.Name + " already exists for " + s.appVendor(existing))
		}
		return nil, err
	}
	return s.toOAuthAppDTO(ctx, saved, CredentialScopeProject)
}

// DeleteProjectOAuthApp removes a named project app. It refuses while a source
// uses it.
func (s *Service) DeleteProjectOAuthApp(ctx context.Context, projectID uuid.UUID, userID, id string) error {
	if err := s.requireProjectAdmin(ctx, projectID, userID); err != nil {
		return err
	}
	appID, err := parseAppID(id)
	if err != nil {
		return err
	}
	pst, ok := s.projectAppStore()
	if !ok {
		return apperror.NewInternal("OAuth apps are not supported by this store", nil)
	}
	refs, err := pst.SourcesUsingProjectOAuthConfig(ctx, projectID, appID)
	if err != nil {
		return err
	}
	if len(refs) > 0 {
		return oauthAppInUseError(refs)
	}
	return pst.DeleteProjectOAuthClientConfigByID(ctx, projectID, appID)
}

// ---- helpers ----

// oauthAppInUseError refuses a delete while real sources use the app and names
// them, so the operator can disconnect or repoint each one instead of chasing a
// bare count (#554). At most five are named; the rest collapse into a count.
func oauthAppInUseError(refs []OAuthAppSourceRef) error {
	const maxNamed = 5
	labels := make([]string, 0, min(len(refs), maxNamed)+1)
	for i, ref := range refs {
		if i == maxNamed {
			labels = append(labels, fmt.Sprintf("and %d more", len(refs)-maxNamed))
			break
		}
		name := strings.TrimSpace(ref.DisplayName)
		if name == "" {
			name = "untitled source"
		}
		labels = append(labels, fmt.Sprintf("%s (%s)", name, ref.ID))
	}
	return apperror.NewConflict(fmt.Sprintf(
		"OAuth app is used by %d data source(s): %s; disconnect or repoint them before deleting",
		len(refs), strings.Join(labels, ", ")))
}

func parseAppID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return uuid.Nil, apperror.NewValidation("invalid OAuth app id")
	}
	return id, nil
}

func (s *Service) validateAppRequest(vendor, name, clientID, secret string, configs map[string]OAuthProviderConfig) (string, string, string, string, map[string]OAuthProviderConfig, error) {
	vendor = strings.TrimSpace(vendor)
	name = strings.TrimSpace(name)
	clientID = strings.TrimSpace(clientID)
	if vendor == "" {
		return "", "", "", "", nil, apperror.NewValidation("vendor is required")
	}
	if len(s.vendorProviders(vendor)) == 0 {
		return "", "", "", "", nil, apperror.NewValidation("no OAuth provider uses vendor " + vendor)
	}
	if name == "" {
		return "", "", "", "", nil, apperror.NewValidation("name is required")
	}
	if utf8.RuneCountInString(name) > maxOAuthAppNameLength {
		return "", "", "", "", nil, apperror.NewValidation(fmt.Sprintf("name must be %d characters or fewer", maxOAuthAppNameLength))
	}
	if clientID == "" {
		return "", "", "", "", nil, apperror.NewValidation("clientId is required")
	}
	if secret == "" {
		return "", "", "", "", nil, apperror.NewValidation("clientSecret is required")
	}
	normalized, err := s.normalizeProviderConfigs(vendor, configs)
	if err != nil {
		return "", "", "", "", nil, err
	}
	return vendor, name, clientID, secret, normalized, nil
}

func (s *Service) normalizeProviderConfigs(vendor string, configs map[string]OAuthProviderConfig) (map[string]OAuthProviderConfig, error) {
	out := map[string]OAuthProviderConfig{}
	allowed := map[string]SourceProvider{}
	for _, p := range s.vendorProviders(vendor) {
		allowed[p.Catalog().Type] = p
	}
	for providerType, cfg := range configs {
		p, ok := allowed[providerType]
		if !ok {
			return nil, apperror.NewValidation("provider " + providerType + " does not belong to vendor " + vendor)
		}
		cfg.Strategy = strings.TrimSpace(cfg.Strategy)
		if cfg.Strategy != "" {
			ss, ok := p.(ScopeStrategies)
			if !ok {
				return nil, apperror.NewValidation("provider " + providerType + " does not support OAuth scope strategies")
			}
			if _, ok := ss.ScopesForStrategy(cfg.Strategy); !ok {
				return nil, apperror.NewValidation("unknown OAuth scope strategy " + cfg.Strategy + " for provider " + providerType)
			}
		}
		out[providerType] = cfg
	}
	return out, nil
}

// applyAppUpdate merges a partial update into the stored row. Rotating the
// client id or secret only changes the app: sources keep the encrypted grant
// they received at connect time (service_oauth.go merge) and keep refreshing
// with the old client until they are reconnected. Do not claim otherwise in the
// UI or docs.
func (s *Service) applyAppUpdate(existing *OAuthClientConfig, req *UpdateOAuthAppRequest) (*OAuthClientConfig, error) {
	updated := *existing
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, apperror.NewValidation("name is required")
		}
		if utf8.RuneCountInString(name) > maxOAuthAppNameLength {
			return nil, apperror.NewValidation(fmt.Sprintf("name must be %d characters or fewer", maxOAuthAppNameLength))
		}
		updated.Name = name
	}
	if req.ClientID != nil {
		id := strings.TrimSpace(*req.ClientID)
		if id == "" {
			return nil, apperror.NewValidation("clientId is required")
		}
		creds, err := s.decryptCredentials(existing.EncryptedClient)
		if err != nil {
			return nil, err
		}
		creds[oauthClientIDKey] = id
		blob, err := s.encryptCredentials(creds)
		if err != nil {
			return nil, err
		}
		updated.EncryptedClient = blob
	}
	if req.ClientSecret != "" {
		creds, err := s.decryptCredentials(updated.EncryptedClient)
		if err != nil {
			return nil, err
		}
		creds[oauthClientSecretKey] = req.ClientSecret
		blob, err := s.encryptCredentials(creds)
		if err != nil {
			return nil, err
		}
		updated.EncryptedClient = blob
	}
	if req.ProviderConfigs != nil {
		vendor := s.appVendor(existing)
		normalized, err := s.normalizeProviderConfigs(vendor, req.ProviderConfigs)
		if err != nil {
			return nil, err
		}
		updated.ProviderConfigs = normalized
	}
	if req.Config != nil {
		updated.Config = req.Config
	}
	return &updated, nil
}

func (s *Service) toOAuthAppDTOs(ctx context.Context, rows []*OAuthClientConfig, scope string) []*OAuthAppDTO {
	out := make([]*OAuthAppDTO, 0, len(rows))
	for _, row := range rows {
		dto, err := s.toOAuthAppDTO(ctx, row, scope)
		if err != nil {
			// A row whose secret cannot be decrypted must not make the whole list
			// unreadable; report it as not configured.
			continue
		}
		out = append(out, dto)
	}
	return out
}

func (s *Service) toOAuthAppDTO(ctx context.Context, row *OAuthClientConfig, scope string) (*OAuthAppDTO, error) {
	client, err := s.decryptOAuthClientConfig(row)
	if err != nil {
		return nil, err
	}
	vendor := s.appVendor(row)
	vendorName := s.vendorDisplayName(vendor)
	redirects := map[string]string{}
	for _, p := range s.vendorProviders(vendor) {
		pt := p.Catalog().Type
		if uri := s.redirectURIOrEmpty(pt); uri != "" {
			redirects[pt] = uri
		}
	}
	usedBy := 0
	if st, ok := s.orgAppStore(); ok {
		if n, cerr := st.CountSourcesUsingOAuthConfig(ctx, row.ID); cerr == nil {
			usedBy = n
		}
	}
	return &OAuthAppDTO{
		ID:               row.ID.String(),
		Scope:            scope,
		Vendor:           vendor,
		VendorName:       vendorName,
		Name:             row.Name,
		ClientID:         client.ClientID,
		RedirectURIs:     redirects,
		JavascriptOrigin: s.oauthJavascriptOrigin(),
		ProviderConfigs:  row.ProviderConfigs,
		Config:           row.Config,
		Configured:       true,
		UsedBy:           usedBy,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}, nil
}

// clientFromApp decrypts an app row and applies the provider's scope
// configuration. inheritedStrategy is used only when the app has no explicit
// strategy for the provider (project app inheriting the org app's strategy).
func (s *Service) clientFromApp(row *OAuthClientConfig, providerType, inheritedStrategy string) (OAuthClient, error) {
	client, err := s.decryptOAuthClientConfig(row)
	if err != nil {
		return OAuthClient{}, err
	}
	// A vendor app backs several providers with different scopes. Never carry
	// the legacy row's provider-specific scopes into a different provider.
	client.Scopes = nil
	client.Strategy = ""
	cfg := appProviderConfig(row, providerType)
	if len(cfg.Scopes) > 0 {
		client.Scopes = cfg.Scopes
		client.Strategy = cfg.Strategy
		return client, nil
	}
	strategy := cfg.Strategy
	if strategy == "" {
		strategy = inheritedStrategy
	}
	if strategy == "" {
		return client, nil
	}
	return s.applyOrgStrategy(providerType, client, strategy, ""), nil
}

// validateSourceApp parses and validates an optional source app selection.
// Stores without the named-app surface (unit fakes) accept the id as-is.
func (s *Service) validateSourceApp(ctx context.Context, projectID uuid.UUID, providerType string, raw *string) (*uuid.UUID, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(*raw))
	if err != nil {
		return nil, apperror.NewValidation("invalid OAuth app id")
	}
	_, hasProjectApps := s.projectAppStore()
	_, hasOrgApps := s.orgAppStore()
	if !hasProjectApps && !hasOrgApps {
		return &id, nil
	}
	row, err := s.getVisibleApp(ctx, projectID, id)
	if err != nil {
		return nil, err
	}
	vendor, _, ok := s.vendorForProvider(providerType)
	if !ok {
		return nil, apperror.NewValidation("unsupported providerType: " + providerType)
	}
	if s.appVendor(row) != vendor {
		return nil, apperror.NewValidation("OAuth app " + id.String() + " belongs to vendor " + s.appVendor(row) + ", not " + vendor)
	}
	return &id, nil
}

// legacyDTO maps an app row into the per-provider DTO shape used by the
// deprecated oauth-clients endpoints.
func (s *Service) legacyDTO(ctx context.Context, row *OAuthClientConfig, providerType string) (*OAuthClientConfigDTO, error) {
	dto, err := s.toOAuthClientDTO(row)
	if err != nil {
		return nil, err
	}
	dto.ProviderType = providerType
	dto.RedirectURI = s.redirectURIOrEmpty(providerType)
	cfg := appProviderConfig(row, providerType)
	dto.Scopes = cfg.Scopes
	dto.Strategy = cfg.Strategy
	return dto, nil
}

// mergeProviderConfig returns a copy of configs with one provider's entry set.
func mergeProviderConfig(configs map[string]OAuthProviderConfig, providerType string, cfg OAuthProviderConfig) map[string]OAuthProviderConfig {
	out := map[string]OAuthProviderConfig{}
	for k, v := range configs {
		out[k] = v
	}
	out[providerType] = cfg
	return out
}

func (s *Service) legacyFromAppDTO(dto *OAuthAppDTO, providerType string) *OAuthClientConfigDTO {
	cfg := dto.ProviderConfigs[providerType]
	return &OAuthClientConfigDTO{
		ProviderType:     providerType,
		ClientID:         dto.ClientID,
		RedirectURI:      dto.RedirectURIs[providerType],
		JavascriptOrigin: dto.JavascriptOrigin,
		Scopes:           cfg.Scopes,
		Strategy:         cfg.Strategy,
		Configured:       true,
		CreatedAt:        dto.CreatedAt,
		UpdatedAt:        dto.UpdatedAt,
		Config:           dto.Config,
	}
}

// implicitSourceAppID returns the sole app a provider would resolve to at
// project or org scope. It is persisted on new sources so adding a second app
// later cannot silently change an existing source's client.
//
// Only a provider that actually offers OAuth may be bound. A credentials (PAT)
// or config-only source never reads oauth_app_id, so binding it would make the
// app undeletable while the refusal names a source that does not use OAuth at
// all (#554). The gate uses the same AuthMethods vocabulary the connect flow
// advertises, so it cannot drift from the catalog.
func (s *Service) implicitSourceAppID(ctx context.Context, projectID uuid.UUID, providerType string) *uuid.UUID {
	p, found := s.providers.Get(providerType)
	if !found || !slices.Contains(p.AuthMethods(), AuthMethodOAuth) {
		return nil
	}
	vendor, _, ok := s.vendorForProvider(providerType)
	if !ok {
		return nil
	}
	if pst, ok := s.projectAppStore(); ok {
		if apps, err := pst.ListProjectOAuthClientConfigsByVendor(ctx, projectID, vendor); err == nil && len(apps) == 1 {
			id := apps[0].ID
			return &id
		}
	}
	if ost, ok := s.orgAppStore(); ok {
		orgID, err := s.orgStore().ResolveOrgID(ctx, projectID)
		if err == nil {
			if apps, lerr := ost.ListOAuthClientConfigsByVendor(ctx, orgID, vendor); lerr == nil && len(apps) == 1 {
				id := apps[0].ID
				return &id
			}
		}
	}
	return nil
}

// ambiguousAppConflict is the legacy per-provider shim's answer when a vendor
// has several apps: the old API cannot name one, so it reports a conflict.
func ambiguousAppConflict(vendor string, apps []*OAuthClientConfig) error {
	names := make([]string, 0, len(apps))
	for _, app := range apps {
		names = append(names, app.Name)
	}
	return apperror.NewConflict("multiple OAuth apps configured for " + vendor + " (" + strings.Join(names, ", ") + "): manage them as named apps")
}

// ensureAppNameFree rejects a case-insensitive duplicate name in the same scope
// and vendor before the database's unique index turns it into a 500.
func (s *Service) ensureAppNameFree(ctx context.Context, isOrg bool, projectID, orgID uuid.UUID, vendor, name string, exclude uuid.UUID) error {
	var apps []*OAuthClientConfig
	var err error
	if isOrg {
		st, ok := s.orgAppStore()
		if !ok {
			return apperror.NewInternal("check OAuth app name: OAuth app store is not configured", nil)
		}
		apps, err = st.ListOAuthClientConfigsByVendor(ctx, orgID, vendor)
	} else {
		st, ok := s.projectAppStore()
		if !ok {
			return apperror.NewInternal("check project OAuth app name: OAuth app store is not configured", nil)
		}
		apps, err = st.ListProjectOAuthClientConfigsByVendor(ctx, projectID, vendor)
	}
	if err != nil {
		return err
	}
	for _, app := range apps {
		if app.ID != exclude && strings.EqualFold(app.Name, name) {
			return apperror.NewConflict("an OAuth app named " + name + " already exists for " + vendor)
		}
	}
	return nil
}

// vendorDisplayName resolves the human label for a vendor from the catalog,
// independent of which provider row happens to be stored on the app.
func (s *Service) vendorDisplayName(vendor string) string {
	for _, p := range s.vendorProviders(vendor) {
		if name := strings.TrimSpace(p.Catalog().VendorName); name != "" {
			return name
		}
	}
	if vendor == "" {
		return ""
	}
	return strings.ToUpper(vendor[:1]) + vendor[1:]
}
