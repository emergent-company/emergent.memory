-- +goose Up
ALTER TABLE kb.agent_definitions ADD COLUMN IF NOT EXISTS banned_tools text[];

-- +goose Down
ALTER TABLE kb.agent_definitions DROP COLUMN IF EXISTS banned_tools;
