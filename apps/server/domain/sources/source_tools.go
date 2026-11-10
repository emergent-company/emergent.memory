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
	sources, err := s.repo.List(ctx, pid)
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
		if !ok {
			continue
		}
		if src.McpSlug == nil {
			slug := s.deriveMcpSlug(ctx, src)
			if err := s.repo.SetMcpSlug(ctx, src.ProjectID, src.ID, slug); err != nil {
				s.log.Warn("failed to assign source MCP slug", "source_id", src.ID, "error", err)
				continue
			}
			src.McpSlug = &slug
			if err := s.provisionSourceMCP(ctx, src); err != nil {
				s.log.Warn("failed to provision source MCP server", "source_id", src.ID, "error", err)
			}
		}
		out = append(out, stp.ToolDefinitions(*src.McpSlug, sourceWriteToolsEnabled(src.Config))...)
	}
	if len(sources) > 0 {
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
		if err := requireSourceToolScope(ctx, scope); err != nil {
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

// requireSourceToolScope enforces token scopes on the HTTP MCP transports and
// fails closed for untrusted in-process callers. Trusted agent runs rely on
// tool enablement/policy, matching authz.AuthorizeTool's seam.
func requireSourceToolScope(ctx context.Context, scope string) error {
	if mcp.TransportEnforcedFromContext(ctx) {
		u := auth.UserFromContext(ctx)
		if u == nil || !u.HasScope(scope) {
			return apperror.NewForbidden("missing required scope: " + scope)
		}
		return nil
	}
	if mcp.TrustedInternalFromContext(ctx) {
		return nil
	}
	return apperror.NewForbidden("source tool is not reachable from an untrusted surface")
}
