## Context

`/embeddings` is a per-project queue monitor. Its data source, `GET /api/embeddings/progress`, returns only job counters. Terminal job rows are deleted 7 days after completion by `EmbeddingJobPurgeTask`, so a fully embedded, idle project shows all-zero counters and the page falls back to "No embedding statistics yet".

Measured on the dev box (Norwegian Law, 107,233 objects / 81,919 relationships, all embedded):

| query | plan | time |
|---|---|---|
| `COUNT(*) ... WHERE project_id=? AND deleted_at IS NULL` (objects) | seq scan, 38k buffers | ~5000 ms |
| `COUNT(*) ... WHERE project_id=? AND deleted_at IS NULL AND embedding_v2 IS NULL` | bitmap on `idx_graph_objects_missing_embedding` | 0.47 ms |
| `COUNT(*) ... WHERE project_id=? AND deleted_at IS NULL` (relationships) | seq scan | ~4200 ms |
| `COUNT(*) ... WHERE project_id=?` (objects, no deleted filter) | index-only | 8 ms |
| `COUNT(*) ... WHERE project_id=?` (relationships, no deleted filter) | index-only | 6 ms |
| `COUNT(*) ... WHERE project_id=? AND deleted_at IS NULL AND embedding IS NULL` | bitmap on `idx_graph_relationships_missing_embedding` | ~0.5 ms |

So the `awaiting` side is already cheap (existing partial indexes). The `total` side is only cheap when it does not filter on `deleted_at` — which would over-count superseded/deleted versions (6% on Norwegian Law). The exact total therefore needs an `embedded`-side partial index.

## Decisions

**1. Separate `GET /api/embeddings/coverage` endpoint, not extra fields on `/progress`.**
Coverage and queue counts are independent signals; folding them into one payload makes a slow/expensive coverage query able to blank the queue section. The gateway already degrades each section independently, so a fourth leg fits the existing pattern. `progress` also keeps its fast path untouched.

**2. Exact counts via two partial indexes, not approximate index-only counts.**
`embedded = COUNT(*) WHERE project_id=? AND embedding IS NOT NULL AND deleted_at IS NULL` served by a new `(project_id)` partial index; `awaiting = COUNT(*) WHERE project_id=? AND embedding IS NULL AND deleted_at IS NULL` served by the existing missing-embedding partial index. `total = embedded + awaiting` is exact over live rows (the two predicates are disjoint). Chosen over `COUNT(*) WHERE project_id=?` (index-only, 6–8 ms) because that includes superseded/deleted rows and would overstate coverage on projects with version history.

**3. Universe = live rows (`deleted_at IS NULL`), matching the sweep.**
`EmbeddingSweepWorker.sweepObjects` enqueues `embedding_v2 IS NULL AND deleted_at IS NULL`. Coverage must count the same set, or "awaiting = 0" could coexist with a sweep that still has work.

**4. Optional columns are merged with `COALESCE` into a single scan per table.**
`SELECT COUNT(*) FILTER (WHERE embedding_v2 IS NOT NULL) AS embedded, COUNT(*) FILTER (WHERE embedding_v2 IS NULL) AS awaiting FROM kb.graph_objects WHERE project_id=? AND deleted_at IS NULL` is one bitmap scan over the union of the two partial indexes. (Implementer may split into two queries if the planner is unhappy; measure.)

## Risks / Trade-offs

- Two extra partial indexes add write overhead to `graph_objects` / `graph_relationships` and ~a few MB each. Acceptable: both tables already carry several partial indexes, and the embedded predicate is the common case.
- If migrations cannot run `CREATE INDEX CONCURRENTLY` (transaction-wrapped), the index build takes a write lock. Follow the existing migration convention (`00192_admin_listing_indexes.sql` is the precedent) and size the risk against the largest deployed table.
- `awaiting` counts objects with no vector regardless of whether the project has an embedding model; a project with no model will show `awaiting > 0` next to the existing no-model warning. That is intended — it explains why nothing is progressing.

## Migration Plan

Additive only: new indexes, no data change, no backfill. Rollback drops the two indexes. No application code depends on them existing (coverage still returns correct results, only slower, without them).

## Open Questions

- Should deleted-object coverage ever be surfaced? No for this change (Non-Goal); the page reports live rows only.
