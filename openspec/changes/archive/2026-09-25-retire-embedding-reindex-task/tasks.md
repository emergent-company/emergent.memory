## 1. Remove the no-op reindex task

- [x] 1.1 Delete `apps/server/domain/scheduler/embedding_index_reindex_task.go` (task, empty `embeddingIndexTargets`, helpers) and `embedding_index_reindex_task_test.go`
- [x] 1.2 Remove the `embedding_index_reindex` registration from `apps/server/domain/scheduler/module.go`
- [x] 1.3 Remove `EmbeddingReindexSchedule` / `EmbeddingReindexInterval` and the `EMBEDDING_REINDEX_SCHEDULE` / `EMBEDDING_REINDEX_INTERVAL` reads from `apps/server/domain/scheduler/config.go`

## 2. Retire the vestigial `ivfflat.probes` knob

- [x] 2.1 `domain/search/repository.go`: remove `configuredIVFFlatProbes` / `beginTxWithIVFFlatProbes`; run `VectorSearch` and `HybridSearch` vector queries directly against `r.db`
- [x] 2.2 `domain/graph/repository.go`: remove `configuredIVFFlatProbes` / `beginTxWithIVFFlatProbes`; run `VectorSearch` and `SimilarObjects` directly against `r.db`
- [x] 2.3 `domain/skills/store.go`: remove `beginTxWithIVFFlatProbes`; `FindRelevant` runs its select against `r.db`
- [x] 2.4 Drop the now-unused `SEARCH_IVFFLAT_PROBES` env surface and the imports that only served the deleted helpers

## 3. Spec

- [x] 3.1 Add this OpenSpec change with a `retrieval-performance-config` delta: `REMOVED` the probes requirement and `MODIFIED` "Embedding ANN indexes use HNSW" to record the retired reindex task (no `probes`/`lists` tuning wording retained)

## 4. Verification

- [x] 4.1 `go build ./...` and `go test ./domain/scheduler/...` in `apps/server`
- [x] 4.2 `go test ./domain/search/... ./domain/graph/... ./domain/skills/...`
- [x] 4.3 `golangci-lint run ./domain/scheduler/...`
- [x] 4.4 `openspec validate --strict`
