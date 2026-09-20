-- +goose Up
ALTER TABLE kb.mcp_server_tools ADD COLUMN IF NOT EXISTS output_schema JSONB;

-- +goose Down
ALTER TABLE kb.mcp_server_tools DROP COLUMN IF EXISTS output_schema;
