## Why

PR #795 removed the **primary** embedding throughput ceiling: `GraphEmbeddingWorker.processBatch` no longer claims a whole tranche and `wg.Wait()`s it, so a slow provider request can no longer gate the batch. That PR deliberately left three further ceilings in scope of issue #796:

1. **`EmbeddingSweepWorker.sweepObjects` admission cap.** The sweep selected at most `BatchSize` (200) objects per sweep on a 30 s cadence, i.e. ≤6.7 objects/s admitted into `kb.graph_embedding_jobs` — a hard admission ceiling independent of how fast the worker can actually drain. With #795 the worker drains faster, so admission becomes the next bound.
2. **`sweepRelationships` is serial.** Up to 200 relationships were embedded **one request at a time**, inside the sweep loop: no batching, no parallelism, even though the embedding path supports multi-input requests (`EmbedDocumentsWithUsage`).
3. **Two workers still use the old `wg.Wait` tranche shape.** `ChunkEmbeddingWorker.processBatch` and `GraphRelationshipEmbeddingWorker.processBatch` still claim `WorkerBatchSize` jobs, run them under a semaphore, and wait for the whole tranche before the poll loop can claim more — the exact defect #795 fixed for the graph worker. A slow request gates the tranche (and inflates `processing` with claimed-but-idle jobs exposed to the stale sweep).

## What Changes

- **Ceiling 1 — object admission keyed to queue depth.** `sweepObjects` reads the number of active (`pending`|`processing`) graph embedding jobs and admits `max(0, ObjectQueueTargetDepth − active)` objects per sweep, instead of a constant `BatchSize` per 30 s. A failed depth read admits nothing (fail-closed) rather than risking a flood. `EnqueueBatch` still de-duplicates (`NOT EXISTS` pre-filter + `ON CONFLICT DO NOTHING`), so no object is double-enqueued. `BatchSize` is retained only as the relationship scan limit.
- **Ceiling 2 — relationships batched per project.** `sweepRelationships` groups the scanned rows by project (credentials and budgets are resolved per project), performs one budget pre-flight per project, and embeds each group with multi-object `EmbedDocumentsWithUsage` requests split into `RelationshipRequestBatchSize` sub-batches (default 100 = the provider clients' own split point). Rows are stored independently; a per-request or per-row failure counts only the affected rows and leaves their `embedding` NULL for a later sweep. A service without a multi-object method (or a negative batch size) keeps the original one-request-per-relationship path.
- **Ceiling 3 — port #795's admission design to the chunk and relationship workers.** Both `processBatch` implementations now claim only as many jobs as there is free in-flight capacity, reserve it, dispatch asynchronously, and release it per job; a `wakeCh` refills capacity as soon as a job finishes instead of waiting for the next poll tick; `Stop` drains in-flight jobs. No whole-tranche `wg.Wait`.
- **Lifecycle semantics preserved exactly.** No job is lost, double-processed, left `processing`, or reaped as stale for being slow. Every claimed job still reaches exactly one state through the untouched `processJob` / `markJobFailed` / `storeEmbedding` / `DeleteJob` paths (permanent-vs-transient classification, budget reschedule, usage recording, metrics counters). Sweep relationships have no queue, so a failed request simply leaves `embedding` NULL for retry — never a wrong-length vector, never a `failed` marker.

## Capabilities

### Added Capabilities

- `embedding-sweep-ceilings`: the embedding sweep worker's admission/batching behaviour and the chunk/relationship embedding workers' batch-admission guarantees.

## Impact

### Code Changes

- `apps/server/domain/extraction/embedding_sweep_worker.go`: `EmbeddingSweepConfig.ObjectQueueTargetDepth` + `RelationshipRequestBatchSize`; depth-keyed `sweepObjects` + `activeGraphEmbeddingJobs`; per-project batched `sweepRelationships` + `groupRelationshipsByProject` / `embedRelationshipGroupBatched` / `embedRelationshipGroupSerial`.
- `apps/server/domain/extraction/chunk_embedding_worker.go`: async, capacity-bounded `processBatch` (`jobsWg`, `inFlight`, `wakeCh`, `concurrency()`), `processTrackedJob`, `Stop` drain.
- `apps/server/domain/extraction/graph_relationship_embedding_worker.go`: the same admission port (keeping its pause check and constructor-built scaler).
- Tests (hermetic, `internal/testdb`): `embedding_sweep_worker_throughput_test.go`, `embedding_worker_admission_test.go`.

### Configuration

- `EmbeddingSweepConfig.ObjectQueueTargetDepth` (default 2000): target active graph embedding jobs; admission per sweep = `max(0, target − active)`. Rationale for the default: it is 10× the old constant per-sweep cap, and comfortably above what the graph worker (200 in-flight jobs, ~100 objects/request → ~2 concurrent requests) drains within one 30 s sweep at the dev-scale request latency observed in #743 (5.4 s average). It is a tunable target, not a measured throughput claim.
- `EmbeddingSweepConfig.RelationshipRequestBatchSize` (default 0 → 100; negative → serial): relationships per embedding request.
- No env plumbing today: `EmbeddingSweepConfig` is built from `DefaultEmbeddingSweepConfig()` in `NewExtractionConfig` (it is not env-driven), so this remains a code-level default.

### Testing Requirements

- `go build ./...` and `go test ./domain/extraction/...` in `apps/server` (`TEST_DATABASE_URL` + `REQUIRE_DB=1` for the DB-backed tests), including `-race` for the changed workers.
- New hermetic tests prove: admission admits up to target depth (not the old constant) and stops at depth; admission leaves room for existing active jobs; relationships are embedded one multi-object request per project; the serial fallback still works; a failed batch counts every row and leaves embeddings NULL; and, for both ported workers, a gated slow request does not gate the tranche (`processBatch` returns immediately, peers complete, no job is failed or left `processing`) and in-flight work is bounded by concurrency.
- `golangci-lint run --new-from-rev=origin/main` and `gofmt -l` on changed files.
- `openspec validate --strict`. No migrations. Embedding providers are never called: tests inject stub embedding services.
