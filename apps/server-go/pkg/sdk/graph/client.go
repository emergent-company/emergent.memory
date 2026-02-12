// Package graph provides the Graph service client for the Emergent API SDK.
// This includes objects, relationships, and graph search functionality.
package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Graph API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	orgID     string
	projectID string
}

// GraphObject represents an object in the knowledge graph.
type GraphObject struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

// GraphRelationship represents a relationship between two objects.
type GraphRelationship struct {
	ID         string                 `json:"id"`
	SourceID   string                 `json:"source_id"`
	TargetID   string                 `json:"target_id"`
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
}

// NewClient creates a new Graph service client.
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

// ListObjects retrieves a list of graph objects.
func (c *Client) ListObjects(ctx context.Context) ([]GraphObject, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/graph/objects", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.auth.Authenticate(req); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	if c.orgID != "" {
		req.Header.Set("X-Org-ID", c.orgID)
	}
	if c.projectID != "" {
		req.Header.Set("X-Project-ID", c.projectID)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result struct {
		Data []GraphObject `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

// GetObject retrieves a single graph object by ID.
func (c *Client) GetObject(ctx context.Context, id string) (*GraphObject, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/graph/objects/"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.auth.Authenticate(req); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	if c.orgID != "" {
		req.Header.Set("X-Org-ID", c.orgID)
	}
	if c.projectID != "" {
		req.Header.Set("X-Project-ID", c.projectID)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result struct {
		Data GraphObject `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result.Data, nil
}

// ListRelationships retrieves a list of graph relationships.
func (c *Client) ListRelationships(ctx context.Context) ([]GraphRelationship, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/graph/relationships", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.auth.Authenticate(req); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	if c.orgID != "" {
		req.Header.Set("X-Org-ID", c.orgID)
	}
	if c.projectID != "" {
		req.Header.Set("X-Project-ID", c.projectID)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result struct {
		Data []GraphRelationship `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}
