package sources

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// sourceAdminToolDefinitions returns the source-management MCP tools. They are
// available when the project has at least one data source.
func sourceAdminToolDefinitions() []mcp.ToolDefinition {
	read := "documents:read"
	write := "sources:write"
	filterProps := map[string]mcp.PropertySchema{
		"categories":      {Type: "array", Items: &mcp.PropertySchema{Type: "string"}, Description: "Primary, Social, Promotions, Updates, Forums."},
		"labels":          {Type: "array", Items: &mcp.PropertySchema{Type: "string"}},
		"excludeLabels":   {Type: "array", Items: &mcp.PropertySchema{Type: "string"}},
		"senders":         {Type: "array", Items: &mcp.PropertySchema{Type: "string"}},
		"excludeSenders":  {Type: "array", Items: &mcp.PropertySchema{Type: "string"}},
		"domains":         {Type: "array", Items: &mcp.PropertySchema{Type: "string"}},
		"excludeDomains":  {Type: "array", Items: &mcp.PropertySchema{Type: "string"}},
		"subjectContains": {Type: "array", Items: &mcp.PropertySchema{Type: "string"}},
		"hasAttachment":   {Type: "boolean"},
		"unreadOnly":      {Type: "boolean"},
		"starredOnly":     {Type: "boolean"},
		"after":           {Type: "string", Description: "YYYY-MM-DD."},
		"before":          {Type: "string", Description: "YYYY-MM-DD."},
	}
	scopeProps := map[string]mcp.PropertySchema{
		"source_id":               {Type: "string", Description: "Data source id."},
		"filter":                  {Type: "object", Description: "Structured Gmail filter. Raw query is not accepted."},
		"object_types":            {Type: "array", Items: &mcp.PropertySchema{Type: "string", Enum: []string{"EmailMessage", "EmailThread"}}},
		"mcp_write_tools_enabled": {Type: "boolean"},
	}
	// Flattened scalar/array fields are also accepted by the executor; the
	// object above is the primary shape an LLM should use.
	for k, v := range filterProps {
		if _, exists := scopeProps[k]; !exists {
			scopeProps[k] = v
		}
	}
	updateProps := make(map[string]mcp.PropertySchema, len(scopeProps)+2)
	for k, v := range scopeProps {
		updateProps[k] = v
	}
	updateProps["preview_hash"] = mcp.PropertySchema{Type: "string"}
	updateProps["base_scope_hash"] = mcp.PropertySchema{Type: "string"}

	return []mcp.ToolDefinition{
		{Name: "source-list", RequiredScope: read, Description: "List data sources in this project with their sync scope summary.", InputSchema: mcp.InputSchema{Type: "object", Properties: map[string]mcp.PropertySchema{}}},
		{Name: "source-get", RequiredScope: read, Description: "Get one data source's non-secret config and sync scope hashes.", InputSchema: mcp.InputSchema{Type: "object", Properties: map[string]mcp.PropertySchema{"source_id": {Type: "string"}}, Required: []string{"source_id"}}},
		{Name: "source-scope-preview", RequiredScope: read, Description: "Validate a proposed Gmail sync scope and return the compiled query, a preview hash, and the current base hash. Call this before source-scope-update.", InputSchema: mcp.InputSchema{Type: "object", Properties: scopeProps, Required: []string{"source_id"}}},
		{Name: "source-scope-update", RequiredScope: write, Description: "Update a Gmail source's sync scope. Requires preview_hash and base_scope_hash from source-scope-preview. Structured filters only; raw Gmail query is rejected.", InputSchema: mcp.InputSchema{Type: "object", Properties: updateProps, Required: []string{"source_id", "preview_hash", "base_scope_hash"}}},
		{Name: "source-sync-trigger", RequiredScope: write, Description: "Trigger a sync for a data source after changing its scope.", InputSchema: mcp.InputSchema{Type: "object", Properties: map[string]mcp.PropertySchema{"source_id": {Type: "string"}}, Required: []string{"source_id"}}},
	}
}

var allowedStructuredFilterKeys = map[string]bool{
	"categories": true, "labels": true, "excludeLabels": true,
	"senders": true, "excludeSenders": true, "domains": true,
	"excludeDomains": true, "subjectContains": true, "hasAttachment": true,
	"unreadOnly": true, "starredOnly": true, "after": true, "before": true,
}

func rejectRawQuery(filter map[string]any) error {
	for k := range filter {
		if !allowedStructuredFilterKeys[k] {
			return apperror.NewValidation("unsupported filter key for MCP: " + k + " (raw Gmail query is available only in the UI/API)")
		}
	}
	return nil
}

