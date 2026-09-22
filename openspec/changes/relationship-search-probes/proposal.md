## Why

Relationship vector search (ANN over `kb.graph_relationships.embedding`) was using the global `SEARCH_IVFFLAT_PROBES` default of 10. pgvector costs an ivfflat index scan roughly linearly in `ivfflat.probes`, while the competing parallel-seq-scan estimate prices a scan of `kb.graph_relationships` as if the TOASTed 768-dim vectors were free to read. On a project with ~82k embedded relationships the two estimates cross over between probes=5 and probes=10, so at the global default the planner abandoned the ivfflat index and the relationship leg degraded from ~250ms to tens of seconds/minutes.

## What Changes

- Introduce a relationship-specific `ivfflat.probes` knob (`SEARCH_RELATIONSHIP_IVFFLAT_PROBES`) defaulting to 5, read via a new `configuredRelationshipIVFFlatProbes` helper. Relationship search transactions set `SET LOCAL ivfflat.probes` from this knob instead of the global default.
- Scope the relationship ANN project predicate on `kb.graph_relationships.project_id` (the row owning the embedding) instead of the joined source object, and retain `src`/`dst` project predicates as defense-in-depth.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `retrieval-performance-config`: relationship vector search now uses a distinct probes knob with a lower default; the global `SEARCH_IVFFLAT_PROBES` requirement is narrowed to chunk/graph-object search.

## Impact

- `apps/server/domain/search/repository.go` — new `configuredRelationshipIVFFlatProbes`, relationship ANN query predicate scoping.
- `apps/server/domain/search/search_test.go` — unit tests for the new knob and a guard test that relationship probes stay below the global default.
- No migration, API, or schema change.
