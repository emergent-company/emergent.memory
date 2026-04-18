-- +goose Up
ALTER TABLE kb.agents ADD COLUMN IF NOT EXISTS agent_definition_id uuid REFERENCES kb.agent_definitions(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE kb.agents DROP COLUMN IF EXISTS agent_definition_id;
