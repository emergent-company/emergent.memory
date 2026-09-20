## Why

`Repository.ListAll` (added in #519) walks **every** object in the project with a keyset cursor and accumulates the whole result set in memory. Both `RollbackSchemaMigration` and `ExecuteSchemaMigration` call it from a synchronous HTTP request, and rollback has no type filter — it fetches every object before the `RunInTx` block. `max_objects` is applied *after* the full fetch (`objs[:maxObjs]`), so it bounds processing, not memory or IO. A project with a large object table turns the migrate/rollback endpoints into long-running, memory-heavy calls. Tracked as GitHub issue #521.

## What Changes

Bound the synchronous scan in three ways, all in one change:

1. **Archive predicate (rollback only).** Add an opt-in `ListParams.OnlyWithMigrationArchive` that appends `migration_archive <> '[]'::jsonb` to the base query, relying on the existing partial index `idx_graph_objects_has_archive`. It is set on **`RollbackSchemaMigration` only** — a rollback only ever restores objects that already carry an archive entry, so archive-free objects are irrelevant to it.
   - The predicate is **NOT** applied to `ExecuteSchemaMigration`: a forward migration must also visit objects with an *empty* archive (a first-ever migration has zero archived objects, as do objects created after a prior migration). Filtering execute by archive would silently skip those objects and regress data migration.
2. **Streaming, memory-bounded iteration.** Add `Repository.ListEach` / `ListEachTx`, keyset-cursor iterators that invoke a callback per page and never materialise the full result set. `ExecuteSchemaMigration` streams each type's objects page-by-page (preserving the per-type `max_objects` cap), and `RollbackSchemaMigration` streams inside its existing `db.RunInTx` so only one page is in memory and the data + registry restore stays atomic in one transaction.
3. **Configurable hard safety cap.** Add `Graph.MigrationScanMaxObjects` (`GRAPH_MIGRATION_SCAN_MAX_OBJECTS`, default `10000`). Both migrate and rollback abort with a clear 4xx when a scan would exceed it. Rollback's abort is atomic — it happens inside the transaction, so nothing is restored or written. Execute is not transactional: a mid-scan abort can leave the current type partially migrated and does not write the `kb.schema_migration_runs` run row. Rollback additionally gains a per-request `MaxObjects` cap on restored objects.
4. **Reject `max_objects` + `restore_type_registry`.** A rollback that combines the two would silently drop the all-or-nothing registry restore when the `max_objects` cap is hit mid-scan. The combination is rejected up front with a 4xx.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `schema-migrator-api`: the batch scan contract is tightened — rollback SHALL bound its scan to archive-carrying objects, migrate/rollback SHALL stream results page-by-page rather than materialising the full set, and the synchronous request path SHALL be bounded by a configurable hard cap that fails loudly when exceeded.

## Impact

- `apps/server/internal/config/config.go`: `Graph.MigrationScanMaxObjects` (`GRAPH_MIGRATION_SCAN_MAX_OBJECTS`, default `10000`).
- `apps/server/domain/graph/repository.go`: `ListParams.OnlyWithMigrationArchive`; predicate in `buildObjectBaseQueryWith`; `buildObjectBaseQuery` refactor to a caller-supplied handle; `ListEach`/`ListEachTx`; `ListAll` reimplemented as a thin wrapper over `ListEach`.
- `apps/server/domain/schemas/entity.go`: `SchemaMigrationRollbackRequest.MaxObjects`.
- `apps/server/domain/schemas/service.go`: stream execute and rollback; wire the config cap; apply `MaxObjects` on rollback.
- `apps/server/domain/graph/migration_archive_list_db_test.go`, `apps/server/domain/schemas/bound_schema_migration_scan_db_test.go` (new): DB tests.
- `apps/server/internal/testutil/server.go`, `apps/server/domain/blueprints/migration_test.go`, `apps/server/domain/schemas/restore_type_registry_db_test.go`, `apps/server/domain/schemas/schemas_list_db_test.go`: `NewService` now takes `*config.Config`.
- No HTTP response shape changes, no database migrations.
