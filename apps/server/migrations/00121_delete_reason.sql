-- +goose Up
ALTER TABLE kb.graph_objects ADD COLUMN IF NOT EXISTS delete_reason text;
ALTER TABLE kb.graph_relationships ADD COLUMN IF NOT EXISTS delete_reason text;

-- +goose Down
ALTER TABLE kb.graph_objects DROP COLUMN IF EXISTS delete_reason;
ALTER TABLE kb.graph_relationships DROP COLUMN IF EXISTS delete_reason;
