package sources

import (
	"context"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// SourceMCPServerManager owns the per-account MCP server rows. Implemented by
// mcpregistry.Service.
type SourceMCPServerManager interface {
	EnsureSourceServer(ctx context.Context, projectID, sourceID string, defs []mcp.ToolDefinition) error
	DeleteSourceServer(ctx context.Context, projectID, sourceID string) error
	EnsureSourceAdminServer(ctx context.Context, projectID string, defs []mcp.ToolDefinition) error
	DeleteSourceAdminServer(ctx context.Context, projectID string) error
}

var slugInvalid = regexp.MustCompile(`[^a-z0-9]+`)

// deriveMcpSlug builds a stable, project-unique tool-name segment for a source.
func (s *Service) deriveMcpSlug(ctx context.Context, src *Source) string {
	base := ""
	if v, ok := src.Config["mcp_alias"].(string); ok {
		base = v
	}
	if base == "" {
		if v, ok := src.Config[oauthAccountEmailKey].(string); ok {
			base = strings.SplitN(v, "@", 2)[0]
		}
	}
	if base == "" {
		base = src.DisplayName
	}
	if base == "" {
		base = src.Provider
	}
	base = strings.ToLower(slugInvalid.ReplaceAllString(base, "-"))
	base = strings.Trim(base, "-")
	if len(base) > 24 {
		base = strings.Trim(base[:24], "-")
	}
	if base == "" {
		base = src.Provider
	}
	if base == "source-admin" {
		base = "source-admin-" + src.ID.String()[:4]
	}
	if !s.mcpSlugTaken(ctx, src.ProjectID, base, src.ID) {
		return base
	}
	short := src.ID.String()[:4]
	return base + "-" + short
}

func (s *Service) mcpSlugTaken(ctx context.Context, projectID uuid.UUID, slug string, exceptID uuid.UUID) bool {
	exists, err := s.repo.McpSlugExists(ctx, projectID, slug, exceptID)
	if err != nil {
		return true
	}
	return exists
}

// provisionSourceMCP creates or refreshes a source's MCP server and tools.
func (s *Service) provisionSourceMCP(ctx context.Context, src *Source) error {
	if s.mcpServers == nil || src.McpSlug == nil {
		return nil
	}
	provider, ok := s.providers.Get(src.Provider)
	if !ok {
		return nil
	}
	stp, ok := provider.(SourceToolProvider)
	if !ok {
		return nil
	}
	defs := stp.ToolDefinitions(*src.McpSlug, sourceWriteToolsEnabled(src.Config))
	if err := s.mcpServers.EnsureSourceServer(ctx, src.ProjectID.String(), src.ID.String(), defs); err != nil {
		return err
	}
	return s.mcpServers.EnsureSourceAdminServer(ctx, src.ProjectID.String(), sourceAdminToolDefinitions())
}

// deleteSourceMCP removes a source's MCP server, ignoring a missing row.
func (s *Service) deleteSourceMCP(ctx context.Context, projectID, sourceID uuid.UUID) error {
	if s.mcpServers == nil {
		return nil
	}
	if err := s.mcpServers.DeleteSourceServer(ctx, projectID.String(), sourceID.String()); err != nil {
		s.log.Warn("failed to delete source MCP server", "source_id", sourceID, "error", err)
	}
	return nil
}

// BackfillSourceMCP provisions MCP servers/tools for sources created before
// source-backed tooling existed. It is best-effort and idempotent.
func (s *Service) BackfillSourceMCP(ctx context.Context) {
	if s.mcpServers == nil {
		return
	}
	sources, err := s.repo.ListAllSources(ctx)
	if err != nil {
		s.log.Warn("source MCP backfill: list failed", "error", err)
		return
	}
	for _, src := range sources {
		provider, ok := s.providers.Get(src.Provider)
		if !ok {
			continue
		}
		if _, ok := provider.(SourceToolProvider); !ok {
			continue
		}
		if src.McpSlug == nil {
			slug := s.deriveMcpSlug(ctx, src)
			if err := s.repo.SetMcpSlug(ctx, src.ProjectID, src.ID, slug); err != nil {
				s.log.Warn("source MCP backfill: set slug failed", "source_id", src.ID, "error", err)
				continue
			}
			src.McpSlug = &slug
		}
		if err := s.provisionSourceMCP(ctx, src); err != nil {
			s.log.Warn("source MCP backfill: provision failed", "source_id", src.ID, "error", err)
		}
	}
}
