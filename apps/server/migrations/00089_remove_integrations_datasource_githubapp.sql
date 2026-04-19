-- +goose Up
-- +goose StatementBegin

-- 1. Drop columns from kb.documents that reference data_source_integrations
ALTER TABLE kb.documents
    DROP COLUMN IF EXISTS data_source_integration_id,
    DROP COLUMN IF EXISTS external_source_id,
    DROP COLUMN IF EXISTS sync_version,
    DROP COLUMN IF EXISTS integration_metadata;

-- 2. Drop clickup tables (FK to kb.integrations)
DROP TABLE IF EXISTS kb.clickup_import_logs;
DROP TABLE IF EXISTS kb.clickup_sync_state;

-- 3. Drop data_source_sync_jobs (FK to kb.data_source_integrations)
DROP TABLE IF EXISTS kb.data_source_sync_jobs;

-- 4. Drop data_source_integrations + its trigger
DROP TRIGGER IF EXISTS update_data_source_integrations_updated_at ON kb.data_source_integrations;
DROP FUNCTION IF EXISTS kb.update_data_source_integrations_updated_at();
DROP TABLE IF EXISTS kb.data_source_integrations;

-- 5. Drop integrations table
DROP TABLE IF EXISTS kb.integrations;

-- 6. Drop github_app_config table
DROP TABLE IF EXISTS core.github_app_config;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Down migration not provided: these tables are being permanently removed
-- +goose StatementEnd
