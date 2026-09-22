## Purpose

`kb.graph_relationships.embedding` is served by an HNSW index (migration `00171`) instead of ivfflat, so relationship vector search no longer has a `probes` knob to configure and cannot hit the ivfflat planner cost crossover. `SEARCH_IVFFLAT_PROBES` now governs chunk search only.

## MODIFIED Requirements

### Requirement: Vector index probe count is configurable
The `ivfflat.probes` value applied in chunk search transactions SHALL be configurable via environment (`SEARCH_IVFFLAT_PROBES`), defaulting to 10, and MUST be applied per-query transaction.

Chunk vector search (`kb.chunks.embedding`) uses the HNSW index created by migration `00170`, graph-object vector search (`kb.graph_objects.embedding_v2`) uses the HNSW index created by migration `00164`, and relationship vector search (`kb.graph_relationships.embedding`) uses the HNSW index created by migration `00171`. None is tuned by `ivfflat.probes`, and relationship search no longer applies `SET LOCAL ivfflat.probes` at all. HNSW requires neither probe tuning nor a training step.

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

#### Scenario: Relationship search is not governed by the probe knob
- **WHEN** a relationship vector search runs
- **THEN** it MUST use the HNSW index on `kb.graph_relationships.embedding`, and its results MUST NOT depend on `SEARCH_IVFFLAT_PROBES` or any relationship-specific probe setting

## REMOVED Requirements

### Requirement: Relationship vector search probe count is configurable
**Reason**: The relationship embedding index is now HNSW (migration `00171`), which has no `probes` setting. `SEARCH_RELATIONSHIP_IVFFLAT_PROBES` is removed so operators cannot reintroduce the ivfflat planner crossover that degraded relationship search from ~250ms to minutes.

**Migration**: Unset `SEARCH_RELATIONSHIP_IVFFLAT_PROBES`; it is ignored. Recall on the relationship leg is now governed by HNSW (`hnsw.ef_search`), and relationship search no longer executes `SET LOCAL ivfflat.probes`.
