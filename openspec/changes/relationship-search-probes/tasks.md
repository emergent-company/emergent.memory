## 1. Relationship probe knob

- [x] 1.1 Add `configuredRelationshipIVFFlatProbes()` reading `SEARCH_RELATIONSHIP_IVFFLAT_PROBES`, default 5, clamped `>= 1`.
- [x] 1.2 Use it in `SearchRelationships` (`beginTxWithIVFFlatProbes(ctx, configuredRelationshipIVFFlatProbes())`).
- [x] 1.3 Scope the ANN project predicate on `r.project_id` and retain `src.project_id`/`dst.project_id` as defense-in-depth.
- [x] 1.4 Unit tests: `TestConfiguredRelationshipIVFFlatProbes` + `TestRelationshipProbesBelowGlobalDefault`.

## 2. Spec

- [x] 2.1 Add/modify `retrieval-performance-config` delta spec documenting the relationship knob, its default (5), clamping, and precedence over `SEARCH_IVFFLAT_PROBES`.
