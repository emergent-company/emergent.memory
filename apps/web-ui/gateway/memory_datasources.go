package main

import (
	"context"
	"net/http"
	"net/url"
	"slices"
)

// --- data-source integrations (Data Source Integrations API) ---

// DSProvider is one entry of the data-source provider catalog
// (GET /api/data-source-integrations/providers).
type DSProvider struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SourceType  string `json:"sourceType"`
	// Vendor/VendorName identify the app vendor behind the provider (e.g.
	// "google"/"Google"). OAuth apps are vendor-level, so the credentials UI
	// labels them by vendor and shows the provider's Name as the data source.
	Vendor     string `json:"vendor"`
	VendorName string `json:"vendorName"`
	IconURL    string `json:"iconUrl,omitempty"`
	Available  bool   `json:"available"`
	// AuthMethods is the provider's advertised connect methods ("oauth" and/or
	// "credentials"). The chooser renders from it rather than a hard-coded list.
	AuthMethods []string `json:"authMethods,omitempty"`
	// Strategies is the provider's advertised org-selectable OAuth scope
	// strategies (e.g. ["drive.file","drive.readonly"]). The org OAuth-client
	// form renders its strategy control from it.
	Strategies []string `json:"strategies,omitempty"`
}

// DSProviderSupportsOAuth reports whether a provider advertises the oauth auth
// method.
func (p DSProvider) DSProviderSupportsOAuth() bool {
	return slices.Contains(p.AuthMethods, "oauth")
}

// DSProviderSupportsStrategy reports whether the provider advertises an
// org-selectable OAuth scope strategy.
func (p DSProvider) DSProviderSupportsStrategy(strategy string) bool {
	return slices.Contains(p.Strategies, strategy)
}

// DSProviderSupportsCredentials reports whether a provider advertises the
// credentials auth method.
func (p DSProvider) DSProviderSupportsCredentials() bool {
	return slices.Contains(p.AuthMethods, "credentials")
}

// DSProviderSchema is a provider's JSON config schema
// (GET /api/data-source-integrations/providers/:providerType/schema). Properties
// are the raw schema objects, so the connect form can be rendered from the
// server's declared contract rather than a hard-coded field list.
type DSProviderSchema struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Required   []string       `json:"required,omitempty"`
}

// DSIntegration is a data-source integration (GET /api/data-source-integrations).
// It carries no credential material — the server never returns it.
type DSIntegration struct {
	ID                  string  `json:"id"`
	ProjectID           string  `json:"projectId"`
	Name                string  `json:"name"`
	Description         *string `json:"description,omitempty"`
	ProviderType        string  `json:"providerType"`
	SourceType          string  `json:"sourceType"`
	SyncMode            string  `json:"syncMode"`
	SyncIntervalMinutes *int    `json:"syncIntervalMinutes,omitempty"`
	// Config carries the non-secret config values (credential keys stripped) so
	// the edit UI can round-trip a source's persisted settings.
	Config       map[string]any `json:"config,omitempty"`
	LastSyncedAt *string        `json:"lastSyncedAt,omitempty"`
	NextSyncAt   *string        `json:"nextSyncAt,omitempty"`
	Status       string         `json:"status"`
	ErrorMessage *string        `json:"errorMessage,omitempty"`
	ErrorCount   int            `json:"errorCount"`
	CreatedAt    string         `json:"createdAt"`
	UpdatedAt    string         `json:"updatedAt"`
}

