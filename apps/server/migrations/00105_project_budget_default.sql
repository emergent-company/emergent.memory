-- +goose Up
-- +goose StatementBegin

-- Set a column-level default so all INSERT paths (including raw SQL in standalone
-- bootstrap and MCP tool) automatically get $10 without any application-layer change.
ALTER TABLE kb.projects
    ALTER COLUMN budget_usd SET DEFAULT 10.0;

-- Backfill existing projects that were created before migration 00063 or via paths
-- that bypassed the service layer (standalone bootstrap, MCP project-create tool).
UPDATE kb.projects
SET budget_usd = 10.0
WHERE budget_usd IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.projects
    ALTER COLUMN budget_usd DROP DEFAULT;
-- +goose StatementEnd
