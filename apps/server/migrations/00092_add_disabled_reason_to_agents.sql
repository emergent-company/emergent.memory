-- +goose Up
ALTER TABLE kb.agents ADD COLUMN IF NOT EXISTS disabled_reason TEXT;

-- +goose Down
ALTER TABLE kb.agents DROP COLUMN IF EXISTS disabled_reason;
