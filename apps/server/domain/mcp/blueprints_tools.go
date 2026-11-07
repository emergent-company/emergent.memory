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
	ExecuteBlueprintApplyPreview(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintUnapply(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintVersions(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintListApplied(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintNewVersion(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintUpdate(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintUpdatesStatus(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintImportPreview(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintModelOverridesGet(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintDelete(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintDeprecate(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintImport(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintUpdatesCheck(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintUpdatesApply(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintModelOverridesPut(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteBlueprintGithubCredentialsPut(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
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
			Description:   "Apply a blueprint to the current project, materializing its manifest: schema packs (create-or-skip, always assigned), agent definitions (create-or-update by name), global skills (create-or-update by name), and seed graph objects/relationships. Idempotent — re-applying converges. Blocks with a conflict when an incoming pack declares an object type name already registered in the project; call blueprint-apply-preview first and retry with mergeTypes=true to confirm the merge. Returns per-type created/updated/skipped counts and accepted typeConflicts.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"id": {
						Type:        "string",
						Description: "UUID of the blueprint to apply",
					},
					"mergeTypes": {
						Type:        "boolean",
						Description: "Allow apply to merge incoming object types into existing registered types. Defaults to false, in which case a type-name collision blocks apply.",
					},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:          "blueprint-apply-preview",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:read",
			Description:   "Preview applying a blueprint to the current project WITHOUT materializing anything. Returns requiresMerge plus the object-type name collisions and their property-level diff detail. Use the result to decide whether to call blueprint-apply with mergeTypes=true.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"id": {
						Type:        "string",
						Description: "UUID of the blueprint to preview",
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
		{
			Name:          "blueprint-updates-status",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:read",
			Description:   "Report the latest upstream-update status for an applied blueprint by name: the last check result plus the latest check/apply job, when one exists. This is a read — it does not contact GitHub and does not enqueue a check or apply an update.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"name": {
						Type:        "string",
						Description: "Blueprint name to report update status for",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:          "blueprint-import-preview",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:read",
			Description:   "Fetch and validate a blueprint from https://github.com/<org>/<repo>[#ref] and return its metadata and manifest WITHOUT creating, publishing, or applying anything. The optional token is used only for a private-repo fetch and is never stored or returned. In project scope the preview also reports the object-type collisions an apply would hit.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"url": {
						Type:        "string",
						Description: "GitHub repository URL, optionally with a #ref suffix",
					},
					"ref": {
						Type:        "string",
						Description: "Optional git ref (branch, tag, or commit) to fetch",
					},
					"token": {
						Type:        "string",
						Description: "Optional GitHub token for a private repository; used only for this fetch and never stored",
					},
					"modelOverrides": {
						Type:        "object",
						Description: "Optional agent-name → provider/model overrides applied to the previewed manifest in memory",
					},
				},
				Required: []string{"url"},
			},
		},
		{
			Name:          "blueprint-model-overrides-get",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:read",
			Description:   "Get the project-scoped model overrides stored for a blueprint name (agent-name → provider/model map). Read-only; use blueprint-model-overrides-put to change them.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"name": {
						Type:        "string",
						Description: "Blueprint name whose stored model overrides to return",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:          "blueprint-delete",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:write",
			Description:   "Remove a project-private blueprint from the registry. Refuses to delete global built-ins and any blueprint with a live (non-unapplied) application for the project — uninstall it first with blueprint-unapply. On success clears the blueprint's pack claims and removes any project-owned schema packs it orphaned.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"id": {
						Type:        "string",
						Description: "UUID of the blueprint to delete",
					},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:          "blueprint-deprecate",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:write",
			Description:   "Mark a blueprint version deprecated. This is a lifecycle transition, not a content mutation, so it is allowed on global built-ins and private blueprints alike. Idempotent — deprecating an already-deprecated version is a no-op. Returns the deprecated blueprint.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"id": {
						Type:        "string",
						Description: "UUID of the blueprint version to deprecate",
					},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:          "blueprint-import",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:write",
			Description:   "Import a blueprint from https://github.com/<org>/<repo>[#ref], creating and publishing it (a project context creates a project-private blueprint; without one it writes the platform-global catalogue and requires superadmin). With apply=true it also applies the blueprint atomically, rolling back the publish on apply failure. The optional token is used only for a private-repo fetch and is never stored or returned. Call blueprint-import-preview first to inspect without creating anything.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"url": {
						Type:        "string",
						Description: "GitHub repository URL, optionally with a #ref suffix",
					},
					"ref": {
						Type:        "string",
						Description: "Optional git ref (branch, tag, or commit) to fetch",
					},
					"token": {
						Type:        "string",
						Description: "Optional GitHub token for a private repository; used only for this fetch and never stored",
					},
					"modelOverrides": {
						Type:        "object",
						Description: "Optional agent-name → provider/model overrides applied at apply time and persisted per-project",
					},
					"apply": {
						Type:        "boolean",
						Description: "When true, publish AND apply the imported blueprint atomically (defaults to false: import/publish only). Requires project context.",
					},
					"mergeTypeConflicts": {
						Type:        "boolean",
						Description: "Allow an apply=true import to proceed when a pack declares an object type name already registered in the project",
					},
				},
				Required: []string{"url"},
			},
		},
		{
			Name:          "blueprint-updates-check",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:write",
			Description:   "Enqueue an upstream-update check for the caller's applied blueprint by name. The check runs asynchronously (idempotent — returns the existing pending/running job when one is present) and returns the queued job; poll blueprint-updates-status for the result.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"name": {
						Type:        "string",
						Description: "Blueprint name to check upstream for",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:          "blueprint-updates-apply",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:write",
			Description:   "Enqueue an upstream-update apply for the caller's applied blueprint by name. The job asynchronously fetches the upstream source and creates + publishes + applies a new version when upstream is newer. Rejected with a conflict while an update for the same blueprint is already pending or running. Returns the queued job; poll blueprint-updates-status for the result.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"name": {
						Type:        "string",
						Description: "Blueprint name to apply the upstream update for",
					},
					"mergeTypeConflicts": {
						Type:        "boolean",
						Description: "Allow the async apply to proceed through object-type name collisions",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:          "blueprint-model-overrides-put",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:write",
			Description:   "Upsert the project-scoped model overrides (agent-name → provider/model map) for a blueprint name and return the resulting stored map. Requires project context. Read them back with blueprint-model-overrides-get.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"name": {
						Type:        "string",
						Description: "Blueprint name whose stored model overrides to set",
					},
					"modelOverrides": {
						Type:        "object",
						Description: "agent-name → provider/model map to store",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:          "blueprint-github-credentials-put",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "schema:write",
			Description:   "Store (or replace) the project's GitHub personal access token, encrypted at rest. The token is used only for outbound private-repo fetches and is never echoed, logged, or returned in responses. Requires project context.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"token": {
						Type:        "string",
						Description: "GitHub personal access token to store (never returned)",
					},
				},
				Required: []string{"token"},
			},
		},
	}
}
