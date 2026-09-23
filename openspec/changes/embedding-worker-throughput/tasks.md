# Tasks

## 1. Decouple batch admission from per-job completion

- [x] 1.1 Rewrite `GraphEmbeddingWorker.processBatch` to claim only jobs for the free in-flight capacity and dispatch them asynchronously (no whole-tranche `wg.Wait`)
- [x] 1.2 Track in-flight jobs (`inFlight`) and in-flight chunk goroutines (`jobsWg`); release capacity when a chunk finishes
- [x] 1.3 Signal the poll loop (`wakeCh`) when a chunk frees capacity so the next tranche is claimed without waiting for the next tick
- [x] 1.4 Make `Stop` drain in-flight chunks after the poll loop exits

## 2. Multi-object requests, grouped per project

- [x] 2.1 Add the optional `batchEmbeddingService` interface and use `EmbedDocumentsWithUsage` per project group
- [x] 2.2 Fetch every chunk's objects in one query and group them by project (credentials/budgets are per project)
- [x] 2.3 Fall back to one request per job when the embedding service has no multi-object method (chunk size 1, jobs never serialized)
- [x] 2.4 Add `GraphEmbeddingConfig.EmbeddingRequestBatchSize` (default 100) and wire `GRAPH_EMBEDDING_REQUEST_BATCH_SIZE` through `EmbeddingQueueConfig` / `NewExtractionConfig`

## 3. Preserve job-lifecycle semantics

- [x] 3.1 Route every failure through `markJobFailed` (permanent-vs-transient classification, `MarkFailed` / `MarkPermanentlyFailed`) and every success through `storeEmbedding` (`MarkCompleted`)
- [x] 3.2 Keep the per-project budget reschedule (`budget_exceeded`, 5-minute delay, no attempt increment) and usage recording (one event per project group)
- [x] 3.3 Keep the missing-object path deleting the job rather than retrying it

## 4. Tests (TDD)

- [x] 4.1 `chunkJobs` pure unit test (single / exact / ragged / per-job / empty)
- [x] 4.2 Hermetic test: a gated (slow) embedding request does not gate the batch — `processBatch` returns immediately, other jobs complete while it is blocked, and the slow job still completes once released
- [x] 4.3 Hermetic test: objects are embedded in one multi-object request per project, with no single-object requests
- [x] 4.4 Hermetic test: admission is bounded by concurrency — no job is claimed without a free slot
- [x] 4.5 Assert the default `EmbeddingRequestBatchSize` in the config test

## 5. Verification

- [x] 5.1 `go build ./...` in `apps/server`
- [x] 5.2 `go test ./domain/extraction/... ./internal/config/...` (with `TEST_DATABASE_URL` + `REQUIRE_DB=1` for the DB-backed tests), including `-race` for the new tests
- [x] 5.3 `golangci-lint run --new-from-rev=origin/main` and `gofmt -l` on changed files
- [x] 5.4 `openspec validate --strict`
