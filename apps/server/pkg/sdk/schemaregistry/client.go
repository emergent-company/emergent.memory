// Package schemaregistry provides the Schema Registry service client for the Emergent API SDK.
package schemaregistry

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

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
)

// Client provides access to the Schema Registry API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// NewClient creates a new schema registry client.
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

// SchemaRegistryEntry represents a schema registry entry in the API response.
type SchemaRegistryEntry struct {
	ID                    string                 `json:"id"`
	Type                  string                 `json:"type"`
	Source                string                 `json:"source"`
	Namespace             *string                `json:"namespace,omitempty"`
	SchemaID              *string                `json:"schema_id,omitempty"`
	SchemaName            *string                `json:"schema_name,omitempty"`
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

// SchemaRegistryStats contains statistics for a project's schema registry.
type SchemaRegistryStats struct {
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
	// Namespace filters by namespace. Empty = only NULL-namespace types (default).
	// Set to "all" to return all types regardless of namespace.
	// Set to a specific value to return types in that namespace.
	Namespace string
}

// CreateTypeRequest is the request to register a custom object type for a project.
type CreateTypeRequest struct {
	TypeName         string          `json:"type_name"`
	Namespace        *string         `json:"namespace,omitempty"`
	Description      *string         `json:"description,omitempty"`
	JSONSchema       json.RawMessage `json:"json_schema"`
	UIConfig         json.RawMessage `json:"ui_config,omitempty"`
	ExtractionConfig json.RawMessage `json:"extraction_config,omitempty"`
	Enabled          *bool           `json:"enabled,omitempty"` // defaults to true
}

// UpdateTypeRequest is the request to update a registered type.
type UpdateTypeRequest struct {
	Namespace        *string         `json:"namespace,omitempty"` // set to "" to clear namespace
	Description      *string         `json:"description,omitempty"`
	JSONSchema       json.RawMessage `json:"json_schema,omitempty"`
	UIConfig         json.RawMessage `json:"ui_config,omitempty"`
	ExtractionConfig json.RawMessage `json:"extraction_config,omitempty"`
	Enabled          *bool           `json:"enabled,omitempty"`
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

// GetProjectTypes returns all object types registered for a project.
func (c *Client) GetProjectTypes(ctx context.Context, projectID string, opts *ListTypesOptions) ([]SchemaRegistryEntry, error) {
	u, err := url.Parse(c.base + "/api/schema-registry/projects/" + url.PathEscape(projectID))
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
		if opts.Namespace != "" {
			q.Set("namespace", opts.Namespace)
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

	var types []SchemaRegistryEntry
	if err := json.NewDecoder(resp.Body).Decode(&types); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return types, nil
}

// GetObjectType returns a specific object type definition by name.
func (c *Client) GetObjectType(ctx context.Context, projectID, typeName string) (*SchemaRegistryEntry, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/schema-registry/projects/"+url.PathEscape(projectID)+"/types/"+url.PathEscape(typeName), nil)
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

	var entry SchemaRegistryEntry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &entry, nil
}

// GetTypeStats returns statistics about a project's schema registry.
func (c *Client) GetTypeStats(ctx context.Context, projectID string) (*SchemaRegistryStats, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/schema-registry/projects/"+url.PathEscape(projectID)+"/stats", nil)
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

	var stats SchemaRegistryStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &stats, nil
}

// CreateType registers a new custom object type for a project.
// POST /api/schema-registry/projects/:projectId/types
func (c *Client) CreateType(ctx context.Context, projectID string, req *CreateTypeRequest) (*SchemaRegistryEntry, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/schema-registry/projects/"+url.PathEscape(projectID)+"/types", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if err := c.setHeaders(httpReq); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var entry SchemaRegistryEntry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &entry, nil
}

// UpdateType updates an existing type in the project schema registry.
// PUT /api/schema-registry/projects/:projectId/types/:typeName
func (c *Client) UpdateType(ctx context.Context, projectID, typeName string, req *UpdateTypeRequest) (*SchemaRegistryEntry, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "PUT", c.base+"/api/schema-registry/projects/"+url.PathEscape(projectID)+"/types/"+url.PathEscape(typeName), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if err := c.setHeaders(httpReq); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var entry SchemaRegistryEntry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &entry, nil
}

// DeleteType removes a type from the project schema registry.
// DELETE /api/schema-registry/projects/:projectId/types/:typeName
func (c *Client) DeleteType(ctx context.Context, projectID, typeName string) error {
	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", c.base+"/api/schema-registry/projects/"+url.PathEscape(projectID)+"/types/"+url.PathEscape(typeName), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(httpReq); err != nil {
		return err
	}

	resp, err := c.http.Do(httpReq)
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
