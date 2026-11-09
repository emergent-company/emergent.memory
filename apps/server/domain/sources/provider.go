package sources

import (
	"context"
	"time"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/outbound"
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
	// AuthMethodConfig is a provider connected by supplying non-secret config
	// only; no credential fields are collected or stored. The catalog advertises
	// it and the gateway chooser treats it as continue-with-config.
	AuthMethodConfig AuthMethod = "config"
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
	// Records carries structured graph records derived from source metadata.
	// The sync worker upserts them alongside the item's document.
	Records []ObjectRecord
}

// ObjectRecord is one structured graph object emitted by a provider.
type ObjectRecord struct {
	Type          string
	Key           string
	Properties    map[string]any
	Labels        []string
	DocumentID    string
	Relationships []ObjectRelationship
}

// ObjectRelationship links this record to another record in the same sync.
type ObjectRelationship struct {
	Type       string
	TargetType string
	TargetKey  string
}

// ObjectTypePack is a provider-declared schema pack installed on connect.
type ObjectTypePack struct {
	Name              string
	Version           string
	Description       string
	ObjectTypes       []ObjectTypeDefinition
	RelationshipTypes []RelationshipTypeDefinition
	// SkipDiscovery suppresses automatic schema discovery for this provider:
	// the declared pack (or a user schema) owns the types.
	SkipDiscovery bool
}

// ObjectTypeDefinition is one object type in a pack.
type ObjectTypeDefinition struct {
	Name           string
	Label          string
	Description    string
	Properties     map[string]any
	Required       []string
	SkipExtraction bool
}

// RelationshipTypeDefinition is one relationship type in a pack.
type RelationshipTypeDefinition struct {
	Name        string
	SourceType  string
	TargetType  string
	Description string
}

// TypedObjectProvider is implemented by providers that emit structured records
// and declare the schema types those records use.
type TypedObjectProvider interface {
	ObjectTypes() *ObjectTypePack
}

