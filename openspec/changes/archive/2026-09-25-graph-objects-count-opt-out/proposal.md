## Why

`GET /api/graph/objects/search` always returns an exact `total`, so the service
(`domain/graph/service.go`) runs `repo.Count` and `repo.List` concurrently and
the endpoint's latency is bounded by the slower of the two. For a project that
dominates a large `kb.graph_objects` table the exact `COUNT(*)` is the slower
one: #733 measured 18–32 s (index-only) versus ~250 ms for the list, and #732
traced the cost to a stale visibility map rather than a missing index.

Two independent problems, one endpoint:

1. **The count is unavoidable (#733).** Every caller pays for the exact total,
   including cursor-only callers that never read it.
2. **The count is expensive because the visibility map is stale (#732).**
   `kb.graph_objects` is a high-churn table; with default autovacuum settings a
   ~100k-row table only triggers vacuum after ~21k dead tuples, which is enough
   churn to leave most pages without a visibility-map bit.

## What Changes

- **Additive API opt-out.** `GET /api/graph/objects/search?include_total=false`
  skips the exact `COUNT(*)` and omits `total` from the response. The default is
  unchanged: callers that do not send the parameter keep receiving the exact
  integer `total`, including `total: 0`. `include_total` is a new, optional
  query parameter — no existing request or response changes shape.
- **Server plumbing.** `ListParams.SkipTotal` (zero value = today's behaviour),
  honoured by `Service.List`, parsed in `Handler.ListObjects`. The response
  `total` becomes `*int` with `omitempty` so "skipped" is distinguishable from a
  real zero.
- **Consumer opt-out where the total is unused.** The web UI's object/memory
  list helpers stop requesting the total; the SDK gains
  `ListObjectsOptions.SkipTotal`.
- **Maintenance, not a new index (migration 00176).** Per-table autovacuum
  tuning on `kb.graph_objects` so the visibility map stays fresh and the
  index-only count keeps returning in tens of milliseconds. No index is created
  or dropped.

## Capabilities

### New Capabilities

- `graph-objects-search-count`: how `GET /api/graph/objects/search` reports (or
  skips) its `total`, and the autovacuum maintenance that keeps the underlying
  count cheap.

### Modified Capabilities

<!-- None: no existing spec covers the objects-search total or its maintenance. -->

## Impact

- **Server**: `domain/graph/dto.go` (`SearchGraphObjectsResponse.Total` becomes
  `*int`), `domain/graph/repository.go` (`ListParams.SkipTotal`),
  `domain/graph/service.go` (skip the count goroutine),
  `domain/graph/handler.go` (parse `include_total`, swagger param).
- **SDK**: `pkg/sdk/graph` (`ListObjectsOptions.SkipTotal`). `Total int` stays
  an `int` for source compatibility; it is 0 when the caller opted out.
- **Web UI**: `gateway/memory_graph.go`, `gateway/memory_conversations.go` stop
  requesting a total they never read.
- **Migration**: `apps/server/migrations/00176_objects_count_optimizations.sql`
  (new) — `ALTER TABLE kb.graph_objects SET (autovacuum_*)`, reversible via
  `RESET`. Catalog-only; no rewrite, no index change.
- **Backwards compatibility**: additive. Default requests and responses are
  unchanged; e2e assertions that `total` is present and `0` for empty results
  keep passing.
- **Operational**: on an already-bloated database the new thresholds also make
  the next autovacuum cycle reclaim existing bloat (dead tuples now far exceed
  the new threshold); no manual `VACUUM` is required, though one is harmless.
