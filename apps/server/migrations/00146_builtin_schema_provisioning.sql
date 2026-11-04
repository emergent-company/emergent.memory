-- +goose Up
-- Builtin schema provisioning.
--
-- Schema listing is now strictly project-scoped, so source='builtin' schemas
-- (e.g. session-message-types) are invisible to projects unless a
-- kb.project_schemas row links them. This migration installs builtins into
-- every existing non-deleted project and installs them automatically for
-- every project created afterwards.
--
-- The trigger covers all project creation paths, including raw INSERTs that
-- bypass the application service layer. The NOT EXISTS guard treats ANY
-- existing kb.project_schemas row (including soft-uninstalled rows with
-- removed_at set) as a prior install decision, so explicit uninstalls are
-- respected and are never re-created by this job. The startup reconcile in
-- the schemas builtin seeder (ProvisionBuiltinSchemasToAllProjects) covers
-- builtin schemas added by future releases.

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

-- Backfill: install every builtin schema into every existing, non-deleted
-- project that has no project_schemas row for it yet.
INSERT INTO kb.project_schemas (project_id, schema_id, active, installed_at)
SELECT p.id, gs.id, true, now()
FROM kb.projects p
JOIN kb.graph_schemas gs ON gs.source = 'builtin'
WHERE p.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM kb.project_schemas ps
      WHERE ps.project_id = p.id AND ps.schema_id = gs.id
  )
ON CONFLICT (project_id, schema_id) DO NOTHING;

-- +goose Down
DROP TRIGGER IF EXISTS trg_projects_install_builtins ON kb.projects;
DROP FUNCTION IF EXISTS kb.trg_projects_install_builtins();
-- Installed kb.project_schemas rows are intentionally NOT removed: they are
-- legitimate project data and dropping them would make builtins invisible to
-- projects again under strict project scoping.
