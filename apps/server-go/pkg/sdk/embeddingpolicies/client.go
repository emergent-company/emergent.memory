// Package embeddingpolicies provides the Embedding Policies service client for the Emergent API SDK.
package embeddingpolicies

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

// Client provides access to the Embedding Policies API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// NewClient creates a new embedding policies client.
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

// EmbeddingPolicy represents an embedding policy response.
type EmbeddingPolicy struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	Name           string    `json:"name"`
	Description    *string   `json:"description,omitempty"`
	ObjectTypes    []string  `json:"object_types"`
	Fields         []string  `json:"fields"`
	Template       string    `json:"template"`
	Model          string    `json:"model"`
	Active         bool      `json:"active"`
	ChunkingConfig any       `json:"chunking_config,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// CreateEmbeddingPolicyRequest is the request to create a new embedding policy.
type CreateEmbeddingPolicyRequest struct {
	ProjectID      string   `json:"projectId"`
	Name           string   `json:"name"`
	Description    *string  `json:"description,omitempty"`
	ObjectTypes    []string `json:"object_types"`
	Fields         []string `json:"fields"`
	Template       string   `json:"template"`
	Model          string   `json:"model"`
	Active         *bool    `json:"active,omitempty"`
	ChunkingConfig any      `json:"chunking_config,omitempty"`
}

// UpdateEmbeddingPolicyRequest is the request to update an embedding policy.
type UpdateEmbeddingPolicyRequest struct {
	Name           *string  `json:"name,omitempty"`
	Description    *string  `json:"description,omitempty"`
	ObjectTypes    []string `json:"object_types,omitempty"`
	Fields         []string `json:"fields,omitempty"`
	Template       *string  `json:"template,omitempty"`
	Model          *string  `json:"model,omitempty"`
	Active         *bool    `json:"active,omitempty"`
	ChunkingConfig any      `json:"chunking_config,omitempty"`
}

// --- Methods ---

// List lists all embedding policies for a project.
// GET /api/graph/embedding-policies?project_id=...
func (c *Client) List(ctx context.Context) ([]EmbeddingPolicy, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	u := c.base + "/api/graph/embedding-policies"
	if projectID != "" {
		u += "?project_id=" + url.QueryEscape(projectID)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
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

	var result []EmbeddingPolicy
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result, nil
}

// GetByID gets a specific embedding policy by ID.
// GET /api/graph/embedding-policies/:id?project_id=...
func (c *Client) GetByID(ctx context.Context, policyID string) (*EmbeddingPolicy, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	u := c.base + "/api/graph/embedding-policies/" + url.PathEscape(policyID)
	if projectID != "" {
		u += "?project_id=" + url.QueryEscape(projectID)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
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

	var result EmbeddingPolicy
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// Create creates a new embedding policy.
// POST /api/graph/embedding-policies
func (c *Client) Create(ctx context.Context, req *CreateEmbeddingPolicyRequest) (*EmbeddingPolicy, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/graph/embedding-policies", bytes.NewReader(body))
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

	var result EmbeddingPolicy
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// Update updates an existing embedding policy.
// PUT /api/graph/embedding-policies/:id?project_id=...
func (c *Client) Update(ctx context.Context, policyID string, req *UpdateEmbeddingPolicyRequest) (*EmbeddingPolicy, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	u := c.base + "/api/graph/embedding-policies/" + url.PathEscape(policyID)
	if projectID != "" {
		u += "?project_id=" + url.QueryEscape(projectID)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(body))
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

	var result EmbeddingPolicy
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// Delete deletes an embedding policy.
// DELETE /api/graph/embedding-policies/:id?project_id=...
func (c *Client) Delete(ctx context.Context, policyID string) error {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	u := c.base + "/api/graph/embedding-policies/" + url.PathEscape(policyID)
	if projectID != "" {
		u += "?project_id=" + url.QueryEscape(projectID)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
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
