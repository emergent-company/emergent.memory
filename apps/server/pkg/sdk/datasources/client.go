// Package datasources provides the Data Source Integrations client for the Emergent API SDK.
// Data source integrations connect external systems (ClickUp, IMAP, Gmail, Google Drive)
// to the knowledge base for automated document syncing.
package datasources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
)

// Client provides access to the Data Source Integrations API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// NewClient creates a new data source integrations client.
func NewClient(httpClient *http.Client, baseURL string, authProvider auth.Provider, orgID, projectID string) *Client {
	return &Client{
		http:      httpClient,
		base:      baseURL,
		auth:      authProvider,
		orgID:     orgID,
		projectID: projectID,
	}
}

// SetContext sets the organization and project context.
func (c *Client) SetContext(orgID, projectID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.orgID = orgID
	c.projectID = projectID
}

// --- Types ---

// Provider represents an available data source provider.
type Provider struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SourceType  string `json:"sourceType"`
	// Vendor is the stable vendor/app identity behind the provider (e.g.
	// "google", "github"); VendorName is its display form. OAuth apps are
	// vendor-level, so the credentials UI labels them by vendor and shows the
	// data source (Name) separately.
	Vendor      string   `json:"vendor"`
	VendorName  string   `json:"vendorName"`
	IconURL     string   `json:"iconUrl,omitempty"`
	Available   bool     `json:"available"`
	AuthMethods []string `json:"authMethods,omitempty"`
}

// ProviderSchema represents the configuration schema for a provider.
type ProviderSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Required   []string               `json:"required,omitempty"`
}

// OAuthAuthorizeRequest is the optional input for starting an OAuth connect.
type OAuthAuthorizeRequest struct {
	Config map[string]interface{} `json:"config,omitempty"`
	Scopes []string               `json:"scopes,omitempty"`
}

// OAuthAuthorizeResponse is the result of starting an OAuth connect.
type OAuthAuthorizeResponse struct {
	AuthorizeURL string `json:"authorizeUrl"`
}

// OAuthClientConfig is an org- or project-scoped OAuth client config (secret
// redacted/never returned). RedirectURI and JavascriptOrigin are the
// deployment's resolved callback/JS-origin values for the provider, so the UI
// can show the Google Cloud Console setup steps without hard-coding them.
type OAuthClientConfig struct {
	ProviderType     string                 `json:"providerType"`
	ClientID         string                 `json:"clientId"`
	RedirectURI      string                 `json:"redirectUri,omitempty"`
	JavascriptOrigin string                 `json:"javascriptOrigin,omitempty"`
	Scopes           []string               `json:"scopes,omitempty"`
	Strategy         string                 `json:"strategy,omitempty"`
	Configured       bool                   `json:"configured"`
	CreatedAt        time.Time              `json:"createdAt"`
	UpdatedAt        time.Time              `json:"updatedAt"`
	Config           map[string]interface{} `json:"config,omitempty"`
}

// OAuthCredentialSource describes one scope's OAuth app for a provider. ClientID
// is not a secret; the client secret is never returned.
type OAuthCredentialSource struct {
	Configured bool       `json:"configured"`
	ClientID   string     `json:"clientId,omitempty"`
	UpdatedAt  *time.Time `json:"updatedAt,omitempty"`
}

// OAuthCredentialDescription is the member-readable credential card for one
// OAuth-capable provider: the apps configured at each scope and the scope that
// wins resolution.
type OAuthCredentialDescription struct {
	ProviderType  string                `json:"providerType"`
	Project       OAuthCredentialSource `json:"project"`
	Organization  OAuthCredentialSource `json:"organization"`
	Deployment    OAuthCredentialSource `json:"deployment"`
	ResolvedScope string                `json:"resolvedScope"`
}

// SetOAuthClientRequest is the write shape for an org OAuth client config.
// The client secret is write-only; on update an empty ClientSecret preserves the stored one.
type SetOAuthClientRequest struct {
	ClientID     string                 `json:"clientId"`
	ClientSecret string                 `json:"clientSecret"`
	Scopes       []string               `json:"scopes,omitempty"`
	Strategy     string                 `json:"strategy,omitempty"`
	Config       map[string]interface{} `json:"config,omitempty"`
}

