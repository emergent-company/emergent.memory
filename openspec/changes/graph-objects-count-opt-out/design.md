## Context

`GET /api/graph/objects/search` returns `{items, next_cursor, total}`. The count
and the list run concurrently, so the endpoint's latency is `max(count, list)`.
Both #732 and #733 point at the count. The list itself was already fixed by
#683 / migration `00163` (`idx_graph_objects_head_main`, partial on
`supersedes_id IS NULL AND branch_id IS NULL AND deleted_at IS NULL`).

## Evidence (scratch PostgreSQL 17, 120k HEAD rows in one dominant project)

Seeded with `pgvector/pgvector:pg17`, a faithful subset of `kb.graph_objects`
(~1.6 KB/row, ~5 rows/page), the `00163` partial index, then vacuumed and
churned so only part of the visibility map was set. Queries are the real ones:

```
SELECT count(*) FROM kb.graph_objects
 WHERE project_id=$P AND supersedes_id IS NULL AND branch_id IS NULL AND deleted_at IS NULL;
```

### BEFORE — partial visibility map (VM stale), `00163` index only

```
 Aggregate  (actual time=118.831..118.832 rows=1)
   Buffers: shared hit=4715 read=23909 dirtied=9663 written=9431
   ->  Seq Scan on graph_objects  (actual time=0.021..113.213 rows=120000)
         Filter: ((supersedes_id IS NULL) AND (branch_id IS NULL) AND (deleted_at IS NULL) AND (project_id = ...))
 Execution Time: 118.887 ms
```

The planner declines the index-only scan entirely and reads the whole heap.

### Adding a narrower `(project_id)` partial index does **not** fix it

With `CREATE INDEX idx_graph_objects_head_count ON kb.graph_objects (project_id)
WHERE supersedes_id IS NULL AND branch_id IS NULL AND deleted_at IS NULL;`
(856 kB vs 9656 kB for the `00163` index) the planner **does** match it — proof
that the predicate matches — but the heap is still hit:

```
 Aggregate  (actual time=34.200..34.201 rows=1)
   ->  Index Only Scan using idx_graph_objects_head_count on graph_objects
         Index Cond: (project_id = ...)
         Heap Fetches: 49525                      <-- ~41% of the rows
 Execution Time: 34.245 ms
```

So the narrow index improves the fallback (103 ms seq scan → 34 ms) but does not
remove the dominant cost. It answers #732's question directly: *a partial index
alone does not make the count cheap.*

### AFTER — `VACUUM (ANALYZE)` (fresh visibility map)

```
 Aggregate  (actual time=14.263..14.264 rows=1)
   ->  Index Only Scan using idx_graph_objects_head_count on graph_objects
         Heap Fetches: 0
 Execution Time: 14.306 ms          (narrow index)

 Aggregate  (actual time=41.181..41.182 rows=1)
   ->  Index Only Scan using idx_graph_objects_head_main on graph_objects
         Heap Fetches: 0
 Execution Time: 41.241 ms          (00163 index only)
```

Heap fetches go to zero and the count drops from ~103–119 ms to 14 ms (narrow)
or 41 ms (the existing `00163` index). The list query is unaffected throughout
(≤ 0.6 ms, `Heap Fetches: 0–163`), so after vacuum the count is no longer the
endpoint floor.

## Decisions

### D1 — Fix the stale visibility map with per-table autovacuum tuning, not a new index

The dominant cost is heap fallback due to a stale VM, and only `VACUUM` clears
it. A narrower `(project_id)` partial index reproduces a smaller but real heap
fetch (`49525`), i.e. it treats the symptom. Migration `00176` instead lowers
`autovacuum_vacuum_scale_factor` to `0.02` and the threshold to `500` for
`kb.graph_objects`, so autovacuum runs while the map is still mostly fresh.

Default thresholds on this table are `50 + 0.20 × live_tup` ≈ 21k dead tuples;
dev carried `n_dead_tup = 18078` with `relallvisible = 67.8%` — under the
default trigger, over the new one. The reloptions take effect for the running
autovacuum launcher with no restart, and the existing bloat is reclaimed by the
next cycle.

**Rejected:** adding `idx_graph_objects_head_count`. It does not remove heap
fetches while the VM is stale (measured above), and once the VM is fresh the
count is already 14–41 ms at this scale — well below the list's ~250 ms dev
latency, so it would be invisible in endpoint latency for the cost of another
index on a high-churn table.

### D2 — Make the exact total an explicit opt-out, not the default off

The total is a documented part of the paginated contract (`items`,
`next_cursor`, `total`), consumed by the CLI's `graph list` output and asserted
by e2e tests. Defaulting it off would be a silent contract change. Instead:

- default (`include_total` absent) → exact total, unchanged wire shape;
- `include_total=false` → count skipped, `total` **omitted** (not `0`, which
  would be indistinguishable from a real empty result).

`Total` becomes `*int` + `omitempty` on the server so "skipped" and "0" differ.
The SDK keeps `Total int` (source-compatible) and documents that it is 0 when
the caller set `SkipTotal`.

### D3 — Do not return an approximate total

`reltuples` is table-wide, so it cannot serve a project-scoped predicate. The
scratch measurement confirms even the table-wide estimate is the wrong shape:
`reltuples = 168907` vs `count(*) = 169380` on the same table, and the target
number is per-project (`119380`). An accurate approximate total would need a
maintained rollup keyed by the count predicates — a much larger change than the
problem justifies once the VM is fresh. Rejected.

### D4 — SDK and web UI opt out only where the total is unused

`memory_graph.go` (`ListGraphObjects`) and `memory_conversations.go`
(`ListMemories`) never read `total`, so they send `include_total=false`. The
CLI's `graph list` prints the total and keeps the default. `ListObjects`
consumers that use `NextCursor` only (blueprint dumper/seeder) are left for a
follow-up; they are not on the reported hot path.

## Risks

- **Autovacuum load.** More frequent vacuuming on a high-churn table trades a
  little background I/O for a fresh VM. The thresholds (2% / 500) are the
  standard "aggressive" settings for large high-churn tables, and only
  `kb.graph_objects` is affected.
- **`total` omission.** Only reachable when a caller explicitly passes
  `include_total=false`; e2e coverage asserts the default still emits `total`.
