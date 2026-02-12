// Package templatepacks provides the Template Packs service client for the Emergent API SDK.
// Template packs define reusable sets of object and relationship types that can be assigned to projects.
package templatepacks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Template Packs API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	orgID     string
	projectID string
}

// NewClient creates a new template packs client.
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

// --- Types ---

// CompiledTypesResponse contains compiled object and relationship type definitions for a project.
type CompiledTypesResponse struct {
	ObjectTypes       []ObjectTypeSchema       `json:"objectTypes"`
	RelationshipTypes []RelationshipTypeSchema `json:"relationshipTypes"`
}

// ObjectTypeSchema represents an object type definition from a template pack.
type ObjectTypeSchema struct {
	Name        string          `json:"name"`
	Label       string          `json:"label,omitempty"`
	Description string          `json:"description,omitempty"`
	Properties  json.RawMessage `json:"properties,omitempty"`
	PackID      string          `json:"packId,omitempty"`
	PackName    string          `json:"packName,omitempty"`
}

// RelationshipTypeSchema represents a relationship type definition from a template pack.
type RelationshipTypeSchema struct {
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	SourceType  string `json:"sourceType,omitempty"`
	TargetType  string `json:"targetType,omitempty"`
	PackID      string `json:"packId,omitempty"`
	PackName    string `json:"packName,omitempty"`
}

// TemplatePackListItem is a simplified template pack for listing.
type TemplatePackListItem struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Version     string  `json:"version"`
	Description *string `json:"description,omitempty"`
	Author      *string `json:"author,omitempty"`
}

// InstalledPackItem represents a template pack installed on a project.
type InstalledPackItem struct {
	ID             string                 `json:"id"` // assignment ID
	TemplatePackID string                 `json:"templatePackId"`
	Name           string                 `json:"name"`
	Version        string                 `json:"version"`
	Description    *string                `json:"description,omitempty"`
	Active         bool                   `json:"active"`
	InstalledAt    time.Time              `json:"installedAt"`
	Customizations map[string]interface{} `json:"customizations,omitempty"`
}

// ProjectTemplatePack represents a project's assignment of a template pack.
type ProjectTemplatePack struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"projectId"`
	TemplatePackID string    `json:"templatePackId"`
	Active         bool      `json:"active"`
	InstalledAt    time.Time `json:"installedAt"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// AssignPackRequest is the request to assign a template pack to a project.
type AssignPackRequest struct {
	TemplatePackID string                 `json:"template_pack_id"`
	Customizations map[string]interface{} `json:"customizations,omitempty"`
}

// UpdateAssignmentRequest is the request to update a pack assignment.
type UpdateAssignmentRequest struct {
	Active *bool `json:"active,omitempty"`
}

// UpdateAssignmentResponse is the response from updating an assignment.
type UpdateAssignmentResponse struct {
	Status string `json:"status"`
}

// --- Methods ---

// basePath returns the base path for template pack API calls with a project ID.
func (c *Client) basePath(projectID string) string {
	return c.base + "/api/template-packs/projects/" + projectID
}

// GetCompiledTypes returns compiled object and relationship type definitions for a project.
// GET /api/template-packs/projects/:projectId/compiled-types
func (c *Client) GetCompiledTypes(ctx context.Context, projectID string) (*CompiledTypesResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.basePath(projectID)+"/compiled-types", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("X-Org-ID", c.orgID)
	httpReq.Header.Set("X-Project-ID", c.projectID)

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

	var result CompiledTypesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// GetAvailablePacks returns template packs available for a project to install.
// GET /api/template-packs/projects/:projectId/available
func (c *Client) GetAvailablePacks(ctx context.Context, projectID string) ([]TemplatePackListItem, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.basePath(projectID)+"/available", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("X-Org-ID", c.orgID)
	httpReq.Header.Set("X-Project-ID", c.projectID)

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

	var result []TemplatePackListItem
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result, nil
}

// GetInstalledPacks returns template packs currently installed on a project.
// GET /api/template-packs/projects/:projectId/installed
func (c *Client) GetInstalledPacks(ctx context.Context, projectID string) ([]InstalledPackItem, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.basePath(projectID)+"/installed", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("X-Org-ID", c.orgID)
	httpReq.Header.Set("X-Project-ID", c.projectID)

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

	var result []InstalledPackItem
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result, nil
}

// AssignPack assigns a template pack to a project.
// POST /api/template-packs/projects/:projectId/assign
func (c *Client) AssignPack(ctx context.Context, projectID string, req *AssignPackRequest) (*ProjectTemplatePack, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.basePath(projectID)+"/assign", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Org-ID", c.orgID)
	httpReq.Header.Set("X-Project-ID", c.projectID)

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

	var result ProjectTemplatePack
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// UpdateAssignment updates a template pack assignment (e.g., toggle active status).
// PATCH /api/template-packs/projects/:projectId/assignments/:assignmentId
func (c *Client) UpdateAssignment(ctx context.Context, projectID, assignmentID string, req *UpdateAssignmentRequest) (*UpdateAssignmentResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.basePath(projectID)+"/assignments/"+assignmentID, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Org-ID", c.orgID)
	httpReq.Header.Set("X-Project-ID", c.projectID)

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

	var result UpdateAssignmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// DeleteAssignment removes a template pack assignment from a project.
// DELETE /api/template-packs/projects/:projectId/assignments/:assignmentId
func (c *Client) DeleteAssignment(ctx context.Context, projectID, assignmentID string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.basePath(projectID)+"/assignments/"+assignmentID, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("X-Org-ID", c.orgID)
	httpReq.Header.Set("X-Project-ID", c.projectID)

	if err := c.auth.Authenticate(httpReq); err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	// Drain response body
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
