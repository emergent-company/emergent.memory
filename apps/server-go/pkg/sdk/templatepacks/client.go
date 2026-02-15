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
	"net/url"
	"sync"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Template Packs API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
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
	c.mu.Lock()
	defer c.mu.Unlock()
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

// TemplatePack is the full template pack representation returned from create/get endpoints.
type TemplatePack struct {
	ID                      string          `json:"id"`
	Name                    string          `json:"name"`
	Version                 string          `json:"version"`
	Description             *string         `json:"description,omitempty"`
	Author                  *string         `json:"author,omitempty"`
	Source                  *string         `json:"source,omitempty"`
	License                 *string         `json:"license,omitempty"`
	RepositoryURL           *string         `json:"repositoryUrl,omitempty"`
	DocumentationURL        *string         `json:"documentationUrl,omitempty"`
	ObjectTypeSchemas       json.RawMessage `json:"objectTypeSchemas,omitempty"`
	RelationshipTypeSchemas json.RawMessage `json:"relationshipTypeSchemas,omitempty"`
	UIConfigs               json.RawMessage `json:"uiConfigs,omitempty"`
	ExtractionPrompts       json.RawMessage `json:"extractionPrompts,omitempty"`
	Checksum                *string         `json:"checksum,omitempty"`
	Draft                   bool            `json:"draft"`
	PublishedAt             *time.Time      `json:"publishedAt,omitempty"`
	DeprecatedAt            *time.Time      `json:"deprecatedAt,omitempty"`
	CreatedAt               time.Time       `json:"createdAt"`
	UpdatedAt               time.Time       `json:"updatedAt"`
}

// TemplatePackListItem is a simplified template pack for listing.
type TemplatePackListItem struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Version     string  `json:"version"`
	Description *string `json:"description,omitempty"`
	Author      *string `json:"author,omitempty"`
}

// CreatePackRequest is the request to create a new template pack.
type CreatePackRequest struct {
	Name                    string          `json:"name"`
	Version                 string          `json:"version"`
	Description             *string         `json:"description,omitempty"`
	Author                  *string         `json:"author,omitempty"`
	License                 *string         `json:"license,omitempty"`
	RepositoryURL           *string         `json:"repository_url,omitempty"`
	DocumentationURL        *string         `json:"documentation_url,omitempty"`
	ObjectTypeSchemas       json.RawMessage `json:"object_type_schemas"`
	RelationshipTypeSchemas json.RawMessage `json:"relationship_type_schemas,omitempty"`
	UIConfigs               json.RawMessage `json:"ui_configs,omitempty"`
	ExtractionPrompts       json.RawMessage `json:"extraction_prompts,omitempty"`
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

// --- Internal helpers ---

// prepareRequest creates an HTTP request with auth and context headers set.
func (c *Client) prepareRequest(ctx context.Context, method, reqURL string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if err := c.auth.Authenticate(req); err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
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

	return req, nil
}

// doJSON executes a request, checks for errors, and decodes JSON response.
func (c *Client) doJSON(req *http.Request, result any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}

	return nil
}

// getJSON performs a GET request and decodes the JSON response.
func (c *Client) getJSON(ctx context.Context, reqURL string, result any) error {
	req, err := c.prepareRequest(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	return c.doJSON(req, result)
}

// postJSON performs a POST request with JSON body and decodes the response.
func (c *Client) postJSON(ctx context.Context, reqURL string, reqBody any, result any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := c.prepareRequest(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doJSON(req, result)
}

// patchJSON performs a PATCH request with JSON body and decodes the response.
func (c *Client) patchJSON(ctx context.Context, reqURL string, reqBody any, result any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := c.prepareRequest(ctx, http.MethodPatch, reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doJSON(req, result)
}

// doDelete performs a DELETE request.
func (c *Client) doDelete(ctx context.Context, reqURL string) error {
	req, err := c.prepareRequest(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return err
	}
	return c.doJSON(req, nil)
}

// projectPath returns the base path for project-scoped template pack API calls.
// Uses the stored projectID from SetContext().
func (c *Client) projectPath() string {
	c.mu.RLock()
	pid := c.projectID
	c.mu.RUnlock()
	return c.base + "/api/template-packs/projects/" + url.PathEscape(pid)
}

// packPath returns the base path for global (non-project-scoped) template pack operations.
func (c *Client) packPath() string {
	return c.base + "/api/template-packs"
}

// --- Project-scoped methods ---

// GetCompiledTypes returns compiled object and relationship type definitions for the current project.
// GET /api/template-packs/projects/:projectId/compiled-types
func (c *Client) GetCompiledTypes(ctx context.Context) (*CompiledTypesResponse, error) {
	var result CompiledTypesResponse
	if err := c.getJSON(ctx, c.projectPath()+"/compiled-types", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetAvailablePacks returns template packs available for the current project to install.
// GET /api/template-packs/projects/:projectId/available
func (c *Client) GetAvailablePacks(ctx context.Context) ([]TemplatePackListItem, error) {
	var result []TemplatePackListItem
	if err := c.getJSON(ctx, c.projectPath()+"/available", &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetInstalledPacks returns template packs currently installed on the current project.
// GET /api/template-packs/projects/:projectId/installed
func (c *Client) GetInstalledPacks(ctx context.Context) ([]InstalledPackItem, error) {
	var result []InstalledPackItem
	if err := c.getJSON(ctx, c.projectPath()+"/installed", &result); err != nil {
		return nil, err
	}
	return result, nil
}

// AssignPack assigns a template pack to the current project.
// POST /api/template-packs/projects/:projectId/assign
func (c *Client) AssignPack(ctx context.Context, req *AssignPackRequest) (*ProjectTemplatePack, error) {
	var result ProjectTemplatePack
	if err := c.postJSON(ctx, c.projectPath()+"/assign", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateAssignment updates a template pack assignment (e.g., toggle active status).
// PATCH /api/template-packs/projects/:projectId/assignments/:assignmentId
func (c *Client) UpdateAssignment(ctx context.Context, assignmentID string, req *UpdateAssignmentRequest) (*UpdateAssignmentResponse, error) {
	var result UpdateAssignmentResponse
	if err := c.patchJSON(ctx, c.projectPath()+"/assignments/"+url.PathEscape(assignmentID), req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteAssignment removes a template pack assignment from the current project.
// DELETE /api/template-packs/projects/:projectId/assignments/:assignmentId
func (c *Client) DeleteAssignment(ctx context.Context, assignmentID string) error {
	return c.doDelete(ctx, c.projectPath()+"/assignments/"+url.PathEscape(assignmentID))
}

// --- Global Template Pack CRUD ---

// CreatePack creates a new template pack.
// POST /api/template-packs
func (c *Client) CreatePack(ctx context.Context, req *CreatePackRequest) (*TemplatePack, error) {
	var result TemplatePack
	if err := c.postJSON(ctx, c.packPath(), req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetPack retrieves a template pack by ID.
// GET /api/template-packs/:packId
func (c *Client) GetPack(ctx context.Context, packID string) (*TemplatePack, error) {
	var result TemplatePack
	if err := c.getJSON(ctx, c.packPath()+"/"+url.PathEscape(packID), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeletePack deletes a template pack by ID. Fails if the pack is assigned to any projects.
// DELETE /api/template-packs/:packId
func (c *Client) DeletePack(ctx context.Context, packID string) error {
	return c.doDelete(ctx, c.packPath()+"/"+url.PathEscape(packID))
}
