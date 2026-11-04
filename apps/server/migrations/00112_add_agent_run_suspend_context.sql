-- +goose Up
ALTER TABLE kb.agent_runs ADD COLUMN IF NOT EXISTS suspend_context jsonb;

-- +goose Down
ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS suspend_context;
