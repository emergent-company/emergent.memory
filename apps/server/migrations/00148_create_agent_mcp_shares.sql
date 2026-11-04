-- +goose Up
-- +goose StatementBegin
-- Per-agent MCP share credentials. Each row binds one core.api_tokens
-- credential to exactly one project agent, exposing that agent as a
-- single-tool MCP server at /api/mcp/agents/:agentId.
CREATE TABLE IF NOT EXISTS core.agent_mcp_shares (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL,
    agent_id    UUID NOT NULL,
    token_id    UUID NOT NULL REFERENCES core.api_tokens(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT,
    created_by  UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at  TIMESTAMPTZ
);

-- One active share per (project, lowercased name). Revoked shares release
-- their name for reuse.
CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_mcp_shares_project_name
    ON core.agent_mcp_shares (project_id, lower(name))
    WHERE revoked_at IS NULL;

-- Hot path: resolve a share from AuthUser.APITokenID on every endpoint request.
-- Partial UNIQUE (active rows only) so one token can back at most one active
-- share, while revoked rows release the token for reuse.
CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_mcp_shares_active_token
    ON core.agent_mcp_shares (token_id)
    WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_agent_mcp_shares_project_agent
    ON core.agent_mcp_shares (project_id, agent_id);

COMMENT ON TABLE core.agent_mcp_shares IS 'Per-agent MCP share credentials binding an API token to one agent';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS core.agent_mcp_shares;
-- +goose StatementEnd
