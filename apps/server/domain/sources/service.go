package sources

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Service handles business logic for data sources. It resolves providers from
// the Registry and never branches on a concrete provider.
type Service struct {
	repo      *Repository
	key       []byte // raw AES-256 key bytes (hex-decoded from config)
	providers *Registry
	log       *slog.Logger

	oauthCfg     config.SourceOAuthConfig
	oauthClients map[string]OAuthClient // deployment-default clients by provider
	oauthHTTP    *http.Client

	// orgOAuth resolves and manages org-scoped OAuth client configs. It defaults
	// to the repository and is overridable in tests.
	orgOAuth OrgOAuthClientStore

	// projectOAuth resolves and manages project-scoped OAuth client configs. It
	// defaults to the repository and is overridable in tests.
	projectOAuth ProjectOAuthClientStore
}

// ServiceOption customises a Service at construction (keeps the API setter-free).
type ServiceOption func(*Service)

// WithOAuthClients overrides the deployment-default OAuth clients by provider.
func WithOAuthClients(clients map[string]OAuthClient) ServiceOption {
	return func(s *Service) { s.oauthClients = clients }
}

// WithOAuthHTTPClient overrides the HTTP client used for token exchange.
func WithOAuthHTTPClient(c *http.Client) ServiceOption {
	return func(s *Service) {
		if c != nil {
			s.oauthHTTP = c
		}
	}
}

// WithOrgOAuthClientStore overrides the org OAuth client store (defaults to the
// repository). Used by tests.
func WithOrgOAuthClientStore(store OrgOAuthClientStore) ServiceOption {
	return func(s *Service) {
		if store != nil {
			s.orgOAuth = store
		}
	}
}

// WithProjectOAuthClientStore overrides the project OAuth client store
// (defaults to the repository). Used by tests.
func WithProjectOAuthClientStore(store ProjectOAuthClientStore) ServiceOption {
	return func(s *Service) {
		if store != nil {
			s.projectOAuth = store
		}
	}
}

