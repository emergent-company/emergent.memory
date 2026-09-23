# Tasks

## 1. Ceiling 1 — object admission keyed to queue depth

- [x] 1.1 Add `EmbeddingSweepConfig.ObjectQueueTargetDepth` (default 2000) and `defaultSweepObjectQueueTargetDepth`; document `BatchSize` as the relationship scan limit only
- [x] 1.2 Add `activeGraphEmbeddingJobs(ctx)` counting `pending`|`processing` `kb.graph_embedding_jobs`
- [x] 1.3 Rewrite `sweepObjects` to admit `max(0, target − active)` objects (`LIMIT admission` replacing `LIMIT BatchSize`), fail-closed on a failed depth read
- [x] 1.4 Keep the existing model-config gate, `NOT EXISTS` active-job filter, `ORDER BY created_at ASC`, `EnqueueBatch` de-duplication and metrics

## 2. Ceiling 2 — relationships batched per project

- [x] 2.1 Add `EmbeddingSweepConfig.RelationshipRequestBatchSize` (0 → 100, negative → serial)
- [x] 2.2 Group scanned rows by project preserving first-seen order (`groupRelationshipsByProject`)
- [x] 2.3 One budget pre-flight per project (fail-open; skip the whole group when exceeded and enforcement is on)
- [x] 2.4 Embed each group with `EmbedDocumentsWithUsage` sub-batches (`embedRelationshipGroupBatched`), store each vector independently, one aggregate usage event per sub-batch
- [x] 2.5 Guard against `nil`/mismatched batch results (count the whole sub-batch as errors, write nothing)
- [x] 2.6 Keep the one-request-per-relationship fallback (`embedRelationshipGroupSerial`) for services without a multi-object method / negative batch size

## 3. Ceiling 3 — port #795 admission to the chunk and relationship workers

- [x] 3.1 `ChunkEmbeddingWorker`: add `jobsWg`, `inFlight`, `wakeCh`; `concurrency()`; capacity-bounded async `processBatch`; `processTrackedJob` releasing capacity; `wakeCh` case in `run`; `Stop` drains in-flight jobs
- [x] 3.2 `GraphRelationshipEmbeddingWorker`: the same port, keeping its pause check and constructor-built scaler
- [x] 3.3 Leave every `processJob` lifecycle path (completion/failure/permanent/dead-letter/delete/budget reschedule/usage/metrics) byte-for-byte unchanged

## 4. Tests (TDD)

- [x] 4.1 Sweep: admission admits up to target depth (500) with `BatchSize` deliberately 200, then admits 0 at depth
- [x] 4.2 Sweep: admission leaves room for existing active jobs (target 10, 6 active → 4 admitted)
- [x] 4.3 Sweep: two projects × two relationships → two multi-object requests of two documents, no single-object request, all stored
- [x] 4.4 Sweep: serial fallback with a single-object-only embedder still stores every relationship
- [x] 4.5 Sweep: a failing batch counts every row as an error and leaves all embeddings NULL
- [x] 4.6 Worker: chunk straggler does not gate the batch; peers complete while the straggler is `processing`; the straggler still completes; none left `processing`
- [x] 4.7 Worker: relationship straggler does not gate the batch (same assertions)
- [x] 4.8 Worker: in-flight admission is bounded by concurrency (2 `processing`, 3 `pending`) for both workers

## 5. Verification

- [x] 5.1 `go build ./...` in `apps/server`
- [x] 5.2 `go test ./domain/extraction/...` with `TEST_DATABASE_URL` + `REQUIRE_DB=1`, including `-race` for the changed workers
- [x] 5.3 `golangci-lint run --new-from-rev=origin/main` and `gofmt -l` on changed files
- [x] 5.4 `openspec validate --strict`
