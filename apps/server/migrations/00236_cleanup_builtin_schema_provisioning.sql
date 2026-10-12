-- +goose Up
-- Clean up bad data left by the removed auto-provisioning.
--
-- Before 00235 every source='builtin' pack was swept into every project (the
-- 00146 trigger + the startup reconcile ProvisionBuiltinSchemasToAllProjects).
-- The four built-in blueprint packs (agent-notes, dreaming-agent, operator,
-- task-board) therefore surfaced in projects that never installed the
-- blueprint. 00235 marked the single system pack (session-message-types)
-- is_system=true; every other source='builtin' pack is a blueprint pack.
--
-- Target packs = source='builtin' AND NOT is_system.
--
-- Cleanup removes, for each target pack:
--   1. project_object_schema_registry / project_edge_schema_registry rows in
--      projects that have no APPLIED blueprint claiming the pack;
--   2. kb.project_schemas rows in projects that have no APPLIED blueprint
--      claiming the pack;
--   3. the global kb.graph_schemas row itself once nothing references it.
--
-- Order matters because the FKs are non-cascading:
--   * kb.project_schemas.schema_id      -> kb.graph_schemas(id)  (no cascade)
--   * kb.blueprint_pack_claims.schema_id-> kb.graph_schemas(id)  (no cascade)
--   * kb.project_edge_schema_registry   -> ON DELETE CASCADE (auto-cleaned)
--   * kb.schema_studio_sessions.pack_id -> ON DELETE CASCADE (auto-cleaned)
--   * kb.project_object_schema_registry.schema_id has NO FK (cleaned explicitly)
-- There is no dedup/repoint step here: 00158's partial unique index
-- graph_schemas_builtin_name_version_key guarantees at most one builtin row per
-- (name, version), so there is no duplicate to repoint references at.

-- The "keep" test: an APPLIED blueprint claims the pack for this project.
-- Inlined per DELETE because a migration cannot share a CTE across statements.

-- 1a. Object registry rows for target packs in projects that never applied a
--     blueprint claiming them.
DELETE FROM kb.project_object_schema_registry por
USING kb.graph_schemas gs
WHERE por.schema_id = gs.id
  AND gs.source = 'builtin'
  AND gs.is_system = false
  AND NOT EXISTS (
      SELECT 1
      FROM kb.blueprint_pack_claims bpc
      JOIN kb.blueprint_applications bpa
        ON bpa.blueprint_id = bpc.blueprint_id
       AND bpa.project_id = bpc.project_id
      WHERE bpc.project_id = por.project_id
        AND bpc.schema_id = por.schema_id
        AND bpa.status = 'applied'
  );

-- 1b. Edge registry rows for the same projects.
DELETE FROM kb.project_edge_schema_registry per
USING kb.graph_schemas gs
WHERE per.schema_id = gs.id
  AND gs.source = 'builtin'
  AND gs.is_system = false
  AND NOT EXISTS (
      SELECT 1
      FROM kb.blueprint_pack_claims bpc
      JOIN kb.blueprint_applications bpa
        ON bpa.blueprint_id = bpc.blueprint_id
       AND bpa.project_id = bpc.project_id
      WHERE bpc.project_id = per.project_id
        AND bpc.schema_id = per.schema_id
        AND bpa.status = 'applied'
  );

-- 2. Assignment rows for target packs in projects that never applied a
--    blueprint claiming them.
DELETE FROM kb.project_schemas ps
USING kb.graph_schemas gs
WHERE ps.schema_id = gs.id
  AND gs.source = 'builtin'
  AND gs.is_system = false
  AND NOT EXISTS (
      SELECT 1
      FROM kb.blueprint_pack_claims bpc
      JOIN kb.blueprint_applications bpa
        ON bpa.blueprint_id = bpc.blueprint_id
       AND bpa.project_id = bpc.project_id
      WHERE bpc.project_id = ps.project_id
        AND bpc.schema_id = ps.schema_id
        AND bpa.status = 'applied'
  );

-- 3. Orphaned global pack rows: no blueprint claim, no assignment, no registry
--    reference remains. project_edge_schema_registry is already empty of these
--    via step 1b (and would cascade anyway); project_object_schema_registry is
--    checked explicitly because it has no FK.
DELETE FROM kb.graph_schemas gs
WHERE gs.source = 'builtin'
  AND gs.is_system = false
  AND NOT EXISTS (SELECT 1 FROM kb.blueprint_pack_claims bpc WHERE bpc.schema_id = gs.id)
  AND NOT EXISTS (SELECT 1 FROM kb.project_schemas ps WHERE ps.schema_id = gs.id)
  AND NOT EXISTS (SELECT 1 FROM kb.project_object_schema_registry por WHERE por.schema_id = gs.id)
  AND NOT EXISTS (SELECT 1 FROM kb.project_edge_schema_registry per WHERE per.schema_id = gs.id);

-- +goose Down
-- No-op: this migration deletes data. The removed project assignments and
-- orphaned global pack rows describe project state that is intentionally gone.
-- The 00235 Down reinstates auto-provisioning, and a fresh server boot (or a
-- blueprint re-apply) recreates blueprint-pack rows on demand, but the
-- per-project assignments removed here are not recoverable from this migration.
