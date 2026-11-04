-- +goose Up
ALTER TABLE kb.mcp_servers ADD COLUMN IF NOT EXISTS description TEXT;

-- +goose Down
ALTER TABLE kb.mcp_servers DROP COLUMN IF EXISTS description;