// DSSyncJob is one sync job of a source. It mirrors the SDK SyncJob shape so the
// full progress contract (phase, message, item counts, trigger, timings) is
// available to the UI.
type DSSyncJob struct {
	ID              string  `json:"id"`
	IntegrationID   string  `json:"integrationId"`
	Status          string  `json:"status"`
	TotalItems      int     `json:"totalItems"`
	ProcessedItems  int     `json:"processedItems"`
	SuccessfulItems int     `json:"successfulItems"`
	FailedItems     int     `json:"failedItems"`
	SkippedItems    int     `json:"skippedItems"`
	CurrentPhase    *string `json:"currentPhase,omitempty"`
	StatusMessage   *string `json:"statusMessage,omitempty"`
	ErrorMessage    *string `json:"errorMessage,omitempty"`
	TriggerType     string  `json:"triggerType"`
	RetryCount      int     `json:"retryCount"`
	MaxRetries      int     `json:"maxRetries"`
	CreatedAt       string  `json:"createdAt"`
	StartedAt       *string `json:"startedAt,omitempty"`
	CompletedAt     *string `json:"completedAt,omitempty"`
}

// DSListSyncJobsResponse is the sync-jobs list envelope.
type DSListSyncJobsResponse struct {
	Items []DSSyncJob `json:"items"`
	Total int         `json:"total"`
}

// DSSourceType is a document count grouped by source type.
type DSSourceType struct {
	SourceType    string `json:"sourceType"`
	DocumentCount int    `json:"documentCount"`
}

// DSTestConnectionResponse is the result of a config or connection test.
type DSTestConnectionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// DSTriggerSyncResponse is the result of a manual sync trigger.
type DSTriggerSyncResponse struct {
	Success bool    `json:"success"`
	Message string  `json:"message"`
	JobID   *string `json:"jobId,omitempty"`
}

// DSCancelSyncJobResponse is the result of cancelling a sync job.
type DSCancelSyncJobResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// DSCreateIntegrationRequest is the create body. Credentials travel inside
// Config (client_id/client_secret/refresh_token) and are stripped + encrypted by
// the server before persistence.
type DSCreateIntegrationRequest struct {
	Name                string         `json:"name"`
	Description         *string        `json:"description,omitempty"`
	ProviderType        string         `json:"providerType"`
	SourceType          string         `json:"sourceType"`
	Config              map[string]any `json:"config"`
	SyncMode            *string        `json:"syncMode,omitempty"`
	SyncIntervalMinutes *int           `json:"syncIntervalMinutes,omitempty"`
}

// DSUpdateIntegrationRequest is the partial update body. Omitted fields are left
// unchanged by the server; Config is only sent when the form supplied it.
type DSUpdateIntegrationRequest struct {
	Name                *string        `json:"name,omitempty"`
	Description         *string        `json:"description,omitempty"`
	Config              map[string]any `json:"config,omitempty"`
	SyncMode            *string        `json:"syncMode,omitempty"`
	SyncIntervalMinutes *int           `json:"syncIntervalMinutes,omitempty"`
}

// DSTestConfigRequest tests an unsaved provider config.
type DSTestConfigRequest struct {
	ProviderType string         `json:"providerType"`
	Config       map[string]any `json:"config"`
}

// DSOAuthAuthorizeResponse is the provider authorization URL returned by the
// server's OAuth authorize endpoint (GET .../providers/:type/oauth/authorize).
type DSOAuthAuthorizeResponse struct {
	AuthorizeURL string `json:"authorizeUrl"`
}

// DSOAuthClientConfig is an org- or project-scoped OAuth client config. The
// client secret is write-only: the server never returns it, so this shape
// carries none. RedirectURI and JavascriptOrigin are the deployment's resolved
// callback/JS-origin values the settings UI shows for the Google Cloud Console
// setup steps.
type DSOAuthClientConfig struct {
	ProviderType     string   `json:"providerType"`
	ClientID         string   `json:"clientId"`
	RedirectURI      string   `json:"redirectUri,omitempty"`
	JavascriptOrigin string   `json:"javascriptOrigin,omitempty"`
	Scopes           []string `json:"scopes,omitempty"`
	Strategy         string   `json:"strategy,omitempty"`
	// Config carries non-secret provider settings (e.g. the Google Picker
	// developer key and app id). Credential material never lives here.
	Config     map[string]any `json:"config,omitempty"`
	Configured bool           `json:"configured"`
	CreatedAt  string         `json:"createdAt"`
	UpdatedAt  string         `json:"updatedAt"`
}