// Integration represents a data source integration.
type Integration struct {
	ID                  string  `json:"id"`
	ProjectID           string  `json:"projectId"`
	Name                string  `json:"name"`
	Description         *string `json:"description,omitempty"`
	ProviderType        string  `json:"providerType"`
	SourceType          string  `json:"sourceType"`
	SyncMode            string  `json:"syncMode"`
	SyncIntervalMinutes *int    `json:"syncIntervalMinutes,omitempty"`
	// Config carries the non-secret config values (credential keys stripped) so
	// the connect/edit UI can round-trip a source's persisted settings.
	Config       map[string]interface{} `json:"config,omitempty"`
	LastSyncedAt *time.Time             `json:"lastSyncedAt,omitempty"`
	NextSyncAt   *time.Time             `json:"nextSyncAt,omitempty"`
	Status       string                 `json:"status"`
	ErrorMessage *string                `json:"errorMessage,omitempty"`
	ErrorCount   int                    `json:"errorCount"`
	CreatedAt    time.Time              `json:"createdAt"`
	UpdatedAt    time.Time              `json:"updatedAt"`
}

// SyncJob represents a data source sync job.
type SyncJob struct {
	ID                string     `json:"id"`
	IntegrationID     string     `json:"integrationId"`
	ConfigurationID   *string    `json:"configurationId,omitempty"`
	ConfigurationName *string    `json:"configurationName,omitempty"`
	Status            string     `json:"status"`
	TotalItems        int        `json:"totalItems"`
	ProcessedItems    int        `json:"processedItems"`
	SuccessfulItems   int        `json:"successfulItems"`
	FailedItems       int        `json:"failedItems"`
	SkippedItems      int        `json:"skippedItems"`
	CurrentPhase      *string    `json:"currentPhase,omitempty"`
	StatusMessage     *string    `json:"statusMessage,omitempty"`
	ErrorMessage      *string    `json:"errorMessage,omitempty"`
	TriggerType       string     `json:"triggerType"`
	RetryCount        int        `json:"retryCount"`
	MaxRetries        int        `json:"maxRetries"`
	CreatedAt         time.Time  `json:"createdAt"`
	StartedAt         *time.Time `json:"startedAt,omitempty"`
	CompletedAt       *time.Time `json:"completedAt,omitempty"`
}

// SourceType represents document counts by source type.
type SourceType struct {
	SourceType    string `json:"sourceType"`
	DocumentCount int    `json:"documentCount"`
}

// TestConnectionResponse is the response from connection tests.
type TestConnectionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// TriggerSyncResponse is the response from triggering a sync.
type TriggerSyncResponse struct {
	Success bool    `json:"success"`
	Message string  `json:"message"`
	JobID   *string `json:"jobId,omitempty"`
}

// CancelSyncJobResponse is the response from cancelling a sync job.
type CancelSyncJobResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// ListSyncJobsResponse is the response from listing sync jobs.
type ListSyncJobsResponse struct {
	Items []SyncJob `json:"items"`
	Total int       `json:"total"`
}

// --- Request types ---

// CreateIntegrationRequest is the request body for creating an integration.
type CreateIntegrationRequest struct {
	Name                string                 `json:"name"`
	Description         *string                `json:"description,omitempty"`
	ProviderType        string                 `json:"providerType"`
	SourceType          string                 `json:"sourceType"`
	Config              map[string]interface{} `json:"config"`
	SyncMode            *string                `json:"syncMode,omitempty"`
	SyncIntervalMinutes *int                   `json:"syncIntervalMinutes,omitempty"`
}

// UpdateIntegrationRequest is the request body for updating an integration.
type UpdateIntegrationRequest struct {
	Name                *string                `json:"name,omitempty"`
	Description         *string                `json:"description,omitempty"`
	Config              map[string]interface{} `json:"config,omitempty"`
	SyncMode            *string                `json:"syncMode,omitempty"`
	SyncIntervalMinutes *int                   `json:"syncIntervalMinutes,omitempty"`
	Enabled             *bool                  `json:"enabled,omitempty"`
}

