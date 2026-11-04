-- +goose Up
ALTER TABLE kb.agent_runs ADD COLUMN IF NOT EXISTS model TEXT;

-- +goose Down
ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS model;
