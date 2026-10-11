package sources

import (
	"context"
	"encoding/hex"
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

// NewService creates a new sources service. The credential-encryption key is
// derived from config.Config.LLMProvider.EncryptionKey — the same config-sourced
// key the provider domain uses — so provider credentials and source credentials
// share one operator-managed secret (LLM_ENCRYPTION_KEY). The provider registry
// is injected (fx supplies NewDefaultRegistry); a nil registry falls back to the
// default set.
func NewService(repo *Repository, cfg *config.Config, log *slog.Logger, registry *Registry, opts ...ServiceOption) *Service {
	if registry == nil {
		registry = NewDefaultRegistry()
	}
	s := &Service{
		repo:         repo,
		providers:    registry,
		oauthClients: map[string]OAuthClient{},
		oauthHTTP:    http.DefaultClient,
		log:          log.With(logger.Scope("sources.svc")),
	}
	if repo != nil {
		s.orgOAuth = repo
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
	return s.testProviderConnection(ctx, p, creds)
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
	return s.testProviderConnection(ctx, p, creds)
}

func (s *Service) testProviderConnection(ctx context.Context, p SourceProvider, creds Credentials) (*TestConnectionResponse, error) {
	connector, err := p.NewConnector(ctx, creds, ConnectorDeps{})
	if err != nil {
		return &TestConnectionResponse{Success: false, Message: "failed to initialize connector: " + err.Error()}, nil
	}
	if err := connector.TestConnection(ctx); err != nil {
		return &TestConnectionResponse{Success: false, Message: err.Error()}, nil
	}
	return &TestConnectionResponse{Success: true, Message: "connection successful"}, nil
}

// ListResources lists the selectable sync targets for an existing source (a
// Drive folder, a GitHub repository) by decrypting its stored credentials and
// asking its provider. Backs the post-consent resource-selection step.
func (s *Service) ListResources(ctx context.Context, projectID, id uuid.UUID) ([]Resource, error) {
	src, err := s.repo.GetByIDWithCredentials(ctx, id, projectID)
	if err != nil {
		return nil, err
	}
	p, ok := s.providers.Get(src.Provider)
	if !ok {
		return nil, apperror.NewValidation("unsupported providerType: " + src.Provider)
	}
	creds, err := s.decryptCredentials(src.EncryptedCredentials)
	if err != nil {
		return nil, err
	}
	resources, err := p.ListResources(ctx, creds, src.Config)
	if err != nil {
		return nil, apperror.NewValidation("list resources failed: " + err.Error())
	}
	if resources == nil {
		resources = []Resource{}
	}
	return resources, nil
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
	if err := p.ValidateConfig(req.Config); err != nil {
		return nil, err
	}
	creds, err := p.ExtractCredentials(req.Config)
	if err != nil {
		return nil, err
	}
	blob, err := s.encryptCredentials(creds)
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
		SyncIntervalMinutes:  req.SyncIntervalMinutes,
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
	return s.toIntegration(src), nil
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
		existing.SyncIntervalMinutes = req.SyncIntervalMinutes
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
		existing.Config = stripCredentialKeys(req.Config, p.CredentialKeys())
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
		LastSyncedAt:        src.LastSyncedAt,
		Status:              src.Status,
		ErrorMessage:        src.LastError,
		ErrorCount:          src.ErrorCount,
		CreatedAt:           src.CreatedAt,
		UpdatedAt:           src.UpdatedAt,
	}
}
