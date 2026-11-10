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
	EnsureSourceServer(ctx context.Context, projectID, sourceID string, defs []mcp.ToolDefinition, writeToolsEnabled bool) error
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
	if s.mcpSlugAvailable(ctx, src, base) {
		return base
	}
	for _, n := range []int{4, 6, 8} {
		candidate := base + "-" + src.ID.String()[:n]
		if s.mcpSlugAvailable(ctx, src, candidate) {
			return candidate
		}
	}
	return base + "-" + src.ID.String()[:12]
}

func (s *Service) mcpSlugAvailable(ctx context.Context, src *Source, slug string) bool {
	if taken, err := s.repo.McpSlugExists(ctx, src.ProjectID, slug, src.ID); err != nil || taken {
		return false
	}
	if taken, err := s.repo.McpServerNameExists(ctx, src.ProjectID, slug); err != nil || taken {
		return false
	}
	return true
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
	writeEnabled := sourceWriteToolsEnabled(src.Config)
	defs := stp.ToolDefinitions(*src.McpSlug, writeEnabled)
	if err := s.mcpServers.EnsureSourceServer(ctx, src.ProjectID.String(), src.ID.String(), defs, writeEnabled); err != nil {
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
//
// It is scoped to rows that still need work — a source-tool provider type with
// no mcp_slug — and pages them, so a boot on a large installation does not list
// every source in every project and re-provision servers that already exist.
// Each row it handles leaves the predicate (it gains a slug), so it always
// re-reads the first page; maxBackfillPages bounds a run that keeps failing to
// write slugs.
func (s *Service) BackfillSourceMCP(ctx context.Context) {
	if s.mcpServers == nil {
		return
	}
	var providers []string
	for _, p := range s.providers.List() {
		if _, ok := p.(SourceToolProvider); ok {
			providers = append(providers, p.Catalog().Type)
		}
	}
	if len(providers) == 0 {
		return
	}
	const pageSize = 100
	const maxBackfillPages = 100
	for page := 0; page < maxBackfillPages; page++ {
		sources, err := s.repo.ListSourcesMissingMcpSlug(ctx, providers, pageSize)
		if err != nil {
			s.log.Warn("source MCP backfill: list failed", "error", err)
			return
		}
		if len(sources) == 0 {
			return
		}
		progressed := 0
		for _, src := range sources {
			handled, err := s.provisionMissingSourceMCP(ctx, src)
			if err != nil {
				s.log.Warn("source MCP backfill: provision failed", "source_id", src.ID, "error", err)
				continue
			}
			if handled {
				progressed++
			}
		}
		if progressed == 0 || len(sources) < pageSize {
			return
		}
	}
}

// provisionMissingSourceMCP assigns a source its slug and provisions its MCP
// server. handled reports whether the row left the backfill predicate, so the
// caller can stop when a page makes no progress.
func (s *Service) provisionMissingSourceMCP(ctx context.Context, src *Source) (handled bool, err error) {
	provider, ok := s.providers.Get(src.Provider)
	if !ok {
		return false, nil
	}
	if _, ok := provider.(SourceToolProvider); !ok {
		return false, nil
	}
	// A legacy source predates its provider's schema pack; install it before the
	// source starts syncing typed objects. Best-effort, like the rest of the
	// backfill: a failed pack must not keep the row out of the predicate forever.
	if err := s.ensureObjectTypes(ctx, src.ProjectID, provider); err != nil {
		s.log.Warn("source MCP backfill: schema pack failed", "source_id", src.ID, "error", err)
	}
	slug := s.deriveMcpSlug(ctx, src)
	if err := s.repo.SetMcpSlug(ctx, src.ProjectID, src.ID, slug); err != nil {
		return false, err
	}
	src.McpSlug = &slug
	return true, s.provisionSourceMCP(ctx, src)
}
