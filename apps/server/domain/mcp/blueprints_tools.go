package mcp

import (
	"context"
)

// BlueprintToolHandler is the interface for executing blueprint-related MCP
// tools. Implemented by the blueprints domain to avoid circular imports
// (blueprints → mcp). Injected into the mcp Service via SetBlueprintToolHandler.
type BlueprintToolHandler interface {
	ExecuteBlueprintCreate(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintList(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintGet(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintPublish(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintApply(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintUnapply(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintVersions(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintListApplied(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintNewVersion(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintUpdate(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
}

// ============================================================================
// Blueprint Tool Definitions
// ============================================================================

func blueprintsToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:          "blueprint-create",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:write",
			Description:   "Create a new blueprint draft, scoped to the caller's project (private). Returns the created blueprint with its id, name, version, and manifest. Publish it with blueprint-publish before applying it to a project. Global built-ins are readable and apply-able but immutable — fork one with blueprint-new-version to change it.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"name": {
						Type:        "string",
						Description: "Blueprint name (e.g. 'team-onboarding')",
					},
					"version": {
						Type:        "string",
						Description: "Blueprint version (e.g. '1.0.0')",
					},
					"description": {
						Type:        "string",
						Description: "Human-readable description of the blueprint",
					},
					"author": {
						Type:        "string",
						Description: "Author of the blueprint",
					},
					"manifest": {
						Type:        "object",
						Description: "Blueprint manifest as a JSON object or a JSON-encoded string. Describes schema packs, agents, skills, and seed graph objects/relationships to materialize on apply.",
					},
				},
				Required: []string{"name", "version"},
			},
		},
		{
			Name:          "blueprint-list",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:read",
			Description:   "List blueprints visible to the caller: global built-ins plus the caller's own private blueprints. Optionally filtered by name. Returns id, name, version, status, scope, and checksum for each blueprint.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"name": {
						Type:        "string",
						Description: "Optional filter — only return blueprints with this name",
					},
				},
				Required: []string{},
			},
		},
		{
			Name:          "blueprint-get",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:read",
			Description:   "Get a single blueprint by id within the caller's read scope (global built-ins plus the caller's own private blueprints). Returns the full blueprint including its manifest.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"id": {
						Type:        "string",
						Description: "UUID of the blueprint to retrieve",
					},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:          "blueprint-publish",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:write",
			Description:   "Publish the caller's own private draft blueprint, computing a sha256 checksum over its manifest. Global built-ins are immutable — fork a version to change one. Returns the published blueprint.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"id": {
						Type:        "string",
						Description: "UUID of the draft blueprint to publish",
					},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:          "blueprint-apply",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:write",
			Description:   "Apply a blueprint to the current project, materializing its manifest: schema packs (create-or-skip, always assigned), agent definitions (create-or-update by name), global skills (create-or-update by name), and seed graph objects/relationships. Idempotent — re-applying converges. Returns per-type created/updated/skipped counts.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"id": {
						Type:        "string",
						Description: "UUID of the blueprint to apply",
					},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:          "blueprint-unapply",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:write",
			Description:   "Reverse a previous blueprint apply for the current project: removes agent definitions owned by the blueprint and pack assignments owned by it, leaving the global pack. Skills and seed graph objects are skipped (shared/global scope). Idempotent. Returns per-type removal counts.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"id": {
						Type:        "string",
						Description: "UUID of the blueprint to unapply",
					},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:          "blueprint-versions",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:read",
			Description:   "List all versions of a blueprint name visible to the caller (global built-ins plus the caller's own private blueprints), newest first. Returns the full blueprint for each version.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"name": {
						Type:        "string",
						Description: "Blueprint name to list versions for",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:          "blueprint-list-applied",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:read",
			Description:   "List the blueprints currently applied to the current project. Returns blueprint id, name, version, and applied-at timestamp for each.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:          "blueprint-new-version",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:write",
			Description:   "Clone an existing blueprint into a new version as a draft. The source may be a global built-in or your own blueprint; the clone is created as a private draft in your project. Publish it (blueprint-publish) before applying.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"id": {
						Type:        "string",
						Description: "UUID of the blueprint to clone from",
					},
					"version": {
						Type:        "string",
						Description: "New semantic version, e.g. '1.1.0'",
					},
				},
				Required: []string{"id", "version"},
			},
		},
		{
			Name:          "blueprint-update",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:write",
			Description:   "Update a draft blueprint's description, author, and/or manifest. Only your own private drafts can be updated — global blueprints are immutable; fork one with blueprint-new-version to change it.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"id": {
						Type:        "string",
						Description: "UUID of the draft blueprint to update",
					},
					"description": {
						Type:        "string",
						Description: "New description (optional)",
					},
					"author": {
						Type:        "string",
						Description: "New author (optional)",
					},
					"manifest": {
						Type:        "object",
						Description: "New manifest as a JSON object or a JSON-encoded string (optional). Describes schema packs, agents, skills, and seed graph objects/relationships to materialize on apply.",
					},
				},
				Required: []string{"id"},
			},
		},
	}
}
