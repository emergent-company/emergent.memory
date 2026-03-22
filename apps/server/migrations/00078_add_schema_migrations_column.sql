-- +goose Up
ALTER TABLE kb.graph_schemas
    ADD COLUMN IF NOT EXISTS migrations JSONB;

-- +goose Down
ALTER TABLE kb.graph_schemas
    DROP COLUMN IF EXISTS migrations;
