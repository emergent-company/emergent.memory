-- +goose Up
-- Retire the global "session-message-types" system schema pack.
--
-- Before this migration, the platform's conversation primitives (Session,
-- Message, has_message) lived in a single global kb.graph_schemas row
-- (source='builtin', name='session-message-types', version='1.0.0',
-- is_system=true) resolved at read time into every project, with no
-- kb.project_schemas row. 00235 marked it is_system=true; 00236 deleted every
-- assignment/registry row for it. This migration converts it into a normal
-- built-in blueprint pack: the Session/Message types must now be assigned to a
-- project to be visible.
--
-- Because existing projects may already own Session/Message graph objects,
-- those projects must keep seeing the types. This migration therefore:
--
--   1. resolves the existing global pack row (the definitions live in that row;
--      nothing is invented here), guarding every step so the migration is a
--      clean no-op on a database where the row is absent;
--   2. backfills a kb.project_schemas assignment for every project that owns at
--      least one Session/Message object (type IN ('Session','Message'),
--      deleted_at IS NULL) and has no active assignment yet;
--   3. materializes the Session/Message object registry rows and the
--      has_message edge registry row for those projects from the pack's JSON
--      (jsonb_array_elements over object_type_schemas / relationship_type_schemas);
--   4. deletes the global pack row only when nothing references it any more;
--   5. drops the is_system column and its partial index.
--
-- Order matters: the FKs are non-cascading (project_schemas.schema_id and
-- blueprint_pack_claims.schema_id have no ON DELETE; project_edge_schema_registry
-- cascades). The pack row is deleted only after all referencing rows are gone.
--
-- The backfilled assignments carry source_blueprint_id NULL because the
-- blueprint row for session-message-types does not exist yet at migration time;
-- the blueprints builtin seeder creates it on next boot and a later blueprint
-- apply stamps the assignment via AssignPackWithTypes.

-- 1. Nothing to do: the pack row is referenced by (source, name, version)
--    throughout, so an absent row makes every step below a no-op.

-- 2. Backfill assignments for projects owning Session/Message objects.
INSERT INTO kb.project_schemas
    (project_id, schema_id, active, installed_at, source_blueprint_id, created_at, updated_at)
SELECT go.project_id, gs.id, true, now(), NULL, now(), now()
FROM (SELECT DISTINCT project_id FROM kb.graph_objects
      WHERE type IN ('Session','Message') AND deleted_at IS NULL) go
JOIN kb.graph_schemas gs
  ON gs.source = 'builtin'
 AND gs.name = 'session-message-types'
 AND gs.version = '1.0.0'
WHERE NOT EXISTS (
    SELECT 1 FROM kb.project_schemas ps
    WHERE ps.project_id = go.project_id
      AND ps.schema_id = gs.id
      AND ps.removed_at IS NULL
)
ON CONFLICT (project_id, schema_id) DO NOTHING;

-- 3a. Materialize the Session/Message object registry rows for those projects.
--     json_schema is the per-type element of the pack's object_type_schemas
--     array, matching what AssignPackWithTypes/assignPackWithTypesTx stores
--     (source='template', ui_config='{}', extraction_config='{}', enabled=true,
--     created_by NULL). Never overwrite an existing (project_id, type_name).
INSERT INTO kb.project_object_schema_registry
    (project_id, type_name, source, schema_id, json_schema, ui_config, extraction_config, enabled, created_by)
SELECT go.project_id, elem->>'name', 'template', gs.id, elem, '{}'::jsonb, '{}'::jsonb, true, NULL
FROM (SELECT DISTINCT project_id FROM kb.graph_objects
      WHERE type IN ('Session','Message') AND deleted_at IS NULL) go
JOIN kb.graph_schemas gs
  ON gs.source = 'builtin'
 AND gs.name = 'session-message-types'
 AND gs.version = '1.0.0'
CROSS JOIN LATERAL jsonb_array_elements(gs.object_type_schemas) AS elem
WHERE elem->>'name' IN ('Session','Message')
  AND NOT EXISTS (
      SELECT 1 FROM kb.project_object_schema_registry por
      WHERE por.project_id = go.project_id
        AND por.type_name = elem->>'name'
  );

