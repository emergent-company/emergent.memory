## 1. Writer (D)

- [x] 1.1 Add bounded writer to `domain/journal/service.go`: buffered queue, fixed worker pool, batch flush, `Stop` drain; capture `CreatedAt` at submit; blocking submit with warn-only on cancelled/stopped.
- [x] 1.2 Add `InsertBatch` to `domain/journal/store.go` (single multi-row insert) and to `repoIface`.
- [x] 1.3 Wire fx lifecycle `OnStop` drain in `domain/journal/module.go`.
- [x] 1.4 Add `Journal` config group (`JOURNAL_WRITER_CONCURRENCY`=8, `JOURNAL_WRITE_QUEUE_SIZE`=4096, `JOURNAL_WRITE_BATCH_SIZE`=256) in `internal/config/config.go`; thread through the service constructor.

## 2. Bulk dedupe (E)

- [x] 2.1 Remove the five redundant trailing `EventTypeBatch` summary writes in `domain/graph/service.go` (`BulkCreateObjects`, `BulkUpsertObjects`, `BulkUpdateObjects`, `BulkCreateRelationships`, `BulkUpsertRelationships`).
- [x] 2.2 Keep `BulkAction`/`MergeBranch`/`ForkBranch`/`MoveObject` summaries; `BulkAction` metadata already carries matched/affected counts.
- [x] 2.3 Remove now-unused locals; `go build ./...`.

## 3. Index + pagination (C)

- [x] 3.1 Add migration `00227_drop_journal_branch_index.sql` (Up drops `idx_project_journal_branch`, Down recreates).
- [x] 3.2 New `domain/journal/cursor.go` encode/decode (`RFC3339Nano|uuid`) + `ErrInvalidCursor`.
- [x] 3.3 `ListParams` gains `Cursor`; `JournalResponse` replaces `Total` with `NextCursor`/`HasMore`.
- [x] 3.4 `List` keyset: `ORDER BY created_at DESC, id DESC`, `(created_at, id) < (?,?)`, `limit+1` probe, drop `ScanAndCount`; preserve branch/`include_branches` semantics.
- [x] 3.5 `handler.go` `ListJournal` accepts `cursor` and maps invalid cursor → 400.
- [x] 3.6 Adapt consumers: `domain/mcp/service.go` `executeJournalList`, `apps/cli/internal/cmd/journal.go`.

## 4. Tests

- [x] 4.1 Writer: flush via `InsertBatch`, `Stop` drains queue (mock repo).
- [x] 4.2 Cursor round-trip + invalid decode.
- [x] 4.3 Bulk: list-style bulk endpoints write no `batch` event; `BulkAction` still writes one.
- [x] 4.4 Update journal/MCP/graph test constructor call sites.

## 5. Verification

- [x] 5.1 `go build ./...` from `apps/server`, and `go build ./...` from `apps/cli`.
- [x] 5.2 `go vet` changed packages; gofmt clean.
- [x] 5.3 `go test ./domain/journal/... ./domain/mcp/... ./domain/graph/...` — journal + mcp pass; graph journal-relevant tests pass (`TestBulkActionIntegration_AuditLog`, `TestBulkCreateObjects_NoBatchEvent`). Unrelated `kb.orgs`-dependent work-object tests transiently fail under parallel test-DB contention in this environment (verified passing on clean `main` and in isolation).
- [x] 5.4 Goose Up/Down for `00227` (reversible; applied cleanly during test bootstrap).
- [x] 5.5 Feed query `EXPLAIN` on dev confirms it uses `idx_project_journal_project_created (project_id, created_at DESC)` with a cheap `branch_id IS NULL` post-filter; the dropped branch index is not used, so the plan is unchanged.
- [x] 5.6 Lint: `golangci-lint run ./domain/journal/... ./internal/config/...` → 0 issues.
- [ ] 5.7 Manual: `memory journal` list + cursor; MCP `journal-list` (pending live server on this branch).
