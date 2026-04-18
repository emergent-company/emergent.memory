-- +goose Up
ALTER TABLE kb.agent_runs ADD COLUMN IF NOT EXISTS tools text[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS tools;
