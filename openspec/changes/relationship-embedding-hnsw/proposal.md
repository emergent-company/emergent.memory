## Why

`kb.graph_relationships.embedding` (~82k embedded 768-dim vectors) is served by an ivfflat index whose planner cost-model estimate scales roughly linearly with `ivfflat.probes`, while the competing Parallel Seq Scan estimate prices the TOASTed vectors as free to read. On dev the two estimates cross over between `probes=5` and `probes=10`, so at the shared default (`SEARCH_IVFFLAT_PROBES=10`) the planner abandons the index:

| filter | `ivfflat.probes` | plan | exec |
|---|---|---|---|
| `src.project_id` | 10 | Parallel Seq Scan + Sort | 400,072 ms |
| `r.project_id` | 10 | Parallel Seq Scan + Sort | 86,674 ms |
| `r.project_id` | 5 | Index Scan (cost ~5.2k) | 248 ms |
| `r.project_id` | 1 | Index Scan (cost ~1.0k) | 451 ms |

PR #696 added a relationship-only `SEARCH_RELATIONSHIP_IVFFLAT_PROBES=5` stopgap. It works, but it trades recall (~90% → ~75% estimated) for a plan crossover that moves as the table grows, and keeps the whole class of probe-dependent plan flips alive.

## What Changes

- Add migration `00171_graph_relationships_embedding_hnsw.sql`: build an HNSW index on `kb.graph_relationships.embedding` (`m = 16`, `ef_construction = 64`) `CONCURRENTLY`, then drop the superseded ivfflat index `CONCURRENTLY` — create-before-drop so an ANN index is always available. Down recreates the ivfflat index and drops HNSW.
- Remove the `SEARCH_RELATIONSHIP_IVFFLAT_PROBES` knob and `configuredRelationshipIVFFlatProbes` helper. HNSW has no `probes` setting, so relationship search no longer opens a probes-tuned transaction (`SET LOCAL ivfflat.probes` would be a no-op for the index); it now runs the single read-only SELECT directly, like the lexical leg.
- Drop the now-stale `idx_graph_relationships_embedding_ivfflat` target from the scheduled embedding-index reindex list (same treatment the graph-objects ivfflat index got when migration `00164` moved it to HNSW); HNSW has no list clustering to degrade and needs no periodic `REINDEX`.
- Document the recall validation plan and comparison against a brute-force baseline.

### Assessment: does `search.Service` still need `ivfflat.probes`?

No — not for the relationship leg, and not for the graph-object leg. After this change the only ivfflat index left in the unified-search path is `kb.chunks` (chunk vector/hybrid search), so `SEARCH_IVFFLAT_PROBES` remains meaningful only for chunk search. `search.Service` itself has no direct `probes` dependency: it calls the search repositories, and the graph-object (`kb.graph_objects.embedding_v2`, HNSW since migration `00164`) and relationship (`kb.graph_relationships.embedding`, HNSW since `00171`) legs are no longer governed by the knob. No service-level code change is needed; the spec is narrowed accordingly.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `retrieval-performance-config`: remove the relationship-specific `SEARCH_RELATIONSHIP_IVFFLAT_PROBES` requirement and narrow the `SEARCH_IVFFLAT_PROBES` requirement to chunk search; the relationship ANN leg uses HNSW (migration `00171`) and is not tuned by `ivfflat.probes`.

## Impact

