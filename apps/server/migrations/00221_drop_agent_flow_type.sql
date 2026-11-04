-- +goose Up
ALTER TABLE kb.agent_definitions DROP COLUMN flow_type;

-- +goose Down
ALTER TABLE kb.agent_definitions ADD COLUMN flow_type VARCHAR(50) NOT NULL DEFAULT 'single';