// NewService creates a new sources service. The credential-encryption key is
// derived from config.Config.LLMProvider.EncryptionKey — the same config-sourced
// key the provider domain uses — so provider credentials and source credentials
// share one operator-managed secret (LLM_ENCRYPTION_KEY). The provider registry
// is injected (fx supplies NewConfiguredRegistry); a nil registry falls back to
// the default set.
func NewService(repo *Repository, cfg *config.Config, log *slog.Logger, registry *Registry, opts ...ServiceOption) *Service {
	if registry == nil {
		registry = NewDefaultRegistry()
	}
	s := &Service{
		repo:         repo,
		providers:    registry,
		oauthClients: map[string]OAuthClient{},
		// oauthHTTP stays nil in production: a dynamic (self-registering) provider
		// must build its own SSRF-guarded client, and the static providers fall back
		// to http.DefaultClient for their fixed vendor endpoints. Tests inject a
		// client with WithOAuthHTTPClient to reach loopback fakes.
		oauthHTTP: nil,
		log:       log.With(logger.Scope("sources.svc")),
	}
	if repo != nil {
		s.orgOAuth = repo
		s.projectOAuth = repo
	}
	if cfg != nil {
		s.oauthCfg = cfg.SourceOAuth
		for provider, d := range cfg.SourceOAuth.OAuthClientsByProvider() {
			s.oauthClients[provider] = OAuthClient{ClientID: d.ClientID, ClientSecret: d.ClientSecret}
		}
	}
	if cfg != nil && cfg.LLMProvider.IsEncryptionConfigured() {
		if key, err := hex.DecodeString(cfg.LLMProvider.EncryptionKey); err == nil {
			s.key = key
		} else {
			s.log.Error("invalid credential encryption key", logger.Error(err))
		}
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Registry exposes the provider registry (wiring/inspection).
func (s *Service) Registry() *Registry { return s.providers }

// ---- Provider catalog + schema ----

// ListProviders returns the registered data-source providers' catalog entries.
// The catalog advertises each provider's auth method(s) so the connect UI can be
// provider-agnostic (design D5).
func (s *Service) ListProviders(ctx context.Context) []Provider {
	ps := s.providers.List()
	out := make([]Provider, len(ps))
	for i, p := range ps {
		entry := p.Catalog()
		methods := p.AuthMethods()
		entry.AuthMethods = make([]string, len(methods))
		for j, m := range methods {
			entry.AuthMethods[j] = string(m)
		}
		if ss, ok := p.(ScopeStrategies); ok {
			strategies := ss.Strategies()
			entry.Strategies = make([]string, len(strategies))
			for j, s := range strategies {
				entry.Strategies[j] = string(s)
			}
		}
		out[i] = entry
	}
	return out
}

// GetProviderSchema returns the config schema for a provider type.
func (s *Service) GetProviderSchema(ctx context.Context, providerType string) (*ProviderSchema, error) {
	p, ok := s.providers.Get(providerType)
	if !ok {
		return nil, apperror.ErrNotFound
	}
	schema := p.ConfigSchema()
	// Credential fields are form fields too: merge them into the schema as
	// write-only properties so the connect form renders one schema-driven
	// surface for every provider. Only the shape travels here; values never do.
	for _, f := range p.CredentialFields() {
		if schema.Properties == nil {
			schema.Properties = map[string]any{}
		}
		if _, exists := schema.Properties[f.Key]; exists {
			continue
		}
		typ := f.Type
		if typ == "" {
			typ = "string"
		}
		prop := map[string]any{"type": typ, "writeOnly": true}
		if f.Label != "" {
			prop["label"] = f.Label
		}
		if f.Description != "" {
			prop["description"] = f.Description
		}
		schema.Properties[f.Key] = prop
		if f.Required {
			schema.Required = append(schema.Required, f.Key)
		}
	}
	return &schema, nil
}

// ListSourceTypes returns document counts grouped by source type for a project.
func (s *Service) ListSourceTypes(ctx context.Context, projectID uuid.UUID) ([]SourceType, error) {
	return s.repo.ListSourceTypes(ctx, projectID)
}

// ---- Connection testing ----

// TestConfig tests an unsaved provider config, building a connector from the
// credentials carried in the request config. It never persists anything.
func (s *Service) TestConfig(ctx context.Context, req *TestConfigRequest) (*TestConnectionResponse, error) {
	p, ok := s.providers.Get(req.ProviderType)
	if !ok {
		return nil, apperror.NewValidation("unsupported providerType: " + req.ProviderType)
	}
	if err := p.ValidateConfig(req.Config); err != nil {
		return nil, err
	}
	creds, err := p.ExtractCredentials(req.Config)
	if err != nil {
		return nil, err
	}
	return s.testProviderConnection(ctx, p, creds, req.Config)
}

// ListProviderResources lists a provider's selectable sync targets (repositories,
// folders) from an unsaved config. It never persists the config or credentials,
// and it does not require the full sync config: only credentials are needed.
func (s *Service) ListProviderResources(ctx context.Context, req *ProviderListRequest) ([]Resource, bool, error) {
	p, creds, err := s.unsavedProviderCredentials(req)
	if err != nil {
		return nil, false, err
	}
	// The connect form filters suggestions client-side, so no name query is
	// needed here; truncated is still reported for providers that cap.
	resources, truncated, err := p.ListResources(ctx, creds, req.Config, "")
	if err != nil {
		return nil, false, providerListError("list resources failed", err)
	}
	if resources == nil {
		resources = []Resource{}
	}
	return resources, truncated, nil
}

// ListProviderRefs lists the refs of the resource named in an unsaved config (a
// GitHub repository's branches and tags). Providers without ref support return a
// validation error. truncated reports that the provider returned only a page.
func (s *Service) ListProviderRefs(ctx context.Context, req *ProviderListRequest) ([]Resource, bool, error) {
	p, creds, err := s.unsavedProviderCredentials(req)
	if err != nil {
		return nil, false, err
	}
	lister, ok := p.(ResourceRefLister)
	if !ok {
		return nil, false, apperror.NewValidation("provider " + req.ProviderType + " does not support ref listing")
	}
	refs, truncated, err := lister.ListRefs(ctx, creds, req.Config)
	if err != nil {
		return nil, false, providerListError("list refs failed", err)
	}
	if refs == nil {
		refs = []Resource{}
	}
	return refs, truncated, nil
}

// providerListError turns a provider listing failure into a validation error
// with a stable prefix. An error that already carries a user-facing apperror
// message (an invalid repository, for example) passes through unchanged so the
// message is not double-prefixed.
func providerListError(prefix string, err error) error {
	var ae *apperror.Error
	if errors.As(err, &ae) {
		return err
	}
	return apperror.NewValidation(prefix + ": " + err.Error())
}

// unsavedProviderCredentials resolves a provider and extracts its credentials
// from an unsaved config, without validating the rest of the config.
func (s *Service) unsavedProviderCredentials(req *ProviderListRequest) (SourceProvider, Credentials, error) {
	p, ok := s.providers.Get(req.ProviderType)
	if !ok {
		return nil, nil, apperror.NewValidation("unsupported providerType: " + req.ProviderType)
	}
	creds, err := p.ExtractCredentials(req.Config)
	if err != nil {
		return nil, nil, err
	}
	return p, creds, nil
}

// TestConnection tests an existing source's connection by decrypting its stored
// credentials and performing a bounded provider call.
func (s *Service) TestConnection(ctx context.Context, projectID, id uuid.UUID) (*TestConnectionResponse, error) {
	src, err := s.repo.GetByIDWithCredentials(ctx, id, projectID)
	if err != nil {
		return nil, err
	}
	p, ok := s.providers.Get(src.Provider)
	if !ok {
		return &TestConnectionResponse{Success: false, Message: "unknown provider: " + src.Provider}, nil
	}
	creds, err := s.decryptCredentials(src.EncryptedCredentials)
	if err != nil {
		return &TestConnectionResponse{Success: false, Message: err.Error()}, nil
	}
	return s.testProviderConnection(ctx, p, creds, src.Config)
}

func (s *Service) testProviderConnection(ctx context.Context, p SourceProvider, creds Credentials, cfg map[string]any) (*TestConnectionResponse, error) {
	connector, err := p.NewConnector(ctx, creds, ConnectorDeps{})
	if err != nil {
		return &TestConnectionResponse{Success: false, Message: "failed to initialize connector: " + err.Error()}, nil
	}
	if err := connector.TestConnection(ctx, cfg); err != nil {
		return &TestConnectionResponse{Success: false, Message: err.Error()}, nil
	}
	return &TestConnectionResponse{Success: true, Message: "connection successful"}, nil
}

// ListResources lists the selectable sync targets for an existing source (a
// Drive folder, a GitHub repository) by decrypting its stored credentials and
// asking its provider. query is an optional name filter passed through to the
// provider; truncated reports that the provider capped its listing. Backs the
// post-consent resource-selection step.
func (s *Service) ListResources(ctx context.Context, projectID, id uuid.UUID, query string) ([]Resource, bool, error) {
	src, err := s.repo.GetByIDWithCredentials(ctx, id, projectID)
	if err != nil {
		return nil, false, err
	}
	p, ok := s.providers.Get(src.Provider)
	if !ok {
		return nil, false, apperror.NewValidation("unsupported providerType: " + src.Provider)
	}
	creds, err := s.decryptCredentials(src.EncryptedCredentials)
	if err != nil {
		return nil, false, err
	}
	resources, truncated, err := p.ListResources(ctx, creds, src.Config, query)
	if err != nil {
		return nil, false, apperror.NewValidation("list resources failed: " + err.Error())
	}
	if resources == nil {
		resources = []Resource{}
	}
	return resources, truncated, nil
}

// ---- CRUD ----

// List returns all sources for a project.
func (s *Service) List(ctx context.Context, projectID uuid.UUID) ([]*Integration, error) {
	srcs, err := s.repo.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]*Integration, len(srcs))
	for i, src := range srcs {
		out[i] = s.toIntegration(src)
	}
	return out, nil
}

