-- +goose Up
-- Defense-in-depth: make builtin schema identity unambiguous.
--
-- kb.graph_schemas holds deployment-global builtin rows (e.g.
-- 'session-message-types'). BuiltinSeeder inserts them without an explicit id,
-- so every deployment gets a different UUID. Backup clone-restore archives
-- kb.project_schemas rows whose schema_id points at the SOURCE deployment's
-- builtin UUID, which does not exist in the target deployment. The restorer
-- now skips those unresolvable links (see domain/backups/restorer.go), but a
-- unique partial index over (name, version) for source='builtin' rows makes
-- builtin identity stable and prevents duplicate builtin rows accumulating.
--
-- Scoped to source='builtin' (NOT project_id IS NULL) because migration 00145
-- leaves source='manual', project_id IS NULL rows that must stay unconstrained.

-- Dedupe existing duplicate builtin rows on (name, version), keeping the
-- lowest-ctid row in each group, before creating the index.
DELETE FROM kb.graph_schemas
WHERE source = 'builtin'
  AND id NOT IN (
      SELECT DISTINCT ON (name, version) id
      FROM kb.graph_schemas
      WHERE source = 'builtin'
      ORDER BY name, version, ctid
  );

CREATE UNIQUE INDEX graph_schemas_builtin_name_version_key
    ON kb.graph_schemas (name, version)
    WHERE source = 'builtin';

-- +goose Down
DROP INDEX IF EXISTS kb.graph_schemas_builtin_name_version_key;
