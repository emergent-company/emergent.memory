package blueprints

import (
	"context"
	"encoding/json"
	"fmt"

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

	result, err := h.svc.Apply(ctx, id, projectID, "", ApplyOptions{})
	if err != nil {
		return errResult("failed to apply blueprint: " + err.Error())
	}
	return h.wrapResult(result)
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
