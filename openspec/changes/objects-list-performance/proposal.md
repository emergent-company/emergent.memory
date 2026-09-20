## Why

The web app's Objects browser is slow on projects with large object graphs. On dev, the "Norwegian Law" project holds 107,233 `kb.graph_objects` rows (708 MB table), and the two most-used pages are unacceptably slow:

- `GET /objects` (list) renders in 4.4s–41s, intermittently exceeding the proxy timeout and returning 502.
- `GET /objects/{id}` (detail) renders in 31.6s–73s because its sub-resource calls run sequentially.

Root causes, confirmed via `EXPLAIN (ANALYZE, BUFFERS)` and server logs:

1. The list query (`GET /api/graph/objects/search?limit=100`) runs a `Parallel Seq Scan` over the full 708 MB table (13s cold) because no index matches its predicates (`project_id` + `supersedes_id IS NULL` + `branch_id IS NULL` + `deleted_at IS NULL`) and ordering (`created_at DESC, id DESC`). The existing `idx_graph_objects_project_type_created_id (project_id, type, created_at, id)` is unusable — `type` sits mid-index and is not filtered.
2. The detail page (`uiObject`) fires six backend calls sequentially, and two of them are slow: `objectLabelSuggestions` re-runs the same list query just to build label-autocomplete hints, and the pgvector similarity search (`/objects/{id}/similar?limit=10`) takes 20–26s on a cold cache.

## What Changes

- Add a partial index `idx_graph_objects_head_main` matching the list query's exact predicates + ordering, so the planner uses an index scan instead of a seq scan, and run `ANALYZE` to refresh planner stats (also fixes multi-hundred-ms planning time on the similar path).
- Parallelize `uiObject`'s independent sub-resource fetches (compiled types, edges, similar objects, label suggestions) with `errgroup`, matching the existing `uiObjects` pattern.
- Add a short-TTL in-memory cache for the project's distinct object labels so the label-autocomplete hint does not re-scan the object table on every page render.

## Capabilities

### New Capabilities

- `web-objects-performance`: the web Objects list and detail pages return promptly on large projects without full-table scans or serial sub-resource waterfalls.

### Modified Capabilities

<!-- none -->

## Impact

- **Database**: one Goose migration (`apps/server/migrations/00163_*.sql`) adding a partial index + `ANALYZE`, complementing the existing `00162` type-filter index (which does not cover the unfiltered list query).
- **Gateway**: `apps/web-ui/gateway/objects.go` — parallelize `uiObject`, cache `objectLabelSuggestions` (new `Server` field + init).
- **No API or behavior changes**: page output, routes, and client contracts are unchanged; only latency characteristics change.
- **Testing**: existing gateway unit tests (`go test ./...`) and `task lint` must stay green; migration validated by `goose` on the dev DB.