// TestConfigRequest is the request body for testing a provider config.
type TestConfigRequest struct {
	ProviderType string                 `json:"providerType"`
	Config       map[string]interface{} `json:"config"`
}

// TriggerSyncRequest is the request body for triggering a sync.
type TriggerSyncRequest struct {
	ConfigurationID *string                `json:"configurationId,omitempty"`
	FullSync        bool                   `json:"fullSync,omitempty"`
	Options         map[string]interface{} `json:"options,omitempty"`
}

// ListIntegrationsOptions are query parameters for listing integrations.
type ListIntegrationsOptions struct {
	ProviderType *string
	SourceType   *string
	Status       *string
}

// ListSyncJobsOptions are query parameters for listing sync jobs.
type ListSyncJobsOptions struct {
	Status *string
}

// --- Internal helpers ---

func (c *Client) setHeaders(req *http.Request) error {
	if err := c.auth.Authenticate(req); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()
	if orgID != "" {
		req.Header.Set("X-Org-ID", orgID)
	}
	if projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}
	return nil
}

// --- Provider Endpoints ---

// ListProviders returns all available data source providers.
// GET /api/data-source-integrations/providers
func (c *Client) ListProviders(ctx context.Context) ([]Provider, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/data-source-integrations/providers", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result []Provider
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

// ListCredentials returns the OAuth apps available to the project, one entry
// per OAuth-capable provider, across the project, organization, and deployment
// scopes. Secrets are never returned.
// GET /api/data-source-integrations/credentials
func (c *Client) ListCredentials(ctx context.Context) ([]OAuthCredentialDescription, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/data-source-integrations/credentials", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result struct {
		Items []OAuthCredentialDescription `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Items, nil
}

// GetProviderSchema returns the configuration schema for a provider type.
// GET /api/data-source-integrations/providers/:providerType/schema
func (c *Client) GetProviderSchema(ctx context.Context, providerType string) (*ProviderSchema, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/data-source-integrations/providers/"+url.PathEscape(providerType)+"/schema", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result ProviderSchema
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// TestConfig tests a provider configuration without creating an integration.
// POST /api/data-source-integrations/test-config
func (c *Client) TestConfig(ctx context.Context, testReq *TestConfigRequest) (*TestConnectionResponse, error) {
	body, err := json.Marshal(testReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/data-source-integrations/test-config", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result TestConnectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// --- Source Types ---

// GetSourceTypes returns document counts grouped by source type.
// GET /api/data-source-integrations/source-types
func (c *Client) GetSourceTypes(ctx context.Context) ([]SourceType, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/data-source-integrations/source-types", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result []SourceType
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

// --- Integration CRUD ---

// List returns all integrations for the current project.
// GET /api/data-source-integrations
func (c *Client) List(ctx context.Context, opts *ListIntegrationsOptions) ([]Integration, error) {
	u, err := url.Parse(c.base + "/api/data-source-integrations")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	if opts != nil {
		q := u.Query()
		if opts.ProviderType != nil {
			q.Set("providerType", *opts.ProviderType)
		}
		if opts.SourceType != nil {
			q.Set("sourceType", *opts.SourceType)
		}
		if opts.Status != nil {
			q.Set("status", *opts.Status)
		}
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result []Integration
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

// Get returns a specific integration by ID.
// GET /api/data-source-integrations/:id
func (c *Client) Get(ctx context.Context, integrationID string) (*Integration, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/data-source-integrations/"+url.PathEscape(integrationID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result Integration
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Create creates a new data source integration.
// POST /api/data-source-integrations
// Returns 201 on success.
func (c *Client) Create(ctx context.Context, createReq *CreateIntegrationRequest) (*Integration, error) {
	body, err := json.Marshal(createReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/data-source-integrations", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result Integration
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Update updates an existing integration (partial update via PATCH).
// PATCH /api/data-source-integrations/:id
func (c *Client) Update(ctx context.Context, integrationID string, updateReq *UpdateIntegrationRequest) (*Integration, error) {
	body, err := json.Marshal(updateReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PATCH", c.base+"/api/data-source-integrations/"+url.PathEscape(integrationID), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result Integration
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Delete deletes an integration by ID.
// DELETE /api/data-source-integrations/:id
// Returns 204 No Content on success.
func (c *Client) Delete(ctx context.Context, integrationID string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.base+"/api/data-source-integrations/"+url.PathEscape(integrationID), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	_, _ = io.Copy(io.Discard, resp.Body)

	return nil
}

// --- Integration Operations ---

// TestConnection tests the connection for an existing integration.
// POST /api/data-source-integrations/:id/test-connection
func (c *Client) TestConnection(ctx context.Context, integrationID string) (*TestConnectionResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/data-source-integrations/"+url.PathEscape(integrationID)+"/test-connection", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result TestConnectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// TriggerSync triggers a sync job for an integration.
// POST /api/data-source-integrations/:id/sync
func (c *Client) TriggerSync(ctx context.Context, integrationID string, syncReq *TriggerSyncRequest) (*TriggerSyncResponse, error) {
	var bodyReader *bytes.Reader
	if syncReq != nil {
		body, err := json.Marshal(syncReq)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(body)
	} else {
		bodyReader = bytes.NewReader([]byte("{}"))
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/data-source-integrations/"+url.PathEscape(integrationID)+"/sync", bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result TriggerSyncResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// --- Sync Job Endpoints ---

// ListSyncJobs returns sync jobs for an integration.
// GET /api/data-source-integrations/:id/sync-jobs
func (c *Client) ListSyncJobs(ctx context.Context, integrationID string, opts *ListSyncJobsOptions) (*ListSyncJobsResponse, error) {
	u, err := url.Parse(c.base + "/api/data-source-integrations/" + url.PathEscape(integrationID) + "/sync-jobs")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	if opts != nil {
		q := u.Query()
		if opts.Status != nil {
			q.Set("status", *opts.Status)
		}
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result ListSyncJobsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetLatestSyncJob returns the most recent sync job for an integration.
// GET /api/data-source-integrations/:id/sync-jobs/latest
// Returns nil if no jobs exist.
func (c *Client) GetLatestSyncJob(ctx context.Context, integrationID string) (*SyncJob, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/data-source-integrations/"+url.PathEscape(integrationID)+"/sync-jobs/latest", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	// Server returns null if no jobs exist
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if string(bodyBytes) == "null" || len(bodyBytes) == 0 {
		return nil, nil
	}

	var result SyncJob
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetSyncJob returns a specific sync job by ID.
// GET /api/data-source-integrations/:id/sync-jobs/:jobId
func (c *Client) GetSyncJob(ctx context.Context, integrationID, jobID string) (*SyncJob, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/data-source-integrations/"+url.PathEscape(integrationID)+"/sync-jobs/"+url.PathEscape(jobID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result SyncJob
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// CancelSyncJob cancels a running or pending sync job.
// POST /api/data-source-integrations/:id/sync-jobs/:jobId/cancel
func (c *Client) CancelSyncJob(ctx context.Context, integrationID, jobID string) (*CancelSyncJobResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/data-source-integrations/"+url.PathEscape(integrationID)+"/sync-jobs/"+url.PathEscape(jobID)+"/cancel", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result CancelSyncJobResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// --- OAuth Connect ---

// StartOAuth starts an OAuth connect for a provider.
// GET /api/data-source-integrations/providers/:providerType/oauth/authorize
func (c *Client) StartOAuth(ctx context.Context, providerType string, oauthReq *OAuthAuthorizeRequest) (*OAuthAuthorizeResponse, error) {
	var bodyReader io.Reader
	if oauthReq != nil {
		body, err := json.Marshal(oauthReq)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/data-source-integrations/providers/"+url.PathEscape(providerType)+"/oauth/authorize", bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if oauthReq != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result OAuthAuthorizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// OAuthCallbackURL builds the OAuth callback URL for a provider.
func (c *Client) OAuthCallbackURL(baseURL, providerType string) string {
	return strings.TrimRight(baseURL, "/") + "/api/data-source-integrations/oauth/" + url.PathEscape(providerType) + "/callback"
}

// --- Org OAuth Client Config ---

// ListOrgOAuthClients returns all OAuth client configs for an org.
// GET /api/orgs/:orgID/data-source-integrations/oauth-clients
func (c *Client) ListOrgOAuthClients(ctx context.Context, orgID string) ([]OAuthClientConfig, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/orgs/"+url.PathEscape(orgID)+"/data-source-integrations/oauth-clients", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result []OAuthClientConfig
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

// GetOrgOAuthClient returns a specific OAuth client config for an org.
// GET /api/orgs/:orgID/data-source-integrations/oauth-clients/:providerType
func (c *Client) GetOrgOAuthClient(ctx context.Context, orgID, providerType string) (*OAuthClientConfig, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/orgs/"+url.PathEscape(orgID)+"/data-source-integrations/oauth-clients/"+url.PathEscape(providerType), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result OAuthClientConfig
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// UpsertOrgOAuthClient creates or updates an OAuth client config for an org.
// PUT /api/orgs/:orgID/data-source-integrations/oauth-clients/:providerType
func (c *Client) UpsertOrgOAuthClient(ctx context.Context, orgID, providerType string, req *SetOAuthClientRequest) (*OAuthClientConfig, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "PUT", c.base+"/api/orgs/"+url.PathEscape(orgID)+"/data-source-integrations/oauth-clients/"+url.PathEscape(providerType), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if err := c.setHeaders(httpReq); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result OAuthClientConfig
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// DeleteOrgOAuthClient deletes an OAuth client config for an org.
// DELETE /api/orgs/:orgID/data-source-integrations/oauth-clients/:providerType
// Returns 204 No Content on success.
func (c *Client) DeleteOrgOAuthClient(ctx context.Context, orgID, providerType string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.base+"/api/orgs/"+url.PathEscape(orgID)+"/data-source-integrations/oauth-clients/"+url.PathEscape(providerType), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	_, _ = io.Copy(io.Discard, resp.Body)

	return nil
}

// --- Project OAuth Client Config ---

// ListProjectOAuthClients returns all OAuth client configs for a project.
// GET /api/projects/:projectID/data-source-integrations/oauth-clients
func (c *Client) ListProjectOAuthClients(ctx context.Context, projectID string) ([]OAuthClientConfig, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/projects/"+url.PathEscape(projectID)+"/data-source-integrations/oauth-clients", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result []OAuthClientConfig
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

// GetProjectOAuthClient returns a specific OAuth client config for a project.
// GET /api/projects/:projectID/data-source-integrations/oauth-clients/:providerType
func (c *Client) GetProjectOAuthClient(ctx context.Context, projectID, providerType string) (*OAuthClientConfig, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/projects/"+url.PathEscape(projectID)+"/data-source-integrations/oauth-clients/"+url.PathEscape(providerType), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result OAuthClientConfig
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// UpsertProjectOAuthClient creates or updates an OAuth client config for a project.
// PUT /api/projects/:projectID/data-source-integrations/oauth-clients/:providerType
func (c *Client) UpsertProjectOAuthClient(ctx context.Context, projectID, providerType string, req *SetOAuthClientRequest) (*OAuthClientConfig, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "PUT", c.base+"/api/projects/"+url.PathEscape(projectID)+"/data-source-integrations/oauth-clients/"+url.PathEscape(providerType), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if err := c.setHeaders(httpReq); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result OAuthClientConfig
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// DeleteProjectOAuthClient deletes an OAuth client config for a project.
// DELETE /api/projects/:projectID/data-source-integrations/oauth-clients/:providerType
// Returns 204 No Content on success.
func (c *Client) DeleteProjectOAuthClient(ctx context.Context, projectID, providerType string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.base+"/api/projects/"+url.PathEscape(projectID)+"/data-source-integrations/oauth-clients/"+url.PathEscape(providerType), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	_, _ = io.Copy(io.Discard, resp.Body)

	return nil
}
