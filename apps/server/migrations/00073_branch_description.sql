-- +goose Up
ALTER TABLE kb.branches ADD COLUMN IF NOT EXISTS description TEXT;

-- +goose Down
ALTER TABLE kb.branches DROP COLUMN IF EXISTS description;
