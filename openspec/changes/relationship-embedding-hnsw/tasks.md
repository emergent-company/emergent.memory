## 1. Migration

- [x] 1.1 Add `apps/server/migrations/00171_graph_relationships_embedding_hnsw.sql`: `CREATE INDEX CONCURRENTLY idx_graph_relationships_embedding_hnsw ... USING hnsw (embedding vector_cosine_ops) WITH (m=16, ef_construction=64)` then `DROP INDEX CONCURRENTLY idx_graph_relationships_embedding_ivfflat` (create-before-drop); `NO TRANSACTION`; Down recreates the ivfflat index and drops HNSW.
- [x] 1.2 Validate apply, idempotent re-apply, and revert on a scratch pgvector DB: HNSW present/valid + ivfflat absent after Up; reversed after Down.

## 2. Server

- [x] 2.1 Remove the `SEARCH_RELATIONSHIP_IVFFLAT_PROBES` knob and `configuredRelationshipIVFFlatProbes` helper.
- [x] 2.2 `SearchRelationships` no longer opens a probes-tuned transaction or executes `SET LOCAL ivfflat.probes`; it runs its single read-only SELECT directly, and the doc comment references HNSW migration `00171`.
- [x] 2.3 Remove the now-invalid tests for the removed knob (`TestConfiguredRelationshipIVFFlatProbes`, `TestRelationshipProbesBelowGlobalDefault`). No replacement unit test: the removed behavior is a SQL/planner concern, and the HNSW behavior is validated by the scratch-DB migration + recall comparison and the existing `search` unit suite.
- [x] 2.4 Remove the stale `idx_graph_relationships_embedding_ivfflat` target from the scheduled embedding-index reindex list and its test expectation (HNSW needs no periodic `REINDEX`), mirroring the graph-objects treatment in `00164`.

## 3. Spec

- [x] 3.1 Add a `retrieval-performance-config` delta: `REMOVED` the relationship probes requirement; `MODIFIED` the global probes requirement to cover chunk search and to note that graph-object (`00164`) and relationship (`00171`) ANN legs are HNSW and not governed by `ivfflat.probes`.

## 4. Verification

- [x] 4.1 `go build ./...`, `go test ./...`, and `golangci-lint run ./...` in `apps/server`.
- [x] 4.2 Recall@10 / recall@30 comparison against a brute-force baseline on a scratch DB seeded with two **synthetic** corpora (worst-case isotropic and embedding-like clustered/anisotropic) — real dev-vector recall was NOT measured (`kb.graph_relationships` has zero rows locally; the ~82k-row dataset is reachable only via the read-only MCP query path). ivfflat `probes=1/5/10` vs HNSW `ef_search=40/100`; numbers recorded in the proposal.
