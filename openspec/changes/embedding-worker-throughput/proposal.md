## Why

The graph embedding worker (`apps/server/domain/extraction/graph_embedding_worker.go`) claimed a whole tranche of jobs and then blocked on `wg.Wait()` for **all** of them before its single-goroutine poll loop could claim the next tranche. Measured on dev during the #684 backfill (issue #743):

- per-job embedding latency: **5.4 s average, 81 s max**;
- sustained throughput: **~5 embeds/s regardless of backlog depth** — raising queue depth did not help, because the pipeline was worker-bound, not enqueue-bound;
- backlog at the time: `pending 84,933`, i.e. ~4.5 h at that rate, most of it spent waiting on a handful of slow requests.

Two independent problems compounded:

1. **Straggler gating.** `processBatch` marked up to `WorkerBatchSize` (200) jobs `processing`, then ran them under a per-batch semaphore of size `WorkerConcurrency`, then waited for every one of them. One 81 s request held the whole tranche open, so no new work was claimed for the duration of the slowest unit.
2. **One object per request.** Each job issued its own single-input `EmbedQueryWithUsage` call, even though the embedding path supports multi-input requests: `embeddings.Service.EmbedDocumentsWithUsage` (and the `vertex`, `genai` and `openai` clients behind it) sends many documents per HTTP request.

Because all 200 jobs were claimed at once while only `WorkerConcurrency` were actually in flight, claimed-but-idle jobs also inflated the `processing` count and sat exposed to the scheduler stale sweep for longer than their real work took.

## What Changes

- **Decouple batch admission from per-job completion.** `GraphEmbeddingWorker.processBatch` now claims only jobs for which there is free in-flight capacity and dispatches them asynchronously. It never waits for a tranche to finish, so a slow request holds only its own chunk's slots. A `wakeCh` signal refills capacity as soon as a chunk completes instead of waiting for the next poll tick.
- **Bound in-flight work.** The number of jobs in flight is capped by the effective concurrency (`WorkerConcurrency`, still optionally scaled by the system-health scaler). Claiming more than can run is what leaves idle-but-`processing` jobs behind.
- **Embed multiple objects per request, grouped per project.** Jobs in a chunk are loaded with one query, grouped by project — embedding credentials and budgets are resolved per project — and embedded with a single `EmbedDocumentsWithUsage` call per group. Services without a multi-object method fall back to one request per job (each job its own chunk, so jobs never serialize behind each other).
- **Preserve job-lifecycle semantics.** Every claimed job still reaches exactly one state through the existing `MarkCompleted` / `MarkFailed` / `MarkPermanentlyFailed` / `DeleteJob` paths; permanent-vs-transient error classification, the per-project budget reschedule, and usage recording are unchanged (#705, #776, #781 are not regressed). A slow job is never failed or reaped as stale for being slow, and no job is lost or double-processed.
- **Drain on shutdown.** `Stop` waits for in-flight chunk goroutines after the poll loop exits, so shutdown no longer abandons claimed jobs mid-flight (previously the loop's `wg.Wait` incidentally provided this).
- **New configuration.** `EmbeddingRequestBatchSize` (env `GRAPH_EMBEDDING_REQUEST_BATCH_SIZE`, default 100) sets objects per embedding request.

## Capabilities

### Added Capabilities

- `embedding-worker-throughput`: the embedding worker's admission, batching, concurrency and job-lifecycle guarantees.

## Impact

### Code Changes

- `apps/server/domain/extraction/graph_embedding_worker.go`: `processBatch` rewritten (capacity-bounded asynchronous admission, no whole-batch wait); new `processChunk` / `processProjectGroup` / `fetchObjects` / `markJobFailed` / `storeEmbedding` / `dropMissingObjectJob` helpers; `Stop` drains in-flight work.
- `apps/server/domain/extraction/graph_embedding_jobs.go`: `GraphEmbeddingConfig.EmbeddingRequestBatchSize` (default 100).
- `apps/server/domain/extraction/module.go`: wires `EmbeddingQueue.GraphRequestBatchSize` into the graph embedding config.
- `apps/server/internal/config/config.go`: `EmbeddingQueueConfig.GraphRequestBatchSize`.
- Tests: `apps/server/domain/extraction/graph_embedding_worker_throughput_test.go` (hermetic, `internal/testdb`).

### Configuration

- Env var added: `GRAPH_EMBEDDING_REQUEST_BATCH_SIZE` (default `100`).
- Existing knobs unchanged: `GRAPH_EMBEDDING_CONCURRENCY` (max in-flight jobs, default 200), `GRAPH_EMBEDDING_BATCH_SIZE` (jobs claimed per poll, default 200), `EMBEDDING_ADAPTIVE_SCALING`.

### Testing Requirements

- `go build ./...` and `go test ./domain/extraction/... ./internal/config/...` in `apps/server` (DB-backed tests need `TEST_DATABASE_URL` + `REQUIRE_DB=1`).
- New tests prove: a straggler does not gate the batch (`processBatch` returns without waiting, other jobs complete while it is blocked), objects are embedded in one request per project, and in-flight admission is bounded by concurrency.
- `golangci-lint run --new-from-rev=origin/main` and `gofmt -l` on changed files.
- `openspec validate --strict`.
- No migrations are touched. Embedding providers are never called: tests inject stub embedding services.