-- 3b. Materialize the has_message edge registry row for those projects, from
--     the pack's relationship_type_schemas array.
INSERT INTO kb.project_edge_schema_registry
    (project_id, schema_id, type_name, json_schema)
SELECT go.project_id, gs.id, elem->>'name', elem
FROM (SELECT DISTINCT project_id FROM kb.graph_objects
      WHERE type IN ('Session','Message') AND deleted_at IS NULL) go
JOIN kb.graph_schemas gs
  ON gs.source = 'builtin'
 AND gs.name = 'session-message-types'
 AND gs.version = '1.0.0'
CROSS JOIN LATERAL jsonb_array_elements(gs.relationship_type_schemas) AS elem
WHERE elem->>'name' = 'has_message'
  AND NOT EXISTS (
      SELECT 1 FROM kb.project_edge_schema_registry per
      WHERE per.project_id = go.project_id
        AND per.type_name = 'has_message'
  );

-- 4. Delete the global pack row only when nothing references it (mirror 00236
--    step 3): no assignment, no object/edge registry row, no blueprint claim.
DELETE FROM kb.graph_schemas gs
WHERE gs.source = 'builtin'
  AND gs.name = 'session-message-types'
  AND gs.version = '1.0.0'
  AND NOT EXISTS (SELECT 1 FROM kb.project_schemas ps WHERE ps.schema_id = gs.id)
  AND NOT EXISTS (SELECT 1 FROM kb.project_object_schema_registry por WHERE por.schema_id = gs.id)
  AND NOT EXISTS (SELECT 1 FROM kb.project_edge_schema_registry per WHERE per.schema_id = gs.id)
  AND NOT EXISTS (SELECT 1 FROM kb.blueprint_pack_claims bpc WHERE bpc.schema_id = gs.id);

-- 5. Drop the is_system column and its partial index (the concept existed only
--    for this pack).
DROP INDEX IF EXISTS kb.graph_schemas_is_system_idx;

ALTER TABLE kb.graph_schemas DROP COLUMN IF EXISTS is_system;

-- +goose Down
-- Restore the is_system column + partial index (mirror 00235's Up), then remove
-- the assignment/registry rows this migration created for projects owning
-- Session/Message objects so up->down->up round-trips cleanly.
--
-- Data-loss boundary: the backfilled project_schemas assignment and the
-- Session/Message object + has_message edge registry rows are deleted here.
-- They describe state derived from existing graph objects (which are untouched)
-- and are regenerated by the Up path (or by a later blueprint apply) on re-up.

ALTER TABLE kb.graph_schemas
    ADD COLUMN IF NOT EXISTS is_system boolean NOT NULL DEFAULT false;

UPDATE kb.graph_schemas
SET is_system = true
WHERE source = 'builtin' AND name = 'session-message-types';

CREATE INDEX IF NOT EXISTS graph_schemas_is_system_idx
    ON kb.graph_schemas (is_system) WHERE is_system = true;

-- Remove the backfilled object registry rows (Session/Message only).
DELETE FROM kb.project_object_schema_registry por
USING kb.graph_schemas gs
WHERE por.schema_id = gs.id
  AND gs.source = 'builtin'
  AND gs.name = 'session-message-types'
  AND gs.version = '1.0.0'
  AND por.type_name IN ('Session','Message');

-- Remove the backfilled edge registry rows (has_message only).
DELETE FROM kb.project_edge_schema_registry per
USING kb.graph_schemas gs
WHERE per.schema_id = gs.id
  AND gs.source = 'builtin'
  AND gs.name = 'session-message-types'
  AND gs.version = '1.0.0'
  AND per.type_name = 'has_message';

-- Remove the backfilled assignments (only the NULL-ownership rows this
-- migration inserted; a blueprint-stamped assignment is left alone).
DELETE FROM kb.project_schemas ps
USING kb.graph_schemas gs
WHERE ps.schema_id = gs.id
  AND gs.source = 'builtin'
  AND gs.name = 'session-message-types'
  AND gs.version = '1.0.0'
  AND ps.source_blueprint_id IS NULL;
