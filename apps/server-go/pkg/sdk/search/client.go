// Package search provides the Search service client for the Emergent API SDK.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Search API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// SearchRequest represents a search query.
type SearchRequest struct {
	Query          string `json:"query"`
	Limit          int    `json:"limit,omitempty"`
	ResultTypes    string `json:"resultTypes,omitempty"`    // graph, text, both
	FusionStrategy string `json:"fusionStrategy,omitempty"` // weighted, rrf, interleave, graph_first, text_first
	IncludeDebug   bool   `json:"includeDebug,omitempty"`
}

// SearchResult represents a unified search result item (can be graph, text, or relationship).
type SearchResult struct {
	// Common fields
	Type  string  `json:"type"` // "graph", "text", or "relationship"
	Score float32 `json:"score"`
	Rank  int     `json:"rank"`

	// Graph object fields
	ObjectID        string         `json:"object_id,omitempty"`
	CanonicalID     string         `json:"canonical_id,omitempty"`
	ObjectType      string         `json:"object_type,omitempty"`
	Key             string         `json:"key,omitempty"`
	Fields          map[string]any `json:"fields,omitempty"`
	LexicalScore    *float32       `json:"lexical_score,omitempty"`
	VectorScore     *float32       `json:"vector_score,omitempty"`
	TruncatedFields []string       `json:"truncated_fields,omitempty"`

	// Text chunk fields
	DocumentID string `json:"document_id,omitempty"`
	ChunkID    string `json:"chunk_id,omitempty"`
	Content    string `json:"content,omitempty"`

	// Relationship fields
	RelationshipType string  `json:"relationship_type,omitempty"`
	SrcObjectID      string  `json:"src_object_id,omitempty"`
	SrcObjectType    string  `json:"src_object_type,omitempty"`
	SrcKey           *string `json:"src_key,omitempty"`
	DstObjectID      string  `json:"dst_object_id,omitempty"`
	DstObjectType    string  `json:"dst_object_type,omitempty"`
	DstKey           *string `json:"dst_key,omitempty"`
}

// SearchMetadata contains metadata about the search results.
type SearchMetadata struct {
	Total      int     `json:"totalResults"`
	GraphCount int     `json:"graphResultCount"`
	TextCount  int     `json:"textResultCount"`
	RelCount   int     `json:"relationshipResultCount"`
	ElapsedMs  float64 `json:"elapsed_ms"`
}

// SearchResponse represents the response from a unified search query.
type SearchResponse struct {
	Results  []SearchResult `json:"results"`
	Metadata SearchMetadata `json:"metadata"`
	Total    int            // Convenience field (same as Metadata.Total)
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
	c.mu.Lock()
	defer c.mu.Unlock()
	c.orgID = orgID
	c.projectID = projectID
}

// Search performs a search query.
func (c *Client) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

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
	if orgID != "" {
		httpReq.Header.Set("X-Org-ID", orgID)
	}
	if projectID != "" {
		httpReq.Header.Set("X-Project-ID", projectID)
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
	var apiResponse struct {
		Results  []SearchResult `json:"results"`
		Metadata SearchMetadata `json:"metadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	result := &SearchResponse{
		Results:  apiResponse.Results,
		Metadata: apiResponse.Metadata,
		Total:    apiResponse.Metadata.Total,
	}

	return result, nil
}
