# Tasks

## 1. Bounded worker pool

- [x] Add `runBulkItems(n, concurrency, fn)` — every index visited exactly once, workers clamped to `[1, n]`.
- [x] Add `GRAPH_BULK_CONCURRENCY` to `GraphConfig` (default `8`).
- [x] Add `Repository.BulkConcurrency`, clamped to `[1, DB_MAX_OPEN_CONNS-1]` (floor 1), and expose it from `NewRepository`.
- [x] Use the pool in `BulkCreateObjects`, `BulkUpsertObjects`, `BulkUpdateObjects`; keep result slices index-aligned.

## 2. Correctness guardrails

- [x] Keep per-item `workerCtx` so a disconnect/expiry still fails each item individually.
- [x] Make the test embedding enqueuer mutex-guarded (parallel enqueue under `-race`).
- [x] DB tests: one HEAD per object + `supersedes_id` link + `ContentHash` after a 25-item parallel batch.
- [x] DB tests: two items on the same object serialize into a linear chain (one HEAD).
- [x] DB tests: per-item isolation and index alignment under the pool.
- [x] DB tests: parallel bulk upsert keeps one HEAD per key.

## 3. Tests + benchmark

- [x] Deterministic concurrency-bound test (`TestRunBulkItems_BoundedConcurrency`) — mutation-check: unbounded spawning fails it.
- [x] `BenchmarkBulkUpdateObjects_SerialVsParallel` (concurrency 1 vs 8, DB-backed) demonstrates the wall-time improvement.
- [x] Full `./domain/graph/...` suite under `REQUIRE_DB=1`, plus `-race` on the parallel paths.

## 4. Verification

- [x] `go build ./...`
- [x] `TEST_DATABASE_URL=… REQUIRE_DB=1 go test -count=1 ./domain/graph/...`
- [x] `bash apps/server/scripts/lint-ratchet.sh`
- [x] `bash scripts/preflight/migration-number.sh`
- [x] `openspec validate bounded-graph-bulk-concurrency --strict`
