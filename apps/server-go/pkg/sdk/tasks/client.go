// Package tasks provides the Tasks service client for the Emergent API SDK.
package tasks

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

// Client provides access to the Tasks API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// NewClient creates a new tasks client.
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

// Task represents a task entity.
type Task struct {
	ID              string          `json:"id"`
	ProjectID       string          `json:"projectId"`
	Title           string          `json:"title"`
	Description     *string         `json:"description,omitempty"`
	Type            string          `json:"type"`
	Status          string          `json:"status"`
	ResolvedAt      *time.Time      `json:"resolvedAt,omitempty"`
	ResolvedBy      *string         `json:"resolvedBy,omitempty"`
	ResolutionNotes *string         `json:"resolutionNotes,omitempty"`
	SourceType      *string         `json:"sourceType,omitempty"`
	SourceID        *string         `json:"sourceId,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

// TaskCounts represents task counts by status.
type TaskCounts struct {
	Pending   int64 `json:"pending"`
	Accepted  int64 `json:"accepted"`
	Rejected  int64 `json:"rejected"`
	Cancelled int64 `json:"cancelled"`
}

// TaskListResponse wraps the list of tasks for the API response.
type TaskListResponse struct {
	Data  []Task `json:"data"`
	Total int    `json:"total"`
}

// TaskResponse wraps a single task for the API response.
type TaskResponse struct {
	Data Task `json:"data"`
}

// ResolveTaskRequest represents a request to resolve a task.
type ResolveTaskRequest struct {
	Resolution      string  `json:"resolution"` // "accepted" or "rejected"
	ResolutionNotes *string `json:"resolutionNotes,omitempty"`
}

// ListOptions holds options for listing tasks.
type ListOptions struct {
	ProjectID string // Project ID (uses X-Project-ID header if empty, or project_id query param)
	Status    string // Filter by status: "pending", "accepted", "rejected", "cancelled"
	Type      string // Filter by task type
	Limit     int    // Max results (1-100)
	Offset    int    // Pagination offset
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

// addListParams adds common list params to URL query.
func addListParams(q url.Values, opts *ListOptions) {
	if opts == nil {
		return
	}
	if opts.ProjectID != "" {
		q.Set("project_id", opts.ProjectID)
	}
	if opts.Status != "" {
		q.Set("status", opts.Status)
	}
	if opts.Type != "" {
		q.Set("type", opts.Type)
	}
	if opts.Limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if opts.Offset > 0 {
		q.Set("offset", fmt.Sprintf("%d", opts.Offset))
	}
}

// List returns paginated tasks for a specific project.
// ProjectID is required either in opts.ProjectID or via the client's projectID context.
func (c *Client) List(ctx context.Context, opts *ListOptions) (*TaskListResponse, error) {
	u, err := url.Parse(c.base + "/api/tasks")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	addListParams(q, opts)
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

	var result TaskListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetCounts returns task counts by status for a specific project.
// ProjectID is required either in opts.ProjectID or via the client's projectID context.
func (c *Client) GetCounts(ctx context.Context, projectID string) (*TaskCounts, error) {
	u, err := url.Parse(c.base + "/api/tasks/counts")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if projectID != "" {
		q.Set("project_id", projectID)
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

	var counts TaskCounts
	if err := json.NewDecoder(resp.Body).Decode(&counts); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &counts, nil
}

// ListAll returns paginated tasks across all projects for the authenticated user.
func (c *Client) ListAll(ctx context.Context, opts *ListOptions) (*TaskListResponse, error) {
	u, err := url.Parse(c.base + "/api/tasks/all")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if opts != nil {
		if opts.Status != "" {
			q.Set("status", opts.Status)
		}
		if opts.Type != "" {
			q.Set("type", opts.Type)
		}
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", opts.Limit))
		}
		if opts.Offset > 0 {
			q.Set("offset", fmt.Sprintf("%d", opts.Offset))
		}
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

	var result TaskListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetAllCounts returns aggregated task counts across all projects.
func (c *Client) GetAllCounts(ctx context.Context) (*TaskCounts, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/tasks/all/counts", nil)
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

	var counts TaskCounts
	if err := json.NewDecoder(resp.Body).Decode(&counts); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &counts, nil
}

// GetByID returns a specific task by ID for a given project.
func (c *Client) GetByID(ctx context.Context, taskID, projectID string) (*TaskResponse, error) {
	u, err := url.Parse(c.base + "/api/tasks/" + url.PathEscape(taskID))
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if projectID != "" {
		q.Set("project_id", projectID)
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

	var result TaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Resolve marks a task as accepted or rejected.
func (c *Client) Resolve(ctx context.Context, taskID, projectID string, resolveReq *ResolveTaskRequest) error {
	u, err := url.Parse(c.base + "/api/tasks/" + url.PathEscape(taskID) + "/resolve")
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if projectID != "" {
		q.Set("project_id", projectID)
	}
	u.RawQuery = q.Encode()

	body, err := json.Marshal(resolveReq)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
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

// Cancel cancels a pending task.
func (c *Client) Cancel(ctx context.Context, taskID, projectID string) error {
	u, err := url.Parse(c.base + "/api/tasks/" + url.PathEscape(taskID) + "/cancel")
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if projectID != "" {
		q.Set("project_id", projectID)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), nil)
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
