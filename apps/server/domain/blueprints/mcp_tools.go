package blueprints

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// MCPBlueprintToolHandler implements mcp.BlueprintToolHandler, providing the
// blueprint-* MCP tools. It bridges the mcp package (which cannot import
// blueprints) to the blueprints domain by implementing the interface defined
// in mcp/blueprints_tools.go.
type MCPBlueprintToolHandler struct {
	svc *Service
}

// NewMCPBlueprintToolHandler creates a new MCPBlueprintToolHandler.
func NewMCPBlueprintToolHandler(svc *Service) *MCPBlueprintToolHandler {
	return &MCPBlueprintToolHandler{svc: svc}
}

// wrapResult marshals data as indented JSON into an MCP ToolResult.
func (h *MCPBlueprintToolHandler) wrapResult(data any) (*mcp.ToolResult, error) {
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}

	return &mcp.ToolResult{
		Content: []mcp.ContentBlock{
			{
				Type: "text",
				Text: string(jsonBytes),
			},
		},
		StructuredContent: mcp.StructuredContentFromJSON(jsonBytes),
	}, nil
}

// errResult creates an error ToolResult (non-fatal tool error returned to LLM).
func errResult(msg string) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{
		Content: []mcp.ContentBlock{
			{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, msg)},
		},
	}, nil
}

// toRawManifest converts the manifest arg (JSON object or JSON-encoded string)
// into json.RawMessage. A nil or empty input becomes an empty object.
func toRawManifest(v any) (json.RawMessage, error) {
	switch m := v.(type) {
	case nil:
		return json.RawMessage("{}"), nil
	case string:
		if m == "" {
			return json.RawMessage("{}"), nil
		}
		return json.RawMessage(m), nil
	default:
		return json.Marshal(m)
	}
}

// ============================================================================
// Blueprint Tools
// ============================================================================

// ExecuteBlueprintCreate creates a new blueprint draft, scoped to the caller's
// project (private) when the caller has a project context; global otherwise.
func (h *MCPBlueprintToolHandler) ExecuteBlueprintCreate(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	name, _ := args["name"].(string)
	version, _ := args["version"].(string)
	description, _ := args["description"].(string)
	author, _ := args["author"].(string)

	manifest, err := toRawManifest(args["manifest"])
	if err != nil {
		return errResult("invalid manifest: " + err.Error())
	}

	req := &CreateBlueprintRequest{
		Name:        name,
		Version:     version,
		Description: description,
		Author:      author,
		Manifest:    manifest,
	}
	if projectID != "" {
		req.ProjectID = &projectID
	}
	// A global-scope creation (project_id IS NULL) is a write to the
	// platform-global catalogue and requires superadmin_full; a project member
	// may only author project-private drafts. The authority decision lives in
	// the shared Service helper so the REST and MCP entrypoints cannot drift
	// (issue #1041).
	if req.ProjectID == nil {
		if err := h.svc.AuthorizeGlobalBlueprintWrite(ctx); err != nil {
			return errResult("forbidden: global blueprint writes require superadmin_full")
		}
	}
	bp, err := h.svc.CreateBlueprint(ctx, req)
	if err != nil {
		return errResult("failed to create blueprint: " + err.Error())
	}
	return h.wrapResult(bp)
}

// ExecuteBlueprintList lists blueprints in the caller's read scope (global +
// own private), optionally filtered by name.
func (h *MCPBlueprintToolHandler) ExecuteBlueprintList(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	name, _ := args["name"].(string)

	bps, err := h.svc.ListBlueprints(ctx, projectID, name)
	if err != nil {
		return errResult("failed to list blueprints: " + err.Error())
	}
	return h.wrapResult(bps)
}

// ExecuteBlueprintGet gets a single blueprint by id within the caller's read
// scope (global + own private).
func (h *MCPBlueprintToolHandler) ExecuteBlueprintGet(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["id"].(string)
	if id == "" {
		return errResult("id is required")
	}

	bp, err := h.svc.GetBlueprint(ctx, projectID, id)
	if err != nil {
		return errResult("failed to get blueprint: " + err.Error())
	}
	return h.wrapResult(bp)
}

