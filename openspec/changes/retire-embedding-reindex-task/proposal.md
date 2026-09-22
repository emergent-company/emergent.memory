## Why

Issue #664 added a nightly `embedding_index_reindex` scheduler task that runs `REINDEX INDEX CONCURRENTLY` (with an INVALID-index recovery pass) over the ivfflat embedding indexes, because ivfflat list clustering degrades as embeddings are backfilled incrementally.

HNSW has no training/list step and does not fragment under incremental inserts the way ivfflat does, so it needs no periodic rebuild. Migrations `00164` (graph_objects, #707), `00170` (chunks + skills, #670 / #727) and `00171` (graph_relationships, #697 / #731) converted every embedding index to HNSW and dropped the ivfflat originals. Sibling PRs removed each migrated index from `embeddingIndexTargets`, leaving that list **empty**: the task is registered, runs every night, and does no work (issue #735). Every `SET LOCAL ivfflat.probes` statement and the `SEARCH_IVFFLAT_PROBES` / `configuredIVFFlatProbes` knob are equally vestigial — every remaining consumer (chunk search, graph-object search, similar-objects search, skills search) now reads an HNSW index, for which the setting is a no-op.

## What Changes

- **Delete the no-op task.** Remove `EmbeddingIndexReindexTask` (+ its `embeddingIndexTargets` list, `Run`/`reindex`/validity helpers and `quoteIdent`), its tests, the `embedding_index_reindex` registration in `module.go`, and the `EmbeddingReindexSchedule` / `EmbeddingReindexInterval` config fields with the `EMBEDDING_REINDEX_SCHEDULE` / `EMBEDDING_REINDEX_INTERVAL` env surface. The job was permanently empty, so no operator-visible behaviour is lost — only a scheduled no-op disappears.
- **Retire the vestigial probes knob.** Remove `configuredIVFFlatProbes` / `beginTxWithIVFFlatProbes` and every `SET LOCAL ivfflat.probes` from the search, graph and skills repositories, and drop `SEARCH_IVFFLAT_PROBES`. The read-only vector queries now run directly against the repository DB handle instead of inside a probes-tuned transaction; SQL text and result scanning are unchanged. HNSW has no `probes`/`lists` knobs, so there is nothing left to tune.
- **Update the `retrieval-performance-config` spec** to drop the probes requirement and to state that HNSW embedding indexes require neither probe tuning nor a scheduled rebuild, and that no periodic embedding-index reindex task exists.

Removal was chosen over leaving an explicitly neutralized no-op job: the list is already empty, so the task carries no defensive value (an index that reappears via a migration rollback must ship with its own code change anyway), and a permanently-empty scheduled job is confusing operational surface with a misleading name.

## Capabilities

### Modified Capabilities

- `retrieval-performance-config`: the vector-index probes requirement is removed, and the "Embedding ANN indexes use HNSW" requirement now records that the periodic reindex task is gone rather than targeting remaining ivfflat indexes.

## Impact

### Code Changes

- `apps/server/domain/scheduler/embedding_index_reindex_task.go` (+ `_test.go`): deleted.
- `apps/server/domain/scheduler/module.go`: `embedding_index_reindex` registration removed.
- `apps/server/domain/scheduler/config.go`: `EmbeddingReindexSchedule` / `EmbeddingReindexInterval` removed.
- `apps/server/domain/search/repository.go`, `apps/server/domain/graph/repository.go`, `apps/server/domain/skills/store.go`: probes helpers and `SET LOCAL ivfflat.probes` removed; read-only vector queries no longer open a wrapping transaction.

### Configuration

- Env vars removed (now ignored if still set): `EMBEDDING_REINDEX_SCHEDULE`, `EMBEDDING_REINDEX_INTERVAL`, `SEARCH_IVFFLAT_PROBES`.

### Operational Impact

- The nightly `embedding_index_reindex` job no longer appears in the scheduler's registered task list.
- Operators may unset the removed env vars; setting them has no effect.

### Testing Requirements

- `go build ./...` and `go test ./domain/scheduler/...` in `apps/server`.
- `go test ./domain/search/... ./domain/graph/... ./domain/skills/...` (DB-free tests; graph's Postgres-dependent suites remain environment-gated).
- `golangci-lint run ./domain/scheduler/...` (full-repo lint has pre-existing unrelated findings).
- `openspec validate --strict`.
- No migrations are touched by this change.
