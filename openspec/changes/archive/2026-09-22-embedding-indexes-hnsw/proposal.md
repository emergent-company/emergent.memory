## Why

Issue #670 follows up #664 (periodic ivfflat `REINDEX`). IVFFlat is a poor fit for
embedding columns that grow by incremental backfill: its `lists` partition count is
fixed at build time and the index must be trained on a representative sample, so
every batch of newly-inserted vectors re-fragments the lists and silently degrades
recall until a full `REINDEX` runs. #664 papered over this with a nightly `REINDEX
INDEX CONCURRENTLY` of every ivfflat embedding index.

HNSW has no training step, tolerates incremental inserts, and needs neither
`ivfflat.probes` tuning nor a scheduled rebuild. It is the better default for these
columns.

`kb.graph_objects.embedding_v2` was already migrated to HNSW in `00164` (#707).
This change migrates the two remaining in-scope indexes; the relationship index is
handled by a separate change and is deliberately untouched here.

## What Changes

- Add migration `apps/server/migrations/00170_embedding_indexes_hnsw.sql`:
  - `kb.idx_chunks_embedding` (on `kb.chunks.embedding`) → `idx_chunks_embedding_hnsw`
  - `kb.idx_skills_embedding_ivfflat` (on `kb.skills.description_embedding`) → `idx_skills_embedding_hnsw`
  - Each index is built `CREATE INDEX CONCURRENTLY ... USING hnsw (col vector_cosine_ops) WITH (m = 16, ef_construction = 64)` and only then is the old ivfflat index dropped, one index at a time, so a valid ANN index is always available and lock impact is bounded. `-- +goose NO TRANSACTION`.
- Remove the chunks and skills indexes from the scheduler's `embeddingIndexTargets`.
  Once their ivfflat indexes are dropped, a scheduled `REINDEX INDEX` would fail
  every night and be reported as an aggregate task failure. The remaining (still
  ivfflat) target is left in place.
- Document that `ivfflat.probes` (and `SEARCH_IVFFLAT_PROBES`) is now a harmless
  no-op for chunk and graph-object vector search. The code keeps applying it per
  transaction to avoid a behaviour change; only the documentation and spec change.
- Add this OpenSpec change and its delta to the `retrieval-performance-config`
  capability.

## Trade-off decision

Costs measured read-only on the deployed dev environment (`ssh memory-dev`); row
counts differ on the local `memtest-db` checkout, which is at migration 155 and
holds almost no rows, so the numbers below are the meaningful ones:

| Column | Rows | Embedded | Current ivfflat index |
|---|---|---|---|
| `kb.graph_objects.embedding_v2` | 107,938 | 107,433 (~99.5%) | 420 MB (already HNSW after `00164`) |
| `kb.chunks.embedding` | 43 | 20 | 1272 kB |
| `kb.skills.description_embedding` | 3 | 0 | 1208 kB |

- **Build time + memory**: HNSW builds are slower and use more RAM than ivfflat,
  so `m` is kept at the pgvector default (`m = 16`) to bound graph size; ingestion
  memory is bounded by `maintenance_work_mem`. For the two columns this change
  touches, the tables hold tens of rows, so the concurrent build is effectively
  instantaneous (verified end-to-end on a scratch database in < 200 ms for the
  migration). The heavy column (`graph_objects`, 420 MB index) was already moved by
  `00164`; the decision recorded here is to make the remaining indexes consistent
  rather than to trade HNSW against ivfflat at scale.
- **Recall/throughput**: ivfflat with `probes=10` scans only ~10% of its lists, and
  the `lists` partition count is frozen at build time, so every incremental
  backfill re-fragments it and silently degrades recall until a full `REINDEX`
  (issue #664). HNSW has no training step and returns near-exact results with a
  bounded `ef_search`, and it degrades far more gracefully under filtered queries
  (project/tenant predicates) because it walks a graph rather than relying on list
  coverage. At the current chunks/skills sizes recall is already ~exact, so the
  change is about removing the training/REINDEX liability and matching `00164`.
- **Version constraint**: HNSW requires pgvector >= 0.5.0. Confirmed 0.8.6 on local
  `memtest-db`; the `00164` graph-objects index already relies on the same floor.

## Nightly REINDEX (#664)

HNSW removes the need for the periodic `REINDEX` on the columns it covers, because
there is no list clustering to restore. This change therefore removes `chunks` and
`skills` from the scheduler's `embeddingIndexTargets`; leaving them in would make
the nightly task issue `REINDEX` against dropped indexes and fail every night.

The task/config (`EMBEDDING_REINDEX_SCHEDULE` / `EMBEDDING_REINDEX_INTERVAL`) is
**left in place**: one ivfflat embedding index remains and is owned by a separate
change, so deleting the task now would remove coverage prematurely. The
operator-visible change is that the nightly `embedding_index_reindex` job has less
work to do; its configuration keys are unchanged. Once the last ivfflat index is
migrated the task becomes a no-op and can be deleted wholesale.

## Migration hygiene

`CREATE INDEX CONCURRENTLY` / `DROP INDEX CONCURRENTLY` cannot run in a
transaction, so `00170` is a `-- +goose NO TRANSACTION` migration and each
statement commits independently. To avoid the silent-partial-application failure
mode (a migration recorded as applied while an index is missing or INVALID):

- each new index is `DROP ... IF EXISTS`'d before `CREATE ... IF NOT EXISTS`, so a
  retry after a failed `CONCURRENTLY` build rebuilds cleanly instead of reusing an
  INVALID index (the convention from `00162`–`00164`);
- the migration ends with a `pg_index`/`pg_am` guard that raises unless both HNSW
  indexes exist and are valid and both legacy ivfflat indexes are gone, so goose
  refuses to record the version on a partial run;
- the read-only verification query is documented in the migration for operators.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `retrieval-performance-config`: embedding ANN indexes for graph objects, chunks and
  skills use HNSW, so `ivfflat.probes` no longer governs chunk/graph-object search and
  the periodic ivfflat reindex task no longer targets them.

## Impact

- **Database**: `apps/server/migrations/00170_embedding_indexes_hnsw.sql` (concurrent
  build/drop, reversible via matching `-- +goose Down` that restores the ivfflat indexes
  with `lists = 100`).
- **Scheduler**: `embedding_index_reindex_task.go` target list shrinks; tests updated.
- **Docs/spec**: `retrieval-performance-config` spec + code doc comments.
- **Operator-visible behaviour**: the nightly `embedding_index_reindex` task now has
  less (currently one index) to rebuild. As more indexes move to HNSW the task will
  eventually have nothing to target; its config (`EMBEDDING_REINDEX_SCHEDULE` /
  `EMBEDDING_REINDEX_INTERVAL`) is intentionally left in place until the last ivfflat
  index is migrated, at which point the task can be deleted wholesale.
- **No API or result-contract changes**: only index type and recall/latency
  characteristics change.
