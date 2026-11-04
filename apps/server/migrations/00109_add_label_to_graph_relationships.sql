-- +goose Up
-- +goose StatementBegin
ALTER TABLE kb.graph_relationships ADD COLUMN IF NOT EXISTS label TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.graph_relationships DROP COLUMN IF EXISTS label;
-- +goose StatementEnd
