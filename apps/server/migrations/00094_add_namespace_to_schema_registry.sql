-- +goose Up
ALTER TABLE kb.project_object_schema_registry ADD COLUMN IF NOT EXISTS namespace TEXT;

-- +goose Down
ALTER TABLE kb.project_object_schema_registry DROP COLUMN IF EXISTS namespace;
