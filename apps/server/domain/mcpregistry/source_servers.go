package mcpregistry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// FindServerBySourceID returns the type=source server for a data source, or nil.
func (r *Repository) FindServerBySourceID(ctx context.Context, projectID, sourceID string) (*MCPServer, error) {
	server := &MCPServer{}
	err := r.db.NewSelect().
		Model(server).
		Where("project_id = ?", projectID).
		Where("source_id = ?", sourceID).
		Where("type = ?", ServerTypeSource).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return server, nil
}

// SetToolEnabledByServerAndName sets a tool row's enabled flag.
func (r *Repository) SetToolEnabledByServerAndName(ctx context.Context, serverID, toolName string, enabled bool) error {
	_, err := r.db.NewUpdate().
		Model((*MCPServerTool)(nil)).
		Set("enabled = ?", enabled).
		Where("server_id = ?", serverID).
		Where("tool_name = ?", toolName).
		Exec(ctx)
	return err
}

// EnsureSourceServer creates or updates the per-account MCP server row and its
// tool rows. Tool enablement follows the definition's required scope: write
// tools are disabled unless the source opted in.
func (s *Service) EnsureSourceServer(ctx context.Context, projectID, sourceID string, defs []mcp.ToolDefinition) error {
	server, err := s.repo.FindServerBySourceID(ctx, projectID, sourceID)
	if err != nil {
		return fmt.Errorf("finding source server: %w", err)
	}
	if server == nil {
		// The server name is the source's stable mcp_slug; the caller resolved it.
		name := sourceServerName(ctx, s, projectID, sourceID)
		if name == "" {
			name = "source-" + sourceID
		}
		server = &MCPServer{
			ProjectID: projectID,
			Name:      name,
			Enabled:   true,
			Type:      ServerTypeSource,
			SourceID:  &sourceID,
		}
		if err := s.repo.CreateServer(ctx, server); err != nil {
			return fmt.Errorf("creating source server: %w", err)
		}
	}

	tools := make([]*MCPServerTool, 0, len(defs))
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		desc := def.Description
		in := schemaToMap(def.InputSchema)
		tools = append(tools, &MCPServerTool{
			ServerID:    server.ID,
			ToolName:    def.Name,
			Description: &desc,
			InputSchema: in,
			Enabled:     def.RequiredScope != "email:write",
			ConfigKeys:  []string{},
		})
		names = append(names, def.Name)
	}
	if err := s.repo.BulkUpsertTools(ctx, tools); err != nil {
		return fmt.Errorf("upserting source tools: %w", err)
	}
	for _, def := range defs {
		if def.RequiredScope == "email:write" {
			if err := s.repo.SetToolEnabledByServerAndName(ctx, server.ID, def.Name, true); err != nil {
				return err
			}
		}
	}
	if _, err := s.repo.DeleteStaleTools(ctx, server.ID, names); err != nil {
		return fmt.Errorf("pruning source tools: %w", err)
	}
	s.invalidateToolPool(projectID)
	return nil
}

// DeleteSourceServer removes a source's MCP server and its tools.
func (s *Service) DeleteSourceServer(ctx context.Context, projectID, sourceID string) error {
	server, err := s.repo.FindServerBySourceID(ctx, projectID, sourceID)
	if err != nil || server == nil {
		return err
	}
	if err := s.repo.DeleteServer(ctx, server.ID); err != nil {
		return err
	}
	s.invalidateToolPool(projectID)
	return nil
}

// sourceServerName looks up the source's mcp_slug through the sources table.
// It is a small raw query so mcpregistry does not depend on the sources domain.
func sourceServerName(ctx context.Context, s *Service, projectID, sourceID string) string {
	var slug string
	if _, err := uuid.Parse(sourceID); err != nil {
		return ""
	}
	err := s.repo.db.NewRaw(`SELECT COALESCE(mcp_slug,'') FROM kb.sources WHERE id = ? AND project_id = ?`, sourceID, projectID).Scan(ctx, &slug)
	if err != nil {
		return ""
	}
	return slug
}

// EnsureSourceAdminServer creates or updates the project-level source-admin
// server that exposes source-list/get/scope tools to the agent tool pool.
func (s *Service) EnsureSourceAdminServer(ctx context.Context, projectID string, defs []mcp.ToolDefinition) error {
	if len(defs) == 0 {
		return nil
	}
	const name = "source-admin"
	server, err := s.repo.FindServerByName(ctx, projectID, name)
	if err != nil {
		return fmt.Errorf("finding source-admin server: %w", err)
	}
	if server == nil {
		server = &MCPServer{ProjectID: projectID, Name: name, Enabled: true, Type: ServerTypeSource}
		if err := s.repo.CreateServer(ctx, server); err != nil {
			return fmt.Errorf("creating source-admin server: %w", err)
		}
	}
	tools := make([]*MCPServerTool, 0, len(defs))
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		desc := def.Description
		tools = append(tools, &MCPServerTool{
			ServerID: server.ID, ToolName: def.Name, Description: &desc,
			InputSchema: schemaToMap(def.InputSchema), Enabled: true, ConfigKeys: []string{},
		})
		names = append(names, def.Name)
	}
	if err := s.repo.BulkUpsertTools(ctx, tools); err != nil {
		return fmt.Errorf("upserting source-admin tools: %w", err)
	}
	if _, err := s.repo.DeleteStaleTools(ctx, server.ID, names); err != nil {
		return fmt.Errorf("pruning source-admin tools: %w", err)
	}
	s.invalidateToolPool(projectID)
	return nil
}

// DeleteSourceAdminServer removes the project-level source tool server.
func (s *Service) DeleteSourceAdminServer(ctx context.Context, projectID string) error {
	server, err := s.repo.FindServerByName(ctx, projectID, "source-admin")
	if err != nil || server == nil {
		return err
	}
	if err := s.repo.DeleteServer(ctx, server.ID); err != nil {
		return err
	}
	s.invalidateToolPool(projectID)
	return nil
}