// ExecuteBlueprintPublish publishes the caller's own private draft blueprint.
// Global blueprints are immutable.
func (h *MCPBlueprintToolHandler) ExecuteBlueprintPublish(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["id"].(string)
	if id == "" {
		return errResult("id is required")
	}

	// Publishing a global blueprint is a platform-global catalogue write
	// (superadmin_full); publishing the caller's own private draft stays
	// project-tier and unchanged. Resolve the target first to decide which tier
	// this write addresses, then use the shared Service helper (issue #1041).
	target, err := h.svc.GetBlueprint(ctx, projectID, id)
	if err != nil {
		return errResult("failed to get blueprint: " + err.Error())
	}
	if target.ProjectID == nil {
		if err := h.svc.AuthorizeGlobalBlueprintWrite(ctx); err != nil {
			return errResult("forbidden: global blueprint writes require superadmin_full")
		}
	}

	bp, err := h.svc.PublishBlueprint(ctx, projectID, id)
	if err != nil {
		return errResult("failed to publish blueprint: " + err.Error())
	}
	return h.wrapResult(bp)
}

// ExecuteBlueprintApply applies a blueprint to the current project. The userID
// is empty (the actor becomes nil); project-scoped materialization is driven by
// the caller's project context.
func (h *MCPBlueprintToolHandler) ExecuteBlueprintApply(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["id"].(string)
	if id == "" {
		return errResult("id is required")
	}

	mergeTypes, _ := args["mergeTypes"].(bool)
	result, err := h.svc.Apply(ctx, id, projectID, "", ApplyOptions{MergeTypeConflicts: mergeTypes})
	if err != nil {
		return errResult("failed to apply blueprint: " + err.Error())
	}
	return h.wrapResult(result)
}

// ExecuteBlueprintApplyPreview previews applying a blueprint to the current
// project without materializing anything, exposing type-name collisions and
// their property-level diff detail.
func (h *MCPBlueprintToolHandler) ExecuteBlueprintApplyPreview(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["id"].(string)
	if id == "" {
		return errResult("id is required")
	}

	preview, err := h.svc.PreviewApply(ctx, id, projectID)
	if err != nil {
		return errResult("failed to preview blueprint apply: " + err.Error())
	}
	return h.wrapResult(preview)
}

// ExecuteBlueprintUnapply reverses a previous blueprint apply for the project.
func (h *MCPBlueprintToolHandler) ExecuteBlueprintUnapply(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["id"].(string)
	if id == "" {
		return errResult("id is required")
	}

	result, err := h.svc.Unapply(ctx, id, projectID)
	if err != nil {
		return errResult("failed to unapply blueprint: " + err.Error())
	}
	return h.wrapResult(result)
}

// ExecuteBlueprintVersions lists all versions of a blueprint name within the
// caller's read scope (global + own private).
func (h *MCPBlueprintToolHandler) ExecuteBlueprintVersions(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return errResult("name is required")
	}

	bps, err := h.svc.ListVersions(ctx, projectID, name)
	if err != nil {
		return errResult("failed to list blueprint versions: " + err.Error())
	}
	return h.wrapResult(bps)
}

// ExecuteBlueprintListApplied lists the blueprints applied to the current project.
func (h *MCPBlueprintToolHandler) ExecuteBlueprintListApplied(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	apps, err := h.svc.ListApplied(ctx, projectID)
	if err != nil {
		return errResult("failed to list applied blueprints: " + err.Error())
	}
	return h.wrapResult(apps)
}

// ExecuteBlueprintNewVersion clones a blueprint (global built-in or the
// caller's own) into a new private draft version in the caller's project.
func (h *MCPBlueprintToolHandler) ExecuteBlueprintNewVersion(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["id"].(string)
	if id == "" {
		return errResult("id is required")
	}
	version, _ := args["version"].(string)
	if version == "" {
		return errResult("version is required")
	}

	// Forking with no project context clones the blueprint as a NEW global
	// version (project_id IS NULL) — a platform-global catalogue write requiring
	// superadmin_full. With a project context the clone is project-private (the
	// documented "fork a version" flow) and unchanged. The authority decision
	// lives in the shared Service helper so the REST and MCP entrypoints cannot
	// drift (issue #1041).
	if projectID == "" {
		if err := h.svc.AuthorizeGlobalBlueprintWrite(ctx); err != nil {
			return errResult("forbidden: global blueprint writes require superadmin_full")
		}
	}

	bp, err := h.svc.NewVersion(ctx, projectID, id, version)
	if err != nil {
		return errResult("failed to create new blueprint version: " + err.Error())
	}
	return h.wrapResult(bp)
}

