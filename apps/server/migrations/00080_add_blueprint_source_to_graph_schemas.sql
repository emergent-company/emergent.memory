-- +goose Up
ALTER TABLE kb.graph_schemas ADD COLUMN IF NOT EXISTS blueprint_source text;

-- +goose Down
ALTER TABLE kb.graph_schemas DROP COLUMN IF EXISTS blueprint_source;
