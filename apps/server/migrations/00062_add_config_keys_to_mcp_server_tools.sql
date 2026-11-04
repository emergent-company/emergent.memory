-- +goose Up
ALTER TABLE kb.mcp_server_tools ADD COLUMN IF NOT EXISTS config_keys TEXT[] DEFAULT '{}';

-- +goose Down
ALTER TABLE kb.mcp_server_tools DROP COLUMN IF EXISTS config_keys;