// ExecuteBlueprintUpdate updates the caller's own private draft blueprint's
// description, author, and/or manifest. Global blueprints are immutable.
func (h *MCPBlueprintToolHandler) ExecuteBlueprintUpdate(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["id"].(string)
	if id == "" {
		return errResult("id is required")
	}

	req := &UpdateBlueprintRequest{}
	if v, ok := args["description"].(string); ok && v != "" {
		req.Description = &v
	}
	if v, ok := args["author"].(string); ok && v != "" {
		req.Author = &v
	}
	if v, ok := args["manifest"]; ok && v != nil {
		manifest, err := toRawManifest(v)
		if err != nil {
			return errResult("invalid manifest: " + err.Error())
		}
		req.Manifest = manifest
	}

	bp, err := h.svc.UpdateBlueprint(ctx, projectID, id, req)
	if err != nil {
		return errResult("failed to update blueprint: " + err.Error())
	}
	return h.wrapResult(bp)
}

// stringMapArg extracts a map[string]string from a tool argument that may be
// passed as a JSON object (map[string]any) or a JSON-encoded object string.
//
// Unlike a silent coercion it refuses input REST refuses: a non-string value, a
// malformed JSON string, or any other shape is an error, so a bad modelOverrides
// cannot quietly preview with no overrides applied (the REST PreviewImport
// binds map[string]string and returns 400 — handler.go:133).
func stringMapArg(v any) (map[string]string, error) {
	switch m := v.(type) {
	case nil:
		return nil, nil
	case map[string]string:
		return m, nil
	case map[string]any:
		return stringMapFromAny(m)
	case string:
		if strings.TrimSpace(m) == "" {
			return nil, nil
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(m), &raw); err != nil {
			return nil, fmt.Errorf("must be a JSON object of strings: %w", err)
		}
		return stringMapFromAny(raw)
	default:
		return nil, fmt.Errorf("must be an object of strings, got %T", v)
	}
}

// stringMapFromAny converts a decoded object, refusing any non-string value
// rather than dropping it.
func stringMapFromAny(m map[string]any) (map[string]string, error) {
	out := make(map[string]string, len(m))
	for k, val := range m {
		s, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("value for %q must be a string, got %T", k, val)
		}
		out[k] = s
	}
	return out, nil
}

// ExecuteBlueprintUpdatesStatus reports the latest upstream-update status for
// an applied blueprint by name. It is a pure read: it does not enqueue a check
// and does not contact GitHub.
func (h *MCPBlueprintToolHandler) ExecuteBlueprintUpdatesStatus(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	if projectID == "" {
		return errResult("project context required for blueprint update status")
	}
	name, _ := args["name"].(string)
	if strings.TrimSpace(name) == "" {
		return errResult("name is required")
	}

	status, err := h.svc.GetUpdateStatus(ctx, projectID, name)
	if err != nil {
		return errResult("failed to get blueprint update status: " + err.Error())
	}
	return h.wrapResult(status)
}

