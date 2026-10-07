## Context

The `extraction-job-model-guard` precedent established one pattern: gate at the shared creation chokepoint,
refuse before a row is written, fail open only on resolver errors. Embeddings are harder in one respect —
there is no single creation service; enqueue happens in graph writes, chunking, relationship enqueue, and a
background sweep. They are easier in another — a background sweep already exists that can self-heal. The
decisions below settle the shape of the guard, the error taxonomy, the blocked state, self-heal, the
`embedding_space` stamp, and the routing-prefix bug that motivated part of the work.

## D1 — Gate-at-enqueue is primary; project-level blocked is the safety net

**Decision.** The primary guard resolves the project's effective embedding model at every enqueue chokepoint
and refuses to enqueue when it is empty. The project-level `embedding_blocked` state is a *safety net*, not
the primary mechanism: it exists because a background sweep can keep re-admitting an object whose job already
failed terminally, and because re-running the resolver at every write is not free. The blocked flag is a
project property, never a per-job `blocked` status.

**Why.** A per-job `blocked` status would put a project-wide fact (this project cannot embed) onto every job
row, and would still leave the sweep free to re-admit the same object every pass. A project property is
checked once, skipped cheaply by every worker and the sweep, and cleared in one place when config changes.

**Alternatives rejected.** (a) Per-job `blocked` status — puts project-wide state on N rows, no sweep benefit,
more bookkeeping. (b) A new `embedding_policies`-style table — over-engineering; the existing
`kb.embedding_policies` table is per-object-type (enablement, size caps, labels), and overloading it to also
carry "this project cannot embed" conflates two concerns. (c) Doing nothing and relying on the resolver at run
time — the status quo that produces the 90k `pending` backlog.

## D2 — Typed errors over substring matching

**Decision.** `pkg/embeddings` defines typed error kinds — `ErrNotConfigured` (no usable credential/model),
a model-denied/status error kind (provider rejected the model), `ErrDimensionMismatch` (returned vector length
≠ 768), and a transient kind — classified with `errors.Is`/`As`. The worker decides terminal vs transient
from these kinds, never by matching error substrings.

**Why.** The worker currently classifies "permanent" vs "transient" by inspecting error text (e.g. "invalid
model", "credentials", "missing model configuration"). That is brittle against provider message drift and
cannot distinguish "no model configured" (a state) from "provider temporarily unavailable" (transient).
Typed errors make the terminal/transient split a compile-time-controllable contract, testable with
`errors.Is`.

**Alternatives rejected.** (a) Keep substring matching, extend the keyword list — the exact fragility this
change removes; a provider rephrasing its 400 would silently flip permanent→retryable. (b) A single generic
`ErrEmbedding` with a `Kind` enum field — works, but `errors.Is/As` on wrapped sentinels is the idiomatic Go
form and composes with `%w` wrapping already used by the resolver path.

## D3 — Project-scoped blocked state

**Decision.** Introduce `kb.projects.embedding_blocked` (bool) + `kb.projects.embedding_blocked_reason`
(text), or a small dedicated `kb.project_embedding_block` table if keeping the projects row lean is preferred.
The decision leaves the concrete shape to implementation, but the semantics are fixed: blocking is a project
property with a machine-readable reason (e.g. `no_embedding_model`, `model_denied`), set when the worker
observes a terminal not-configured/denied outcome and the project's effective model is unresolvable.

**Why.** A project property is the only place both the sweep and all three workers can cheaply agree to skip
without each re-running the resolver, and it gives the status page one place to read "why is nothing embedding".

**Alternatives rejected.** (a) Per-job `blocked` status (see D1). (b) Storing the reason only in job
`last_error` rows — disappears with the job, no project-level signal. (c) A dedicated
`kb.embedding_policies` row per project — over-engineering (see D1).

## D4 — Self-heal via the throttled sweep + backfill marker, no mass flip

**Decision.** When a project's embedding model is set/updated, clear `embedding_blocked` and set a backfill
marker (e.g. a project-scoped `embedding_backfill_since` timestamp or a flagged sweep cursor). The existing
sweep backfills under its admission throttle (`ObjectQueueTargetDepth`). The change MUST NOT bulk-flip
blocked→pending jobs.

**Why.** The sweep already backfills objects missing embeddings and already throttles admission to available
queue depth. Reusing it means self-heal costs nothing new and cannot thundering-herd the provider or the
queue. A mass flip of every blocked job to `pending` would instantly rebuild the very backlog this change
removes.

**Alternatives rejected.** (a) Mass-flip blocked→pending on config change — thundering herd, re-creates the
backlog. (b) A dedicated backfill worker — duplicated admission logic, more moving parts. (c) Do nothing
(require manual re-trigger) — self-heal is the point; a user fixing config should not need an operator.

## D5 — `embedding_space` stamping, keep 768, defer dimension change

**Decision.** Stamp an `embedding_space` (resolved model id + dimension) on graph-object (and relationship)
writes. On embedding-model change, the write path re-embeds rather than comparing hash/vector across spaces.
The dimension stays fixed at 768 (`EmbeddingDimension`); a dimension change is out of scope.

**Why.** Vectors produced by different models (or different dimensions) are not comparable; carrying the
resolved model id alongside the vector lets the write path detect "the model changed" and re-embed instead of
silently mixing vectors in one space. Keeping 768 avoids a vector-column migration and a reindex of every
row, which is a separate, expensive change.

**Alternatives rejected.** (a) Per-embedding model stamping on every vector — over-engineering at a fixed
dimension; one project-level space is enough. (b) Ignore the model change and trust the existing hash — the
hash detects content change, not model change, so a model swap would leave old-model vectors searchable as if
they were current. (c) Bump the dimension now — out of scope, large migration.

## D6 — `StripRoutingPrefix`: slug vs dialect

**Decision.** `StripRoutingPrefix` strips the first routing segment only when that segment names a
recognized dialect / registered routing provider AND is **not** a project-registered instance/gateway slug;
the decision is made with project context at the credential boundary, not globally.

**Why.** The current `isDialectName`-only strip runs before knowing whether the first segment is a
provider instance slug. `gemini` is an accepted dialect alias (for `google`), but it can also be a
project-registered instance/gateway slug, so the gateway alias `gemini/gemini-embedding-001` is stripped to
the bare `gemini-embedding-001`, which the provider then rejects. Slug-first resolution must win over
dialect-name stripping.

**Alternatives rejected.** (a) Keep the global `isDialectName` strip — the bug. (b) Strip only for a hard-coded
set of provider names, no project context — still wrong for any project that registers a slug colliding with
a dialect name. (c) Never strip — breaks the existing dialect-routing callers that pass bare dialect prefixes.

## Risks

- **Thundering herd.** Mass re-enqueue of blocked work on config change (D4) — mitigated by routing self-heal
  through the throttled sweep, never a bulk flip.
- **Cross-project starvation in the global dequeue.** The sweep and workers dequeue globally; a blocked
  project must not hold queue slots or stall others — mitigated by skipping blocked projects before claim.
- **Retry-cap differences across the three queues.** Chunk, graph-object, and graph-relationship queues have
  drifted on max-attempts / dead-letter; unifying them is a required part of this change, not an assumption.
- **Fail-open on resolver errors can still enqueue doomed work.** A transient resolver error (DB hiccup)
  admits the job; the terminal typed error is what eventually blocks the project via the safety net, so the
  failure mode is bounded.
