## MODIFIED Requirements

### Requirement: Vector index probe count is configurable
The `ivfflat.probes` value applied in chunk search transactions SHALL be configurable via environment (`SEARCH_IVFFLAT_PROBES`), defaulting to 10, and MUST be applied per-query transaction. Relationship vector search uses a separate knob (see Relationship vector search probe count is configurable).

Chunk vector search (`kb.chunks.embedding`) uses the HNSW index created by migration `00170`, and graph-object vector search (`kb.graph_objects.embedding_v2`) uses the HNSW index created by migration `00164`. Neither is tuned by `ivfflat.probes`: their transactions still apply the configured value, but that setting does not affect the query's plan, latency, or recall. HNSW requires neither probe tuning nor a training step.

#### Scenario: Probe count driven by environment
- **WHEN** the server starts with `SEARCH_IVFFLAT_PROBES=40`
- **THEN** chunk search queries MUST execute with `SET LOCAL ivfflat.probes = 40`

#### Scenario: Omitted setting preserves default
- **WHEN** `SEARCH_IVFFLAT_PROBES` is unset
- **THEN** chunk search queries MUST use the default of 10

#### Scenario: Graph-object search is not governed by the probe knob
- **WHEN** a graph-object vector search runs
- **THEN** it MUST use the HNSW index on `kb.graph_objects.embedding_v2`, and its results MUST NOT depend on `SEARCH_IVFFLAT_PROBES`

#### Scenario: Chunk search is not governed by the probe knob
- **WHEN** a chunk vector or hybrid search runs
- **THEN** it MUST use the HNSW index on `kb.chunks.embedding`, and its results MUST NOT depend on `SEARCH_IVFFLAT_PROBES`

## ADDED Requirements

### Requirement: Embedding ANN indexes use HNSW
The embedding ANN indexes on `kb.graph_objects.embedding_v2` (migration `00164`), `kb.chunks.embedding` and `kb.skills.description_embedding` (migration `00170`) SHALL be pgvector HNSW indexes using `vector_cosine_ops` with `m = 16` and `ef_construction = 64`. The periodic embedding index reindex task SHALL NOT target HNSW or dropped indexes; it SHALL continue to target only indexes still built with ivfflat.

#### Scenario: HNSW indexes present
- **WHEN** migrations `00164` and `00170` have applied
- **THEN** `kb.chunks.embedding` and `kb.skills.description_embedding` are served by HNSW indexes and their ivfflat indexes have been dropped

#### Scenario: Reindex task skips migrated indexes
- **WHEN** the scheduled embedding index reindex task runs
- **THEN** it issues `REINDEX` only for indexes still built with ivfflat and never for an HNSW or dropped index
