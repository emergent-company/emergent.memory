## Why

Embedding jobs — graph objects, graph relationships, and document chunks — are enqueued regardless of whether
the addressed project has a usable embedding model. A project that *looks* configured (a provider credential
exists but yields no resolvable embedding model) retries forever: the embeddings module silently falls back to
its static `NoopClient`, which returns an empty vector, and the worker treats that empty result as a transient
failure and requeues. "No embedding model configured" is a **state**, not a transient error — yet the queues
have no way to express that, so misconfigured-but-configured projects accumulate a large `pending` backlog
(~90k observed on dev) that can never drain.

This is the embedding analogue of `extraction-job-model-guard` (PR #1555), which gated extraction-job *creation*
on a resolvable generative model. That change proved the pattern — resolve the effective model at the shared
creation chokepoint, refuse before a row is written, and fail open only on resolver errors. Embeddings have no
such guard, so doomed work is enqueued and only fails at run time.

## What Changes

- **Gate at every enqueue chokepoint** (`embedding-job-gating`). Resolve the addressed project's effective
  embedding model (`modelconfig.Service.ResolveEmbeddingModel`: project config first, then provider-credential
  fallback) before inserting/enqueueing — on graph-object write paths (including bulk), document chunking,
  relationship enqueue, and the sweep. An empty effective model refuses/skips with no job row. Content
  write/ingest still succeeds (log + skip); the admin enqueue API responds `409 no_embedding_model`; MCP/tool
  enqueue returns a clear error with no row; the sweep admits nothing. Fail open only on a resolver *error*.
  Project membership is enforced before the guard.

- **Typed embedding errors, not substring matching** (`embedding-job-gating`). New error kinds in
  `pkg/embeddings` — not-configured, model-denied/status, dimension-mismatch, transient — classified with
  `errors.Is`/`As`. The worker MUST NOT decide terminal vs transient by matching on error substrings.

- **The static noop fallback is terminal** (`embedding-job-gating`). When no usable credential resolves, the
  embeddings module returns a terminal not-configured error instead of silently using `NoopClient` and
  returning an empty vector.

- **A project that cannot embed is blocked at project scope** (`embedding-job-gating`). A project-level blocked
  state (`kb.projects.embedding_blocked` + `embedding_blocked_reason`, or a small dedicated table) — a project
  property, NOT a per-job status. Workers and the sweep skip blocked projects.

- **Self-heal on configuration change** (`embedding-job-gating`). Setting/updating a project's embedding model
  clears the blocked state and sets a backfill marker; the existing sweep backfills under its admission
  throttle. No mass flip of blocked→pending jobs (thundering herd).

- **One terminal contract across the three queues** (`embedding-job-gating`, `embedding-worker-throughput`).
  Chunk, graph-object, and graph-relationship queues share max-attempts / dead-letter semantics.

- **Correct routing-prefix stripping** (`provider-model-name-routing`). Strip a routing prefix only when the
  first segment names a dialect/registered routing provider AND is not a project-registered instance/gateway
  slug — decided with project context at the credential boundary. The current `isDialectName`-only strip turns
  the gateway model alias `gemini/gemini-embedding-001` into the rejected bare `gemini-embedding-001`.

- **Status and refresh consistency** (`embedding-status`, `graph-embedding-refresh`). The status page surfaces
  the blocked state/reason, includes blocked in counts, and warns on *misconfigured* (unusable) models — not
  only unset. Graph writes stamp an `embedding_space` (resolved model id + dimension) and re-embed on model
  change rather than compare across spaces; dimension stays fixed at 768.

## Capabilities

### New Capabilities

- `embedding-job-gating` — every embedding enqueue path (graph-object write incl. bulk, document chunking,
  relationship enqueue, sweep) refuses to enqueue for a project with no resolvable embedding model; typed
  embedding errors replace substring classification; the noop fallback is terminal; a project-level blocked
  state is the safety net; self-heal clears it on config change; the three queues share terminal semantics.
- `provider-model-name-routing` — routing-prefix stripping is project-context aware, so a provider
  instance/gateway slug is never mistaken for a dialect prefix.

### Modified Capabilities

- `embedding-worker-throughput` — "permanent failure is not retried" is re-expressed in typed errors; noop
  fallback is terminal; the relationship queue gains dead-letter / max-attempts parity.
- `embedding-sweep-ceilings` — admission refuses blocked / unresolvable-model projects (resolvability, not
  `embedding_model != ''`); a terminal job's object is not re-admitted every sweep while the project is blocked.
- `embedding-status` — surfaces the blocked state/reason; counts include blocked; the "no embedding model"
  warning covers misconfigured (unusable) models, not only unset.
- `graph-embedding-refresh` — stamps `embedding_space` at write; re-embeds on model change rather than
  comparing across spaces; keeps the fixed 768 dimension.

## Scope Boundary

- **Backend server only** (`apps/server`). No gateway/web-ui, CLI, connector, or iOS change.
- **Dimension is fixed at 768.** A dimension change (and any vector-reindex migration it would require) is
  explicitly out of scope and deferred.
- **No mass re-enqueue.** Self-heal routes through the throttled sweep + backfill marker; the design MUST NOT
  flip blocked→pending jobs in bulk.
- **No new per-object-type policy machinery.** The existing `kb.embedding_policies` table (per-object-type
  enablement, size caps, labels) is untouched; gating lives at the project + enqueue level.
- **No per-embedding model stamping on the vector.** One `embedding_space` stamp (model id + dimension) per
  project write is sufficient at the fixed dimension; per-vector model provenance is deferred.

## Impact

- **Backend**: `domain/extraction` enqueue chokepoints (graph object write incl. bulk, document chunking,
  relationship enqueue, sweep), `domain/modelconfig` (`ResolveEmbeddingModel` reuse), `domain/provider`
  (`StripRoutingPrefix` slug-vs-dialect), `pkg/embeddings` (typed errors, noop-terminal), the embedding workers
  and sweep (skip blocked, typed classification), `domain/embedding-status` surface, and the `kb.projects`
  blocked columns (or a small dedicated table).
- **Database**: one migration adding `kb.projects.embedding_blocked` + `embedding_blocked_reason` (or a small
  dedicated table), plus the `embedding_space` stamp columns on graph-object (and relationship) writes.
- **Tests**: DB-backed guard tests per enqueue surface (chokepoint + admin 409 + MCP error + ingest-skip +
  sweep-admit-nothing + fail-open), `pkg/embeddings` typed-error classification tests, provider routing-prefix
  tests, blocked-state self-heal tests, and status/refresh tests.
- **No production behaviour change for correctly configured projects**: a resolvable embedding model still
  enqueues and drains exactly as before.

## Provenance

Fresh change, not a resurrection. It applies the `extraction-job-model-guard` (PR #1555) pattern — resolve at
the shared creation chokepoint, refuse before a row is written, fail open only on resolver errors — to the
three embedding queues, and adds the project-scoped blocked safety net the extraction guard deliberately
omitted (extraction has no background sweep to starve).
