-- +goose Up
-- +goose StatementBegin
ALTER TABLE kb.mcp_server_tools
    ADD COLUMN IF NOT EXISTS config JSONB;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.mcp_server_tools
    DROP COLUMN IF EXISTS config;
-- +goose StatementEnd
