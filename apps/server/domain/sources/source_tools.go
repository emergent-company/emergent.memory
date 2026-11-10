package sources

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// GetSourceToolDefinitionsForProject implements mcp.SourceToolHandler.
func (s *Service) GetSourceToolDefinitionsForProject(ctx context.Context, projectID string) []mcp.ToolDefinition {
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return nil
	}
	sources, err := s.repo.ListMcpSources(ctx, pid)
	if err != nil {
		s.log.Warn("failed to list source tools", "error", err)
		return nil
	}
	var out []mcp.ToolDefinition
	for _, src := range sources {
		provider, ok := s.providers.Get(src.Provider)
		if !ok {
			continue
		}
		stp, ok := provider.(SourceToolProvider)
		if !ok || src.McpSlug == nil {
			continue
		}
		out = append(out, stp.ToolDefinitions(*src.McpSlug, sourceWriteToolsEnabled(src.Config))...)
	}
	if all, err := s.repo.List(ctx, pid); err == nil && len(all) > 0 {
		out = append(out, sourceAdminToolDefinitions()...)
	}
	return out
}

// HandlesSourceTool reports whether a tool name belongs to a source-backed
// server in this build.
func (s *Service) HandlesSourceTool(toolName string) bool {
	return strings.HasPrefix(toolName, "gmail-") || strings.HasPrefix(toolName, "source-")
}

// ExecuteSourceTool implements mcp.SourceToolHandler.
func (s *Service) ExecuteSourceTool(ctx context.Context, projectID, toolName string, args map[string]any) (*mcp.ToolResult, error) {
	if strings.HasPrefix(toolName, "source-") {
		return s.executeSourceAdminTool(ctx, projectID, toolName, args)
	}
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return nil, apperror.NewValidation("invalid project id")
	}
	sources, err := s.repo.ListMcpSources(ctx, pid)
	if err != nil {
		return nil, err
	}
	for _, listed := range sources {
		if listed.McpSlug == nil {
			continue
		}
		prefix := listed.Provider + "-" + *listed.McpSlug + "-"
		if !strings.HasPrefix(toolName, prefix) {
			continue
		}
		action := strings.TrimPrefix(toolName, prefix)
		scope := "email:read"
		if isGmailWriteAction(action) {
			scope = "email:write"
		}
		if err := requireUserScope(ctx, scope); err != nil {
			return nil, err
		}
		if isGmailWriteAction(action) && !sourceWriteToolsEnabled(listed.Config) {
			return nil, apperror.NewValidation("write tools are not enabled for this Gmail source")
		}
		src, err := s.repo.GetByIDWithCredentials(ctx, listed.ID, pid)
		if err != nil {
			return nil, err
		}
		provider, ok := s.providers.Get(src.Provider)
		if !ok {
			return nil, apperror.NewValidation("unsupported provider")
		}
		stp, ok := provider.(SourceToolProvider)
		if !ok {
			return nil, apperror.NewValidation("provider does not expose tools")
		}
		creds, err := s.credentialsForSource(ctx, src)
		if err != nil {
			return nil, err
		}
		return stp.ExecuteTool(ctx, creds, src.Config, action, args)
	}
	return nil, apperror.ErrNotFound
}

func sourceWriteToolsEnabled(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	v, _ := cfg["mcp_write_tools_enabled"].(bool)
	return v
}

func isGmailWriteAction(action string) bool {
	return action == "message-send" || strings.HasPrefix(action, "draft-")
}

// requireUserScope enforces a scope for HTTP MCP callers. Internal agent runs
// have no user in context; their tool pool is already restricted by enablement.
func requireUserScope(ctx context.Context, scope string) error {
	u := auth.UserFromContext(ctx)
	if u == nil {
		return nil
	}
	if !u.HasScope(scope) {
		return apperror.NewForbidden("missing required scope: " + scope)
	}
	return nil
}
