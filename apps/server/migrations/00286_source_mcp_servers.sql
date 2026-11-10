-- +goose Up
-- +goose StatementBegin

-- Per-account source MCP servers. A Gmail data source materializes one
-- kb.mcp_servers row of type 'source' linked to that source. The row gives the
-- account its own tool namespace; tools are also exposed through the official
-- /api/mcp surface and the agent tool pool.
ALTER TABLE kb.mcp_servers ADD COLUMN IF NOT EXISTS source_id uuid NULL;

ALTER TABLE kb.mcp_servers
    DROP CONSTRAINT IF EXISTS mcp_servers_source_id_fkey;
ALTER TABLE kb.mcp_servers
    ADD CONSTRAINT mcp_servers_source_id_fkey
    FOREIGN KEY (source_id) REFERENCES kb.sources(id) ON DELETE CASCADE;

CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_servers_source_id
    ON kb.mcp_servers (source_id)
    WHERE source_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_mcp_servers_source_id;
ALTER TABLE kb.mcp_servers DROP CONSTRAINT IF EXISTS mcp_servers_source_id_fkey;
ALTER TABLE kb.mcp_servers DROP COLUMN IF EXISTS source_id;

-- +goose StatementEnd
