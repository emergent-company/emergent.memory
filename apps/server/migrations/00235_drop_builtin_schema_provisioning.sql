-- +goose Up
-- Remove built-in schema auto-provisioning.
--
-- Schema packs must no longer be auto-assigned to projects. This drops the
-- trigger (00146) that swept every source='builtin' pack into every newly
-- created project and lets the startup reconcile (ProvisionBuiltinSchemasToAllProjects)
-- disappear with its call site in the schemas builtin seeder. Instead:
--
--   * the platform's conversation primitives (Session/Message) become a
--     read-time-resolved "system" pack with no kb.project_schemas row; and
--   * built-in blueprint packs materialize only when their blueprint is applied
--     (the defensive resolvePack -> upsertBuiltinSchemaPack path in apply.go).
--
-- The new kb.graph_schemas.is_system flag marks the single system pack
-- (session-message-types). source stays 'builtin' so the partial unique index
-- graph_schemas_builtin_name_version_key and the apply path's
-- ON CONFLICT (name, version) WHERE source='builtin' keep working unchanged.

DROP TRIGGER IF EXISTS trg_projects_install_builtins ON kb.projects;
DROP FUNCTION IF EXISTS kb.trg_projects_install_builtins();

ALTER TABLE kb.graph_schemas
    ADD COLUMN IF NOT EXISTS is_system boolean NOT NULL DEFAULT false;

UPDATE kb.graph_schemas
SET is_system = true
WHERE source = 'builtin' AND name = 'session-message-types';

-- +goose Down
-- Restore the auto-provisioning mechanism and drop the column. Data cleanup
-- from 00236 is not reversible here; this only reinstates the trigger/function
-- (copied from 00146) and removes the is_system flag.

ALTER TABLE kb.graph_schemas DROP COLUMN IF EXISTS is_system;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kb.trg_projects_install_builtins() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path = kb, pg_temp
    AS $$
    BEGIN
        INSERT INTO kb.project_schemas (project_id, schema_id, active, installed_at)
        SELECT NEW.id, gs.id, true, now()
        FROM kb.graph_schemas gs
        WHERE gs.source = 'builtin'
          AND NOT EXISTS (
              SELECT 1 FROM kb.project_schemas ps
              WHERE ps.project_id = NEW.id AND ps.schema_id = gs.id
          )
        ON CONFLICT (project_id, schema_id) DO NOTHING;

        RETURN NEW;
    END;
    $$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS trg_projects_install_builtins ON kb.projects;
CREATE TRIGGER trg_projects_install_builtins
    AFTER INSERT ON kb.projects
    FOR EACH ROW
    EXECUTE FUNCTION kb.trg_projects_install_builtins();
