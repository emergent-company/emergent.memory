-- +goose Up
ALTER TABLE kb.agent_definitions ADD COLUMN IF NOT EXISTS auto_load_skills BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE kb.agent_definitions DROP COLUMN IF EXISTS auto_load_skills;
