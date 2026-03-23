// Package schemas provides the Memory Schema service client for the Emergent API SDK.
// Memory Schema define reusable sets of object and relationship types that can be assigned to projects.
package schemas

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

// Client provides access to the Memory Schema API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// NewClient creates a new memory schemas client.
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

// ObjectTypeSchema represents an object type definition from a memory schema.
type ObjectTypeSchema struct {
	Name          string          `json:"name"`
	Label         string          `json:"label,omitempty"`
	Description   string          `json:"description,omitempty"`
	Properties    json.RawMessage `json:"properties,omitempty"`
	SchemaID      string          `json:"schemaId,omitempty"`
	SchemaName    string          `json:"schemaName,omitempty"`
	SchemaVersion string          `json:"schemaVersion,omitempty"`
	Shadowed      bool            `json:"shadowed,omitempty"`
}

// RelationshipTypeSchema represents a relationship type definition from a memory schema.
type RelationshipTypeSchema struct {
	Name          string `json:"name"`
	Label         string `json:"label,omitempty"`
	Description   string `json:"description,omitempty"`
	SourceType    string `json:"sourceType,omitempty"`
	TargetType    string `json:"targetType,omitempty"`
	SchemaID      string `json:"schemaId,omitempty"`
	SchemaName    string `json:"schemaName,omitempty"`
	SchemaVersion string `json:"schemaVersion,omitempty"`
	Shadowed      bool   `json:"shadowed,omitempty"`
}

// SchemaHistoryItem represents one assignment record including soft-deleted ones.
type SchemaHistoryItem struct {
	ID          string     `json:"id"`
	SchemaID    string     `json:"schemaId"`
	Name        string     `json:"name"`
	SchemaName  string     `json:"schemaName"`
	Version     string     `json:"version"`
	Active      bool       `json:"active"`
	InstalledAt time.Time  `json:"installedAt"`
	RemovedAt   *time.Time `json:"removedAt,omitempty"`
}

// TypeRename describes a single object or edge type rename.
type TypeRename struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// PropertyRename describes a property key rename scoped to a type.
type PropertyRename struct {
	TypeName string `json:"type_name"`
	From     string `json:"from"`
	To       string `json:"to"`
}

// MigrateRequest is the body for POST /api/schemas/projects/:projectId/migrate.
type MigrateRequest struct {
	TypeRenames     []TypeRename     `json:"type_renames,omitempty"`
	PropertyRenames []PropertyRename `json:"property_renames,omitempty"`
	DryRun          bool             `json:"dry_run,omitempty"`
}

// TypeRenameResult reports counts for one type rename operation.
type TypeRenameResult struct {
	From            string `json:"from"`
	To              string `json:"to"`
	ObjectsAffected int    `json:"objects_affected"`
	EdgesAffected   int    `json:"edges_affected"`
}

// PropertyRenameResult reports counts for one property rename operation.
type PropertyRenameResult struct {
	TypeName        string `json:"type_name"`
	From            string `json:"from"`
	To              string `json:"to"`
	ObjectsAffected int    `json:"objects_affected"`
}

// MigrateResponse is the result from the migrate endpoint.
type MigrateResponse struct {
	DryRun                bool                   `json:"dry_run"`
	TypeRenameResults     []TypeRenameResult     `json:"type_rename_results,omitempty"`
	PropertyRenameResults []PropertyRenameResult `json:"property_rename_results,omitempty"`
}

// MemorySchema is the full memory schema representation returned from create/get endpoints.
type MemorySchema struct {
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
	Visibility              string          `json:"visibility,omitempty"`
	PublishedAt             *time.Time      `json:"publishedAt,omitempty"`
	DeprecatedAt            *time.Time      `json:"deprecatedAt,omitempty"`
	CreatedAt               time.Time       `json:"createdAt"`
	UpdatedAt               time.Time       `json:"updatedAt"`
}

// MemorySchemaListItem is a simplified memory schema for listing.
type MemorySchemaListItem struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Version     string  `json:"version"`
	Description *string `json:"description,omitempty"`
	Author      *string `json:"author,omitempty"`
	Visibility  string  `json:"visibility,omitempty"`
}

