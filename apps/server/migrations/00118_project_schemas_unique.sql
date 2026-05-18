-- +goose Up
-- Remove duplicate (project_id, schema_id) rows before adding unique constraint,
-- keeping the row with the lowest id in each group.
DELETE FROM kb.project_schemas
WHERE id NOT IN (
    SELECT MIN(id)
    FROM kb.project_schemas
    GROUP BY project_id, schema_id
);

-- Add unique constraint on (project_id, schema_id) so InstallSchemaToProject's
-- ON CONFLICT clause is valid and schema installation works correctly.
ALTER TABLE kb.project_schemas
    ADD CONSTRAINT project_schemas_project_id_schema_id_key UNIQUE (project_id, schema_id);

-- +goose Down
ALTER TABLE kb.project_schemas
    DROP CONSTRAINT IF EXISTS project_schemas_project_id_schema_id_key;
