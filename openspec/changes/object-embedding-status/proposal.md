## Why

Users can browse graph objects but cannot tell whether an object has been embedded, so they have no way to know if it is recallable via semantic/vector search. Embedding generation runs asynchronously in background workers (per-object `graph_embedding_jobs`, per-relationship, and per-chunk queues), but the object API hides the embedding signal (`embedding_v2`, `embedding_updated_at` are `json:"-"`) and no UI surfaces either per-object embedding state or overall embedding progress. This leaves operators and users unable to see when the knowledge graph is fully searchable or to diagnose stuck/failed embeddings.

## What Changes

- **Server (graph domain)**: expose per-object embedding status on the object API (search, list, and detail) as a computed `embedding_status` — derived from `embedding_v2` (has embedding) joined with the latest `graph_embedding_jobs` row (pending/processing/failed/dead-letter), plus the `embedding_updated_at` timestamp.
- **Gateway data layer**: add `EmbeddingStatus` (and `EmbeddingUpdatedAt`) to `GraphObject` so the web UI can render it.
- **Object detail view**: show an embedding-status indicator (embedded / pending / processing / failed / missing) on the object detail header.
- **Object list cards**: show a compact embedding-status badge on each object card.
- **New embeddings status page**: a web UI page surfacing global embedding-generation stats and progress — object and relationship queue counts (pending/processing/completed/failed/dead-letter), worker running/paused state, and worker config — sourced from the existing `GET /api/embeddings/progress` and `GET /api/embeddings/status` endpoints.

## Capabilities

### New Capabilities
- `embedding-status`: a web UI page that shows embedding-generation stats and progress — queue counts and worker state for object and relationship embeddings.

### Modified Capabilities
- `object-browser`: object list cards and the object detail view surface per-object embedding status.

## Impact

- **Server** (`apps/server/domain/graph`): object search/list/detail queries gain a computed embedding-status column (join `kb.graph_embedding_jobs`); response models expose `embedding_status` + `embedding_updated_at`. No schema migration — `embedding_v2` and `embedding_updated_at` already exist.
- **Gateway** (`apps/web-ui/gateway`): `memory_graph.go` `GraphObject` gains embedding fields; `objects.templ` card + detail badge; new embeddings page (`embeddings.templ`), route, handler, and `MemoryClient` methods for `/api/embeddings/progress` + `/status`.
- **No new external dependencies**; the embedding status/progress endpoints already exist.