// ExecuteBlueprintImportPreview fetches and validates a blueprint from GitHub
// and returns its metadata and manifest WITHOUT creating, publishing, or
// applying anything. In project scope it also reports the object-type
// collisions an apply would hit, mirroring the REST preview endpoint.
func (h *MCPBlueprintToolHandler) ExecuteBlueprintImportPreview(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	url, _ := args["url"].(string)
	if strings.TrimSpace(url) == "" {
		return errResult("url is required")
	}
	ref, _ := args["ref"].(string)
	token, _ := args["token"].(string)

	// A global (no project) preview reads the platform-global catalogue
	// surface and requires the same superadmin authority as the REST
	// endpoint; project-scoped previews are unchanged.
	if projectID == "" {
		if err := h.svc.AuthorizeGlobalBlueprintWrite(ctx); err != nil {
			return errResult("forbidden: global blueprint previews require superadmin_full")
		}
	}

	modelOverrides, err := stringMapArg(args["modelOverrides"])
	if err != nil {
		return errResult("invalid modelOverrides: " + err.Error())
	}

	req := &ImportGitHubRequest{
		URL:            url,
		Ref:            ref,
		Token:          token,
		ModelOverrides: modelOverrides,
	}
	preview, err := h.svc.PreviewFromGitHub(ctx, req)
	if err != nil {
		return errResult("failed to preview blueprint import: " + err.Error())
	}

	// Project-scoped previews report object-type collisions an apply would
	// hit. Global previews have no registry to compare against.
	if projectID != "" {
		conflicts, cerr := h.svc.PreviewManifestConflicts(ctx, projectID, preview.Manifest)
		if cerr != nil {
			return errResult("failed to preview type conflicts: " + cerr.Error())
		}
		preview.TypeConflicts = conflicts
	}
	return h.wrapResult(preview)
}

// ExecuteBlueprintModelOverridesGet returns the project-scoped model overrides
// stored for a blueprint name. Read-only: changing them remains a REST PUT.
func (h *MCPBlueprintToolHandler) ExecuteBlueprintModelOverridesGet(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	if projectID == "" {
		return errResult("project context required for blueprint model overrides")
	}
	name, _ := args["name"].(string)
	if strings.TrimSpace(name) == "" {
		return errResult("name is required")
	}

	overrides, err := h.svc.GetModelOverrides(ctx, projectID, name)
	if err != nil {
		return errResult("failed to get blueprint model overrides: " + err.Error())
	}
	return h.wrapResult(map[string]any{"modelOverrides": overrides})
}

// ExecuteBlueprintDelete removes a project-private blueprint from the registry.
// Global built-ins are immutable and any blueprint with a live application is
// refused by the shared Service (guardMutable / uninstall-first), so no
// additional global-write gate is needed here — mirroring the REST handler.
func (h *MCPBlueprintToolHandler) ExecuteBlueprintDelete(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["id"].(string)
	if strings.TrimSpace(id) == "" {
		return errResult("id is required")
	}

	if err := h.svc.DeleteBlueprint(ctx, projectID, id); err != nil {
		return errResult("failed to delete blueprint: " + err.Error())
	}
	return h.wrapResult(map[string]any{"deleted": true, "id": id})
}

// ExecuteBlueprintDeprecate marks a blueprint version deprecated. Deprecating a
// global blueprint is a platform-global catalogue write (superadmin_full);
// deprecating the caller's own private blueprint stays project-tier — the same
// shared Service helper the REST handler uses (issue #1041).
func (h *MCPBlueprintToolHandler) ExecuteBlueprintDeprecate(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["id"].(string)
	if strings.TrimSpace(id) == "" {
		return errResult("id is required")
	}

	target, err := h.svc.GetBlueprint(ctx, projectID, id)
	if err != nil {
		return errResult("failed to get blueprint: " + err.Error())
	}
	if target.ProjectID == nil {
		if err := h.svc.AuthorizeGlobalBlueprintWrite(ctx); err != nil {
			return errResult("forbidden: global blueprint writes require superadmin_full")
		}
	}

	bp, err := h.svc.DeprecateBlueprint(ctx, projectID, id)
	if err != nil {
		return errResult("failed to deprecate blueprint: " + err.Error())
	}
	return h.wrapResult(bp)
}

