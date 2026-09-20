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
--
-- Dedupe first, keeping one canonical builtin row per (name, version). Rows
-- that reference a duplicate must be handled BEFORE the duplicate is deleted:
-- kb.project_schemas.schema_id and kb.blueprint_pack_claims.schema_id are
-- non-cascading FKs, so deleting a referenced duplicate would otherwise abort
-- the migration with a foreign-key violation. References are repointed at the
-- canonical row; where repointing would violate a child uniqueness constraint
-- (project_schemas, project_edge_schema_registry, blueprint_pack_claims) the
-- redundant link is dropped instead, since the surviving link expresses the
-- same association.

CREATE TEMP TABLE _gs_builtin_dedup AS
SELECT id AS dup_id, canon_id
FROM (
    SELECT id,
           first_value(id) OVER (PARTITION BY name, version ORDER BY ctid) AS canon_id
    FROM kb.graph_schemas
    WHERE source = 'builtin'
) t
WHERE id <> canon_id;

-- kb.project_schemas: UNIQUE (project_id, schema_id).
-- Keep exactly one link per (project, canonical schema), then repoint.
WITH resolved AS (
    SELECT ps.id,
           ps.project_id,
           ps.schema_id,
           COALESCE(d.canon_id, ps.schema_id) AS canon_id
    FROM kb.project_schemas ps
    LEFT JOIN _gs_builtin_dedup d
      ON d.dup_id = ps.schema_id
    WHERE EXISTS (
        SELECT 1 FROM _gs_builtin_dedup x
        WHERE x.dup_id = ps.schema_id OR x.canon_id = ps.schema_id
    )
),
ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY project_id, canon_id
               ORDER BY (schema_id = canon_id) DESC, id
           ) AS rn
    FROM resolved
)
DELETE FROM kb.project_schemas ps
USING ranked r
WHERE ps.id = r.id AND r.rn > 1;

UPDATE kb.project_schemas ps
SET schema_id = d.canon_id
FROM _gs_builtin_dedup d
WHERE ps.schema_id = d.dup_id;

-- kb.project_edge_schema_registry: UNIQUE (project_id, schema_id, type_name).
WITH resolved AS (
    SELECT r.id,
           r.project_id,
           r.type_name,
           r.schema_id,
           COALESCE(d.canon_id, r.schema_id) AS canon_id
    FROM kb.project_edge_schema_registry r
    LEFT JOIN _gs_builtin_dedup d
      ON d.dup_id = r.schema_id
    WHERE EXISTS (
        SELECT 1 FROM _gs_builtin_dedup x
        WHERE x.dup_id = r.schema_id OR x.canon_id = r.schema_id
    )
),
ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY project_id, canon_id, type_name
               ORDER BY (schema_id = canon_id) DESC, id
           ) AS rn
    FROM resolved
)
DELETE FROM kb.project_edge_schema_registry r
USING ranked k
WHERE r.id = k.id AND k.rn > 1;

UPDATE kb.project_edge_schema_registry r
SET schema_id = d.canon_id
FROM _gs_builtin_dedup d
WHERE r.schema_id = d.dup_id;

-- kb.blueprint_pack_claims: UNIQUE (project_id, blueprint_id, schema_id).
WITH resolved AS (
    SELECT b.id,
           b.project_id,
           b.blueprint_id,
           b.schema_id,
           COALESCE(d.canon_id, b.schema_id) AS canon_id
    FROM kb.blueprint_pack_claims b
    LEFT JOIN _gs_builtin_dedup d
      ON d.dup_id = b.schema_id
    WHERE EXISTS (
        SELECT 1 FROM _gs_builtin_dedup x
        WHERE x.dup_id = b.schema_id OR x.canon_id = b.schema_id
    )
),
ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY project_id, blueprint_id, canon_id
               ORDER BY (schema_id = canon_id) DESC, id
           ) AS rn
    FROM resolved
)
DELETE FROM kb.blueprint_pack_claims b
USING ranked k
WHERE b.id = k.id AND k.rn > 1;

UPDATE kb.blueprint_pack_claims b
SET schema_id = d.canon_id
FROM _gs_builtin_dedup d
WHERE b.schema_id = d.dup_id;

-- kb.schema_studio_sessions.pack_id has no uniqueness constraint.
UPDATE kb.schema_studio_sessions s
SET pack_id = d.canon_id
FROM _gs_builtin_dedup d
WHERE s.pack_id = d.dup_id;

-- kb.graph_schemas.parent_version_id is ON DELETE SET NULL, so rows that point
-- at a duplicate are nulled automatically when the duplicate is removed.

DELETE FROM kb.graph_schemas g
USING _gs_builtin_dedup d
WHERE g.id = d.dup_id;

DROP TABLE _gs_builtin_dedup;

CREATE UNIQUE INDEX graph_schemas_builtin_name_version_key
    ON kb.graph_schemas (name, version)
    WHERE source = 'builtin';

-- +goose Down
DROP INDEX IF EXISTS kb.graph_schemas_builtin_name_version_key;
