## Why

`SchemaMigrator.RollbackObject` consumed the matched archive entry by truncating the slice at the matched index:

```go
obj.MigrationArchive = obj.MigrationArchive[:archiveIndex]
```

Because the archive is a multi-hop history (each migration appends an entry), truncating at the matched index drops the matched entry **and every newer entry** — without restoring their data. Rolling back an out-of-order hop therefore silently destroys later archive records, which is exactly the multi-hop case the archive exists to support. Tracked as GitHub issue #522.

## What Changes

- Replace the truncation with a splice that removes only the matched entry, preserving every newer entry:
  `obj.MigrationArchive = append(obj.MigrationArchive[:i], obj.MigrationArchive[i+1:]...)`.
- Add a unit test (`TestSchemaMigration_RollbackPreservesNewerArchiveEntries`) covering the out-of-order case: archive `[1.0.0 → 2.0.0, 2.0.0 → 3.0.0]`, rollback to `2.0.0` → the `2.0.0 → 3.0.0` entry and its `dropped_data` survive.

Matching semantics are unchanged: the search still walks newest-first and consumes the newest entry whose `to_version == toVersion`, per the existing round-trip contract.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `schema-migrator-api`: the rollback consume contract is tightened — a rollback SHALL remove only the entry it matches, never newer entries.

## Impact

- `apps/server/domain/graph/migration.go`: `RollbackObject` splice instead of truncate.
- `apps/server/domain/graph/migration_test.go`: new out-of-order rollback regression test.
- No HTTP response shape changes, no database migrations.
