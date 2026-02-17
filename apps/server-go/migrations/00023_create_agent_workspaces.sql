-- +goose Up
-- +goose StatementBegin

-- Agent Workspaces: isolated execution environments for AI agents and persistent MCP server containers.
-- Supports three providers (Firecracker, E2B, gVisor) with both ephemeral and persistent lifecycles.
CREATE TABLE IF NOT EXISTS kb.agent_workspaces (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_session_id      UUID,
    container_type        TEXT NOT NULL,            -- 'agent_workspace' or 'mcp_server'
    provider              TEXT NOT NULL,            -- 'firecracker', 'e2b', or 'gvisor'
    provider_workspace_id TEXT NOT NULL DEFAULT '', -- Provider's internal container/VM ID
    repository_url        TEXT,
    branch                TEXT,
    deployment_mode       TEXT NOT NULL DEFAULT 'self-hosted', -- 'managed' or 'self-hosted'
    lifecycle             TEXT NOT NULL DEFAULT 'ephemeral',   -- 'ephemeral' or 'persistent'
    status                TEXT NOT NULL DEFAULT 'creating',    -- 'creating', 'ready', 'stopping', 'stopped', 'error'
    created_at            TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    last_used_at          TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    expires_at            TIMESTAMPTZ,             -- TTL for auto-cleanup; NULL for persistent MCP servers
    resource_limits       JSONB DEFAULT '{}',      -- {cpu: "2", memory: "4G", disk: "10G"}
    snapshot_id           TEXT,                     -- For snapshot-based persistence
    mcp_config            JSONB,                   -- For MCP servers: {name, image, stdio_bridge, restart_policy, environment}
    metadata              JSONB DEFAULT '{}'        -- Provider-specific data
);

COMMENT ON TABLE kb.agent_workspaces IS 'Tracks isolated agent workspaces and persistent MCP server containers';
COMMENT ON COLUMN kb.agent_workspaces.container_type IS 'Type of container: agent_workspace (ephemeral compute) or mcp_server (persistent daemon)';
COMMENT ON COLUMN kb.agent_workspaces.provider IS 'Sandbox provider: firecracker (microVM), e2b (managed), or gvisor (Docker runtime)';
COMMENT ON COLUMN kb.agent_workspaces.lifecycle IS 'Lifecycle mode: ephemeral (session-scoped) or persistent (daemon)';
COMMENT ON COLUMN kb.agent_workspaces.mcp_config IS 'MCP server configuration including stdio bridge settings and restart policy';

-- Index for finding active workspaces by session
CREATE INDEX IF NOT EXISTS idx_agent_workspaces_session
    ON kb.agent_workspaces(agent_session_id)
    WHERE agent_session_id IS NOT NULL;

-- Index for finding workspaces by status (for health monitoring)
CREATE INDEX IF NOT EXISTS idx_agent_workspaces_status
    ON kb.agent_workspaces(status);

-- Index for TTL cleanup queries: find expired ephemeral workspaces
CREATE INDEX IF NOT EXISTS idx_agent_workspaces_expires
    ON kb.agent_workspaces(expires_at)
    WHERE expires_at IS NOT NULL;

-- Partial index for finding persistent MCP servers that should be running
CREATE INDEX IF NOT EXISTS idx_agent_workspaces_persistent_mcp
    ON kb.agent_workspaces(container_type, lifecycle, status)
    WHERE container_type = 'mcp_server' AND lifecycle = 'persistent';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.agent_workspaces;
-- +goose StatementEnd
