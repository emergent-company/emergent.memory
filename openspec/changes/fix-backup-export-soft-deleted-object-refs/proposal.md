## Why

Backup export keeps references to soft-deleted graph objects, which breaks overwrite restore. Tracked as GitHub issue #602.

`kb.chat_conversations.object_id` has a hard FK to `kb.graph_objects(id)` (`chat_conversations_object_id_fkey`). `kb.graph_objects` is soft-deletable and is excluded from the archive via `deletedColumn: "deleted_at"`. The conversation row, however, is exported with its `object_id` intact, so it references an object absent from the archive.

In **overwrite** mode, `restoreOverwrite` wipes the project's rows and re-inserts exactly what the archive holds — the dangling `object_id` violates the FK and rolls back the entire restore. Clone mode is already safe (`object_id` is nulled via `nullIfUnmapped` in the restorer).

## What Changes

- When `IncludeDeleted == false`, `chat_conversations.object_id` is emitted as NULL if its referenced object is missing or soft-deleted, via `LEFT JOIN kb.graph_objects go ON go.id = t.object_id` and a `CASE WHEN go.id IS NULL OR go.deleted_at IS NOT NULL THEN NULL ELSE t.object_id END` select expression.
- When `IncludeDeleted == true`, the object IS exported, so `object_id` is emitted plainly (`t.object_id`) with no join and no CASE.
- The mechanism is a general per-table pair of fields — `columnExprs map[string]string` (per-column SELECT-expression override, applied only when `IncludeDeleted == false`) and `leftJoin` (the join supplying the alias the expression references) — but it is wired ONLY for `chat_conversations.object_id`. No speculative config for other tables.

Rationale: null the optional pointer, keep the conversation. The exporter is the only place that knows the precise condition ("parent excluded because soft-deleted"); the restorer cannot distinguish that from a non-exported global row.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `backup-restore`: backup export now nulls `chat_conversations.object_id` when the referenced graph object is excluded due to soft-delete, so overwrite restore no longer emits a dangling reference that violates `chat_conversations_object_id_fkey`.

## Impact

- `apps/server/domain/backups/exporter.go`: new `tableConfig.columnExprs` and `tableConfig.leftJoin` fields; `projectionColumns` and `deletedColumnFilter` pure helpers; `exportTable` consumes them and appends `leftJoin` only when overrides are active. `chat_conversations` config wires `object_id` to `CASE WHEN go.id IS NULL OR go.deleted_at IS NOT NULL THEN NULL ELSE t."object_id" END AS "object_id"` with `LEFT JOIN kb.graph_objects go ON go.id = t.object_id`.
- `apps/server/domain/backups/exporter_test.go`: pure tests for `projectionColumns` (override applied when excluded, plain column when included), the `chat_conversations` config guard, and `deletedColumnFilter` (guard).
- `apps/server/domain/backups/exporter_db_test.go` (new): DB-backed `TestExportChatConversationsDanglingObjectRef` — a live and a soft-deleted graph object with one conversation each; asserts the dangling `object_id` is NULL when deleted rows are excluded and the original UUID when `IncludeDeleted` is set, and that the live reference survives.
- No archive format change, no version bump, no migration, and no changes to `restorer.go`, `creator.go`, or `importer.go`.
