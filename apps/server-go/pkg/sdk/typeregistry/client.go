// Package typeregistry provides the Type Registry service client for the Emergent API SDK.
package typeregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Type Registry API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	orgID     string
	projectID string
}

// NewClient creates a new type registry client.
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

// TypeRegistryEntry represents a type registry entry in the API response.
type TypeRegistryEntry struct {
	ID                    string                 `json:"id"`
	Type                  string                 `json:"type"`
	Source                string                 `json:"source"`
	TemplatePackID        *string                `json:"template_pack_id,omitempty"`
	TemplatePackName      *string                `json:"template_pack_name,omitempty"`
	SchemaVersion         int                    `json:"schema_version"`
	JSONSchema            json.RawMessage        `json:"json_schema"`
	UIConfig              map[string]interface{} `json:"ui_config"`
	ExtractionConfig      map[string]interface{} `json:"extraction_config"`
	Enabled               bool                   `json:"enabled"`
	DiscoveryConfidence   *float64               `json:"discovery_confidence,omitempty"`
	Description           *string                `json:"description,omitempty"`
	ObjectCount           int                    `json:"object_count,omitempty"`
	CreatedAt             time.Time              `json:"created_at"`
	UpdatedAt             time.Time              `json:"updated_at"`
	OutgoingRelationships []RelationshipTypeInfo `json:"outgoing_relationships,omitempty"`
	IncomingRelationships []RelationshipTypeInfo `json:"incoming_relationships,omitempty"`
}

// RelationshipTypeInfo describes a relationship type for a specific object type.
type RelationshipTypeInfo struct {
	Type         string   `json:"type"`
	Label        *string  `json:"label,omitempty"`
	InverseLabel *string  `json:"inverse_label,omitempty"`
	Description  *string  `json:"description,omitempty"`
	TargetTypes  []string `json:"target_types,omitempty"`
	SourceTypes  []string `json:"source_types,omitempty"`
}

// TypeRegistryStats contains statistics for a project's type registry.
type TypeRegistryStats struct {
	TotalTypes       int `json:"total_types"`
	EnabledTypes     int `json:"enabled_types"`
	TemplateTypes    int `json:"template_types"`
	CustomTypes      int `json:"custom_types"`
	DiscoveredTypes  int `json:"discovered_types"`
	TotalObjects     int `json:"total_objects"`
	TypesWithObjects int `json:"types_with_objects"`
}

// ListTypesOptions holds query parameters for listing types.
type ListTypesOptions struct {
	EnabledOnly *bool  // Filter enabled types only (default true on server)
	Source      string // Filter by source: "template", "custom", "discovered", "all"
	Search      string // Search in type names
}

// setHeaders adds auth and context headers to the request.
func (c *Client) setHeaders(req *http.Request) error {
	if err := c.auth.Authenticate(req); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	if c.orgID != "" {
		req.Header.Set("X-Org-ID", c.orgID)
	}
	if c.projectID != "" {
		req.Header.Set("X-Project-ID", c.projectID)
	}
	return nil
}

// GetProjectTypes returns all object types registered for a project.
func (c *Client) GetProjectTypes(ctx context.Context, projectID string, opts *ListTypesOptions) ([]TypeRegistryEntry, error) {
	u, err := url.Parse(c.base + "/api/type-registry/projects/" + projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if opts != nil {
		if opts.EnabledOnly != nil {
			q.Set("enabled_only", fmt.Sprintf("%t", *opts.EnabledOnly))
		}
		if opts.Source != "" {
			q.Set("source", opts.Source)
		}
		if opts.Search != "" {
			q.Set("search", opts.Search)
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

	var types []TypeRegistryEntry
	if err := json.NewDecoder(resp.Body).Decode(&types); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return types, nil
}

// GetObjectType returns a specific object type definition by name.
func (c *Client) GetObjectType(ctx context.Context, projectID, typeName string) (*TypeRegistryEntry, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/type-registry/projects/"+projectID+"/types/"+typeName, nil)
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

	var entry TypeRegistryEntry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &entry, nil
}

// GetTypeStats returns statistics about a project's type registry.
func (c *Client) GetTypeStats(ctx context.Context, projectID string) (*TypeRegistryStats, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/type-registry/projects/"+projectID+"/stats", nil)
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

	var stats TypeRegistryStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &stats, nil
}
