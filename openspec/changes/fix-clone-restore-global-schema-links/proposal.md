## Why

Clone restore (`POST /api/v1/organizations/:orgId/restore`, `mode=clone`) rolls back when the archive is replayed into a differently-bootstrapped deployment. Tracked as GitHub issue #592.

`kb.graph_schemas` holds deployment-global builtin rows (e.g. `session-message-types`). `BuiltinSeeder` INSERTs them without an explicit `id`, so each deployment gets a different UUID. Migration 00146 installs trigger `trg_projects_install_builtins`, which links every new project to that deployment's own builtin row via `kb.project_schemas`. The exporter archives every table with `projectFilter: byProject` (`t.project_id = ?`), so global rows (`project_id IS NULL`) are never archived. The archive therefore contains `kb.project_schemas` rows whose `schema_id` points at the source deployment's builtin UUID, which does not exist in the target deployment — the FK `project_schemas_schema_id_fkey` fires and the whole restore transaction rolls back.

## What Changes

- **Clone skips unresolvable global schema links.** In clone mode, any row whose `schema_id` cannot be resolved through the remap table is dropped (not inserted). The target's own trigger already creates the builtin link, so skipping is correct and lossless. Project-owned schemas are inserted earlier in the restore order (`graph_schemas` before `project_schemas`) and registered in the remap map, so any legitimately-owned `schema_id` IS in remap — anything missing is a global/foreign row.
- **Declared per-column FK policy replaces ad-hoc nulling.** The ad-hoc `nullIfUnmapped []string` on `restoreTableSpec` becomes a declared `refs map[string]refPolicy` with four actions: `refRemap` (default — rewrite via remap, leave raw if unmapped), `refNull` (NULL the column), `refSkip` (drop the row), `refFail` (error). Existing nulling columns are converted verbatim to `refNull`; `project_schemas.schema_id` and `project_edge_schema_registry.schema_id` get `refSkip`.
- **Optional builtin uniqueness index (defense-in-depth).** Migration 00158 adds a partial unique index `(name, version) WHERE source = 'builtin'` on `kb.graph_schemas`, deduping existing builtin duplicates first, so builtin schema identity is unambiguous going forward.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `backup-restore`: clone restore now skips (rather than fails on) foreign-key links that resolve to deployment-global rows absent from the archive; foreign-key resolution is governed by a declared per-column policy instead of an ad-hoc nulling list.

## Impact

- `apps/server/domain/backups/restorer.go`: `refAction`/`refPolicy` types, `restoreTableSpec.refs` map, `applyRefs` helper, `insertTableRows` wiring, `remapRowUUIDs` narrowed to PK registration.
- `apps/server/migrations/00158_builtin_graph_schemas_unique.sql`: dedupe + partial unique index; before deleting duplicate builtin rows it repoints dependents (project_schemas, project_edge_schema_registry, blueprint_pack_claims, schema_studio_sessions) to the canonical row, dropping redundant links that would violate child uniqueness.
- `apps/server/domain/backups/restorer_test.go`: pure unit tests for `applyRefs`.
- `apps/server/domain/backups/restorer_clone_db_test.go`: DB-backed regression test for #592 + overwrite-identity assertion.
- No archive format change, no version bump, no exporter/importer/creator/entity changes.
