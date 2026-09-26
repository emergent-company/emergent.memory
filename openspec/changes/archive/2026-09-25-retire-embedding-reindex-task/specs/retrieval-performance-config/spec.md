## Purpose

All embedding ANN indexes are pgvector HNSW (migrations `00164`, `00170`, `00171`), so the ivfflat-specific maintenance surface is removed: `ivfflat.probes` has no effect on any index and the periodic embedding-index reindex task no longer exists. This change drops the `SEARCH_IVFFLAT_PROBES` knob and retires the nightly `embedding_index_reindex` scheduler task.

## MODIFIED Requirements

### Requirement: Embedding ANN indexes use HNSW
The embedding ANN indexes on `kb.graph_objects.embedding_v2` (migration `00164`), `kb.chunks.embedding` and `kb.skills.description_embedding` (migration `00170`), and `kb.graph_relationships.embedding` (migration `00171`) SHALL be pgvector HNSW indexes using `vector_cosine_ops` with `m = 16` and `ef_construction = 64`. HNSW indexes require neither probe tuning nor a training/list step. There SHALL be no periodic embedding-index `REINDEX` scheduler task: the former `embedding_index_reindex` task targeted only ivfflat indexes and was permanently empty once every embedding index was migrated to HNSW, so it has been removed along with its schedule/interval configuration.

#### Scenario: HNSW indexes present
- **WHEN** migrations `00164`, `00170` and `00171` have applied
- **THEN** `kb.graph_objects.embedding_v2`, `kb.chunks.embedding`, `kb.skills.description_embedding` and `kb.graph_relationships.embedding` are served by HNSW indexes and their ivfflat indexes have been dropped

#### Scenario: Reindex task skips migrated indexes
- **WHEN** the scheduler starts and its registered task list is inspected
- **THEN** it MUST NOT register an `embedding_index_reindex` task, MUST NOT read `EMBEDDING_REINDEX_SCHEDULE` or `EMBEDDING_REINDEX_INTERVAL`, and MUST NOT issue `REINDEX` against any HNSW or dropped embedding index

## REMOVED Requirements

### Requirement: Vector index probe count is configurable
**Reason**: Every embedding ANN index is now HNSW (migrations `00164`, `00170`, `00171`), which has no `probes` setting. The `ivfflat.probes` value applied by the chunk, graph-object, similar-objects and skills vector searches had no effect on their plan, latency or recall, so `SEARCH_IVFFLAT_PROBES` and the `configuredIVFFlatProbes` / `beginTxWithIVFFlatProbes` helpers were dead configuration.

**Migration**: Unset `SEARCH_IVFFLAT_PROBES`; it is ignored. Recall on every ANN leg is governed by HNSW (`hnsw.ef_search`), and vector search no longer executes `SET LOCAL ivfflat.probes`.
