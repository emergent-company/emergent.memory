## Why

Backup export keeps references to soft-deleted graph objects, which breaks overwrite restore. Tracked as GitHub issue #602.

`kb.chat_conversations.object_id` has a hard FK to `kb.graph_objects(id)` (`chat_conversations_object_id_fkey`). `kb.graph_objects` is soft-deletable and is excluded from the archive via `deletedColumn: "deleted_at"`. The conversation row, however, is exported with its `object_id` intact, so it references an object absent from the archive.

In **overwrite** mode, `restoreOverwrite` wipes the project's rows and re-inserts exactly what the archive holds — the dangling `object_id` violates the FK and rolls back the entire restore. Clone mode is already safe (`object_id` is nulled via `nullIfUnmapped` in the restorer).

## What Changes

- When `IncludeDeleted == false`, `chat_conversations.object_id` is emitted as NULL if its referenced object is missing or soft-deleted, via `LEFT JOIN kb.graph_objects go ON go.id = t.object_id` and a `CASE WHEN go.id IS NULL OR go.deleted_at IS NOT NULL THEN NULL ELSE t.object_id END` select expression.
- When `IncludeDeleted == true`, the object IS exported, so `object_id` is emitted plainly (`t.object_id`) with no join and no CASE.
- The mechanism is a general per-table field (`softNull map[string]softNullRef` — child column → referenced-row join info) but is wired ONLY for `chat_conversations.object_id`. No speculative config for other tables.

Rationale: null the optional pointer, keep the conversation. The exporter is the only place that knows the precise condition ("parent excluded because soft-deleted"); the restorer cannot distinguish that from a non-exported global row.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `backup-restore`: backup export now nulls `chat_conversations.object_id` when the referenced graph object is excluded due to soft-delete, so overwrite restore no longer emits a dangling reference that violates `chat_conversations_object_id_fkey`.

## Impact

- `apps/server/domain/backups/exporter.go`: new `softNullRef` type and `tableConfig.softNull` field; `softNullResolution`, `softNullCaseExpr`, `deletedColumnFilter` pure helpers; `exportTable` consumes them. `chat_conversations` config wires `object_id`.
- `apps/server/domain/backups/exporter_test.go`: pure tests for `softNullResolution` (excluded → CASE + join; included → none; missing column → none) and `deletedColumnFilter` (guard).
- No archive format change, no version bump, no migration, and no changes to `restorer.go`, `creator.go`, or `importer.go`.
