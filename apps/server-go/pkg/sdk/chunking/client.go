// Package chunking provides the Chunking service client for the Emergent API SDK.
// This client allows re-chunking of documents using the current chunking strategy.
package chunking

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Chunking API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// NewClient creates a new chunking client.
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

// RecreateChunksResponse is the response from recreating document chunks.
type RecreateChunksResponse struct {
	Status  string                `json:"status"`
	Summary RecreateChunksSummary `json:"summary"`
}

// RecreateChunksSummary contains details about the re-chunking operation.
type RecreateChunksSummary struct {
	OldChunks int            `json:"oldChunks"`
	NewChunks int            `json:"newChunks"`
	Strategy  string         `json:"strategy"`
	Config    map[string]any `json:"config,omitempty"`
}

// --- Methods ---

// RecreateChunks deletes existing chunks for a document and regenerates them
// using the current chunking strategy.
// POST /api/documents/:id/recreate-chunks
func (c *Client) RecreateChunks(ctx context.Context, documentID string) (*RecreateChunksResponse, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/documents/"+url.PathEscape(documentID)+"/recreate-chunks", nil)
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

	var result RecreateChunksResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}