// UnifiedSchemaItem represents a schema in the unified list, showing both installed and available schemas.
type UnifiedSchemaItem struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Version         string     `json:"version"`
	Description     *string    `json:"description,omitempty"`
	Author          *string    `json:"author,omitempty"`
	Visibility      string     `json:"visibility,omitempty"`
	Installed       bool       `json:"installed"`
	InstalledAt     *time.Time `json:"installedAt,omitempty"`
	AssignmentID    *string    `json:"assignmentId,omitempty"`
	BlueprintSource *string    `json:"blueprintSource,omitempty"`
}

// CreatePackRequest is the request to create a new memory schema.
// Both snake_case and camelCase field names are accepted when unmarshalling
// from a user-supplied JSON file, so that files written with either convention
// work without manual conversion.
type CreatePackRequest struct {
	Name             string  `json:"name"`
	Version          string  `json:"version"`
	Description      *string `json:"description,omitempty"`
	Author           *string `json:"author,omitempty"`
	License          *string `json:"license,omitempty"`
	RepositoryURL    *string `json:"repository_url,omitempty"`
	DocumentationURL *string `json:"documentation_url,omitempty"`
	Visibility       string  `json:"visibility,omitempty"`

	// ObjectTypeSchemas accepts both snake_case and camelCase keys in input files.
	ObjectTypeSchemasSnake json.RawMessage `json:"object_type_schemas"`
	ObjectTypeSchemasCamel json.RawMessage `json:"objectTypeSchemas"`

	// RelationshipTypeSchemas accepts both snake_case and camelCase keys.
	RelationshipTypeSchemasSnake json.RawMessage `json:"relationship_type_schemas,omitempty"`
	RelationshipTypeSchemasCamel json.RawMessage `json:"relationshipTypeSchemas,omitempty"`

	// UIConfigs accepts both snake_case and camelCase keys.
	UIConfigsSnake json.RawMessage `json:"ui_configs,omitempty"`
	UIConfigsCamel json.RawMessage `json:"uiConfigs,omitempty"`

	// ExtractionPrompts accepts both snake_case and camelCase keys.
	ExtractionPromptsSnake json.RawMessage `json:"extraction_prompts,omitempty"`
	ExtractionPromptsCamel json.RawMessage `json:"extractionPrompts,omitempty"`

	// Migrations carries optional schema migration hints (snake_case or YAML).
	Migrations *SchemaMigrationHints `json:"migrations,omitempty" yaml:"migrations,omitempty"`

	// BlueprintSource records the file path or URL from which this schema was installed.
	// Set by the blueprint applier; nil for schemas created directly via API.
	BlueprintSource *string `json:"blueprint_source,omitempty"`
}

// MarshalJSON serialises the request for transmission to the server,
// normalising both naming variants into the canonical snake_case fields
// that the server API expects.
func (r CreatePackRequest) MarshalJSON() ([]byte, error) {
	type wire struct {
		Name                    string                `json:"name"`
		Version                 string                `json:"version"`
		Description             *string               `json:"description,omitempty"`
		Author                  *string               `json:"author,omitempty"`
		License                 *string               `json:"license,omitempty"`
		RepositoryURL           *string               `json:"repository_url,omitempty"`
		DocumentationURL        *string               `json:"documentation_url,omitempty"`
		Visibility              string                `json:"visibility,omitempty"`
		ObjectTypeSchemas       json.RawMessage       `json:"object_type_schemas"`
		RelationshipTypeSchemas json.RawMessage       `json:"relationship_type_schemas,omitempty"`
		UIConfigs               json.RawMessage       `json:"ui_configs,omitempty"`
		ExtractionPrompts       json.RawMessage       `json:"extraction_prompts,omitempty"`
		Migrations              *SchemaMigrationHints `json:"migrations,omitempty"`
		BlueprintSource         *string               `json:"blueprint_source,omitempty"`
	}
	w := wire{
		Name:             r.Name,
		Version:          r.Version,
		Description:      r.Description,
		Author:           r.Author,
		License:          r.License,
		RepositoryURL:    r.RepositoryURL,
		DocumentationURL: r.DocumentationURL,
		Visibility:       r.Visibility,
		Migrations:       r.Migrations,
		BlueprintSource:  r.BlueprintSource,
	}
	if len(r.ObjectTypeSchemasSnake) > 0 {
		w.ObjectTypeSchemas = r.ObjectTypeSchemasSnake
	} else {
		w.ObjectTypeSchemas = r.ObjectTypeSchemasCamel
	}
	if len(r.RelationshipTypeSchemasSnake) > 0 {
		w.RelationshipTypeSchemas = r.RelationshipTypeSchemasSnake
	} else {
		w.RelationshipTypeSchemas = r.RelationshipTypeSchemasCamel
	}
	if len(r.UIConfigsSnake) > 0 {
		w.UIConfigs = r.UIConfigsSnake
	} else {
		w.UIConfigs = r.UIConfigsCamel
	}
	if len(r.ExtractionPromptsSnake) > 0 {
		w.ExtractionPrompts = r.ExtractionPromptsSnake
	} else {
		w.ExtractionPrompts = r.ExtractionPromptsCamel
	}
	return json.Marshal(w)
}

