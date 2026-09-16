## Why

`Repository.ListAll` (added in #519) walks **every** object in the project with a keyset cursor. Both `RollbackSchemaMigration` and `ExecuteSchemaMigration` call it from a synchronous HTTP request, and rollback has no type filter — it fetches every object before the `RunInTx` block. `max_objects` is applied *after* the full fetch (`objs[:maxObjs]`), so it bounds processing, not memory or IO. A project with a large object table turns the migrate/rollback endpoints into long-running, memory-heavy calls. Tracked as GitHub issue #521.

## What Changes

Push the archive predicate into SQL so the scan is bounded to objects that actually carry an archive, instead of fetching the whole project up front:

- Add an opt-in `ListParams` filter (e.g. `OnlyWithMigrationArchive`) that appends `migration_archive <> '[]'::jsonb` to the `List`/`ListAll` query. Only objects with at least one archive entry are scanned.
- Set the filter on the `RollbackSchemaMigration` and `ExecuteSchemaMigration` `ListAll` call sites.
- Keep the correctness guarantee from #519: every *matching* object is still visited; the bound is on the scan set, not on completeness.

This is the smallest of the directions in #521 and the highest-value for the common case (most projects have few or no archived objects). Page-by-page commits and a documented request-path bound remain possible follow-ups if measurement still shows a large archived population.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `schema-migrator-api`: the batch scan contract is tightened — migrate/rollback SHALL bound their object scan to archive-carrying objects rather than scanning the entire project.

## Impact

- `apps/server/domain/graph/repository.go`: `ListParams` gains an archive predicate flag; `buildObjectBaseQuery`/`List` applies the `migration_archive <> '[]'::jsonb` predicate when set.
- `apps/server/domain/schemas/service.go`: `ExecuteSchemaMigration` and `RollbackSchemaMigration` set the flag on their `ListAll` calls.
- No HTTP response shape changes, no database migrations.
