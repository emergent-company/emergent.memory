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

// sourceAdminToolNames is the fixed set of project-level source tool names. A
// name outside it is never claimed by this domain.
var sourceAdminToolNames = func() map[string]bool {
	defs := sourceAdminToolDefinitions()
	out := make(map[string]bool, len(defs))
	for _, def := range defs {
		out[def.Name] = true
	}
	return out
}()

// HandlesSourceTool implements mcp.SourceToolHandler. It claims a name only when
// this build registered it for the project: dispatchTool's default branch asks
// before the relay/registry fallbacks, so a broad prefix claim would hijack a
// foreign tool that happens to be called gmail-* or source-* and answer it
// ErrNotFound/forbidden instead of by its own handler.
func (s *Service) HandlesSourceTool(ctx context.Context, projectID, toolName string) bool {
	ref, err := s.resolveSourceTool(ctx, projectID, toolName)
	return err == nil && ref != nil
}

// sourceToolRef is what a source tool name resolves to: one provider action on
// one connected account, or a project-level admin tool.
type sourceToolRef struct {
	// source is nil for a project-level admin tool.
	source *Source
	// action is the provider action (e.g. message-search) or the admin tool name.
	action string
	admin  bool
}

// resolveSourceTool maps a registered source tool name to its target. It returns
// (nil, nil) for a name this build did not register.
func (s *Service) resolveSourceTool(ctx context.Context, projectID, toolName string) (*sourceToolRef, error) {
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return nil, apperror.NewValidation("invalid project id")
	}
	if sourceAdminToolNames[toolName] {
		return &sourceToolRef{action: toolName, admin: true}, nil
	}
	if !s.mayBeSourceTool(toolName) {
		return nil, nil
	}
	sources, err := s.repo.ListMcpSources(ctx, pid)
	if err != nil {
		return nil, err
	}
	return matchSourceTool(toolName, sources, s.providers), nil
}

// mayBeSourceTool is a cheap prefix gate: a per-account tool name starts with
// the type of a provider that exposes such tools. It only avoids a lookup —
// matchSourceTool still requires the name to be a registered definition.
func (s *Service) mayBeSourceTool(toolName string) bool {
	for _, p := range s.providers.List() {
		if _, ok := p.(SourceToolProvider); !ok {
			continue
		}
		if strings.HasPrefix(toolName, p.Catalog().Type+"-") {
			return true
		}
	}
	return false
}

// matchSourceTool resolves toolName against the project's registered source
// servers. Provider tool names are "<provider>-<slug>-<action>", and one slug
// may be a dash-prefix of another (alice and alice-work from alice@ and
// alice.work@), so the longest matching slug wins: taking the first match would
// resolve gmail-alice-work-thread-get to the alice source and fail it as an
// unknown action. A name resolves only when that server registered it.
func matchSourceTool(toolName string, sources []*Source, providers *Registry) *sourceToolRef {
	var best *sourceToolRef
	bestSlug := -1
	for _, src := range sources {
		if src == nil || src.McpSlug == nil || *src.McpSlug == "" {
			continue
		}
		slug := *src.McpSlug
		if len(slug) <= bestSlug {
			continue
		}
		provider, ok := providers.Get(src.Provider)
		if !ok {
			continue
		}
		stp, ok := provider.(SourceToolProvider)
		if !ok {
			continue
		}
		prefix := src.Provider + "-" + slug + "-"
		if !strings.HasPrefix(toolName, prefix) {
			continue
		}
		action := strings.TrimPrefix(toolName, prefix)
		if action == "" || !sourceToolDefinitionRegistered(stp, slug, toolName) {
			continue
		}
		bestSlug = len(slug)
		best = &sourceToolRef{source: src, action: action}
	}
	return best
}

// sourceToolDefinitionRegistered reports whether the account registers toolName.
// Write definitions are generated as well: whether a write tool may run is an
// execution-time decision (the source's opt-in), not part of the naming contract.
func sourceToolDefinitionRegistered(stp SourceToolProvider, slug, toolName string) bool {
	for _, def := range stp.ToolDefinitions(slug, true) {
		if def.Name == toolName {
			return true
		}
	}
	return false
}

// ExecuteSourceTool implements mcp.SourceToolHandler.
func (s *Service) ExecuteSourceTool(ctx context.Context, projectID, toolName string, args map[string]any) (*mcp.ToolResult, error) {
	ref, err := s.resolveSourceTool(ctx, projectID, toolName)
	if err != nil {
		return nil, err
	}
	if ref == nil {
		return nil, apperror.ErrNotFound
	}
	if ref.admin {
		return s.executeSourceAdminTool(ctx, projectID, ref.action, args)
	}
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return nil, apperror.NewValidation("invalid project id")
	}
	scope := "email:read"
	if isGmailWriteAction(ref.action) {
		scope = "email:write"
	}
	if err := requireSourceToolScope(ctx, scope); err != nil {
		return nil, err
	}
	if isGmailWriteAction(ref.action) && !sourceWriteToolsEnabled(ref.source.Config) {
		return nil, apperror.NewValidation("write tools are not enabled for this Gmail source")
	}
	src, err := s.repo.GetByIDWithCredentials(ctx, ref.source.ID, pid)
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
	return stp.ExecuteTool(ctx, creds, src.Config, ref.action, args)
}

func sourceWriteToolsEnabled(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	v, _ := cfg["mcp_write_tools_enabled"].(bool)
	return v
}

// isGmailWriteAction reports whether an action mutates the mailbox. It is the
// gate for both email:write and the source's write-tool opt-in, so a read that
// happens to share the draft- prefix (draft-list) must not be included.
func isGmailWriteAction(action string) bool {
	switch action {
	case "message-send", "draft-create", "draft-update", "draft-send", "draft-delete":
		return true
	}
	return false
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
