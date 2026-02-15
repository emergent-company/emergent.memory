// Package branches provides the Branches service client for the Emergent API SDK.
package branches

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Branches API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// NewClient creates a new branches client.
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

// Branch represents a branch entity in the API response.
type Branch struct {
	ID             string  `json:"id"`
	ProjectID      *string `json:"project_id,omitempty"`
	Name           string  `json:"name"`
	ParentBranchID *string `json:"parent_branch_id,omitempty"`
	CreatedAt      string  `json:"created_at"`
}

// CreateBranchRequest represents a branch creation request.
type CreateBranchRequest struct {
	Name           string  `json:"name"`
	ProjectID      *string `json:"project_id,omitempty"`
	ParentBranchID *string `json:"parent_branch_id,omitempty"`
}

// UpdateBranchRequest represents a branch update request.
type UpdateBranchRequest struct {
	Name *string `json:"name,omitempty"`
}

// ListOptions holds options for listing branches.
type ListOptions struct {
	ProjectID string // Filter by project ID
}

// setHeaders adds auth and context headers to the request.
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

// List returns all branches, optionally filtered by project.
func (c *Client) List(ctx context.Context, opts *ListOptions) ([]*Branch, error) {
	u, err := url.Parse(c.base + "/api/graph/branches")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if opts != nil && opts.ProjectID != "" {
		q.Set("project_id", opts.ProjectID)
	}
	u.RawQuery = q.Encode()

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

	var branches []*Branch
	if err := json.NewDecoder(resp.Body).Decode(&branches); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return branches, nil
}

// Get retrieves a single branch by ID.
func (c *Client) Get(ctx context.Context, id string) (*Branch, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/graph/branches/"+url.PathEscape(id), nil)
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

	var branch Branch
	if err := json.NewDecoder(resp.Body).Decode(&branch); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &branch, nil
}

// Create creates a new branch. Returns the created branch (HTTP 201).
func (c *Client) Create(ctx context.Context, createReq *CreateBranchRequest) (*Branch, error) {
	body, err := json.Marshal(createReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/graph/branches", bytes.NewReader(body))
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

	var branch Branch
	if err := json.NewDecoder(resp.Body).Decode(&branch); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &branch, nil
}

// Update updates a branch by ID (PATCH).
func (c *Client) Update(ctx context.Context, id string, updateReq *UpdateBranchRequest) (*Branch, error) {
	body, err := json.Marshal(updateReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PATCH", c.base+"/api/graph/branches/"+url.PathEscape(id), bytes.NewReader(body))
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

	var branch Branch
	if err := json.NewDecoder(resp.Body).Decode(&branch); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &branch, nil
}

// Delete deletes a branch by ID. Returns nil on success (HTTP 204).
func (c *Client) Delete(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.base+"/api/graph/branches/"+url.PathEscape(id), nil)
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

	// Drain response body
	_, _ = io.Copy(io.Discard, resp.Body)

	return nil
}
