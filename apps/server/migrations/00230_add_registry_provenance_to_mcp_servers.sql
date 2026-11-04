-- +goose Up
-- +goose StatementBegin
-- Records where a registry-installed MCP server came from, so the configure form
-- can link to the provider's credential docs when a required key is still blank.
-- Nullable: manual creates carry no provenance and simply render no link.
ALTER TABLE kb.mcp_servers ADD COLUMN IF NOT EXISTS registry_name text;
ALTER TABLE kb.mcp_servers ADD COLUMN IF NOT EXISTS registry_repository_url text;
COMMENT ON COLUMN kb.mcp_servers.registry_name IS 'registry server name a row was installed from; NULL for manual creates';
COMMENT ON COLUMN kb.mcp_servers.registry_repository_url IS 'source repository URL from the registry entry; backs the configure form docs link';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.mcp_servers DROP COLUMN IF EXISTS registry_repository_url;
ALTER TABLE kb.mcp_servers DROP COLUMN IF EXISTS registry_name;
-- +goose StatementEnd
