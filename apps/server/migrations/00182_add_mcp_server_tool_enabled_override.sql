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

-- +goose StatementBegin
-- Backfill pre-existing explicitly-disabled rows. A disable (`enabled = false`)
-- can only have come from an explicit project toggle (ToggleTool), so it must
-- survive as a project override. `enabled = true` rows are left NULL because
-- they are indistinguishable from EnsureBuiltinServer's bulk upsert. Idempotent:
-- a re-run matches no rows because the backfilled rows now carry
-- enabled_override = false (issue #988).
UPDATE kb.mcp_server_tools
SET enabled_override = false
WHERE enabled_override IS NULL AND enabled = false;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.mcp_server_tools
    DROP COLUMN IF EXISTS enabled_override;
-- +goose StatementEnd
