## Context

Embedding generation is a background, asynchronous concern. The server already has the data needed for per-object status and for global progress, but none of it is exposed where the web UI can read it:

- `kb.graph_objects.embedding_v2` (vector(768)) is the "has embedding" signal, but the column is `json:"-"` on the graph entity and not returned by the object API.
- `kb.graph_objects.embedding_updated_at` timestamps when the embedding was last (re)generated, also `json:"-"`.
- `kb.graph_embedding_jobs` holds one row per object embedding job with `status` ∈ {pending, processing, completed, failed, dead_letter} plus `scheduled_at`, `started_at`, `completed_at`, `last_error`.
- `GET /api/embeddings/progress` already returns object + relationship queue counts (pending/processing/completed/failed/dead-letter); `GET /api/embeddings/status` returns worker running/paused state + config. Both are `RequireAuth` (not admin-gated) in `apps/server/domain/extraction`.
- The gateway's `GraphObject` type has no embedding field; the object detail page (`objects.templ`) renders properties/relationships/similar only.

## Goals / Non-Goals

**Goals:**
- Surface a per-object embedding status (embedded / pending / processing / failed / missing) on object cards and the object detail view.
- Surface a global embeddings status/progress page (queue counts + worker state + config) reusing the existing endpoints.
- Compute status server-side in the graph domain so the gateway stays a thin renderer.

**Non-Goals:**
- No changes to how embeddings are generated, scheduled, or retried — this is visibility only.
- No per-chunk (document) embedding status surfaced in this change; the page covers object + relationship queues.
- No embedding re-index trigger from the UI (existing `ReindexEmbeddings` remains the mechanism; out of scope unless trivially reused).

## Decisions

### D1 — Derive per-object status server-side via `embedding_v2` + latest job row

Status is computed in SQL, not in the gateway. For each object, classify:

- `embedded` — `embedding_v2 IS NOT NULL`
- otherwise, from the latest `graph_embedding_jobs` row: `pending`, `processing`, `failed`, `dead_letter`
- `missing` — no vector and no job row

*Rationale:* the signal already lives in the DB; computing it in the query avoids the gateway making extra round-trips and keeps the object API a single source of truth. *Alternatives considered:* gateway-side per-object lookups (N+1, more code); a dedicated batch endpoint (more surface for the same data).

### D2 — Use `LEFT JOIN LATERAL` for the latest job row, not a correlated subquery per field

Attach exactly one job row (the most recent, by `created_at` or `scheduled_at` desc) per object via a lateral join, and expose it alongside a boolean `has_embedding`. *Rationale:* avoids N+1 queries when listing many objects; the existing object search/list queries already scan `graph_objects`. *Alternative:* one extra query per object — rejected for latency.

### D3 — Reuse existing embedding endpoints for the stats page

The stats page calls `GET /api/embeddings/progress` and `GET /api/embeddings/status` via new `MemoryClient` methods; no new server endpoints are added for the page itself. *Rationale:* the endpoints already exist and are auth-scoped; adding gateway client methods is the cheapest path. *Alternative:* a new project-scoped progress endpoint — deferred (see Risk R2).

### D4 — Embedding status enum + UI mapping lives in the gateway

Define a single `embedding_status` string enum shared between the server response and the gateway `GraphObject`, with a fixed set of display labels and badge colors for the templ layer. *Rationale:* one vocabulary, no drift between server and UI.

## Risks / Trade-offs

- **R1 — Global vs project scoping of progress stats** → The existing `/api/embeddings/progress` aggregates across the whole instance, not per project, so the stats page shows instance-wide numbers even though the gateway is project-scoped. Mitigation: document this in the page copy ("across all projects") for v1; project-scoped counts exist in `health/metrics_handler.go` and can be a follow-up.
- **R2 — `embedding_updated_at` semantics** → it reflects the object row, not necessarily the job's `completed_at`; for "embedded" objects we surface `embedding_updated_at` and accept it as the last-embedding time. Low risk.
- **R3 — Large object lists** → the lateral join adds one indexed probe per object; acceptable at current list sizes. Mitigation: limit to the same page size the object list already uses.
- **R4 — Job row churn** → failed/dead-letter rows persist after retries enqueue a new row; the lateral join must pick the *latest* row so a retried object does not show a stale failure.

## Open Questions

- Whether the stats page should be gated to admins only (the endpoints are `RequireAuth`, not admin). Resolved in implementation by matching the current endpoint auth; no spec change required either way.