// Create validates the request, encrypts the supplied credentials, strips the
// credential keys from the persisted config, and stores the source.
func (s *Service) Create(ctx context.Context, projectID uuid.UUID, req *CreateIntegrationRequest) (*Integration, error) {
	p, ok := s.providers.Get(req.ProviderType)
	if !ok {
		return nil, apperror.NewValidation("unsupported providerType: " + req.ProviderType)
	}
	if !req.AllowIncompleteConfig {
		if err := p.ValidateConfig(req.Config); err != nil {
			return nil, err
		}
	}
	creds, err := p.ExtractCredentials(req.Config)
	if err != nil {
		return nil, err
	}
	blob, err := s.encryptCredentials(creds)
	if err != nil {
		return nil, err
	}

	// Apply a provider's sync-cadence bounds (default + minimum) when it
	// declares them. Providers without the interface keep today's behaviour:
	// whatever the request carried, or nil.
	interval, err := applySyncIntervalBounds(p, req.SyncIntervalMinutes)
	if err != nil {
		return nil, err
	}

	now := s.now()
	src := &Source{
		ID:                   uuid.New(),
		ProjectID:            projectID,
		Provider:             req.ProviderType,
		DisplayName:          req.Name,
		Config:               stripCredentialKeys(req.Config, p.CredentialKeys()),
		EncryptedCredentials: blob,
		SyncPolicy:           "manual",
		SyncIntervalMinutes:  interval,
		Cursor:               map[string]any{},
		Status:               "active",
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if req.SyncMode != nil && *req.SyncMode != "" {
		src.SyncPolicy = *req.SyncMode
	}

	created, err := s.repo.Create(ctx, src)
	if err != nil {
		return nil, err
	}
	return s.toIntegration(created), nil
}

// Get returns a single source scoped by project (cross-project → not-found).
func (s *Service) Get(ctx context.Context, projectID, id uuid.UUID) (*Integration, error) {
	src, err := s.repo.GetByID(ctx, id, projectID)
	if err != nil {
		return nil, err
	}
	out := s.toIntegration(src)
	// Report the effective scope strategy so the UI can explain an empty
	// resource listing caused by a scope that cannot list (drive.file without
	// the Google Picker) instead of re-deriving the resolution itself.
	if p, ok := s.providers.Get(src.Provider); ok {
		if _, oauth := p.OAuth(); oauth {
			out.OAuthStrategy = s.effectiveOAuthStrategy(ctx, projectID, src.Provider)
			if g, ok := src.Config[oauthGrantedStrategyKey].(string); ok {
				out.OAuthGrantedStrategy = g
			}
		}
	}
	return out, nil
}

// Update applies a partial update. When the request config carries credential
// keys, they are re-encrypted and replace the stored blob; otherwise the stored
// credentials are preserved.
func (s *Service) Update(ctx context.Context, projectID, id uuid.UUID, req *UpdateIntegrationRequest) (*Integration, error) {
	existing, err := s.repo.GetByIDWithCredentials(ctx, id, projectID)
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		existing.DisplayName = *req.Name
	}
	if req.SyncMode != nil && *req.SyncMode != "" {
		existing.SyncPolicy = *req.SyncMode
	}
	if req.SyncIntervalMinutes != nil {
		if p, ok := s.providers.Get(existing.Provider); ok {
			resolved, err := applySyncIntervalBounds(p, req.SyncIntervalMinutes)
			if err != nil {
				return nil, err
			}
			existing.SyncIntervalMinutes = resolved
		} else {
			existing.SyncIntervalMinutes = req.SyncIntervalMinutes
		}
	}
	if req.Enabled != nil {
		if *req.Enabled {
			existing.Status = SourceStatusActive
		} else {
			existing.Status = SourceStatusDisabled
		}
	}
	if req.Config != nil {
		p, ok := s.providers.Get(existing.Provider)
		if !ok {
			return nil, apperror.NewValidation("unsupported providerType: " + existing.Provider)
		}
		if err := p.ValidateConfig(req.Config); err != nil {
			return nil, err
		}
		if hasCredentialKeys(req.Config, p.CredentialKeys()) {
			creds, err := p.ExtractCredentials(req.Config)
			if err != nil {
				return nil, err
			}
			blob, err := s.encryptCredentials(creds)
			if err != nil {
				return nil, err
			}
			existing.EncryptedCredentials = blob
		}
		// Preserve the granted strategy recorded at connect: the edit form
		// submits only the provider's declared fields, so a replace would drop
		// the key and the value cannot be recovered later. An explicit value in
		// the submitted config still wins.
		granted, hasGranted := existing.Config[oauthGrantedStrategyKey]
		existing.Config = stripCredentialKeys(req.Config, p.CredentialKeys())
		if _, submitted := existing.Config[oauthGrantedStrategyKey]; !submitted && hasGranted {
			existing.Config[oauthGrantedStrategyKey] = granted
		}
	}

	updated, err := s.repo.Update(ctx, existing)
	if err != nil {
		return nil, err
	}
	return s.toIntegration(updated), nil
}

