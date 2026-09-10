-- +goose Up
-- +goose StatementBegin
-- Named, project-scoped MCP share instances. Each row binds one core.api_tokens
-- credential to an explicit tool allowlist and an optional agent allowlist.
-- NULL allowlists mean "unrestricted" (all scope-permitted entries), matching
-- legacy share tokens.
CREATE TABLE IF NOT EXISTS core.mcp_share_instances (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     UUID NOT NULL,
    name           TEXT NOT NULL,
    description    TEXT,
    token_id       UUID NOT NULL REFERENCES core.api_tokens(id) ON DELETE CASCADE,
    allowed_tools  TEXT[],
    allowed_agents UUID[],
    is_legacy      BOOLEAN NOT NULL DEFAULT false,
    created_by     UUID,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at     TIMESTAMPTZ
);

-- One active instance per (project, lowercased name). Revoked instances release
-- their name for reuse.
CREATE UNIQUE INDEX IF NOT EXISTS uq_mcp_share_instances_project_name
    ON core.mcp_share_instances (project_id, lower(name))
    WHERE revoked_at IS NULL;

-- Hot path: resolve an instance from AuthUser.APITokenID on every MCP request.
CREATE INDEX IF NOT EXISTS idx_mcp_share_instances_token
    ON core.mcp_share_instances (token_id);

CREATE INDEX IF NOT EXISTS idx_mcp_share_instances_project
    ON core.mcp_share_instances (project_id);

COMMENT ON TABLE core.mcp_share_instances IS 'Named MCP access grants binding an API token to tool/agent allowlists';
COMMENT ON COLUMN core.mcp_share_instances.allowed_tools IS 'Explicit memory-tool allowlist; NULL means all scope-permitted tools';
COMMENT ON COLUMN core.mcp_share_instances.allowed_agents IS 'Explicit agent allowlist (kb.agents.id); NULL means all scope-permitted agents';
COMMENT ON COLUMN core.mcp_share_instances.is_legacy IS 'True for instances recorded from the legacy /mcp/share flow';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS core.mcp_share_instances;
-- +goose StatementEnd
