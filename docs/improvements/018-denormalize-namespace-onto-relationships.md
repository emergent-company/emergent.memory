# Improvement Suggestion: Denormalize namespace onto kb.graph_relationships for ANN-scoped search

**Status:** Proposed
**Priority:** Low
**Category:** Performance
**Proposed:** 2026-09-22
**Proposed by:** AI Agent
**Assigned to:** Unassigned

---

## Summary

Denormalize `namespace` onto `kb.graph_relationships` so namespace-scoped relationship vector search can keep the ivfflat index instead of falling back to a sequential scan.

---

## Current State

- Relationship vector search scopes by project on `kb.graph_relationships.project_id`, so the planner keeps the ivfflat index (see PR #696).
- `namespace` lives only on `kb.graph_objects` (the joined `src`/`dst` tables). A namespace predicate (`src.namespace = ?`) forces the planner to abandon the ivfflat index, so namespace-scoped relationship search remains a sequential scan.
- A code comment in `apps/server/domain/search/repository.go` (SearchRelationships) documents this limitation explicitly.

---

## Proposed Improvement

- Add a `namespace` column to `kb.graph_relationships` (backfilled from its `src` object), keep it in sync on write, and predicate relationship ANN search on `r.namespace = ?` instead of `src.namespace = ?`.
- This lets namespace-scoped relationship search stay on the ivfflat index, removing the seq-scan degradation.

---

## Benefits

- **User Benefits:** Faster namespace-scoped relationship search on large graphs.
- **Developer Benefits:** Removes a documented planner limitation and an explanatory comment.
- **System Benefits:** Consistent sub-250ms ANN latency regardless of namespace scoping.
- **Business Benefits:** Better retrieval latency at scale for multi-namespace projects.

---

## Implementation Approach

1. Add `namespace` to `kb.graph_relationships` (Goose migration, schema-qualified, reversible).
2. Backfill from the `src` object's `namespace`; set on insert/update in the graph service.
3. Switch the namespace predicate in `SearchRelationships` to `r.namespace = ?`.

**Affected Components:**

- `apps/server/migrations/` — new migration.
- `apps/server/domain/graph/` — write path sets `namespace`.
- `apps/server/domain/search/repository.go` — predicate change.

**Estimated Effort:** Medium

---

## Alternatives Considered

### Alternative 1: HNSW index
- Description: Replace the ivfflat index on `kb.graph_relationships.embedding` with HNSW, which is less sensitive to filter predicates.
- Pros: No denormalization.
- Cons: Larger index build/memory cost; still may not help a cross-table namespace filter.
- Why not chosen: Denormalization is more targeted to this specific filter.

---

## Risks & Considerations

- **Breaking Changes:** No (additive column + backfill).
- **Performance Impact:** Positive for namespace-scoped ANN; negligible write overhead.
- **Security Impact:** Neutral (namespace is not an authz boundary here; RLS/project scoping unaffected).
- **Dependencies:** None.
- **Migration Required:** Yes — new column + backfill.

---

## Success Metrics

- Namespace-scoped relationship search keeps using the ivfflat index (EXPLAIN shows Index Scan, not Parallel Seq Scan).
- Latency stays in the ~250ms range for namespace-scoped queries on ~82k-relationship projects.

---

## Testing Strategy

- [ ] Unit tests
- [ ] Integration tests (EXPLAIN plan check)
- [ ] Performance tests (namespace-scoped vs unscoped)

---

## Related Items

- Related to PR #696 — `fix/search: keep ivfflat index for relationship vector search`.

---

## References

- `apps/server/domain/search/repository.go` — namespace note comment in `SearchRelationships`.

---

## Notes

Optional enhancement; only matters if namespace-scoped relationship ANN latency becomes a problem at scale.

---

**Last Updated:** 2026-09-22 by AI Agent
