## 1. Exporter — null references to excluded soft-deleted objects

- [x] 1.1 Add `tableConfig.columnExprs map[string]string` (per-column SELECT override) and `tableConfig.leftJoin string` (alias source) fields
- [x] 1.2 Wire `chat_conversations.object_id` → `LEFT JOIN kb.graph_objects go ON go.id = t.object_id` plus `CASE WHEN go.id IS NULL OR go.deleted_at IS NOT NULL THEN NULL ELSE t."object_id" END AS "object_id"`; applied only when `IncludeDeleted == false`
- [x] 1.3 Add `projectionColumns(cols, cfg, includeDeleted)` pure helper; overrides applied only when `IncludeDeleted == false`
- [x] 1.4 Extract `deletedColumnFilter(cfg, includeDeleted, colSet)` (preserves the existing `colSet` no-op guard)
- [x] 1.5 Consume both helpers in `exportTable`; append `leftJoin` only when overrides are active; no behaviour change for tables without `columnExprs`/`deletedColumn`

## 2. Tests

- [x] 2.1 `projectionColumns` with `IncludeDeleted=false` yields the CASE `object_id` expression; with `IncludeDeleted=true` yields plain `t."object_id"`
- [x] 2.2 `chat_conversations` config guard: `columnExprs["object_id"]` present and `leftJoin` set
- [x] 2.3 `deletedColumnFilter` guard: no filter when the column is absent from the live schema
- [x] 2.4 DB-backed `TestExportChatConversationsDanglingObjectRef`: live + soft-deleted graph objects; soft-deleted reference exported as NULL when excluded and preserved when `IncludeDeleted`; live reference preserved

## 3. Verification

- [x] 3.1 `go build ./...` clean
- [x] 3.2 `go test ./domain/backups/... -count=1` passes
- [x] 3.3 `POSTGRES_PASSWORD=emergent go test ./domain/backups/ -run TestExportChatConversationsDanglingObjectRef -count=1 -v` actually runs and passes
- [x] 3.4 `go vet ./domain/backups/...` clean
- [x] 3.5 `golangci-lint run ./domain/backups/...` shows only the pre-existing `service.go` errcheck
- [x] 3.6 `openspec validate fix-backup-export-soft-deleted-object-refs` passes
