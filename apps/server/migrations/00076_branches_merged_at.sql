-- +goose Up
ALTER TABLE kb.branches ADD COLUMN IF NOT EXISTS merged_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE kb.branches DROP COLUMN IF EXISTS merged_at;
