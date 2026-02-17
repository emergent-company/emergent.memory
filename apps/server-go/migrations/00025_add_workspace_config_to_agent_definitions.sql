-- +goose Up
-- +goose StatementBegin
ALTER TABLE kb.agent_definitions
    ADD COLUMN IF NOT EXISTS workspace_config JSONB DEFAULT NULL;

COMMENT ON COLUMN kb.agent_definitions.workspace_config IS 'Declarative workspace configuration: enabled, repo_source, tools, resource_limits, setup_commands, base_image';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.agent_definitions
    DROP COLUMN IF EXISTS workspace_config;
-- +goose StatementEnd
