-- +goose Up
-- Add unique constraint on (project_id, schema_id) so InstallSchemaToProject's
-- ON CONFLICT clause is valid and schema installation works correctly.
ALTER TABLE kb.project_schemas
    ADD CONSTRAINT project_schemas_project_id_schema_id_key UNIQUE (project_id, schema_id);

-- +goose Down
ALTER TABLE kb.project_schemas
    DROP CONSTRAINT IF EXISTS project_schemas_project_id_schema_id_key;
