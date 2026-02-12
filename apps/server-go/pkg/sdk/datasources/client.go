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
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Data Source Integrations API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
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
	IconURL     string `json:"iconUrl,omitempty"`
	Available   bool   `json:"available"`
}

// ProviderSchema represents the configuration schema for a provider.
type ProviderSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Required   []string               `json:"required,omitempty"`
}

// Integration represents a data source integration.
type Integration struct {
	ID                  string     `json:"id"`
	ProjectID           string     `json:"projectId"`
	Name                string     `json:"name"`
	Description         *string    `json:"description,omitempty"`
	ProviderType        string     `json:"providerType"`
	SourceType          string     `json:"sourceType"`
	SyncMode            string     `json:"syncMode"`
	SyncIntervalMinutes *int       `json:"syncIntervalMinutes,omitempty"`
	LastSyncedAt        *time.Time `json:"lastSyncedAt,omitempty"`
	NextSyncAt          *time.Time `json:"nextSyncAt,omitempty"`
	Status              string     `json:"status"`
	ErrorMessage        *string    `json:"errorMessage,omitempty"`
	ErrorCount          int        `json:"errorCount"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
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
	if c.orgID != "" {
		req.Header.Set("X-Org-ID", c.orgID)
	}
	if c.projectID != "" {
		req.Header.Set("X-Project-ID", c.projectID)
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

// GetProviderSchema returns the configuration schema for a provider type.
// GET /api/data-source-integrations/providers/:providerType/schema
func (c *Client) GetProviderSchema(ctx context.Context, providerType string) (*ProviderSchema, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/data-source-integrations/providers/"+providerType+"/schema", nil)
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
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/data-source-integrations/"+integrationID, nil)
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

	req, err := http.NewRequestWithContext(ctx, "PATCH", c.base+"/api/data-source-integrations/"+integrationID, bytes.NewReader(body))
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
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.base+"/api/data-source-integrations/"+integrationID, nil)
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
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/data-source-integrations/"+integrationID+"/test-connection", nil)
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

	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/data-source-integrations/"+integrationID+"/sync", bodyReader)
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
	u, err := url.Parse(c.base + "/api/data-source-integrations/" + integrationID + "/sync-jobs")
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
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/data-source-integrations/"+integrationID+"/sync-jobs/latest", nil)
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
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/data-source-integrations/"+integrationID+"/sync-jobs/"+jobID, nil)
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
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/data-source-integrations/"+integrationID+"/sync-jobs/"+jobID+"/cancel", nil)
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
