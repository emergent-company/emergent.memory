## 1. Exporter — null soft-deleted object references

- [x] 1.1 Add `softNullRef` type (join + alias) and `tableConfig.softNull map[string]softNullRef` field
- [x] 1.2 Wire `chat_conversations.object_id` → `LEFT JOIN kb.graph_objects go ON go.id = t.object_id`, alias `go`
- [x] 1.3 Add `softNullResolution(cfg, includeDeleted, colSet)` pure helper returning extra joins + per-column select overrides; gated on `!includeDeleted`; skips columns absent from `colSet`
- [x] 1.4 Add `softNullCaseExpr(col, alias)` building `CASE WHEN <alias>.id IS NULL OR <alias>.deleted_at IS NOT NULL THEN NULL ELSE t.<col> END AS <col>`
- [x] 1.5 Extract `deletedColumnFilter(cfg, includeDeleted, colSet)` (preserves the existing `colSet` no-op guard)
- [x] 1.6 Consume both helpers in `exportTable`; no behaviour change for tables without `softNull` or `deletedColumn`

## 2. Tests (pure)

- [x] 2.1 `softNullResolution` with `IncludeDeleted=false` emits the graph_objects LEFT JOIN and the CASE object_id expression
- [x] 2.2 `softNullResolution` with `IncludeDeleted=true` emits no join and no override
- [x] 2.3 `softNullResolution` skips a column absent from the live column set
- [x] 2.4 `deletedColumnFilter` guard: no filter when the column is absent from the live schema

## 3. Verification

- [x] 3.1 `go build ./...` clean
- [x] 3.2 `go test ./domain/backups/... -count=1` passes
- [x] 3.3 `go vet ./domain/backups/...` clean
- [x] 3.4 `golangci-lint run ./domain/backups/...` shows only the pre-existing `service.go` errcheck
- [x] 3.5 `openspec validate fix-backup-export-soft-deleted-object-refs` passes