// ExecuteBlueprintImport imports a blueprint from a GitHub repo URL, mirroring
// the REST POST /api/blueprints/import handler (create + publish, optional
// atomic apply). A global (no project) import is a platform-global catalogue
// write and requires superadmin_full.
func (h *MCPBlueprintToolHandler) ExecuteBlueprintImport(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	url, _ := args["url"].(string)
	if strings.TrimSpace(url) == "" {
		return errResult("url is required")
	}
	ref, _ := args["ref"].(string)
	token, _ := args["token"].(string)
	apply, _ := args["apply"].(bool)
	mergeTypeConflicts, _ := args["mergeTypeConflicts"].(bool)

	if projectID == "" {
		if err := h.svc.AuthorizeGlobalBlueprintWrite(ctx); err != nil {
			return errResult("forbidden: global blueprint imports require superadmin_full")
		}
	}

	modelOverrides, err := stringMapArg(args["modelOverrides"])
	if err != nil {
		return errResult("invalid modelOverrides: " + err.Error())
	}

	req := &ImportGitHubRequest{
		URL:                url,
		Ref:                ref,
		Token:              token,
		ModelOverrides:     modelOverrides,
		Apply:              apply,
		MergeTypeConflicts: mergeTypeConflicts,
	}
	bp, err := h.svc.ImportFromGitHub(ctx, projectID, req)
	if err != nil {
		return errResult("failed to import blueprint: " + err.Error())
	}
	return h.wrapResult(bp)
}

// ExecuteBlueprintUpdatesCheck enqueues an upstream-update check for the
// caller's applied blueprint by name, mirroring POST /api/blueprints/:name/updates/check.
func (h *MCPBlueprintToolHandler) ExecuteBlueprintUpdatesCheck(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	if projectID == "" {
		return errResult("project context required for blueprint update check")
	}
	name, _ := args["name"].(string)
	if strings.TrimSpace(name) == "" {
		return errResult("name is required")
	}

	job, err := h.svc.EnqueueUpdateCheck(ctx, projectID, name)
	if err != nil {
		return errResult("failed to enqueue blueprint update check: " + err.Error())
	}
	return h.wrapResult(job)
}

// ExecuteBlueprintUpdatesApply enqueues an upstream-update apply for the
// caller's applied blueprint by name, mirroring POST /api/blueprints/:name/updates/apply.
func (h *MCPBlueprintToolHandler) ExecuteBlueprintUpdatesApply(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	if projectID == "" {
		return errResult("project context required for blueprint update apply")
	}
	name, _ := args["name"].(string)
	if strings.TrimSpace(name) == "" {
		return errResult("name is required")
	}
	mergeTypeConflicts, _ := args["mergeTypeConflicts"].(bool)

	job, err := h.svc.EnqueueBlueprintUpdateWithOptions(ctx, projectID, name, UpdateJobOptions{
		MergeTypeConflicts: mergeTypeConflicts,
	})
	if err != nil {
		return errResult("failed to enqueue blueprint update apply: " + err.Error())
	}
	return h.wrapResult(job)
}

// ExecuteBlueprintModelOverridesPut upserts the project-scoped model overrides
// for a blueprint name, mirroring PUT /api/blueprints/model-overrides.
func (h *MCPBlueprintToolHandler) ExecuteBlueprintModelOverridesPut(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	if projectID == "" {
		return errResult("project context required for blueprint model overrides")
	}
	name, _ := args["name"].(string)
	if strings.TrimSpace(name) == "" {
		return errResult("name is required")
	}

	modelOverrides, err := stringMapArg(args["modelOverrides"])
	if err != nil {
		return errResult("invalid modelOverrides: " + err.Error())
	}

	overrides, err := h.svc.UpsertModelOverrides(ctx, projectID, name, modelOverrides)
	if err != nil {
		return errResult("failed to upsert blueprint model overrides: " + err.Error())
	}
	return h.wrapResult(map[string]any{"modelOverrides": overrides})
}

// ExecuteBlueprintGithubCredentialsPut stores the project's encrypted GitHub PAT,
// mirroring PUT /api/blueprints/github-credentials. The token is never echoed,
// logged, or returned — only a confirmation is produced.
func (h *MCPBlueprintToolHandler) ExecuteBlueprintGithubCredentialsPut(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	if projectID == "" {
		return errResult("project context required for GitHub credentials")
	}
	token, _ := args["token"].(string)
	if strings.TrimSpace(token) == "" {
		return errResult("token is required")
	}

	if err := h.svc.SaveGithubCredential(ctx, projectID, token); err != nil {
		return errResult("failed to store GitHub credentials: " + err.Error())
	}
	return h.wrapResult(map[string]any{"stored": true})
}
