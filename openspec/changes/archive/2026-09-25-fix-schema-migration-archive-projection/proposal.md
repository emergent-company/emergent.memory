## Why

Schema migration rollback silently restored **zero** objects. `POST /api/schemas/projects/:projectId/migrate/rollback` returned `objects_restored: 0` while the dropped property stayed dropped.

Root cause: `graph.Repository.List` builds an explicit `Column(...)` projection that omitted `migration_archive`. The rollback path listed objects through `List`, then skipped every one with `if len(obj.MigrationArchive) == 0 { continue }`. Because the column was never selected the slice always scanned empty, so every object was skipped.

The same omission caused a second data-integrity bug in the forward-migrate path: `SchemaMigrator.MigrateObject` **appends** the dropped fields to `obj.MigrationArchive`, but the patch wrote back an always-empty slice, so a second migration hop overwrote (rather than appended to) the archive — destroying the first hop's archive entries and making that hop unrecoverable.

A third problem: `List` clamps `Limit` to `MaxListLimit` and pages are capped, so the migrate/rollback paths would only ever examine the first page of objects.

## What Changes

- Add an opt-in `ListParams.IncludeMigrationArchive` flag; `List` selects the `migration_archive` column only when it is set. The column stays out of the default projection so the public graph list/search API never leaks archive JSON.
- Add `Repository.ListAll`, which walks the full result set with keyset cursors (instead of a single capped page). Migrate and rollback now use it, so every object is visited regardless of project size — no more silent row cap.
- Set `IncludeMigrationArchive` and use `ListAll` on the two call sites that read or write `obj.MigrationArchive` (`ExecuteSchemaMigration`, `RollbackSchemaMigration`).
- Tests: repository-level projection opt-in + multi-page coverage, a service-level migrate/rollback round trip (including a two-hop regression for the archive-clobber bug), and an HTTP end-to-end integration test for the rollback route.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `schema-migrator-api`: the archive-backed execute/rollback contract is tightened — the archive column MUST be loaded by batch operations, rolled-back entries MUST be consumed, multi-hop archives MUST be appended to (never clobbered), and batch operations MUST visit every matching object. Note: this capability currently exists only as a delta in the active `schema-migration-hints` change, not yet under `openspec/specs/`, so this change contributes `## ADDED Requirements` to the same capability.

## Impact

- `apps/server/domain/graph/repository.go`: new `ListParams.IncludeMigrationArchive`; conditional `migration_archive` projection in `List`; new `ListAll` cursor-paged full scan.
- `apps/server/domain/schemas/service.go`: `ExecuteSchemaMigration` and `RollbackSchemaMigration` set the flag and list via `ListAll`; `max_objects` still bounds the migrated set.
- No HTTP response shape changes; only the corrected restore behaviour (a rollback that previously returned `0` now returns the real restored count).
- No database migrations.
