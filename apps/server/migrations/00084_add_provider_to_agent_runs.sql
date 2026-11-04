-- +goose Up
ALTER TABLE kb.agent_runs ADD COLUMN IF NOT EXISTS provider TEXT;

-- +goose Down
ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS provider;
