// Package useractivity provides the User Activity service client for the Emergent API SDK.
package useractivity

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

// Client provides access to the User Activity API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// NewClient creates a new user activity client.
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

// RecentItem represents a recent activity item.
type RecentItem struct {
	ID              string    `json:"id"`
	ResourceType    string    `json:"resourceType"`
	ResourceID      string    `json:"resourceId"`
	ResourceName    *string   `json:"resourceName,omitempty"`
	ResourceSubtype *string   `json:"resourceSubtype,omitempty"`
	ActionType      string    `json:"actionType"`
	AccessedAt      time.Time `json:"accessedAt"`
	ProjectID       string    `json:"projectId"`
}

// RecentItemsResponse wraps the list of recent items.
type RecentItemsResponse struct {
	Data []RecentItem `json:"data"`
}

// RecordActivityRequest represents a request to record user activity.
type RecordActivityRequest struct {
	ResourceType    string  `json:"resourceType"`
	ResourceID      string  `json:"resourceId"`
	ResourceName    *string `json:"resourceName,omitempty"`
	ResourceSubtype *string `json:"resourceSubtype,omitempty"`
	ActionType      string  `json:"actionType"`
}

// RecordOptions holds options for recording activity.
type RecordOptions struct {
	ProjectID string // Required: project_id query param
}

// ListOptions holds options for listing recent activity.
type ListOptions struct {
	Limit int // Max results (default 20, max 100)
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

// Record records a user activity event.
// The projectID parameter is required and passed as a query param.
func (c *Client) Record(ctx context.Context, projectID string, recordReq *RecordActivityRequest) error {
	u, err := url.Parse(c.base + "/api/user-activity/record")
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	q.Set("project_id", projectID)
	u.RawQuery = q.Encode()

	body, err := json.Marshal(recordReq)
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

	// Drain response body
	_, _ = io.Copy(io.Discard, resp.Body)

	return nil
}

// GetRecent retrieves recent activity across all resource types.
func (c *Client) GetRecent(ctx context.Context, opts *ListOptions) (*RecentItemsResponse, error) {
	u, err := url.Parse(c.base + "/api/user-activity/recent")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if opts != nil && opts.Limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", opts.Limit))
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

	var result RecentItemsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetRecentByType retrieves recent activity filtered by resource type.
func (c *Client) GetRecentByType(ctx context.Context, resourceType string, opts *ListOptions) (*RecentItemsResponse, error) {
	u, err := url.Parse(c.base + "/api/user-activity/recent/" + url.PathEscape(resourceType))
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if opts != nil && opts.Limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", opts.Limit))
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

	var result RecentItemsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// DeleteAll deletes all recent activity for the authenticated user.
func (c *Client) DeleteAll(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.base+"/api/user-activity/recent", nil)
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

// DeleteByResource deletes a specific recent activity record by type and resource ID.
func (c *Client) DeleteByResource(ctx context.Context, resourceType, resourceID string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.base+"/api/user-activity/recent/"+url.PathEscape(resourceType)+"/"+url.PathEscape(resourceID), nil)
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