// ConnectorDeps carries the generic collaborators a provider's connector may
// need. All fields are optional (nil-safe); the core passes what it has without
// knowing the provider's protocol.
type ConnectorDeps struct {
	// Extract converts binary content to text (backed by the xberg service). A
	// provider calls it for formats it cannot decode itself.
	Extract func(ctx context.Context, content []byte, filename, mimeType string) (string, error)

	// PersistCredentials writes a rotated credential set back to the source the
	// connector was built for, through the same encrypted store the connect flow
	// uses. Every path that works against a saved source supplies it — the sync
	// worker, a resource listing, a connection test — because each of them can
	// refresh; it is nil only when there is no stored row to update (a form the
	// user is still filling in, or a connection test on unsaved credentials).
	// Connectors must treat it as best-effort: an authorization server that
	// rotates refresh tokens on use (Trale/Supabase) would otherwise strand the
	// source at the next expiry (#2091), but a failed write must not fail the work
	// that just succeeded.
	PersistCredentials func(ctx context.Context, creds Credentials) error
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

// CursorConnector is implemented by a connector whose enumeration is chunked
// across syncs and therefore needs to remember where it stopped. The worker
// passes the source row's stored cursor and persists the returned one after the
// batch applies, so an interrupted or capped sync resumes instead of restarting.
// A connector that does not implement it is enumerated from scratch each run.
type CursorConnector interface {
	EnumerateWithCursor(ctx context.Context, cfg map[string]any, cursor map[string]any) (items []SourceItem, nextCursor map[string]any, err error)
}

// RecordFetchConnector is implemented by a connector that derives typed records
// while fetching an item (Gmail decodes message metadata from the same thread
// response it uses for the document body).
type RecordFetchConnector interface {
	FetchWithRecords(ctx context.Context, item SourceItem) (content, mimeType string, records []ObjectRecord, err error)
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
	// the listing (more matches exist than were returned). deps carries the
	// credential write-back: a listing can refresh an expired token (Trale), and
	// a rotation that is not persisted would strand the next sync (#2091).
	ListResources(ctx context.Context, creds Credentials, cfg map[string]any, query string, deps ConnectorDeps) ([]Resource, bool, error)
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

// SyncIntervalBoundsProvider is implemented by providers that impose a default
// and minimum sync cadence (the web crawler defaults to 60 minutes and rejects
// intervals below 15). The Service applies the bounds when a source sets a sync
// interval, and uses the declared default when a scheduled source omits one.
type SyncIntervalBoundsProvider interface {
	// SyncIntervalBounds returns the minimum accepted interval and the default
	// applied when a source omits the interval, both in minutes.
	SyncIntervalBounds() (minMinutes, defaultMinutes int)
}

// ConfigNormalizer is implemented by a provider whose config accepts a legacy
// shape alongside its canonical one. The core calls it before validating and
// persisting a create/update so stored rows converge on the canonical shape;
// it must not remove credential material (the core strips that separately).
type ConfigNormalizer interface {
	NormalizeConfig(cfg map[string]any)
}

// ResourceTargetResolver is implemented by providers that can validate a
// resource id against the source's own credentials and return its authoritative
// name/metadata (a Drive folder's name and shared-drive id). It backs the
// picker save path: the browser submits ids only, and the server resolves the
// rest so a client cannot name a folder it cannot see.
type ResourceTargetResolver interface {
	ResolveResourceTargets(ctx context.Context, creds Credentials, ids []string) ([]ResourceRef, error)
}

// ResourceRef is a resolved provider resource: the id, its display name, and
// the owning drive id for providers that namespace resources by drive.
type ResourceRef struct {
	ID      string
	Name    string
	DriveID string
}

// PickerConfig is the non-secret browser bootstrap material for a hosted
// resource picker (the Google Picker developer key and Cloud project id), plus
// the source's effective OAuth scope strategy so the provider can reject a
// picker that cannot browse the drive.
type PickerConfig struct {
	DeveloperKey string
	AppID        string
	Origin       string
	Strategy     string
}

// PickerSession is a short-lived credential for a provider-hosted resource
// picker. The access token is the source's own, so the picker browses exactly
// the Google account the source syncs.
type PickerSession struct {
	AccessToken  string    `json:"accessToken"`
	DeveloperKey string    `json:"developerKey"`
	AppID        string    `json:"appId"`
	Origin       string    `json:"origin,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

// ResourcePicker is implemented by providers whose resource selection runs in
// the provider's own browser picker instead of the generic listing step.
type ResourcePicker interface {
	PickerSession(ctx context.Context, creds Credentials, cfg map[string]any, picker PickerConfig) (*PickerSession, error)
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
	return NewRegistry(defaultProviders("", "", "", outbound.DefaultPolicy())...)
}

// NewConfiguredRegistry builds the registry of providers shipped with this
// build, applying deployment-level settings: the GitHub API origin
// (GITHUB_API_BASE_URL), which lets an operator point the connector at GitHub
// Enterprise or a hermetic test wrapper without letting a source config choose
// an endpoint; the Gmail API origin (GMAIL_API_BASE_URL) and token endpoint
// (GMAIL_TOKEN_URL), which exist only so a hermetic test wrapper can stand in
// for Google; and the web crawler's outbound SSRF policy (WEB_SOURCE_ALLOWED_HOSTS
// / WEB_SOURCE_ALLOW_PRIVATE_HOSTS).
func NewConfiguredRegistry(cfg *config.Config) *Registry {
	baseURL := ""
	gmailBaseURL := ""
	gmailTokenURL := ""
	webPolicy := outbound.DefaultPolicy()
	if cfg != nil {
		baseURL = cfg.GitHubSource.APIBaseURL
		gmailBaseURL = cfg.GmailSource.APIBaseURL
		gmailTokenURL = cfg.GmailSource.TokenURL
		webPolicy = outbound.Policy{
			AllowedHosts: cfg.WebSource.AllowedHosts,
			AllowPrivate: cfg.WebSource.AllowPrivate,
		}
	}
	return NewRegistry(defaultProviders(gmailBaseURL, gmailTokenURL, baseURL, webPolicy)...)
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

// defaultProviders returns the providers registered for this build.
// gmailBaseURL is the deployment's Gmail API origin ("" means the real API);
// gmailTokenURL is the deployment's OAuth2 token endpoint for the Gmail refresh
// grant ("" means Google's); githubBaseURL is the deployment's GitHub API origin
// ("" means api.github.com); webPolicy is the deployment's outbound SSRF policy
// for the web crawler (see internal/outbound). The URLs are deployment settings
// only, so a source config can never point a connector at an arbitrary host.
func defaultProviders(gmailBaseURL, gmailTokenURL, githubBaseURL string, webPolicy outbound.Policy) []SourceProvider {
	return []SourceProvider{
		newGoogleDriveProvider(),
		newGmailProvider(gmailBaseURL, gmailTokenURL),
		newGitHubProvider(githubBaseURL),
		newTraleProvider(),
		newWebProvider(webPolicy),
	}
}
