-- +goose Up
ALTER TABLE kb.agent_definitions ADD COLUMN enabled boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE kb.agent_definitions DROP COLUMN enabled;