// Delete removes a source scoped by project.
func (s *Service) Delete(ctx context.Context, projectID, id uuid.UUID) error {
	return s.repo.Delete(ctx, id, projectID)
}

// ---- Credential helpers ----

// encryptCredentials encrypts a provider credential map into a single
// self-contained blob using the config-sourced key.
func (s *Service) encryptCredentials(creds Credentials) ([]byte, error) {
	if len(s.key) == 0 {
		return nil, apperror.NewInternal("credential encryption is not configured (LLM_ENCRYPTION_KEY missing)", nil)
	}
	blob, err := encryptCredentialMap(s.key, creds)
	if err != nil {
		return nil, apperror.NewInternal("encrypt credentials", err)
	}
	return blob, nil
}

// decryptCredentials reverses encryptCredentials.
func (s *Service) decryptCredentials(blob []byte) (Credentials, error) {
	if len(s.key) == 0 {
		return nil, apperror.NewInternal("credential encryption is not configured (LLM_ENCRYPTION_KEY missing)", nil)
	}
	creds, err := decryptCredentialMap(s.key, blob)
	if err != nil {
		return nil, err
	}
	return creds, nil
}

func (s *Service) now() time.Time {
	return time.Now().UTC()
}

// hasCredentialKeys reports whether any of keys is present in the config.
func hasCredentialKeys(cfg map[string]any, keys []string) bool {
	for _, k := range keys {
		if _, ok := cfg[k]; ok {
			return true
		}
	}
	return false
}

