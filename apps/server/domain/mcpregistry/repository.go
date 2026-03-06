package mcpregistry

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

// Repository handles database operations for MCP server registry.
type Repository struct {
	db bun.IDB
}

// NewRepository creates a new MCP registry repository.
func NewRepository(db bun.IDB) *Repository {
	return &Repository{db: db}
}

// --- MCP Servers ---

// FindAllServers returns all MCP servers for a project.
func (r *Repository) FindAllServers(ctx context.Context, projectID string) ([]*MCPServer, error) {
	var servers []*MCPServer
	err := r.db.NewSelect().
		Model(&servers).
		Where("project_id = ?", projectID).
		Order("name ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return servers, nil
}

// FindEnabledServers returns all enabled MCP servers for a project.
func (r *Repository) FindEnabledServers(ctx context.Context, projectID string) ([]*MCPServer, error) {
	var servers []*MCPServer
	err := r.db.NewSelect().
		Model(&servers).
		Where("project_id = ?", projectID).
		Where("enabled = true").
		Order("name ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return servers, nil
}

// FindServerByID returns an MCP server by ID, optionally filtering by project.
func (r *Repository) FindServerByID(ctx context.Context, id string, projectID *string) (*MCPServer, error) {
	server := new(MCPServer)
	q := r.db.NewSelect().
		Model(server).
		Where("ms.id = ?", id)
	if projectID != nil {
		q = q.Where("ms.project_id = ?", *projectID)
	}
	err := q.Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return server, nil
}

// FindServerByIDWithTools returns an MCP server by ID with its tools loaded.
func (r *Repository) FindServerByIDWithTools(ctx context.Context, id string, projectID *string) (*MCPServer, error) {
	server := new(MCPServer)
	q := r.db.NewSelect().
		Model(server).
		Relation("Tools").
		Where("ms.id = ?", id)
	if projectID != nil {
		q = q.Where("ms.project_id = ?", *projectID)
	}
	err := q.Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return server, nil
}

// FindServerByName returns an MCP server by name within a project.
func (r *Repository) FindServerByName(ctx context.Context, projectID, name string) (*MCPServer, error) {
	server := new(MCPServer)
	err := r.db.NewSelect().
		Model(server).
		Where("project_id = ?", projectID).
		Where("name = ?", name).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return server, nil
}

// CreateServer creates a new MCP server.
func (r *Repository) CreateServer(ctx context.Context, server *MCPServer) error {
	_, err := r.db.NewInsert().
		Model(server).
		Returning("*").
		Exec(ctx)
	return err
}

// UpdateServer updates an MCP server.
func (r *Repository) UpdateServer(ctx context.Context, server *MCPServer) error {
	server.UpdatedAt = time.Now()
	_, err := r.db.NewUpdate().
		Model(server).
		WherePK().
		Returning("*").
		Exec(ctx)
	return err
}

// DeleteServer deletes an MCP server by ID (cascades to tools).
func (r *Repository) DeleteServer(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().
		Model((*MCPServer)(nil)).
		Where("id = ?", id).
		Exec(ctx)
	return err
}

// --- MCP Server Tools ---

// FindToolsByServerID returns all tools for a server.
func (r *Repository) FindToolsByServerID(ctx context.Context, serverID string) ([]*MCPServerTool, error) {
	var tools []*MCPServerTool
	err := r.db.NewSelect().
		Model(&tools).
		Where("server_id = ?", serverID).
		Order("tool_name ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return tools, nil
}

// FindEnabledToolsByServerID returns all enabled tools for a server.
func (r *Repository) FindEnabledToolsByServerID(ctx context.Context, serverID string) ([]*MCPServerTool, error) {
	var tools []*MCPServerTool
	err := r.db.NewSelect().
		Model(&tools).
		Where("server_id = ?", serverID).
		Where("enabled = true").
		Order("tool_name ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return tools, nil
}

// FindToolByID returns a tool by ID.
func (r *Repository) FindToolByID(ctx context.Context, id string) (*MCPServerTool, error) {
	tool := new(MCPServerTool)
	err := r.db.NewSelect().
		Model(tool).
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return tool, nil
}

// FindToolByServerAndName returns a tool by server ID and tool name.
func (r *Repository) FindToolByServerAndName(ctx context.Context, serverID, toolName string) (*MCPServerTool, error) {
	tool := new(MCPServerTool)
	err := r.db.NewSelect().
		Model(tool).
		Where("server_id = ?", serverID).
		Where("tool_name = ?", toolName).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return tool, nil
}

// UpsertTool creates or updates a tool for a server (by server_id + tool_name).
func (r *Repository) UpsertTool(ctx context.Context, tool *MCPServerTool) error {
	_, err := r.db.NewInsert().
		Model(tool).
		On("CONFLICT (server_id, tool_name) DO UPDATE").
		Set("description = EXCLUDED.description").
		Set("input_schema = EXCLUDED.input_schema").
		Set("enabled = EXCLUDED.enabled").
		Returning("*").
		Exec(ctx)
	return err
}

// BulkUpsertTools creates or updates multiple tools in a single transaction.
func (r *Repository) BulkUpsertTools(ctx context.Context, tools []*MCPServerTool) error {
	if len(tools) == 0 {
		return nil
	}
	_, err := r.db.NewInsert().
		Model(&tools).
		On("CONFLICT (server_id, tool_name) DO UPDATE").
		Set("description = EXCLUDED.description").
		Set("input_schema = EXCLUDED.input_schema").
		Returning("*").
		Exec(ctx)
	return err
}

// UpdateToolEnabled updates the enabled status of a tool.
func (r *Repository) UpdateToolEnabled(ctx context.Context, id string, enabled bool) error {
	_, err := r.db.NewUpdate().
		Model((*MCPServerTool)(nil)).
		Set("enabled = ?", enabled).
		Where("id = ?", id).
		Exec(ctx)
	return err
}

// DeleteToolsByServerID removes all tools for a server.
func (r *Repository) DeleteToolsByServerID(ctx context.Context, serverID string) error {
	_, err := r.db.NewDelete().
		Model((*MCPServerTool)(nil)).
		Where("server_id = ?", serverID).
		Exec(ctx)
	return err
}

// DeleteStaleTools removes tools that no longer exist on the server.
// toolNames is the current set of tool names from tools/list.
func (r *Repository) DeleteStaleTools(ctx context.Context, serverID string, currentToolNames []string) (int, error) {
	if len(currentToolNames) == 0 {
		// If the server reports zero tools, delete all
		res, err := r.db.NewDelete().
			Model((*MCPServerTool)(nil)).
			Where("server_id = ?", serverID).
			Exec(ctx)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		return int(n), nil
	}

	res, err := r.db.NewDelete().
		Model((*MCPServerTool)(nil)).
		Where("server_id = ?", serverID).
		Where("tool_name NOT IN (?)", bun.In(currentToolNames)).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// --- Aggregated queries for ToolPool integration ---

// EnabledServerTool is a flat row combining server + tool info for ToolPool cache building.
type EnabledServerTool struct {
	ServerName  string         `bun:"server_name"`
	ServerType  MCPServerType  `bun:"server_type"`
	ToolName    string         `bun:"tool_name"`
	Description *string        `bun:"description"`
	InputSchema map[string]any `bun:"input_schema"`
}

// FindAllEnabledTools returns all enabled tools from enabled servers for a project.
// This is the primary query used by ToolPool.buildCache() to load external MCP tools.
func (r *Repository) FindAllEnabledTools(ctx context.Context, projectID string) ([]*EnabledServerTool, error) {
	var tools []*EnabledServerTool
	err := r.db.NewSelect().
		TableExpr("kb.mcp_server_tools AS mst").
		Join("JOIN kb.mcp_servers AS ms ON ms.id = mst.server_id").
		ColumnExpr("ms.name AS server_name").
		ColumnExpr("ms.type AS server_type").
		ColumnExpr("mst.tool_name AS tool_name").
		ColumnExpr("mst.description AS description").
		ColumnExpr("mst.input_schema AS input_schema").
		Where("ms.project_id = ?", projectID).
		Where("ms.enabled = true").
		Where("mst.enabled = true").
		Where("ms.type != ?", ServerTypeBuiltin). // exclude builtins — they come from mcp.Service
		Order("ms.name ASC", "mst.tool_name ASC").
		Scan(ctx, &tools)
	if err != nil {
		return nil, err
	}
	return tools, nil
}

// CountToolsByServerID returns the count of tools for a server.
func (r *Repository) CountToolsByServerID(ctx context.Context, serverID string) (int, error) {
	return r.db.NewSelect().
		Model((*MCPServerTool)(nil)).
		Where("server_id = ?", serverID).
		Count(ctx)
}