// executeSourceAdminTool routes source-* MCP tools.
func (s *Service) executeSourceAdminTool(ctx context.Context, projectID, toolName string, args map[string]any) (*mcp.ToolResult, error) {
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return nil, apperror.NewValidation("invalid project id")
	}
	switch toolName {
	case "source-list":
		sources, err := s.repo.List(ctx, pid)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(sources))
		for _, src := range sources {
			items = append(items, sourceSummary(src))
		}
		return toolJSON(map[string]any{"sources": items})
	case "source-get":
		src, err := s.getSourceArg(ctx, pid, args)
		if err != nil {
			return nil, err
		}
		scope := sourceScopeFromConfig(src.Config)
		return toolJSON(map[string]any{
			"source":       sourceSummary(src),
			"config":       s.publicConfig(src),
			"scope":        scope,
			"scopeHash":    sourceScopeHash(src.Provider, scope),
			"providerType": src.Provider,
			"objectTypes":  scope.ObjectTypes,
			"mcpSlug":      src.McpSlug,
		})
	case "source-scope-preview":
		src, err := s.getSourceArg(ctx, pid, args)
		if err != nil {
			return nil, err
		}
		req, err := scopeRequestFromArgs(args)
		if err != nil {
			return nil, err
		}
		preview, err := s.PreviewSourceScope(ctx, pid, src.ID, req)
		if err != nil {
			return nil, err
		}
		return toolJSON(preview)
	case "source-scope-update":
		src, err := s.getSourceArg(ctx, pid, args)
		if err != nil {
			return nil, err
		}
		req, err := scopeRequestFromArgs(args)
		if err != nil {
			return nil, err
		}
		preview, err := s.UpdateSourceScope(ctx, pid, src.ID, req, stringArg(args, "preview_hash"), stringArg(args, "base_scope_hash"))
		if err != nil {
			return nil, err
		}
		return toolJSON(preview)
	case "source-sync-trigger":
		src, err := s.getSourceArg(ctx, pid, args)
		if err != nil {
			return nil, err
		}
		resp, err := s.TriggerSync(ctx, pid, src.ID)
		if err != nil {
			return nil, err
		}
		return toolJSON(resp)
	default:
		return nil, apperror.NewValidation("unknown source tool: " + toolName)
	}
}

func (s *Service) getSourceArg(ctx context.Context, projectID uuid.UUID, args map[string]any) (*Source, error) {
	id, err := uuid.Parse(stringArg(args, "source_id"))
	if err != nil {
		return nil, apperror.NewValidation("source_id is required")
	}
	return s.repo.GetByID(ctx, id, projectID)
}

func scopeRequestFromArgs(args map[string]any) (*SourceScopeRequest, error) {
	req := &SourceScopeRequest{}
	if raw, ok := args["filter"].(map[string]any); ok && raw != nil {
		if err := rejectRawQuery(raw); err != nil {
			return nil, err
		}
		req.Filter = raw
	} else {
		flat := map[string]any{}
		for k := range allowedStructuredFilterKeys {
			if v, ok := args[k]; ok {
				flat[k] = v
			}
		}
		if len(flat) > 0 {
			req.Filter = flat
		}
	}
	if types, err := stringList(args["object_types"]); err == nil && len(types) > 0 {
		req.ObjectTypes = types
	}
	if _, ok := args["query"]; ok {
		return nil, apperror.NewValidation("raw Gmail query is not accepted by MCP; use structured filter fields")
	}
	if v, ok := args["mcp_write_tools_enabled"]; ok {
		b, ok := v.(bool)
		if !ok {
			return nil, apperror.NewValidation("mcp_write_tools_enabled must be a boolean")
		}
		req.MCPWriteToolsEnabled = &b
	}
	return req, nil
}

func sourceSummary(src *Source) map[string]any {
	scope := sourceScopeFromConfig(src.Config)
	out := map[string]any{
		"id":            src.ID.String(),
		"name":          src.DisplayName,
		"providerType":  src.Provider,
		"status":        src.Status,
		"syncMode":      src.SyncPolicy,
		"objectTypes":   scope.ObjectTypes,
		"lastSyncedAt":  src.LastSyncedAt,
		"scopeHash":     sourceScopeHash(src.Provider, scope),
		"mcpWriteTools": scope.MCPWriteToolsEnabled,
	}
	if src.McpSlug != nil {
		out["mcpSlug"] = *src.McpSlug
	}
	if len(scope.Filter) > 0 {
		out["filter"] = scope.Filter
	}
	return out
}

func stringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}
