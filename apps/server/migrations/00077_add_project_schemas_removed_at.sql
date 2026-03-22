-- +goose Up
ALTER TABLE kb.project_schemas
    ADD COLUMN IF NOT EXISTS removed_at TIMESTAMPTZ DEFAULT NULL;

-- +goose Down
ALTER TABLE kb.project_schemas
    DROP COLUMN IF EXISTS removed_at;