// stripCredentialKeys returns a copy of cfg with the given credential keys
// removed so no plaintext secret is ever stored in the config jsonb column.
func stripCredentialKeys(cfg map[string]any, keys []string) map[string]any {
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if slices.Contains(keys, k) {
			continue
		}
		out[k] = v
	}
	return out
}

// applySyncIntervalBounds applies a provider's sync-cadence bounds (when it
// declares them via SyncIntervalBoundsProvider) to a requested interval. A nil
// interval becomes the provider default; a below-minimum interval is rejected.
// Providers without the interface are unchanged: nil stays nil and a set value
// passes through.
func applySyncIntervalBounds(p SourceProvider, interval *int) (*int, error) {
	bp, ok := p.(SyncIntervalBoundsProvider)
	if !ok {
		return interval, nil
	}
	minMin, defMin := bp.SyncIntervalBounds()
	if interval == nil {
		return &defMin, nil
	}
	if *interval < minMin {
		return nil, apperror.NewValidation(fmt.Sprintf("syncIntervalMinutes must be at least %d minutes for %s", minMin, p.Catalog().Type))
	}
	return interval, nil
}

// publicConfig returns the source's config with credential keys removed, ready
// to serialise on an Integration. The persisted config is already stripped at
// write time, but this re-strips defensively in case a provider changed its
// credential key set since the source was stored. Unknown providers fall back to
// the raw stored config (already stripped at write time).
func (s *Service) publicConfig(src *Source) map[string]any {
	if src.Config == nil {
		return nil
	}
	if p, ok := s.providers.Get(src.Provider); ok {
		return stripCredentialKeys(src.Config, p.CredentialKeys())
	}
	return src.Config
}

// toIntegration maps a persisted source to the SDK integration shape. It never
// includes credential material, and derives the source type from the provider
// catalog so it stays provider-agnostic.
func (s *Service) toIntegration(src *Source) *Integration {
	sourceType := src.Provider
	if p, ok := s.providers.Get(src.Provider); ok {
		sourceType = p.Catalog().SourceType
	}
	return &Integration{
		ID:                  src.ID.String(),
		ProjectID:           src.ProjectID.String(),
		Name:                src.DisplayName,
		ProviderType:        src.Provider,
		SourceType:          sourceType,
		SyncMode:            src.SyncPolicy,
		SyncIntervalMinutes: src.SyncIntervalMinutes,
		Config:              s.publicConfig(src),
		LastSyncedAt:        src.LastSyncedAt,
		Status:              src.Status,
		ErrorMessage:        src.LastError,
		ErrorCount:          src.ErrorCount,
		CreatedAt:           src.CreatedAt,
		UpdatedAt:           src.UpdatedAt,
	}
}
