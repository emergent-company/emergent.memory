// Package search provides the Search service client for the Emergent API SDK.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Search API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	orgID     string
	projectID string
}

// SearchRequest represents a search query.
type SearchRequest struct {
	Query    string `json:"query"`
	Strategy string `json:"strategy,omitempty"` // lexical, semantic, hybrid
	Limit    int    `json:"limit,omitempty"`
}

// SearchResult represents a single search result.
type SearchResult struct {
	DocumentID string  `json:"document_id"`
	ChunkID    string  `json:"chunk_id"`
	Content    string  `json:"content"`
	Score      float64 `json:"score"`
}

// SearchResponse represents the response from a search query.
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Total   int            `json:"total"`
}

// NewClient creates a new Search service client.
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

// Search performs a search query.
func (c *Client) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/search/unified", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Add authentication
	if err := c.auth.Authenticate(httpReq); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Add context headers
	if c.orgID != "" {
		httpReq.Header.Set("X-Org-ID", c.orgID)
	}
	if c.projectID != "" {
		httpReq.Header.Set("X-Project-ID", c.projectID)
	}

	// Execute request
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for errors
	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	// Parse response
	var result struct {
		Data SearchResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result.Data, nil
}
