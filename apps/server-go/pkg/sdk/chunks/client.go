// Package chunks provides the Chunks service client for the Emergent API SDK.
package chunks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/emergent/emergent-core/pkg/sdk/auth"
	sdkerrors "github.com/emergent/emergent-core/pkg/sdk/errors"
)

// Client provides access to the Chunks API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	orgID     string
	projectID string
}

// ChunkDTO represents a document chunk in the Emergent API.
type ChunkDTO struct {
	ID         string    `json:"id"`
	DocumentID string    `json:"document_id"`
	Content    string    `json:"content"`
	Position   int       `json:"position"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListOptions holds options for listing chunks.
type ListOptions struct {
	DocumentID string `json:"document_id,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	Cursor     string `json:"cursor,omitempty"`
}

// ListResponse represents the response from listing chunks.
type ListResponse struct {
	Data []ChunkDTO `json:"data"`
	Meta struct {
		NextCursor string `json:"next_cursor,omitempty"`
	} `json:"meta"`
}

// NewClient creates a new Chunks service client.
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

// List retrieves a list of chunks.
func (c *Client) List(ctx context.Context, opts *ListOptions) (*ListResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/chunks", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication
	if err := c.auth.Authenticate(req); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Add context headers
	if c.orgID != "" {
		req.Header.Set("X-Org-ID", c.orgID)
	}
	if c.projectID != "" {
		req.Header.Set("X-Project-ID", c.projectID)
	}

	// Add query parameters
	if opts != nil {
		q := req.URL.Query()
		if opts.DocumentID != "" {
			q.Set("document_id", opts.DocumentID)
		}
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", opts.Limit))
		}
		if opts.Cursor != "" {
			q.Set("cursor", opts.Cursor)
		}
		req.URL.RawQuery = q.Encode()
	}

	// Execute request
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for errors
	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	// Parse response
	var result ListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}
