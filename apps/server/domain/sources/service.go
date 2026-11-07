package sources

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
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

	// accountEmail resolves the authenticated Google account's email after an
	// OAuth exchange, so the source read can show which account it syncs. It is
	// best-effort metadata: nil (unit tests) simply skips the lookup.
	accountEmail func(ctx context.Context, accessToken string) (string, error)

	// publisher announces a committed park and clears the item when the source
	// stops being parked. It is optional: nil means "do not publish", which is
	// what the CLI, the dev MCP and every unit test get.
	publisher NotificationPublisher
}

// carriedConfigKeys are non-secret config keys owned by dedicated flows rather
// than the settings form. An update that does not submit them preserves the
// stored value instead of replacing it, so a name/interval save never clears a
// source's folder targets, granted scope, or account email.
var carriedConfigKeys = []string{"folders", oauthGrantedStrategyKey, oauthGrantedScopesKey, oauthAccountEmailKey}

// cloneConfig shallow-copies a config map so normalization cannot mutate the
// caller's map (which may be the persisted struct or a request body).
func cloneConfig(cfg map[string]any) map[string]any {
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		out[k] = v
	}
	return out
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

// WithAccountEmailResolver overrides the post-connect account-email lookup. The
// production wiring supplies the Drive implementation; tests leave it nil so no
// network call is made.
func WithAccountEmailResolver(fn func(ctx context.Context, accessToken string) (string, error)) ServiceOption {
	return func(s *Service) { s.accountEmail = fn }
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
// credentials carried in the request config or from a referenced saved
// credential. It never persists anything.
func (s *Service) TestConfig(ctx context.Context, projectID uuid.UUID, req *TestConfigRequest) (*TestConnectionResponse, error) {
	p, ok := s.providers.Get(req.ProviderType)
	if !ok {
		return nil, apperror.NewValidation("unsupported providerType: " + req.ProviderType)
	}
	if err := p.ValidateConfig(req.Config); err != nil {
		return nil, err
	}
	creds, err := s.credentialsForRequest(ctx, projectID, p, req.Config, req.SavedCredentialID)
	if err != nil {
		return nil, err
	}
	return s.testProviderConnection(ctx, p, creds, req.Config, unsavedCredentials, ConnectorDeps{})
}

// ListProviderResources lists a provider's selectable sync targets (repositories,
// folders) from an unsaved config. It never persists the config or credentials,
// and it does not require the full sync config: only credentials are needed.
func (s *Service) ListProviderResources(ctx context.Context, projectID uuid.UUID, req *ProviderListRequest) ([]Resource, bool, error) {
	p, creds, err := s.unsavedProviderCredentials(ctx, projectID, req)
	if err != nil {
		return nil, false, err
	}
	// The connect form filters suggestions client-side, so no name query is
	// needed here; truncated is still reported for providers that cap.
	resources, truncated, err := p.ListResources(ctx, creds, req.Config, "", ConnectorDeps{})
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
func (s *Service) ListProviderRefs(ctx context.Context, projectID uuid.UUID, req *ProviderListRequest) ([]Resource, bool, error) {
	p, creds, err := s.unsavedProviderCredentials(ctx, projectID, req)
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

// unsavedProviderCredentials resolves a provider and its credentials from an
// unsaved config, without validating the rest of the config. Credentials come
// from a referenced saved credential when one is supplied.
func (s *Service) unsavedProviderCredentials(ctx context.Context, projectID uuid.UUID, req *ProviderListRequest) (SourceProvider, Credentials, error) {
	p, ok := s.providers.Get(req.ProviderType)
	if !ok {
		return nil, nil, apperror.NewValidation("unsupported providerType: " + req.ProviderType)
	}
	creds, err := s.credentialsForRequest(ctx, projectID, p, req.Config, req.SavedCredentialID)
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
	creds, err := s.credentialsForSource(ctx, src)
	if err != nil {
		return &TestConnectionResponse{Success: false, Message: err.Error()}, nil
	}
	return s.testProviderConnection(ctx, p, creds, src.Config, storedCredentials, s.credentialDeps(projectID, src.ID))
}

// credentialContext says where the credential under test came from. It decides
// the remediation wording when the provider rejects the grant outright.
type credentialContext int

const (
	// storedCredentials: the grant belongs to a saved source, so the fix is to
	// reconnect that source.
	storedCredentials credentialContext = iota
	// unsavedCredentials: TestConfig on a form the user is still editing. There is
	// no source yet, so "reconnect the source" would name a control that does not
	// exist on the screen.
	unsavedCredentials
)

// connectionFailureMessage picks the remediation for a rejected credential. The
// provider cannot tell the two cases apart; the caller can.
func connectionFailureMessage(cc credentialContext, err error) string {
	if cc == unsavedCredentials {
		switch {
		case errors.Is(err, errDriveGrantRejected):
			return driveUnsavedGrantRequired
		case errors.Is(err, errDriveClientRejected):
			// The connect form cannot fix this either: the server's OAuth client
			// is the broken part, so saying "sign in again" would send the user
			// through consent that must fail the same way (#2407).
			return driveUnsavedClientRejected
		}
	}
	return err.Error()
}

func (s *Service) testProviderConnection(ctx context.Context, p SourceProvider, creds Credentials, cfg map[string]any, cc credentialContext, deps ConnectorDeps) (*TestConnectionResponse, error) {
	connector, err := p.NewConnector(ctx, creds, deps)
	if err != nil {
		return &TestConnectionResponse{Success: false, Message: "failed to initialize connector: " + err.Error()}, nil
	}
	if err := connector.TestConnection(ctx, cfg); err != nil {
		return &TestConnectionResponse{Success: false, Message: connectionFailureMessage(cc, err)}, nil
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
	creds, err := s.credentialsForSource(ctx, src)
	if err != nil {
		return nil, false, err
	}
	resources, truncated, err := p.ListResources(ctx, creds, src.Config, query, s.credentialDeps(projectID, src.ID))
	if err != nil {
		// providerListError passes an *apperror.Error through untouched, so a
		// provider's actionable error (a dead Drive grant → "reconnect the
		// source") reaches the UI intact. Re-wrapping it here flattened that copy
		// into "list resources failed: oauth2: invalid_grant …" (#2149).
		return nil, false, providerListError("list resources failed", err)
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
	// Normalize before validating/persisting so a legacy shape submitted by a
	// caller converges on the canonical contract instead of being stored as-is.
	cfg := cloneConfig(req.Config)
	if normalizer, ok := p.(ConfigNormalizer); ok {
		normalizer.NormalizeConfig(cfg)
	}
	if !req.AllowIncompleteConfig {
		if err := p.ValidateConfig(cfg); err != nil {
			return nil, err
		}
	}
	var credentialID *uuid.UUID
	var blob []byte
	if req.SavedCredentialID != nil && strings.TrimSpace(*req.SavedCredentialID) != "" {
		id, err := parseCredentialID(*req.SavedCredentialID)
		if err != nil {
			return nil, err
		}
		cred, err := s.repo.GetCredentialByID(ctx, id, projectID)
		if err != nil {
			return nil, err
		}
		if cred.Provider != req.ProviderType {
			return nil, apperror.NewValidation(fmt.Sprintf("saved credential provider %q does not match source provider %q", cred.Provider, req.ProviderType))
		}
		credentialID = &id
	} else {
		creds, err := p.ExtractCredentials(cfg)
		if err != nil {
			return nil, err
		}
		blob, err = s.encryptCredentials(creds)
		if err != nil {
			return nil, err
		}
	}

	// Resolve the sync mode before the interval: the policy decides whether this
	// source carries an interval at all, so a manual source created through a
	// form that also posts an interval is stored without one.
	mode, err := defaultSyncMode(p, req.SyncMode)
	if err != nil {
		return nil, err
	}
	interval, err := applyScheduledInterval(p, mode, req.SyncIntervalMinutes)
	if err != nil {
		return nil, err
	}

	now := s.now()
	src := &Source{
		ID:                   uuid.New(),
		ProjectID:            projectID,
		Provider:             req.ProviderType,
		DisplayName:          req.Name,
		Config:               stripCredentialKeys(cfg, p.CredentialKeys()),
		EncryptedCredentials: blob,
		CredentialID:         credentialID,
		SyncPolicy:           mode,
		SyncIntervalMinutes:  interval,
		Cursor:               map[string]any{},
		Status:               "active",
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	created, err := s.repo.Create(ctx, src)
	if err != nil {
		return nil, err
	}

	// Bind the creating credential to the new source (slice 2 of #2376). A
	// source created after migration 00265 carries no wildcard grant, so an
	// api_key creator must hold an exact grant to push as the source it just
	// created — without it the very next POST /api/graph/changesets as this
	// source is refused. A session creator gets no grant: its source is pushed
	// by the system sync worker (exempt from the grant check), and a
	// ('session', NULL) row would be the hidden wildcard 00265 forbids. The row
	// is recorded so the binding is auditable and revocable.
	if kind, credID, ok := auth.CredentialFromContext(ctx); ok && kind == auth.CredentialAPIKey && credID != nil {
		if err := s.repo.InsertSourceGrant(ctx, projectID, created.ID, kind, *credID); err != nil {
			return nil, err
		}
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
		s.resolveLegacyTargetNames(ctx, projectID, src, out, p)
	}
	return out, nil
}

// resolveLegacyTargetNames fills in the display name of a legacy single-target
// resource (Drive's `folder_id`) so the UI never renders a bare opaque id where
// a name exists. Sources connected before canonical `folders` targets were
// introduced persist only the id; the first folder-picker save migrates them.
//
// The lookup is best-effort and bounded: a provider that cannot resolve the
// name, a credential error, or a timeout leaves the response untouched, and the
// existing chip label (derived from the id) stays in place. The resolved name is
// annotation-only — it is not persisted, so a read never mutates the source.
func (s *Service) resolveLegacyTargetNames(ctx context.Context, projectID uuid.UUID, src *Source, out *Integration, p SourceProvider) {
	if src == nil || out == nil || out.Config == nil {
		return
	}
	if _, ok := out.Config["folders"]; ok {
		return
	}
	folderID := configString(out.Config, "folder_id")
	if folderID == "" {
		return
	}
	resolver, ok := p.(ResourceTargetResolver)
	if !ok {
		return
	}
	withCreds, err := s.repo.GetByIDWithCredentials(ctx, src.ID, projectID)
	if err != nil {
		return
	}
	creds, err := s.credentialsForSource(ctx, withCreds)
	if err != nil {
		return
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	refs, err := resolver.ResolveResourceTargets(lookupCtx, creds, []string{folderID})
	if err != nil || len(refs) != 1 {
		return
	}
	entry := map[string]any{"folder_id": refs[0].ID, "name": refs[0].Name}
	if driveID := refs[0].DriveID; driveID != "" {
		entry["drive_id"] = driveID
	}
	cfg := cloneConfig(out.Config)
	cfg["folders"] = []any{entry}
	out.Config = cfg
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
		mode, err := NormalizeSyncMode(*req.SyncMode)
		if err != nil {
			return nil, err
		}
		existing.SyncPolicy = mode
	}
	// The policy owns the interval. A source switched back to manual keeps no
	// interval — leaving one behind is what let a "manual" source keep syncing on
	// the scheduler sweep — and a scheduled source never ends up without one. A
	// scheduled source whose interval is already set keeps it through an edit that
	// does not mention the interval.
	switch {
	case existing.SyncPolicy != SyncPolicyScheduled:
		existing.SyncIntervalMinutes = nil
	case req.SyncIntervalMinutes != nil, existing.SyncIntervalMinutes == nil:
		// The provider lookup only supplies bounds; a source whose provider is no
		// longer registered still gets a cadence, because the sweep needs one.
		interval := req.SyncIntervalMinutes
		if p, ok := s.providers.Get(existing.Provider); ok {
			resolved, err := applyScheduledInterval(p, existing.SyncPolicy, interval)
			if err != nil {
				return nil, err
			}
			existing.SyncIntervalMinutes = resolved
		} else if interval == nil {
			def := defaultSyncIntervalMinutes
			existing.SyncIntervalMinutes = &def
		} else {
			existing.SyncIntervalMinutes = interval
		}
	}
	// `status` is a lifecycle decision, so an explicit enable/disable is applied
	// as its own transition after the fields write (#2437). A settings save that
	// says nothing about `enabled` leaves the column alone, which is what stops
	// its snapshot from silently un-parking a source a failing sync parked between
	// the read at the top of this method and the write below.
	var statusChange *string
	if req.Enabled != nil {
		status := SourceStatusDisabled
		if *req.Enabled {
			status = SourceStatusActive
		}
		statusChange = &status
	}
	savedRefActive := false
	if req.SavedCredentialID != nil {
		raw := strings.TrimSpace(*req.SavedCredentialID)
		if raw == "" {
			// Clear the reference; the source returns to its inline blob.
			existing.CredentialID = nil
		} else {
			id, err := parseCredentialID(raw)
			if err != nil {
				return nil, err
			}
			cred, err := s.repo.GetCredentialByID(ctx, id, projectID)
			if err != nil {
				return nil, err
			}
			if cred.Provider != existing.Provider {
				return nil, apperror.NewValidation(fmt.Sprintf("saved credential provider %q does not match source provider %q", cred.Provider, existing.Provider))
			}
			existing.CredentialID = &id
			// A referenced source keeps no inline copy of the secret.
			existing.EncryptedCredentials = nil
			savedRefActive = true
		}
	}
	if req.Config != nil {
		p, ok := s.providers.Get(existing.Provider)
		if !ok {
			return nil, apperror.NewValidation("unsupported providerType: " + existing.Provider)
		}
		// Normalize both sides so a legacy stored shape and a canonical
		// submission compare and persist in one form.
		cfg := cloneConfig(req.Config)
		if normalizer, ok := p.(ConfigNormalizer); ok {
			normalizer.NormalizeConfig(cfg)
		}
		stored := cloneConfig(existing.Config)
		if normalizer, ok := p.(ConfigNormalizer); ok {
			normalizer.NormalizeConfig(stored)
		}
		// Preserve the keys the forms do not manage. The picker and the connect
		// flow own the folder targets; the OAuth callback owns the granted
		// strategy and account email. A settings save must not drop them.
		for _, key := range carriedConfigKeys {
			if _, submitted := cfg[key]; submitted {
				continue
			}
			if v, ok := stored[key]; ok {
				cfg[key] = v
			}
		}
		// A settings save on a source that has not chosen its sync target yet must
		// still work (name/interval edits). Validation is relaxed only when the
		// source was already incomplete: a complete source can never be made
		// incomplete by an update.
		if verr := p.ValidateConfig(cfg); verr != nil {
			if storedErr := p.ValidateConfig(stored); storedErr == nil {
				return nil, verr
			}
		}
		if !savedRefActive && hasCredentialKeys(cfg, p.CredentialKeys()) {
			creds, err := p.ExtractCredentials(cfg)
			if err != nil {
				return nil, err
			}
			blob, err := s.encryptCredentials(creds)
			if err != nil {
				return nil, err
			}
			existing.EncryptedCredentials = blob
		}
		existing.Config = stripCredentialKeys(cfg, p.CredentialKeys())
	}

	// Clearing a saved-credential reference leaves the source with no usable
	// credential unless the same request supplied replacement inline fields.
	// Reject that end state instead of persisting a source that cannot sync.
	if existing.CredentialID == nil && len(existing.EncryptedCredentials) == 0 {
		return nil, apperror.NewValidation("clearing a saved credential requires supplying replacement credential fields")
	}

	updated, err := s.repo.Update(ctx, existing)
	if err != nil {
		return nil, err
	}
	if statusChange != nil {
		if err := s.repo.UpdateStatus(ctx, updated.ID, updated.ProjectID, *statusChange); err != nil {
			return nil, err
		}
	}
	// An update that leaves the source not parked has nothing left to act on: an
	// explicit enable retries the credential, an explicit disable stops the
	// source, and a settings save preserves the status another writer chose.
	// Re-read rather than trusting `updated`: the edit does not own `status`, so a
	// park that landed during the write is the truth the response and the
	// notification must reflect. Resolving is idempotent.
	current, gErr := s.repo.GetByID(ctx, updated.ID, updated.ProjectID)
	if gErr != nil {
		s.log.Warn("failed to check source status after update",
			"source_id", updated.ID.String(),
			"error", gErr)
		return s.toIntegration(updated), nil
	}
	if current.Status != SourceStatusNeedsReconnect {
		s.resolveSourceNotification(ctx, updated.ProjectID, updated.ID)
	}
	return s.toIntegration(current), nil
}

// Delete removes a source scoped by project.
func (s *Service) Delete(ctx context.Context, projectID, id uuid.UUID) error {
	if err := s.repo.Delete(ctx, id, projectID); err != nil {
		return err
	}
	// A deleted source must not leave an actionable item behind: the member can
	// no longer fix it, so the item is resolved rather than left pointing at a
	// row that is gone.
	s.resolveSourceNotification(ctx, projectID, id)
	return nil
}

// ---- Credential helpers ----

// persistRotatedCredentials writes a rotated credential set back to a saved
// source. Only the encrypted column moves: the row's config and status may have
// changed since the caller read it.
func (s *Service) persistRotatedCredentials(ctx context.Context, projectID, id uuid.UUID, creds Credentials) error {
	blob, err := s.encryptCredentials(creds)
	if err != nil {
		return fmt.Errorf("encrypt rotated credentials: %w", err)
	}
	if err := s.repo.UpdateCredentials(ctx, id, projectID, blob); err != nil {
		return fmt.Errorf("persist rotated credentials: %w", err)
	}
	return nil
}

// credentialDeps returns the connector deps for work against a saved source:
// a rotation is persisted, and a failure is logged rather than propagated (the
// access token the refresh just obtained is valid). A zero source id — an
// unsaved form — yields no write path, so nothing is written.
func (s *Service) credentialDeps(projectID, id uuid.UUID) ConnectorDeps {
	if id == uuid.Nil {
		return ConnectorDeps{}
	}
	return ConnectorDeps{PersistCredentials: func(ctx context.Context, creds Credentials) error {
		if err := s.persistRotatedCredentials(ctx, projectID, id, creds); err != nil {
			s.log.Warn("failed to persist rotated credentials", "source_id", id, "error", err)
			return err
		}
		return nil
	}}
}

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

// defaultSyncIntervalMinutes is the interval a provider without sync-cadence
// bounds gets when a source is switched to scheduled mode without an explicit
// interval. Without it, "scheduled" with a blank interval field would be a
// silent no-op — the mirror image of a manual source that still syncs.
const defaultSyncIntervalMinutes = 60

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

// NormalizeSyncMode maps a requested sync mode onto the canonical policy. The
// public API documented `recurring` for interval-driven syncs before the policy
// gated the scheduler, so it stays accepted as an alias for `scheduled`.
// Anything else is rejected rather than stored: an unrecognised policy is inert
// (only `scheduled` is admitted by the sweep), which would be a silent no-op.
func NormalizeSyncMode(mode string) (string, error) {
	switch strings.TrimSpace(mode) {
	case "":
		return "", nil
	case SyncPolicyManual:
		return SyncPolicyManual, nil
	case SyncPolicyScheduled, "recurring":
		return SyncPolicyScheduled, nil
	default:
		return "", apperror.NewValidation("syncMode must be manual or scheduled")
	}
}

// defaultSyncMode resolves the policy for a new source. An explicit mode always
// wins — the connect form's Manual selection must not be overridden by a provider
// default. Without an explicit mode, a provider that declares sync-cadence
// bounds is one that expects to be polled (Gmail, the web crawler), so its
// sources start scheduled; every other provider starts manual and syncs only
// when triggered.
func defaultSyncMode(p SourceProvider, requested *string) (string, error) {
	if requested != nil && *requested != "" {
		return NormalizeSyncMode(*requested)
	}
	if _, ok := p.(SyncIntervalBoundsProvider); ok {
		return SyncPolicyScheduled, nil
	}
	return SyncPolicyManual, nil
}

// applyScheduledInterval resolves the interval to persist for a source in the
// given sync mode. Mode is authoritative in both directions: a manual source
// carries no interval, so nothing can schedule it behind the operator's back; a
// scheduled source always carries one, falling back to the provider default
// (or defaultSyncIntervalMinutes when the provider declares no bounds).
func applyScheduledInterval(p SourceProvider, mode string, interval *int) (*int, error) {
	if mode != SyncPolicyScheduled {
		return nil, nil
	}
	resolved, err := applySyncIntervalBounds(p, interval)
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		def := defaultSyncIntervalMinutes
		resolved = &def
	}
	return resolved, nil
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
	var credentialID *string
	if src.CredentialID != nil {
		id := src.CredentialID.String()
		credentialID = &id
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
		CredentialID:        credentialID,
		CreatedAt:           src.CreatedAt,
		UpdatedAt:           src.UpdatedAt,
	}
}