- **Database**: new Goose migration `apps/server/migrations/00171_graph_relationships_embedding_hnsw.sql` (HNSW create + ivfflat drop, `CONCURRENTLY`/`NO TRANSACTION`; reversible).
- **Server**: `apps/server/domain/search/repository.go` — remove `configuredRelationshipIVFFlatProbes`, drop the probes transaction in `SearchRelationships`, update the index doc comment.
- **Scheduler**: `apps/server/domain/scheduler/embedding_index_reindex_task.go` (+ its test) — remove the stale `idx_graph_relationships_embedding_ivfflat` reindex target.
- **Docs**: `docs/features/graph/triplet-embeddings.md` — index section and operations SQL now reference the HNSW index.
- **Tests**: `apps/server/domain/search/search_test.go` — remove the now-invalid knob/default/guard tests; `embedding_index_reindex_task_test.go` — drop the removed target.
- **Deploy order**: apply migration `00171` before rolling the new binary. Between the two, relationship search runs on the still-present ivfflat index with the session default (`probes=1`): a transient recall dip, never the seq-scan cliff.
- **Out of scope / overlapping lane**: graph-objects/chunks/skills embedding indexes are handled by a sibling lane (#670); this change touches only `kb.graph_relationships`.
- No API or response-shape change.

## Verification

### Migration apply / revert

Validated against a throwaway pgvector 0.8.6 container (768-dim, 8000 rows), each `CONCURRENTLY` statement run as a single autocommit statement with the app-default `search_path` (`"$user", public`, no `kb`):

- **Bug reproduced + fixed**: with the default search_path, an unqualified `DROP INDEX CONCURRENTLY IF EXISTS idx_graph_relationships_embedding_hnsw` emits `NOTICE: index ... does not exist, skipping` and leaves the index in place (HNSW and ivfflat would coexist, and rollback would leave HNSW behind). All drops are now schema-qualified with `kb.`; the `CREATE` names must stay unqualified because `CREATE INDEX` derives the schema from the table reference.
- **UP** → only `idx_graph_relationships_embedding_hnsw` present, `indisvalid = true`; re-running UP succeeds and is a no-op (idempotent).
- **DOWN** → only `idx_graph_relationships_embedding_ivfflat` present, valid.

### recall@k vs brute-force baseline

**Not measured on real dev vectors.** The ~82k-embedded-relationship dev dataset lives on the remote `memory-infra` host and is only reachable through the read-only, single-statement MCP query path (no psql/docker there); the localhost dev Postgres (`memtest-db`, `kb.graph_relationships`) has **zero** rows, and no other local database holds real embeddings. Measured instead on a scratch pgvector DB with two synthetic corpora — a worst-case isotropic corpus and an embedding-like clustered/anisotropic corpus. Absolute recall at 82k real rows will differ; the comparison is qualitative.

Setup: 768-dim, cosine distance; exact baseline with `enable_indexscan=off` + `max_parallel_workers_per_gather=0`; one index family present at a time; recall@k = mean over 30 queries of `|exact_topk ∩ ann_topk| / k`; latency = median in-DB execution time at k=30.

Isotropic uniform vectors (8000 rows) — pathological worst case for ANN:

| config | recall@10 | recall@30 | median ms |
|---|---|---|---|
| exact seq scan (baseline) | 1.0000 | 1.0000 | 36.0 |
| ivfflat lists=100, probes=1 | 0.1567 | 0.0944 | 0.67 |
| ivfflat lists=100, probes=5 | 0.2267 | 0.1656 | 0.99 |
| ivfflat lists=100, probes=10 | 0.3100 | 0.2444 | 1.74 |
| hnsw m=16, ef_construction=64, ef_search=40 | 0.7933 | 0.7333 | 3.77 |
| hnsw m=16, ef_construction=64, ef_search=100 | 0.8500 | 0.8000 | 4.34 |

Clustered/anisotropic vectors (4000 rows, 200 centers) — embedding-like:

| config | recall@10 | recall@30 | median ms |
|---|---|---|---|
| exact seq scan (baseline) | 1.0000 | 1.0000 | 17.5 |
| ivfflat lists=100, probes=1 | 0.7300 | 0.7778 | 0.8 |
| ivfflat lists=100, probes=5 | 0.8433 | 0.8744 | 0.9 |
| ivfflat lists=100, probes=10 | 0.8700 | 0.9044 | 1.1 |
| hnsw m=16, ef_construction=64, ef_search=40 | 1.0000 | 1.0000 | 1.8 |
| hnsw m=16, ef_construction=64, ef_search=100 | 1.0000 | 1.0000 | 2.7 |

Takeaway: on embedding-like data HNSW reaches exact recall at ~2ms with no probes knob, while ivfflat tops out at ~0.87/0.90 and needs per-deployment probe tuning — and on the real 82k-row table it is a high probes value that triggered the planner's seq-scan cliff (`probes=10` → 400s/86s vs `probes=5` → 248ms).
