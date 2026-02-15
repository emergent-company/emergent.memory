// Package integrations provides the Integrations service client for the Emergent API SDK.
package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Integrations API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// NewClient creates a new integrations client.
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

// AvailableIntegration represents an available integration provider.
type AvailableIntegration struct {
	Name             string   `json:"name"`
	DisplayName      string   `json:"display_name"`
	Description      string   `json:"description"`
	Category         string   `json:"category"`
	AuthType         string   `json:"auth_type"`
	RequiredFields   []string `json:"required_fields"`
	OptionalFields   []string `json:"optional_fields,omitempty"`
	DocumentationURL string   `json:"documentation_url,omitempty"`
}

// Integration represents a configured integration.
type Integration struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	DisplayName  string     `json:"display_name"`
	ProviderType string     `json:"provider_type"`
	OrgID        string     `json:"org_id"`
	ProjectID    string     `json:"project_id"`
	Status       string     `json:"status"`
	Config       any        `json:"config,omitempty"`
	LastSyncAt   *time.Time `json:"last_sync_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// PublicIntegration represents the public (non-sensitive) view of an integration.
type PublicIntegration struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	DisplayName  string     `json:"display_name"`
	ProviderType string     `json:"provider_type"`
	Status       string     `json:"status"`
	LastSyncAt   *time.Time `json:"last_sync_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// CreateIntegrationRequest is the request to create a new integration.
type CreateIntegrationRequest struct {
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	ProviderType string `json:"provider_type"`
	Config       any    `json:"config"`
}

// UpdateIntegrationRequest is the request to update an integration (PUT).
type UpdateIntegrationRequest struct {
	DisplayName string `json:"display_name,omitempty"`
	Config      any    `json:"config,omitempty"`
	Status      string `json:"status,omitempty"`
}

// TestConnectionResponse is the response from testing a connection.
type TestConnectionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// TriggerSyncResponse is the response from triggering a sync.
type TriggerSyncResponse struct {
	Success bool    `json:"success"`
	Message string  `json:"message"`
	JobID   *string `json:"job_id,omitempty"`
}

// TriggerSyncConfig is an optional configuration for triggering a sync.
type TriggerSyncConfig struct {
	FullSync        *bool    `json:"full_sync,omitempty"`
	SourceTypes     []string `json:"source_types,omitempty"`
	SpaceIDs        []string `json:"space_ids,omitempty"`
	IncludeArchived *bool    `json:"includeArchived,omitempty"`
	BatchSize       *int     `json:"batchSize,omitempty"`
}

// --- Methods ---

// ListAvailable lists all available integration providers.
// GET /api/integrations/available
func (c *Client) ListAvailable(ctx context.Context) ([]AvailableIntegration, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/integrations/available", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("X-Org-ID", orgID)
	httpReq.Header.Set("X-Project-ID", projectID)

	if err := c.auth.Authenticate(httpReq); err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result []AvailableIntegration
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result, nil
}

// List lists all configured integrations for the current project.
// GET /api/integrations
func (c *Client) List(ctx context.Context) ([]Integration, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/integrations", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("X-Org-ID", orgID)
	httpReq.Header.Set("X-Project-ID", projectID)

	if err := c.auth.Authenticate(httpReq); err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result []Integration
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result, nil
}

// Get gets a specific integration by name.
// GET /api/integrations/:name
func (c *Client) Get(ctx context.Context, name string) (*Integration, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/integrations/"+url.PathEscape(name), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("X-Org-ID", orgID)
	httpReq.Header.Set("X-Project-ID", projectID)

	if err := c.auth.Authenticate(httpReq); err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result Integration
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// GetPublic gets the public (non-sensitive) view of an integration.
// GET /api/integrations/:name/public
func (c *Client) GetPublic(ctx context.Context, name string) (*PublicIntegration, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/integrations/"+url.PathEscape(name)+"/public", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("X-Org-ID", orgID)
	httpReq.Header.Set("X-Project-ID", projectID)

	if err := c.auth.Authenticate(httpReq); err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result PublicIntegration
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// Create creates a new integration.
// POST /api/integrations
func (c *Client) Create(ctx context.Context, req *CreateIntegrationRequest) (*Integration, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/integrations", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Org-ID", orgID)
	httpReq.Header.Set("X-Project-ID", projectID)

	if err := c.auth.Authenticate(httpReq); err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result Integration
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// Update updates an existing integration (full replacement).
// PUT /api/integrations/:name
func (c *Client) Update(ctx context.Context, name string, req *UpdateIntegrationRequest) (*Integration, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, c.base+"/api/integrations/"+url.PathEscape(name), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Org-ID", orgID)
	httpReq.Header.Set("X-Project-ID", projectID)

	if err := c.auth.Authenticate(httpReq); err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result Integration
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// Delete deletes an integration.
// DELETE /api/integrations/:name
func (c *Client) Delete(ctx context.Context, name string) error {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.base+"/api/integrations/"+url.PathEscape(name), nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("X-Org-ID", orgID)
	httpReq.Header.Set("X-Project-ID", projectID)

	if err := c.auth.Authenticate(httpReq); err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	// Drain response body
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// TestConnection tests the connection for an integration.
// POST /api/integrations/:name/test
func (c *Client) TestConnection(ctx context.Context, name string) (*TestConnectionResponse, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/integrations/"+url.PathEscape(name)+"/test", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("X-Org-ID", orgID)
	httpReq.Header.Set("X-Project-ID", projectID)

	if err := c.auth.Authenticate(httpReq); err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result TestConnectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// TriggerSync triggers a sync for an integration.
// POST /api/integrations/:name/sync
func (c *Client) TriggerSync(ctx context.Context, name string, config *TriggerSyncConfig) (*TriggerSyncResponse, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	var body io.Reader
	if config != nil {
		b, err := json.Marshal(config)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		body = bytes.NewReader(b)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/integrations/"+url.PathEscape(name)+"/sync", body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if config != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("X-Org-ID", orgID)
	httpReq.Header.Set("X-Project-ID", projectID)

	if err := c.auth.Authenticate(httpReq); err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result TriggerSyncResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}
