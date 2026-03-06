-- +goose Up
-- Decouple scheduling from agent definitions.
-- Trigger/scheduling config belongs on kb.agents (runtime), not kb.agent_definitions (config).
-- kb.agents already has: cron_schedule, trigger_type, reaction_config.
ALTER TABLE kb.agent_definitions DROP COLUMN IF EXISTS trigger;

-- +goose Down
ALTER TABLE kb.agent_definitions ADD COLUMN trigger text;
