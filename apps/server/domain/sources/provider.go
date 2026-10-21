package sources

import (
	"context"
	"time"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// AuthMethod classifies how a provider authenticates a source.
type AuthMethod string

const (
	// AuthMethodOAuth is a provider connected through an OAuth authorize/callback
	// flow (the server obtains and stores the tokens).
	AuthMethodOAuth AuthMethod = "oauth"
	// AuthMethodCredentials is a provider connected by supplying credential
	// fields directly (write-only).
	AuthMethodCredentials AuthMethod = "credentials"
)

// Credentials is a provider-defined, opaque credential payload. Its keys are
// provider-specific; the core encrypts and stores the whole map without
// interpreting it, and never assumes a particular field (e.g. a refresh token).
type Credentials = map[string]any

// SourceItem is a provider-neutral enumerable item from a source.
type SourceItem struct {
	ID           string
	Name         string
	MimeType     string
	RevisionID   string
	Trashed      bool
	ModifiedTime time.Time
}

// ConnectorDeps carries the generic collaborators a provider's connector may
// need. All fields are optional (nil-safe); the core passes what it has without
// knowing the provider's protocol.
type ConnectorDeps struct {
	// Extract converts binary content to text (backed by the xberg service). A
	// provider calls it for formats it cannot decode itself.
	Extract func(ctx context.Context, content []byte, filename, mimeType string) (string, error)
}

// Connector is the provider-neutral transport the sync worker drives: it
// enumerates a source's items and fetches each item's text content. All
// provider specifics (Google-native export, xberg routing, GitHub API) live
// behind it.
type Connector interface {
	// Enumerate lists the items in scope for the source's config.
	Enumerate(ctx context.Context, cfg map[string]any) ([]SourceItem, error)
	// Fetch returns the text content and effective MIME type of one item.
	Fetch(ctx context.Context, item SourceItem) (content, mimeType string, err error)
	// TestConnection performs a bounded provider call to validate credentials
	// and the source config's target (repository/ref, folder, ...). cfg is the
	// stored source config so a provider can validate the actual sync target, not
	// just that the credentials parse.
	TestConnection(ctx context.Context, cfg map[string]any) error
}

// Resource is a provider-neutral selectable sync target (a Drive folder, a
// GitHub repository) returned by a provider's resource listing (design D7).
type Resource struct {
	ID   string
	Name string
	Type string
}

// ScopeStrategy is a named, provider-defined OAuth scope selection an org may
// choose (design D6: Drive drive.file vs drive.readonly).
type ScopeStrategy string

// ScopeStrategies is implemented by a provider whose OAuth scope set is
// selectable per org. The catalog advertises the names, and the Service maps an
// org-selected strategy to the scopes it authorizes.
type ScopeStrategies interface {
	// Strategies returns the named strategies this provider supports.
	Strategies() []ScopeStrategy
	// ScopesForStrategy returns the scope set for a strategy. ok is false when
	// the strategy is not recognised (the caller then falls back to the default).
	ScopesForStrategy(strategy string) ([]string, bool)
	// StrategyForScopes names the advertised strategy that produced these
	// scopes, or "" when they match none of them (explicit, unrecognised
	// scopes). It lets a caller report which strategy a source effectively
	// uses without re-deriving it from the stored row.
	StrategyForScopes(scopes []string) string
}

// SourceProvider is the extension point for a data-source provider. The CRUD,
// sync, and HTTP layers resolve providers by type through the Registry and never
// branch on a concrete provider name; adding a provider is a registration.
type SourceProvider interface {
	// Catalog returns the provider's catalog entry (type, name, description,
	// sourceType, availability).
	Catalog() Provider
	// ConfigSchema returns the (non-secret) config schema for the connect form.
	ConfigSchema() ProviderSchema
	// CredentialFields returns the write-only credential fields the connect form
	// renders for a credentials-based provider.
	CredentialFields() []ProviderField
	// AuthMethods lists how a source can be connected (oauth and/or credentials).
	AuthMethods() []AuthMethod
	// OAuth returns the provider's OAuth strategy when it supports OAuth connect.
	OAuth() (OAuthStrategy, bool)
	// ValidateConfig validates the non-secret config for a create/update.
	ValidateConfig(cfg map[string]any) error
	// ExtractCredentials pulls the credential payload out of a submitted config
	// (used by the credentials-based connect path), validating it.
	ExtractCredentials(cfg map[string]any) (Credentials, error)
	// CredentialKeys returns the config keys that carry write-only credential
	// material. The core strips them from the persisted config and never returns
	// them, so the set is provider-defined rather than a global list.
	CredentialKeys() []string
	// ListResources lists the selectable sync targets for the given decrypted
	// credentials (a Drive folder, a GitHub repository). It backs the
	// post-consent resource-selection step. query is an optional name filter
	// (a provider may ignore it); truncated reports that the provider capped
	// the listing (more matches exist than were returned).
	ListResources(ctx context.Context, creds Credentials, cfg map[string]any, query string) ([]Resource, bool, error)
	// NewConnector builds a connector for the given decrypted credentials.
	NewConnector(ctx context.Context, creds Credentials, deps ConnectorDeps) (Connector, error)
}

// ResourceRefLister is implemented by providers whose selected resource has a
// second-level selector (a GitHub repository's branches and tags). It backs the
// connect form's ref picker and is optional: providers that do not implement it
// simply offer no ref suggestions.
type ResourceRefLister interface {
	// ListRefs lists refs for the resource named in cfg. truncated reports that
	// the provider has more refs than it returned.
	ListRefs(ctx context.Context, creds Credentials, cfg map[string]any) (items []Resource, truncated bool, err error)
}

// ProviderField describes one config or credential field in a provider schema.
type ProviderField struct {
	Key         string `json:"key"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type,omitempty"`
	Required    bool   `json:"required,omitempty"`
	WriteOnly   bool   `json:"writeOnly,omitempty"`
}

// Registry holds the registered providers, keyed by provider type.
type Registry struct {
	providers map[string]SourceProvider
	order     []string
}

// NewRegistry builds a registry from the given providers.
func NewRegistry(providers ...SourceProvider) *Registry {
	r := &Registry{providers: make(map[string]SourceProvider, len(providers))}
	for _, p := range providers {
		r.Register(p)
	}
	return r
}

// NewDefaultRegistry builds the registry of providers shipped with this build
// using default deployment settings (api.github.com). Package-level callers and
// tests use it; fx wires NewConfiguredRegistry instead.
func NewDefaultRegistry() *Registry {
	return NewRegistry(defaultProviders("")...)
}

// NewConfiguredRegistry builds the registry of providers shipped with this
// build, applying deployment-level settings. Today that is the GitHub API
// origin (GITHUB_API_BASE_URL), which lets an operator point the connector at
// GitHub Enterprise or a hermetic test wrapper without letting a source config
// choose an endpoint.
func NewConfiguredRegistry(cfg *config.Config) *Registry {
	baseURL := ""
	if cfg != nil {
		baseURL = cfg.GitHubSource.APIBaseURL
	}
	return NewRegistry(defaultProviders(baseURL)...)
}

// Register adds or replaces a provider by its catalog type, preserving insertion
// order for the catalog listing.
func (r *Registry) Register(p SourceProvider) {
	t := p.Catalog().Type
	if _, ok := r.providers[t]; !ok {
		r.order = append(r.order, t)
	}
	r.providers[t] = p
}

// Get resolves a provider by type.
func (r *Registry) Get(providerType string) (SourceProvider, bool) {
	p, ok := r.providers[providerType]
	return p, ok
}

// List returns the registered providers in registration order.
func (r *Registry) List() []SourceProvider {
	out := make([]SourceProvider, 0, len(r.order))
	for _, t := range r.order {
		out = append(out, r.providers[t])
	}
	return out
}

// defaultProviders returns the providers registered for this build. githubBaseURL
// is the deployment's GitHub API origin ("" means api.github.com).
func defaultProviders(githubBaseURL string) []SourceProvider {
	return []SourceProvider{newGoogleDriveProvider(), newGitHubProvider(githubBaseURL)}
}