// InstalledSchemaItem represents a memory schema installed on a project.
type InstalledSchemaItem struct {
	ID             string                 `json:"id"` // assignment ID
	SchemaID       string                 `json:"schemaId"`
	Name           string                 `json:"name"`
	Version        string                 `json:"version"`
	Description    *string                `json:"description,omitempty"`
	Active         bool                   `json:"active"`
	InstalledAt    time.Time              `json:"installedAt"`
	Customizations map[string]interface{} `json:"customizations,omitempty"`
}

// ProjectMemorySchema represents a project's assignment of a memory schema.
type ProjectMemorySchema struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	SchemaID    string    `json:"schemaId"`
	Active      bool      `json:"active"`
	InstalledAt time.Time `json:"installedAt"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// AssignPackRequest is the request to assign a memory schema to a project.
type AssignPackRequest struct {
	SchemaID       string                 `json:"schema_id"`
	Customizations map[string]interface{} `json:"customizations,omitempty"`
	// DryRun requests a preview without making any database changes.
	DryRun bool `json:"dry_run,omitempty"`
	// Merge requests additive schema merging for conflicting type names.
	Merge bool `json:"merge,omitempty"`
	// Force bypasses the dangerous-risk block when auto-migration is triggered.
	Force bool `json:"force,omitempty"`
	// AutoUninstall uninstalls the from-version schema after a successful chain migration.
	AutoUninstall bool `json:"auto_uninstall,omitempty"`
}

// PropertyConflict describes a single property-level conflict during a schema merge.
type PropertyConflict struct {
	Property    string          `json:"property"`
	ExistingDef json.RawMessage `json:"existing_def"`
	IncomingDef json.RawMessage `json:"incoming_def"`
	Resolution  string          `json:"resolution"` // "existing_wins"
}

// SchemaConflict describes a type-level conflict when assigning a pack whose
// type names overlap with types already registered in the project.
type SchemaConflict struct {
	TypeName              string             `json:"type_name"`
	ExistingSchema        json.RawMessage    `json:"existing_schema"`
	IncomingSchema        json.RawMessage    `json:"incoming_schema"`
	MergedSchema          json.RawMessage    `json:"merged_schema,omitempty"`
	AddedProperties       []string           `json:"added_properties,omitempty"`
	ConflictingProperties []PropertyConflict `json:"conflicting_properties,omitempty"`
}

// AssignPackResult is the response from the assign endpoint.
// Replaces the bare ProjectMemorySchema return so callers get conflict details.
type AssignPackResult struct {
	DryRun           bool             `json:"dry_run"`
	AssignmentID     string           `json:"assignment_id,omitempty"`
	SchemaID         string           `json:"schema_id"`
	SchemaName       string           `json:"schema_name"`
	InstalledTypes   []string         `json:"installed_types"`
	SkippedTypes     []string         `json:"skipped_types,omitempty"`
	MergedTypes      []string         `json:"merged_types,omitempty"`
	Conflicts        []SchemaConflict `json:"conflicts,omitempty"`
	AlreadyInstalled bool             `json:"already_installed,omitempty"`
	// Migration fields — populated when the schema has a migrations block
	MigrationJobID       *string `json:"migration_job_id,omitempty"`
	MigrationStatus      string  `json:"migration_status,omitempty"`
	MigrationBlockReason string  `json:"migration_block_reason,omitempty"`
}

// UpdateAssignmentRequest is the request to update a pack assignment.
type UpdateAssignmentRequest struct {
	Active *bool `json:"active,omitempty"`
}

// UpdateAssignmentResponse is the response from updating an assignment.
type UpdateAssignmentResponse struct {
	Status string `json:"status"`
}

// UpdatePackRequest is the request to partially update a memory schema.
// All fields are optional; only non-nil / non-empty values are applied.
type UpdatePackRequest struct {
	Name                    *string         `json:"name,omitempty"`
	Version                 *string         `json:"version,omitempty"`
	Description             *string         `json:"description,omitempty"`
	Author                  *string         `json:"author,omitempty"`
	License                 *string         `json:"license,omitempty"`
	RepositoryURL           *string         `json:"repository_url,omitempty"`
	DocumentationURL        *string         `json:"documentation_url,omitempty"`
	ObjectTypeSchemas       json.RawMessage `json:"object_type_schemas,omitempty"`
	RelationshipTypeSchemas json.RawMessage `json:"relationship_type_schemas,omitempty"`
	UIConfigs               json.RawMessage `json:"ui_configs,omitempty"`
	ExtractionPrompts       json.RawMessage `json:"extraction_prompts,omitempty"`
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

// putJSON performs a PUT request with JSON body and decodes the response.
func (c *Client) putJSON(ctx context.Context, reqURL string, reqBody any, result any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := c.prepareRequest(ctx, http.MethodPut, reqURL, bytes.NewReader(body))
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

// projectPath returns the base path for project-scoped memory schema API calls.
// Uses the stored projectID from SetContext().
func (c *Client) projectPath() string {
	c.mu.RLock()
	pid := c.projectID
	c.mu.RUnlock()
	return c.base + "/api/schemas/projects/" + url.PathEscape(pid)
}

// packPath returns the base path for global (non-project-scoped) memory schema operations.
func (c *Client) packPath() string {
	return c.base + "/api/schemas"
}

// --- Project-scoped methods ---

// GetCompiledTypes returns compiled object and relationship type definitions for the current project.
// GET /api/schemas/projects/:projectId/compiled-types
func (c *Client) GetCompiledTypes(ctx context.Context) (*CompiledTypesResponse, error) {
	var result CompiledTypesResponse
	if err := c.getJSON(ctx, c.projectPath()+"/compiled-types", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetAvailablePacks returns memory schemas available for the current project to install.
// GET /api/schemas/projects/:projectId/available
func (c *Client) GetAvailablePacks(ctx context.Context) ([]MemorySchemaListItem, error) {
	var result []MemorySchemaListItem
	if err := c.getJSON(ctx, c.projectPath()+"/available", &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetInstalledPacks returns memory schemas currently installed on the current project.
// GET /api/schemas/projects/:projectId/installed
func (c *Client) GetInstalledPacks(ctx context.Context) ([]InstalledSchemaItem, error) {
	var result []InstalledSchemaItem
	if err := c.getJSON(ctx, c.projectPath()+"/installed", &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetAllPacks returns all memory schemas visible to the current project — both installed and available.
// GET /api/schemas/projects/:projectId/all
func (c *Client) GetAllPacks(ctx context.Context) ([]UnifiedSchemaItem, error) {
	var result []UnifiedSchemaItem
	if err := c.getJSON(ctx, c.projectPath()+"/all", &result); err != nil {
		return nil, err
	}
	return result, nil
}

// AssignPack assigns a memory schema to the current project.
// When req.DryRun is true, returns a preview (HTTP 200) without changes.
// POST /api/schemas/projects/:projectId/assign
func (c *Client) AssignPack(ctx context.Context, req *AssignPackRequest) (*AssignPackResult, error) {
	var result AssignPackResult
	if err := c.postJSON(ctx, c.projectPath()+"/assign", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateAssignment updates a memory schema assignment (e.g., toggle active status).
// PATCH /api/schemas/projects/:projectId/assignments/:assignmentId
func (c *Client) UpdateAssignment(ctx context.Context, assignmentID string, req *UpdateAssignmentRequest) (*UpdateAssignmentResponse, error) {
	var result UpdateAssignmentResponse
	if err := c.patchJSON(ctx, c.projectPath()+"/assignments/"+url.PathEscape(assignmentID), req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteAssignment removes a memory schema assignment from the current project.
// DELETE /api/schemas/projects/:projectId/assignments/:assignmentId
func (c *Client) DeleteAssignment(ctx context.Context, assignmentID string) error {
	return c.doDelete(ctx, c.projectPath()+"/assignments/"+url.PathEscape(assignmentID))
}

// --- Global Memory Schema CRUD ---

// CreatePack creates a new memory schema.
// POST /api/schemas
func (c *Client) CreatePack(ctx context.Context, req *CreatePackRequest) (*MemorySchema, error) {
	var result MemorySchema
	if err := c.postJSON(ctx, c.packPath(), req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetPack retrieves a memory schema by ID.
// GET /api/schemas/:packId
func (c *Client) GetPack(ctx context.Context, packID string) (*MemorySchema, error) {
	var result MemorySchema
	if err := c.getJSON(ctx, c.packPath()+"/"+url.PathEscape(packID), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeletePack deletes a memory schema by ID. Fails if the pack is assigned to any projects.
// DELETE /api/schemas/:packId
func (c *Client) DeletePack(ctx context.Context, packID string) error {
	return c.doDelete(ctx, c.packPath()+"/"+url.PathEscape(packID))
}

// UpdatePack partially updates a memory schema by ID.
// PUT /api/schemas/:packId
func (c *Client) UpdatePack(ctx context.Context, packID string, req *UpdatePackRequest) (*MemorySchema, error) {
	var result MemorySchema
	if err := c.putJSON(ctx, c.packPath()+"/"+url.PathEscape(packID), req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetPackHistory returns all schema assignment records for the current project,
// including soft-deleted (uninstalled) ones.
// GET /api/schemas/projects/:projectId/history
func (c *Client) GetPackHistory(ctx context.Context) ([]SchemaHistoryItem, error) {
	var result []SchemaHistoryItem
	if err := c.getJSON(ctx, c.projectPath()+"/history", &result); err != nil {
		return nil, err
	}
	return result, nil
}

// MigratePack renames object/edge types and/or property keys across live graph data
// in the current project. When req.DryRun is true, returns a preview without changes.
// POST /api/schemas/projects/:projectId/migrate
func (c *Client) MigratePack(ctx context.Context, req *MigrateRequest) (*MigrateResponse, error) {
	var result MigrateResponse
	if err := c.postJSON(ctx, c.projectPath()+"/migrate", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// System A migration types (schema-version–aware, async)
// ─────────────────────────────────────────────────────────────────────────────

// SchemaMigrationHints describes how to migrate from a previous schema version.
type SchemaMigrationHints struct {
	FromVersion       string            `json:"from_version" yaml:"from_version"`
	TypeRenames       map[string]string `json:"type_renames,omitempty" yaml:"type_renames,omitempty"`
	PropertyRenames   map[string]string `json:"property_renames,omitempty" yaml:"property_renames,omitempty"`
	RemovedProperties []RemovedProperty `json:"removed_properties,omitempty" yaml:"removed_properties,omitempty"`
}

// RemovedProperty identifies a property intentionally removed in this schema version.
type RemovedProperty struct {
	TypeName string `json:"type_name" yaml:"type_name"`
	Name     string `json:"name" yaml:"name"`
}

// SchemaMigrationPreviewRequest is sent to POST .../migrate/preview.
type SchemaMigrationPreviewRequest struct {
	FromSchemaID string `json:"from_schema_id"`
	ToSchemaID   string `json:"to_schema_id"`
}

// TypeMigrationRisk holds per-type risk in a migration preview.
type TypeMigrationRisk struct {
	TypeName      string `json:"type_name"`
	RiskLevel     string `json:"risk_level"`
	ObjectCount   int    `json:"object_count"`
	BlockReason   string `json:"block_reason,omitempty"`
	DroppedFields int    `json:"dropped_fields,omitempty"`
}

// SchemaMigrationPreviewResponse is returned from POST .../migrate/preview.
type SchemaMigrationPreviewResponse struct {
	ProjectID        string              `json:"project_id"`
	FromSchemaID     string              `json:"from_schema_id"`
	ToSchemaID       string              `json:"to_schema_id"`
	TotalObjects     int                 `json:"total_objects"`
	OverallRiskLevel string              `json:"overall_risk_level"`
	TypeBreakdown    []TypeMigrationRisk `json:"type_breakdown,omitempty"`
}

// SchemaMigrationExecuteRequest is sent to POST .../migrate/execute.
type SchemaMigrationExecuteRequest struct {
	FromSchemaID string `json:"from_schema_id"`
	ToSchemaID   string `json:"to_schema_id"`
	Force        bool   `json:"force,omitempty"`
	MaxObjects   int    `json:"max_objects,omitempty"`
}

// SchemaMigrationExecuteResponse is returned from POST .../migrate/execute.
type SchemaMigrationExecuteResponse struct {
	ProjectID       string `json:"project_id"`
	FromSchemaID    string `json:"from_schema_id"`
	ToSchemaID      string `json:"to_schema_id"`
	ObjectsMigrated int    `json:"objects_migrated"`
	ObjectsFailed   int    `json:"objects_failed"`
}

// SchemaMigrationRollbackRequest is sent to POST .../migrate/rollback.
type SchemaMigrationRollbackRequest struct {
	ToVersion           string `json:"to_version"`
	RestoreTypeRegistry bool   `json:"restore_type_registry,omitempty"`
}

// SchemaMigrationRollbackResponse is returned from POST .../migrate/rollback.
type SchemaMigrationRollbackResponse struct {
	ProjectID       string `json:"project_id"`
	ToVersion       string `json:"to_version"`
	ObjectsRestored int    `json:"objects_restored"`
}

// CommitMigrationArchiveRequest is sent to POST .../migrate/commit.
type CommitMigrationArchiveRequest struct {
	ThroughVersion string `json:"through_version"`
}

// CommitMigrationArchiveResponse is returned from POST .../migrate/commit.
type CommitMigrationArchiveResponse struct {
	ProjectID      string `json:"project_id"`
	ThroughVersion string `json:"through_version"`
	ObjectsUpdated int    `json:"objects_updated"`
}

// SchemaMigrationJob is returned from GET .../migration-jobs/:jobId.
type SchemaMigrationJob struct {
	ID              string  `json:"id"`
	ProjectID       string  `json:"project_id"`
	FromSchemaID    string  `json:"from_schema_id"`
	ToSchemaID      string  `json:"to_schema_id"`
	Status          string  `json:"status"`
	RiskLevel       string  `json:"risk_level,omitempty"`
	ObjectsMigrated int     `json:"objects_migrated"`
	ObjectsFailed   int     `json:"objects_failed"`
	Error           *string `json:"error,omitempty"`
	CreatedAt       string  `json:"created_at"`
	StartedAt       *string `json:"started_at,omitempty"`
	CompletedAt     *string `json:"completed_at,omitempty"`
}

// PreviewMigration previews a schema migration without making changes.
// POST /api/schemas/projects/:projectId/migrate/preview
func (c *Client) PreviewMigration(ctx context.Context, req *SchemaMigrationPreviewRequest) (*SchemaMigrationPreviewResponse, error) {
	var result SchemaMigrationPreviewResponse
	if err := c.postJSON(ctx, c.projectPath()+"/migrate/preview", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ExecuteMigration executes a schema migration.
// POST /api/schemas/projects/:projectId/migrate/execute
func (c *Client) ExecuteMigration(ctx context.Context, req *SchemaMigrationExecuteRequest) (*SchemaMigrationExecuteResponse, error) {
	var result SchemaMigrationExecuteResponse
	if err := c.postJSON(ctx, c.projectPath()+"/migrate/execute", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RollbackMigration rolls back a schema migration.
// POST /api/schemas/projects/:projectId/migrate/rollback
func (c *Client) RollbackMigration(ctx context.Context, req *SchemaMigrationRollbackRequest) (*SchemaMigrationRollbackResponse, error) {
	var result SchemaMigrationRollbackResponse
	if err := c.postJSON(ctx, c.projectPath()+"/migrate/rollback", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CommitMigrationArchive commits (prunes) migration archive data.
// POST /api/schemas/projects/:projectId/migrate/commit
func (c *Client) CommitMigrationArchive(ctx context.Context, req *CommitMigrationArchiveRequest) (*CommitMigrationArchiveResponse, error) {
	var result CommitMigrationArchiveResponse
	if err := c.postJSON(ctx, c.projectPath()+"/migrate/commit", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetMigrationJobStatus returns the status of an async migration job.
// GET /api/schemas/projects/:projectId/migration-jobs/:jobId
func (c *Client) GetMigrationJobStatus(ctx context.Context, jobID string) (*SchemaMigrationJob, error) {
	var result SchemaMigrationJob
	if err := c.getJSON(ctx, c.projectPath()+"/migration-jobs/"+url.PathEscape(jobID), &result); err != nil {
		return nil, err
	}
	return &result, nil
}
