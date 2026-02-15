// Package discoveryjobs provides the Discovery Jobs service client for the Emergent API SDK.
package discoveryjobs

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

// Client provides access to the Discovery Jobs API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// NewClient creates a new discovery jobs client.
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

// JSONMap is a generic JSON object.
type JSONMap map[string]any

// JSONArray is a generic JSON array.
type JSONArray []any

// StartDiscoveryRequest is the request to start a discovery job.
type StartDiscoveryRequest struct {
	DocumentIDs          []string `json:"document_ids"`
	BatchSize            int      `json:"batch_size,omitempty"`
	MinConfidence        float32  `json:"min_confidence,omitempty"`
	IncludeRelationships bool     `json:"include_relationships,omitempty"`
	MaxIterations        int      `json:"max_iterations,omitempty"`
}

// StartDiscoveryResponse is the response from starting a discovery job.
type StartDiscoveryResponse struct {
	JobID string `json:"job_id"`
}

// JobStatusResponse is the response from getting a job's status.
type JobStatusResponse struct {
	ID                      string     `json:"id"`
	Status                  string     `json:"status"`
	Progress                JSONMap    `json:"progress"`
	CreatedAt               time.Time  `json:"created_at"`
	StartedAt               *time.Time `json:"started_at,omitempty"`
	CompletedAt             *time.Time `json:"completed_at,omitempty"`
	ErrorMessage            *string    `json:"error_message,omitempty"`
	DiscoveredTypes         JSONArray  `json:"discovered_types"`
	DiscoveredRelationships JSONArray  `json:"discovered_relationships"`
	TemplatePackID          *string    `json:"template_pack_id,omitempty"`
}

// JobListItem is a summary of a discovery job for listing.
type JobListItem struct {
	ID                      string     `json:"id"`
	Status                  string     `json:"status"`
	Progress                JSONMap    `json:"progress"`
	CreatedAt               time.Time  `json:"created_at"`
	CompletedAt             *time.Time `json:"completed_at,omitempty"`
	DiscoveredTypes         JSONArray  `json:"discovered_types"`
	DiscoveredRelationships JSONArray  `json:"discovered_relationships"`
	TemplatePackID          *string    `json:"template_pack_id,omitempty"`
}

// CancelJobResponse is the response from cancelling a job.
type CancelJobResponse struct {
	Message string `json:"message"`
}

// FinalizeDiscoveryRequest is the request to finalize a discovery job into a template pack.
type FinalizeDiscoveryRequest struct {
	PackName              string                 `json:"packName"`
	Mode                  string                 `json:"mode"`
	ExistingPackID        *string                `json:"existingPackId,omitempty"`
	IncludedTypes         []IncludedType         `json:"includedTypes"`
	IncludedRelationships []IncludedRelationship `json:"includedRelationships,omitempty"`
}

// IncludedType represents a type selected for the template pack.
type IncludedType struct {
	TypeName           string         `json:"type_name"`
	Description        string         `json:"description"`
	Properties         map[string]any `json:"properties"`
	RequiredProperties []string       `json:"required_properties"`
	ExampleInstances   []any          `json:"example_instances"`
	Frequency          int            `json:"frequency"`
}

// IncludedRelationship represents a relationship selected for the template pack.
type IncludedRelationship struct {
	SourceType   string `json:"source_type"`
	TargetType   string `json:"target_type"`
	RelationType string `json:"relation_type"`
	Description  string `json:"description"`
	Cardinality  string `json:"cardinality"`
}

// FinalizeDiscoveryResponse is the response from finalizing discovery.
type FinalizeDiscoveryResponse struct {
	TemplatePackID string `json:"template_pack_id"`
	Message        string `json:"message"`
}

// --- Methods ---

// StartDiscovery starts a new schema discovery job.
// POST /api/discovery-jobs
func (c *Client) StartDiscovery(ctx context.Context, req *StartDiscoveryRequest) (*StartDiscoveryResponse, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/discovery-jobs", bytes.NewReader(body))
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

	var result StartDiscoveryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// ListJobs lists all discovery jobs for the current project.
// GET /api/discovery-jobs
func (c *Client) ListJobs(ctx context.Context) ([]JobListItem, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/discovery-jobs", nil)
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

	var result []JobListItem
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result, nil
}

// GetJobStatus gets the status of a specific discovery job.
// GET /api/discovery-jobs/:id
func (c *Client) GetJobStatus(ctx context.Context, jobID string) (*JobStatusResponse, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/discovery-jobs/"+url.PathEscape(jobID), nil)
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

	var result JobStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// CancelJob cancels a running discovery job.
// POST /api/discovery-jobs/:id/cancel
func (c *Client) CancelJob(ctx context.Context, jobID string) (*CancelJobResponse, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/discovery-jobs/"+url.PathEscape(jobID)+"/cancel", nil)
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

	var result CancelJobResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// FinalizeDiscovery finalizes a discovery job into a template pack.
// POST /api/discovery-jobs/:id/finalize
func (c *Client) FinalizeDiscovery(ctx context.Context, jobID string, req *FinalizeDiscoveryRequest) (*FinalizeDiscoveryResponse, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/discovery-jobs/"+url.PathEscape(jobID)+"/finalize", bytes.NewReader(body))
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

	var result FinalizeDiscoveryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Drain any remaining body
	_, _ = io.Copy(io.Discard, resp.Body)

	return &result, nil
}
