-- +goose Up
-- +goose StatementBegin
-- Distinguishes an explicit project-level enable/disable from the bulk-upserted
-- default `enabled = true` that EnsureBuiltinServer materialises for every builtin
-- tool. NULL means "no project override" (fall through to org default, then the
-- builtin default); non-NULL means the project explicitly toggled the tool
-- (issue #988).
ALTER TABLE kb.mcp_server_tools
    ADD COLUMN IF NOT EXISTS enabled_override BOOLEAN;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.mcp_server_tools
    DROP COLUMN IF EXISTS enabled_override;
-- +goose StatementEnd
