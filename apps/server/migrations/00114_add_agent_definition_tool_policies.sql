-- +goose Up
ALTER TABLE kb.agent_definitions
    ADD COLUMN IF NOT EXISTS tool_policies jsonb NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE kb.agent_definitions
    DROP COLUMN IF EXISTS tool_policies;
