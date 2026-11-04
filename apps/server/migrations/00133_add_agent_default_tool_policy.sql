-- +goose Up
ALTER TABLE kb.agent_definitions ADD COLUMN IF NOT EXISTS default_tool_policy TEXT NOT NULL DEFAULT 'allow';

-- +goose Down
ALTER TABLE kb.agent_definitions DROP COLUMN IF EXISTS default_tool_policy;
