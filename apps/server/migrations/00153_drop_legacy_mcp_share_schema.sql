-- +goose Up
-- +goose StatementBegin
-- Drop the superseded legacy per-agent MCP share schema. 00152 already
-- backfilled core.agent_mcp_endpoints/core.agent_mcp_keys from
-- core.agent_mcp_shares, and no code path reads either object any more:
--   * core.agent_mcp_shares  — superseded by agent_mcp_endpoints + agent_mcp_keys.
--   * core.mcp_share_instances.allowed_agents — project share instances are
--     tools-only now; the column is unmapped by the Go model and store.
-- The legacy read-only /mcp/share flow (core.mcp_share_instances.is_legacy,
-- HandleShareMCPAccess, recordLegacyShareInstance) is unaffected.
DROP TABLE IF EXISTS core.agent_mcp_shares;

ALTER TABLE core.mcp_share_instances DROP COLUMN IF EXISTS allowed_agents;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Restore core.mcp_share_instances.allowed_agents with its original comment.
ALTER TABLE core.mcp_share_instances
    ADD COLUMN IF NOT EXISTS allowed_agents UUID[];

COMMENT ON COLUMN core.mcp_share_instances.allowed_agents IS 'Explicit agent allowlist (kb.agents.id); NULL means all scope-permitted agents';

-- Recreate core.agent_mcp_shares exactly as 00148 defines it.
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
