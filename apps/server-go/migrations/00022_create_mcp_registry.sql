-- +goose Up
-- +goose StatementBegin

-- MCP Server Registry: centralized registry of MCP servers (builtin + external).
-- Each server configuration is project-scoped and can be of type: builtin, stdio, sse, http.
-- External servers are connected via their transport protocol and their tools are
-- discovered via tools/list. Builtin tools are registered as type 'builtin'.

CREATE TABLE IF NOT EXISTS kb.mcp_servers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID NOT NULL,
    name            VARCHAR(255) NOT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    type            VARCHAR(50) NOT NULL,           -- builtin, stdio, sse, http
    command         TEXT,                             -- for stdio type
    args            TEXT[] DEFAULT '{}',              -- for stdio type
    env             JSONB DEFAULT '{}',               -- environment variables
    url             TEXT,                             -- for sse/http type
    headers         JSONB DEFAULT '{}',               -- for sse/http type
    created_at      TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT current_timestamp
);

-- Unique constraint: one server per name per project
CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_servers_project_name
    ON kb.mcp_servers(project_id, name);

-- Index for project-scoped lookups
CREATE INDEX IF NOT EXISTS idx_mcp_servers_project_id
    ON kb.mcp_servers(project_id);

-- Index for enabled server lookups
CREATE INDEX IF NOT EXISTS idx_mcp_servers_project_enabled
    ON kb.mcp_servers(project_id, enabled)
    WHERE enabled = true;

COMMENT ON TABLE kb.mcp_servers IS 'Registry of MCP servers (builtin and external) per project';
COMMENT ON COLUMN kb.mcp_servers.type IS 'Server transport type: builtin, stdio, sse, http';
COMMENT ON COLUMN kb.mcp_servers.command IS 'Command to execute for stdio-type servers';
COMMENT ON COLUMN kb.mcp_servers.args IS 'Command arguments for stdio-type servers';
COMMENT ON COLUMN kb.mcp_servers.env IS 'Environment variables passed to the server process';
COMMENT ON COLUMN kb.mcp_servers.url IS 'Endpoint URL for sse/http-type servers';
COMMENT ON COLUMN kb.mcp_servers.headers IS 'HTTP headers for sse/http-type servers';

-- MCP Server Tools: cached tool metadata discovered from each server.
-- Populated via tools/list for external servers, or directly registered for builtins.
-- Provides per-tool enable/disable granularity.

CREATE TABLE IF NOT EXISTS kb.mcp_server_tools (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id       UUID NOT NULL REFERENCES kb.mcp_servers(id) ON DELETE CASCADE,
    tool_name       VARCHAR(255) NOT NULL,
    description     TEXT,
    input_schema    JSONB DEFAULT '{}',
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT current_timestamp
);

-- Unique constraint: one tool per name per server
CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_server_tools_server_name
    ON kb.mcp_server_tools(server_id, tool_name);

-- Index for server-scoped lookups
CREATE INDEX IF NOT EXISTS idx_mcp_server_tools_server_id
    ON kb.mcp_server_tools(server_id);

-- Index for enabled tool lookups
CREATE INDEX IF NOT EXISTS idx_mcp_server_tools_enabled
    ON kb.mcp_server_tools(server_id, enabled)
    WHERE enabled = true;

COMMENT ON TABLE kb.mcp_server_tools IS 'Cached tool definitions from MCP servers with per-tool enable/disable';
COMMENT ON COLUMN kb.mcp_server_tools.tool_name IS 'Tool name as reported by the MCP server (unprefixed)';
COMMENT ON COLUMN kb.mcp_server_tools.description IS 'Tool description for display and agent introspection';
COMMENT ON COLUMN kb.mcp_server_tools.input_schema IS 'JSON Schema for tool input parameters';
COMMENT ON COLUMN kb.mcp_server_tools.enabled IS 'Whether this tool is available for use (per-tool toggle)';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.mcp_server_tools;
DROP TABLE IF EXISTS kb.mcp_servers;
-- +goose StatementEnd
