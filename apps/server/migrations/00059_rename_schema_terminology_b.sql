-- +goose Up
-- +goose StatementBegin
-- Rename tables
ALTER TABLE kb.agent_workspaces RENAME TO agent_sandboxes;
ALTER TABLE kb.workspace_images RENAME TO sandbox_images;
-- +goose StatementEnd

-- +goose StatementBegin
-- Rename column workspace_config → sandbox_config on agent_definitions
ALTER TABLE kb.agent_definitions RENAME COLUMN workspace_config TO sandbox_config;
-- +goose StatementEnd

-- +goose StatementBegin
-- Update container_type data value
UPDATE kb.agent_sandboxes SET container_type = 'agent_sandbox' WHERE container_type = 'agent_workspace';
-- +goose StatementEnd

-- +goose StatementBegin
SET search_path TO kb;
-- Rename primary key indexes
ALTER INDEX agent_workspaces_pkey RENAME TO agent_sandboxes_pkey;
ALTER INDEX workspace_images_pkey RENAME TO sandbox_images_pkey;
-- Rename indexes on agent_sandboxes
ALTER INDEX idx_agent_workspaces_session RENAME TO idx_agent_sandboxes_session;
ALTER INDEX idx_agent_workspaces_status RENAME TO idx_agent_sandboxes_status;
ALTER INDEX idx_agent_workspaces_expires RENAME TO idx_agent_sandboxes_expires;
ALTER INDEX idx_agent_workspaces_persistent_mcp RENAME TO idx_agent_sandboxes_persistent_mcp;
-- Rename indexes on sandbox_images
ALTER INDEX idx_workspace_images_project_name RENAME TO idx_sandbox_images_project_name;
ALTER INDEX idx_workspace_images_project_id RENAME TO idx_sandbox_images_project_id;
RESET search_path;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kb;
-- Reverse: rename indexes back
ALTER INDEX idx_sandbox_images_project_name RENAME TO idx_workspace_images_project_name;
ALTER INDEX idx_sandbox_images_project_id RENAME TO idx_workspace_images_project_id;
ALTER INDEX idx_agent_sandboxes_session RENAME TO idx_agent_workspaces_session;
ALTER INDEX idx_agent_sandboxes_status RENAME TO idx_agent_workspaces_status;
ALTER INDEX idx_agent_sandboxes_expires RENAME TO idx_agent_workspaces_expires;
ALTER INDEX idx_agent_sandboxes_persistent_mcp RENAME TO idx_agent_workspaces_persistent_mcp;
-- Reverse: rename primary key indexes back
ALTER INDEX agent_sandboxes_pkey RENAME TO agent_workspaces_pkey;
ALTER INDEX sandbox_images_pkey RENAME TO workspace_images_pkey;
RESET search_path;
-- +goose StatementEnd

-- +goose StatementBegin
-- Reverse: update container_type data value
UPDATE kb.agent_sandboxes SET container_type = 'agent_workspace' WHERE container_type = 'agent_sandbox';
-- +goose StatementEnd

-- +goose StatementBegin
-- Reverse: rename column back
ALTER TABLE kb.agent_definitions RENAME COLUMN sandbox_config TO workspace_config;
-- +goose StatementEnd

-- +goose StatementBegin
-- Reverse: rename tables back
ALTER TABLE kb.sandbox_images RENAME TO workspace_images;
ALTER TABLE kb.agent_sandboxes RENAME TO agent_workspaces;
-- +goose StatementEnd
