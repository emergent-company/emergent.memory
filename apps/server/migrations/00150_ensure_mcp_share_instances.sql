-- +goose Up
-- +goose StatementBegin
-- Defensive re-creation of core.mcp_share_instances.
--
-- Why this exists: migration 00144_add_mcp_share_instances.sql can be silently
-- skipped on environments that had already recorded Goose version 144 from a
-- different migration that was later renumbered (see 00145). Goose de-duplicates
-- by version number, so such environments never created this table even though
-- version 144 shows as applied. This migration is idempotent and guarantees the
-- object exists everywhere regardless of that history.
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

CREATE UNIQUE INDEX IF NOT EXISTS uq_mcp_share_instances_project_name
    ON core.mcp_share_instances (project_id, lower(name))
    WHERE revoked_at IS NULL;

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
-- Intentionally a no-op: 00144 owns the lifecycle of this table. Dropping it
-- here would break environments where 00144 was applied normally.
SELECT 1;
-- +goose StatementEnd
