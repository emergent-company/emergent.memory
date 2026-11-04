-- +goose Up
-- Per-type object schema version history.
--
-- kb.project_object_schema_registry keeps a single live row per
-- (project_id, type_name); schema_version is bumped in place and the prior
-- json_schema is overwritten. This migration persists a snapshot per version
-- and maintains it via trigger so every write path (including paths that
-- mutate json_schema without bumping schema_version) is captured.

CREATE TABLE IF NOT EXISTS kb.project_object_schema_registry_versions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    registry_id UUID NOT NULL REFERENCES kb.project_object_schema_registry(id) ON DELETE CASCADE,
    project_id UUID NOT NULL,
    type_name TEXT NOT NULL,
    schema_version INT NOT NULL,
    source TEXT NOT NULL,
    schema_id UUID,
    json_schema JSONB NOT NULL,
    ui_config JSONB,
    extraction_config JSONB,
    enabled BOOLEAN NOT NULL DEFAULT true,
    discovery_confidence DOUBLE PRECISION,
    description TEXT,
    namespace TEXT,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT project_object_schema_registry_versions_registry_version_key
        UNIQUE (registry_id, schema_version)
);

CREATE INDEX IF NOT EXISTS idx_object_type_versions_project_type
    ON kb.project_object_schema_registry_versions (project_id, type_name, schema_version DESC);

-- Backfill: one snapshot per existing row at its CURRENT schema_version.
INSERT INTO kb.project_object_schema_registry_versions
    (registry_id, project_id, type_name, schema_version, source, schema_id,
     json_schema, ui_config, extraction_config, enabled, discovery_confidence,
     description, namespace, created_by, created_at)
SELECT id, project_id, type_name, schema_version, source, schema_id,
       json_schema, ui_config, extraction_config, enabled, discovery_confidence,
       description, namespace, created_by, updated_at
FROM kb.project_object_schema_registry;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kb.snapshot_object_type_version() RETURNS trigger AS $$
BEGIN
    IF (TG_OP = 'INSERT')
       OR (NEW.schema_version IS DISTINCT FROM OLD.schema_version)
       OR (NEW.json_schema IS DISTINCT FROM OLD.json_schema)
       OR (NEW.ui_config IS DISTINCT FROM OLD.ui_config)
       OR (NEW.extraction_config IS DISTINCT FROM OLD.extraction_config) THEN
        INSERT INTO kb.project_object_schema_registry_versions
            (registry_id, project_id, type_name, schema_version, source, schema_id,
             json_schema, ui_config, extraction_config, enabled, discovery_confidence,
             description, namespace, created_by, created_at)
        VALUES
            (NEW.id, NEW.project_id, NEW.type_name, NEW.schema_version, NEW.source, NEW.schema_id,
             NEW.json_schema, NEW.ui_config, NEW.extraction_config, NEW.enabled, NEW.discovery_confidence,
             NEW.description, NEW.namespace, NEW.created_by, COALESCE(NEW.updated_at, NEW.created_at, now()))
        ON CONFLICT (registry_id, schema_version) DO UPDATE SET
            json_schema = EXCLUDED.json_schema,
            ui_config = EXCLUDED.ui_config,
            extraction_config = EXCLUDED.extraction_config,
            enabled = EXCLUDED.enabled,
            description = EXCLUDED.description,
            namespace = EXCLUDED.namespace,
            created_at = EXCLUDED.created_at;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS trg_project_object_schema_registry_version ON kb.project_object_schema_registry;
CREATE TRIGGER trg_project_object_schema_registry_version
AFTER INSERT OR UPDATE ON kb.project_object_schema_registry
FOR EACH ROW EXECUTE FUNCTION kb.snapshot_object_type_version();

-- +goose Down
DROP TRIGGER IF EXISTS trg_project_object_schema_registry_version ON kb.project_object_schema_registry;
DROP FUNCTION IF EXISTS kb.snapshot_object_type_version();
DROP TABLE IF EXISTS kb.project_object_schema_registry_versions;