// DSSetOAuthClientRequest is the write shape for an org OAuth client config. On
// an update an empty ClientSecret preserves the stored secret.
type DSSetOAuthClientRequest struct {
	ClientID     string   `json:"clientId"`
	ClientSecret string   `json:"clientSecret,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
	Strategy     string   `json:"strategy,omitempty"`
	// Config carries non-secret provider settings (e.g. the Google Picker
	// developer key and app id). Never secret material.
	Config map[string]any `json:"config,omitempty"`
}

// DSOAuthCredentialSource describes one scope's OAuth app for a provider. The
// client id is not a secret; the client secret is never returned.
type DSOAuthCredentialSource struct {
	Configured bool   `json:"configured"`
	ClientID   string `json:"clientId,omitempty"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
}

// DSOAuthCredentialDescription is the member-readable credential card for one
// OAuth-capable provider: the apps at each scope and the scope that wins.
type DSOAuthCredentialDescription struct {
	ProviderType  string                  `json:"providerType"`
	Project       DSOAuthCredentialSource `json:"project"`
	Organization  DSOAuthCredentialSource `json:"organization"`
	Deployment    DSOAuthCredentialSource `json:"deployment"`
	ResolvedScope string                  `json:"resolvedScope"`
}

// ListDSCredentials lists the OAuth apps available to the active project across
// the project, organization, and deployment scopes (GET
// /api/data-source-integrations/credentials). Secrets are never returned.
func (m *MemoryClient) ListDSCredentials(ctx context.Context) ([]DSOAuthCredentialDescription, error) {
	var out struct {
		Items []DSOAuthCredentialDescription `json:"items"`
	}
	if err := m.doH(ctx, http.MethodGet, "/api/data-source-integrations/credentials", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// DSResource is a selectable sync target for a source (a Drive folder or a
// GitHub repository) returned by the provider's resource listing.
type DSResource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

// ListDSProviderResources lists a provider's selectable sync targets from an
// unsaved config (POST /api/data-source-integrations/resources). The credential
// travels inside config and is never persisted. truncated mirrors the backend's
// X-Resources-Truncated header, the same convention as ListDSResources.
func (m *MemoryClient) ListDSProviderResources(ctx context.Context, providerType string, config map[string]any) ([]DSResource, bool, error) {
	var out []DSResource
	body := map[string]any{"providerType": providerType, "config": config}
	hdr, err := m.doHResp(ctx, http.MethodPost, "/api/data-source-integrations/resources", body, m.documentHeaders(ctx), &out)
	if err != nil {
		return nil, false, err
	}
	return out, hdr.Get("X-Resources-Truncated") == "true", nil
}

// ListDSProviderRefs lists the refs (branches and tags) of the resource named in
// an unsaved config (POST /api/data-source-integrations/refs).
func (m *MemoryClient) ListDSProviderRefs(ctx context.Context, providerType string, config map[string]any) ([]DSResource, bool, error) {
	var out []DSResource
	body := map[string]any{"providerType": providerType, "config": config}
	hdr, err := m.doHResp(ctx, http.MethodPost, "/api/data-source-integrations/refs", body, m.documentHeaders(ctx), &out)
	if err != nil {
		return nil, false, err
	}
	return out, hdr.Get("X-Resources-Truncated") == "true", nil
}

// ListDSProviders returns the available data-source providers.
func (m *MemoryClient) ListDSProviders(ctx context.Context) ([]DSProvider, error) {
	var out []DSProvider
	if err := m.doH(ctx, http.MethodGet, "/api/data-source-integrations/providers", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// StartDSOAuth starts an OAuth connect for a provider: it builds the provider
// authorization URL (state + PKCE) server-side and returns it. config is the
// intended (non-secret) source config; scopes optionally overrides the default.
func (m *MemoryClient) StartDSOAuth(ctx context.Context, providerType string, config map[string]any, scopes []string) (*DSOAuthAuthorizeResponse, error) {
	path := "/api/data-source-integrations/providers/" + url.PathEscape(providerType) + "/oauth/authorize"
	var body any
	if len(config) > 0 || len(scopes) > 0 {
		body = map[string]any{"config": config, "scopes": scopes}
	}
	var out DSOAuthAuthorizeResponse
	if err := m.doH(ctx, http.MethodGet, path, body, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListDSOAuthClients lists an org's configured OAuth clients (GET
// /api/orgs/:orgId/data-source-integrations/oauth-clients). Secrets are redacted.
func (m *MemoryClient) ListDSOAuthClients(ctx context.Context, orgID string) ([]DSOAuthClientConfig, error) {
	path := "/api/orgs/" + url.PathEscape(orgID) + "/data-source-integrations/oauth-clients"
	var out []DSOAuthClientConfig
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetDSOAuthClient returns an org's OAuth client for a provider. An unconfigured
// provider returns configured=false rather than an error.
func (m *MemoryClient) GetDSOAuthClient(ctx context.Context, orgID, providerType string) (*DSOAuthClientConfig, error) {
	path := "/api/orgs/" + url.PathEscape(orgID) + "/data-source-integrations/oauth-clients/" + url.PathEscape(providerType)
	var out DSOAuthClientConfig
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpsertDSOAuthClient creates or replaces an org's OAuth client for a provider
// (PUT; secret write-only).
func (m *MemoryClient) UpsertDSOAuthClient(ctx context.Context, orgID, providerType string, req *DSSetOAuthClientRequest) (*DSOAuthClientConfig, error) {
	path := "/api/orgs/" + url.PathEscape(orgID) + "/data-source-integrations/oauth-clients/" + url.PathEscape(providerType)
	var out DSOAuthClientConfig
	if err := m.doH(ctx, http.MethodPut, path, req, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteDSOAuthClient removes an org's OAuth client for a provider (DELETE).
func (m *MemoryClient) DeleteDSOAuthClient(ctx context.Context, orgID, providerType string) error {
	path := "/api/orgs/" + url.PathEscape(orgID) + "/data-source-integrations/oauth-clients/" + url.PathEscape(providerType)
	return m.doH(ctx, http.MethodDelete, path, nil, m.documentHeaders(ctx), nil)
}

// ListProjectDSOAuthClients lists a project's configured OAuth clients (GET
// /api/projects/:projectId/data-source-integrations/oauth-clients). Secrets are
// redacted.
func (m *MemoryClient) ListProjectDSOAuthClients(ctx context.Context, projectID string) ([]DSOAuthClientConfig, error) {
	path := "/api/projects/" + url.PathEscape(projectID) + "/data-source-integrations/oauth-clients"
	var out []DSOAuthClientConfig
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetProjectDSOAuthClient returns a project's OAuth client for a provider. An
// unconfigured provider returns configured=false rather than an error.
func (m *MemoryClient) GetProjectDSOAuthClient(ctx context.Context, projectID, providerType string) (*DSOAuthClientConfig, error) {
	path := "/api/projects/" + url.PathEscape(projectID) + "/data-source-integrations/oauth-clients/" + url.PathEscape(providerType)
	var out DSOAuthClientConfig
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpsertProjectDSOAuthClient creates or replaces a project's OAuth client for a
// provider (PUT; secret write-only).
func (m *MemoryClient) UpsertProjectDSOAuthClient(ctx context.Context, projectID, providerType string, req *DSSetOAuthClientRequest) (*DSOAuthClientConfig, error) {
	path := "/api/projects/" + url.PathEscape(projectID) + "/data-source-integrations/oauth-clients/" + url.PathEscape(providerType)
	var out DSOAuthClientConfig
	if err := m.doH(ctx, http.MethodPut, path, req, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteProjectDSOAuthClient removes a project's OAuth client for a provider
// (DELETE).
func (m *MemoryClient) DeleteProjectDSOAuthClient(ctx context.Context, projectID, providerType string) error {
	path := "/api/projects/" + url.PathEscape(projectID) + "/data-source-integrations/oauth-clients/" + url.PathEscape(providerType)
	return m.doH(ctx, http.MethodDelete, path, nil, m.documentHeaders(ctx), nil)
}

// ListDSResources lists a source's selectable sync resources (folders /
// repositories). Providers without a listing (e.g. github today) surface an
// error the UI renders as "selection unavailable". query is an optional name
// filter threaded to the backend as the `q` parameter; truncated mirrors the
// backend's X-Resources-Truncated header and is true only when the listing was
// capped.
func (m *MemoryClient) ListDSResources(ctx context.Context, id, query string) ([]DSResource, bool, error) {
	path := "/api/data-source-integrations/" + url.PathEscape(id) + "/resources"
	if query != "" {
		path += "?q=" + url.QueryEscape(query)
	}
	var out []DSResource
	hdr, err := m.doHResp(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out)
	if err != nil {
		return nil, false, err
	}
	return out, hdr.Get("X-Resources-Truncated") == "true", nil
}

// GetDSProviderSchema returns a provider's config schema.
func (m *MemoryClient) GetDSProviderSchema(ctx context.Context, providerType string) (*DSProviderSchema, error) {
	var out DSProviderSchema
	path := "/api/data-source-integrations/providers/" + url.PathEscape(providerType) + "/schema"
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListDSSourceTypes returns document counts grouped by source type.
func (m *MemoryClient) ListDSSourceTypes(ctx context.Context) ([]DSSourceType, error) {
	var out []DSSourceType
	if err := m.doH(ctx, http.MethodGet, "/api/data-source-integrations/source-types", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// TestDSConfig tests an unsaved provider config (POST
// /api/data-source-integrations/test-config).
func (m *MemoryClient) TestDSConfig(ctx context.Context, req *DSTestConfigRequest) (*DSTestConnectionResponse, error) {
	var out DSTestConnectionResponse
	if err := m.doH(ctx, http.MethodPost, "/api/data-source-integrations/test-config", req, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TestDSConnection tests an existing source's stored credentials (POST
// /api/data-source-integrations/:id/test-connection).
func (m *MemoryClient) TestDSConnection(ctx context.Context, id string) (*DSTestConnectionResponse, error) {
	var out DSTestConnectionResponse
	path := "/api/data-source-integrations/" + url.PathEscape(id) + "/test-connection"
	if err := m.doH(ctx, http.MethodPost, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListDSIntegrations lists the project's data sources.
func (m *MemoryClient) ListDSIntegrations(ctx context.Context) ([]DSIntegration, error) {
	var out []DSIntegration
	if err := m.doH(ctx, http.MethodGet, "/api/data-source-integrations", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetDSIntegration fetches one data source by id.
func (m *MemoryClient) GetDSIntegration(ctx context.Context, id string) (*DSIntegration, error) {
	var out DSIntegration
	if err := m.doH(ctx, http.MethodGet, "/api/data-source-integrations/"+url.PathEscape(id), nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateDSIntegration creates a data source.
func (m *MemoryClient) CreateDSIntegration(ctx context.Context, req *DSCreateIntegrationRequest) (*DSIntegration, error) {
	var out DSIntegration
	if err := m.doH(ctx, http.MethodPost, "/api/data-source-integrations", req, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateDSIntegration applies a partial update to a data source (PATCH).
func (m *MemoryClient) UpdateDSIntegration(ctx context.Context, id string, req *DSUpdateIntegrationRequest) (*DSIntegration, error) {
	var out DSIntegration
	path := "/api/data-source-integrations/" + url.PathEscape(id)
	if err := m.doH(ctx, http.MethodPatch, path, req, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SaveDSResource persists a chosen sync resource onto a source's config. It is a
// thin convenience wrapper over UpdateDSIntegration (PATCH
// /api/data-source-integrations/:id), which owns the config merge semantics.
func (m *MemoryClient) SaveDSResource(ctx context.Context, id string, cfg map[string]any) (*DSIntegration, error) {
	return m.UpdateDSIntegration(ctx, id, &DSUpdateIntegrationRequest{Config: cfg})
}

// DeleteDSIntegration deletes a data source.
func (m *MemoryClient) DeleteDSIntegration(ctx context.Context, id string) error {
	path := "/api/data-source-integrations/" + url.PathEscape(id)
	return m.doH(ctx, http.MethodDelete, path, nil, m.documentHeaders(ctx), nil)
}

// TriggerDSSync admits a manual sync job for a source. When a job is already
// active the server returns it in the response rather than erroring.
func (m *MemoryClient) TriggerDSSync(ctx context.Context, id string) (*DSTriggerSyncResponse, error) {
	var out DSTriggerSyncResponse
	path := "/api/data-source-integrations/" + url.PathEscape(id) + "/sync"
	if err := m.doH(ctx, http.MethodPost, path, map[string]any{}, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListDSSyncJobs lists a source's sync jobs (optionally filtered by status).
func (m *MemoryClient) ListDSSyncJobs(ctx context.Context, id, status string) (*DSListSyncJobsResponse, error) {
	path := "/api/data-source-integrations/" + url.PathEscape(id) + "/sync-jobs"
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	var out DSListSyncJobsResponse
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetLatestDSSyncJob returns a source's most recent sync job, or nil when it has
// never synced (the server returns JSON null).
func (m *MemoryClient) GetLatestDSSyncJob(ctx context.Context, id string) (*DSSyncJob, error) {
	var out *DSSyncJob
	path := "/api/data-source-integrations/" + url.PathEscape(id) + "/sync-jobs/latest"
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetDSSyncJob fetches one sync job of a source.
func (m *MemoryClient) GetDSSyncJob(ctx context.Context, id, jobID string) (*DSSyncJob, error) {
	var out DSSyncJob
	path := "/api/data-source-integrations/" + url.PathEscape(id) + "/sync-jobs/" + url.PathEscape(jobID)
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelDSSyncJob cancels an active sync job.
func (m *MemoryClient) CancelDSSyncJob(ctx context.Context, id, jobID string) (*DSCancelSyncJobResponse, error) {
	var out DSCancelSyncJobResponse
	path := "/api/data-source-integrations/" + url.PathEscape(id) + "/sync-jobs/" + url.PathEscape(jobID) + "/cancel"
	if err := m.doH(ctx, http.MethodPost, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// --- document version history ---

// DocumentVersion is one version of a document
// (GET /api/documents/:id/versions), ordered by version number.
type DocumentVersion struct {
	ID               string  `json:"id"`
	CanonicalID      string  `json:"canonicalId"`
	Version          int     `json:"version"`
	SupersedesID     *string `json:"supersedesId,omitempty"`
	SourceStatus     string  `json:"sourceStatus,omitempty"`
	SourceRevisionID *string `json:"sourceRevisionId,omitempty"`
	SourceDeletedAt  *string `json:"sourceDeletedAt,omitempty"`
	CreatedAt        string  `json:"createdAt"`
}

// ListDocumentVersions returns a document's full version history.
func (m *MemoryClient) ListDocumentVersions(ctx context.Context, documentID string) ([]DocumentVersion, error) {
	var out struct {
		Versions []DocumentVersion `json:"versions"`
	}
	path := "/api/documents/" + url.PathEscape(documentID) + "/versions"
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out.Versions, nil
}
