## Why

`search.Repository.SearchRelationships` applies the namespace filter on the joined `kb.graph_objects` row (`src.namespace = ?`), but the embedding and its ivfflat index live on `kb.graph_relationships`. A predicate on a joined relation cannot be satisfied by pgvector's ANN scan, so the planner is free to choose a Parallel Seq Scan + Sort over every embedded relationship in the table. PR #696 fixed this for the project predicate by scoping it on `r.project_id`; namespace could not be fixed the same way because the column did not exist on the relationship row. Any search with an explicit namespace (anything other than `namespace: "all"` or unset at the MCP layer, per `domain/mcp/graph_tools.go`) degrades the relationship leg from hundreds of milliseconds to minutes.

## What Changes

- Denormalise `namespace` onto `kb.graph_relationships` (mirroring `r.project_id`):
  - migration `00172_graph_relationships_namespace.sql` adds `namespace text NULL`, a partial btree index `(project_id, namespace) WHERE namespace IS NOT NULL`, and backfills from the source object (`UPDATE ... FROM kb.graph_objects src WHERE src.id = gr.src_id`).
- Populate it on every relationship write path: new HEAD inserts (`CreateRelationship`, `maybeCreateInverse`, merge clone, `CreateSubgraph`, branch bulk-copy) inherit the source object's namespace, and `CreateRelationshipVersion` inherits it from the previous HEAD so tombstone/restore/patch/fast-forward/similarity-merge versions keep it.
- Apply the ANN predicate on `r.namespace` instead of `src.namespace`, keeping the `r.project_id` and `src`/`dst` project predicates as-is.
- Keep `namespace: "all"` / unset working unchanged (nil namespace means no predicate).

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `retrieval-performance-config`: relationship vector search applies the namespace predicate on `kb.graph_relationships.namespace` so the planner keeps the ANN index for namespace-scoped searches.

## Impact

- `apps/server/migrations/00172_graph_relationships_namespace.sql` — new column + index + backfill, plus reversible Down.
- `apps/server/domain/graph/entity.go` — `GraphRelationship.namespace` field.
- `apps/server/domain/graph/repository.go` — `CreateRelationshipVersion` inherits namespace; `BranchRelationshipHead`/`GetBranchRelationshipHeads`/`BulkCopyRelationshipsToBranch` carry it.
- `apps/server/domain/graph/service.go` — namespace set on the new-HEAD insert paths.
- `apps/server/domain/search/repository.go` — query helper `buildRelationshipSearchQuery`; predicate switched to `r.namespace`.
- `apps/server/domain/search/search_test.go` — unit test for the query builder.
- `apps/server/internal/testutil/schema.sql` — test schema mirror (column + index).
- No API change. The denormalised value is derived state and can be rebuilt from `kb.graph_objects` if stale (e.g. after restore/backfill).
