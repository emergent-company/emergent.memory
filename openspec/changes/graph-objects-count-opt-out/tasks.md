## 1. Evidence — scratch Postgres plans (issues #732, #733)

- [x] 1.1 Spin up an isolated `pgvector/pgvector:pg17` container (host port 55432–55435, never the shared dev DB) with a faithful subset of `kb.graph_objects` (≈1.6 KB/row) and the `00163` partial index. Verify: `EXPLAIN (ANALYZE, BUFFERS)` runs against the scratch DB only.
- [x] 1.2 Seed ≥120k HEAD rows in one dominant project plus non-HEAD/branch/deleted rows, vacuum, then churn so the visibility map is partially stale. Verify: `pg_class.relallvisible` and `Heap Fetches` recorded.
- [x] 1.3 Capture BEFORE count/list plans with the `00163` index, then with an additional narrow `(project_id)` partial index, then AFTER `VACUUM (ANALYZE)`. Verify: raw plans recorded in `design.md` / PR body.
- [x] 1.4 Confirm the narrow partial index is *matched* (Index Cond) yet still reports `Heap Fetches: 49525` while the VM is stale. Verify: plan captured.
- [x] 1.5 Record what could not be measured: absolute dev-scale latency (18–32 s) on dev storage; only the mechanism and the *relative* improvement are reproduced locally.

## 2. Migration — autovacuum tuning (issue #732)

- [x] 2.1 Add `apps/server/migrations/00176_objects_count_optimizations.sql`: `ALTER TABLE kb.graph_objects SET (autovacuum_vacuum_scale_factor=0.02, autovacuum_vacuum_threshold=500, autovacuum_analyze_scale_factor=0.02, autovacuum_analyze_threshold=500)`, with a header recording the measured plans and why no index is added. Verify: `reloptions` readback on the scratch DB shows the four settings.
- [x] 2.2 Down migration `RESET`s the same four reloptions. Verify: `reloptions` is empty after rollback on the scratch DB.
- [x] 2.3 Do not run `VACUUM` inside the migration (goose wraps it in a transaction) and do not touch any real environment. Verify: migration contains only the `ALTER TABLE`.

## 3. API — opt out of the exact total (issue #733)

- [x] 3.1 `SearchGraphObjectsResponse.Total` becomes `*int` with `json:"total,omitempty"`. Verify: unit test — nil total omits the field, non-nil zero still emits `"total":0`.
- [x] 3.2 `ListParams.SkipTotal` (zero value = exact total) and `Service.List` skips the count goroutine when set. Verify: `go build ./...`; DB test asserts `SkipTotal: true` returns no error and a nil `Total`.
- [x] 3.3 `Handler.ListObjects` parses `include_total=false`; swagger `@Param` added. Verify: handler sets `SkipTotal` only for the literal `false`.
- [x] 3.4 SDK `ListObjectsOptions.SkipTotal` sends `include_total=false`; `SearchObjectsResponse.Total` documents the 0-when-skipped case. Verify: `go build ./...` in `pkg/sdk`.
- [x] 3.5 Web UI `ListGraphObjects` and `ListMemories` opt out (they never read `total`). Verify: gateway `go build ./...`.

## 4. Spec / OpenSpec

- [x] 4.1 Add this change directory with delta specs for the new capability `graph-objects-search-count`. Verify: `openspec validate graph-objects-count-opt-out --strict`.

## 5. Verification

- [x] 5.1 `cd apps/server && go build ./...`. Verify: clean.
- [x] 5.2 `go test ./domain/graph/... -count=1` (unit; DB tests skip under `-short`). Verify: pass.
- [x] 5.3 `go build ./...` in `apps/web-ui/gateway`. Verify: clean.
- [x] 5.4 `golangci-lint run` on the touched packages (repo has ~269 pre-existing findings elsewhere). Verify: no new findings in the changed files.
- [x] 5.5 `openspec validate graph-objects-count-opt-out --strict`. Verify: valid.
