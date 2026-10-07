## Purpose

Define that embedding-job creation is refused, at every enqueue chokepoint, when the addressed project has no
resolvable effective embedding model — so every enqueue path (graph-object write including bulk, document
chunking, relationship enqueue, and the sweep) fails fast and consistently instead of enqueuing doomed work,
and so "no embedding model configured" is a project-level state, not a transient retry loop.

## ADDED Requirements

### Requirement: Embedding-job creation is gated on a resolvable embedding model

The server SHALL resolve the addressed project's effective embedding model at every embedding enqueue
chokepoint — the graph-object write paths (including bulk writes), document chunking, relationship enqueue,
and the sweep — before inserting or enqueueing a job, using the same resolution signal as the status warning
(`modelconfig.Service.ResolveEmbeddingModel` / effective model config: project config first, then the
provider-credential fallback). When no embedding model resolves (an empty effective model), creation SHALL be
refused or skipped and no embedding job row SHALL be inserted.

The gate SHALL cover every enqueue path, mirroring the per-surface behaviour of `extraction-job-model-guard`:

- content write/ingest (graph-object write and document chunking) SHALL still succeed — it SHALL log and skip
  the embedding enqueue, never fail the write;
- the admin enqueue API SHALL respond `409 no_embedding_model`;
- MCP/tool enqueue SHALL return a clear error (not a panic or 500) and insert no row;
- the sweep SHALL admit nothing for a project whose model does not resolve.

The gate SHALL fail open only on an error returned by `ResolveEmbeddingModel` to the chokepoint: a transient
model-config lookup failure, or a service constructed without a resolver, SHALL NOT block job creation. By
contrast, an empty effective model — including one produced when the provider-credential fallback yields
nothing (its own resolution error is swallowed inside `ResolveEmbeddingModel`, so it surfaces as empty rather
than as an error) — is a definitive "no model" and SHALL be refused. The admin guard is project-scoped and
SHALL run after project membership enforcement, so an unauthorized project request is refused before model
resolution is consulted.

#### Scenario: Graph-object write skips without failing the write

- **WHEN** an object is created or updated for a project with no resolvable embedding model
- **THEN** the write still succeeds
- **AND** the embedding enqueue is logged and skipped
- **AND** no `kb.graph_embedding_jobs` row is inserted for that project

#### Scenario: Bulk write skips per object

- **WHEN** a bulk object write addresses a project with no resolvable embedding model
- **THEN** no embedding job is enqueued for any object in the bulk write, while the object writes themselves still succeed

#### Scenario: Document chunking skips without failing ingestion

- **WHEN** a document is chunked and the project has no resolvable embedding model
- **THEN** the chunks are still created
- **AND** no `kb.chunk_embedding_jobs` row is inserted

#### Scenario: Relationship enqueue skips

- **WHEN** a relationship is written or enqueued for embedding and the project has no resolvable embedding model
- **THEN** no `kb.graph_relationship_embedding_jobs` row is inserted

#### Scenario: Sweep admits nothing

- **WHEN** the sweep evaluates objects for a project with no resolvable embedding model
- **THEN** the sweep enqueues no embedding job for that project

#### Scenario: Admin enqueue API rejects with no embedding model

- **WHEN** a project has no resolvable embedding model and a caller posts a valid enqueue request to the admin API
- **THEN** the server responds `409` with a `no_embedding_model` error explaining that no embedding model is configured
- **AND** no embedding job row is inserted

#### Scenario: MCP/tool enqueue rejects with no embedding model

- **WHEN** a project has no resolvable embedding model and an MCP/tool caller triggers an embedding enqueue
- **THEN** the caller receives a clear error explaining that no embedding model is configured
- **AND** no embedding job row is inserted

#### Scenario: Resolvable model enqueues normally

- **WHEN** a project's effective embedding model resolves
- **THEN** the graph-object write, chunking, relationship enqueue, admin API, MCP/tool, and sweep paths all enqueue exactly as before

#### Scenario: Resolution error fails open

- **WHEN** `ResolveEmbeddingModel` returns an error to the chokepoint (e.g. the project-config lookup fails) rather than an empty model name, or the service has no resolver attached
- **THEN** the server proceeds to enqueue, preserving pre-existing behaviour

#### Scenario: Provider-fallback failure is treated as no model

- **WHEN** the provider-credential fallback inside `ResolveEmbeddingModel` yields no model (its error is swallowed) so the effective model is empty
- **THEN** enqueue is refused — the empty model, not a swallowed provider error, is what the gate observes

#### Scenario: Membership is enforced before the guard

- **WHEN** an unauthorized caller posts an enqueue request to the admin API for a project they cannot access
- **THEN** the server responds with the authorization error (`403`) without consulting embedding-model resolution

### Requirement: Terminal vs transient outcomes use typed errors, not string matching

The embeddings module (`pkg/embeddings`) SHALL define typed embedding error kinds — not-configured (no usable
credential/model), model-denied/status (the provider rejected the model), dimension-mismatch (returned vector
length ≠ `EmbeddingDimension`), and transient — and the worker SHALL classify terminal vs transient outcomes
with `errors.Is`/`errors.As` against these kinds. The worker MUST NOT classify by inspecting error substrings.

#### Scenario: Not-configured is terminal

- **WHEN** the embeddings module returns a not-configured error kind
- **THEN** the worker classifies it as terminal and marks the job `failed` without a retry, using `errors.Is`, never substring matching

#### Scenario: Model-denied is terminal

- **WHEN** the embeddings module returns a model-denied/status error kind
- **THEN** the worker classifies it as terminal and marks the job `failed` without a retry

#### Scenario: Dimension mismatch is terminal

- **WHEN** the embeddings module returns a dimension-mismatch error kind (vector length ≠ 768)
- **THEN** the worker classifies it as terminal and marks the job `failed` without a retry

#### Scenario: Transient is retryable

- **WHEN** the embeddings module returns a transient error kind
- **THEN** the worker requeues the job with backoff (and dead-letters only once `MaxAttempts` is reached)

#### Scenario: Classification never uses substring matching

- **WHEN** the worker decides whether a job is terminal or transient
- **THEN** the decision depends only on the typed error kind via `errors.Is`/`As`, and no substring of the error message is consulted

### Requirement: The static noop fallback is terminal, not retryable

When no usable credential resolves for a request, the embeddings module SHALL return a terminal not-configured
error instead of silently falling back to the static `NoopClient` and returning an empty vector. An empty
vector MUST NOT be presented to the worker as a successful (or transient) outcome.

#### Scenario: No usable credential returns a terminal error

- **WHEN** the resolver yields no usable credential and there is no static config for the project
- **THEN** the embeddings module returns a terminal not-configured error, not a nil/empty vector

#### Scenario: Noop fallback is not silently used

- **WHEN** a project's embedding model is unresolvable
- **THEN** the request does not fall through to `NoopClient` and return an empty vector that the worker would treat as transient

### Requirement: A project that cannot embed is blocked at project scope

The server SHALL introduce a project-level blocked state — `kb.projects.embedding_blocked` +
`embedding_blocked_reason` (or a small dedicated table) — representing "this project cannot embed". Blocking
is a project property, NOT a per-job status. Workers and the sweep SHALL skip blocked projects.

#### Scenario: Blocked project is skipped by workers

- **WHEN** a project is marked blocked and its jobs are claimed
- **THEN** the chunk, graph-object, and graph-relationship workers skip that project's jobs rather than re-attempting them

#### Scenario: Blocked project is skipped by the sweep

- **WHEN** a project is marked blocked and the sweep evaluates its objects
- **THEN** the sweep admits nothing for that project

#### Scenario: Blocking is a project property, not a job status

- **WHEN** a project is blocked
- **THEN** the blocked fact is stored once at project scope, not duplicated as a `blocked` status on each job row

### Requirement: Self-heal on configuration change

When a project's embedding model is set or updated, the server SHALL clear the blocked state and set a backfill
marker; the existing sweep backfills under its admission throttle. The change MUST NOT mass-flip
blocked→pending jobs.

#### Scenario: Config change clears the block

- **WHEN** a blocked project's embedding model is set or updated
- **THEN** the project's blocked state is cleared and a backfill marker is set so the sweep re-admits its unembedded objects

#### Scenario: No mass flip of blocked jobs

- **WHEN** a blocked project becomes configurable
- **THEN** no existing blocked job rows are bulk-flipped to `pending`; re-enqueue happens only through the throttled sweep

### Requirement: Retry caps and dead-letter are consistent across the three queues

The chunk (`kb.chunk_embedding_jobs`), graph-object (`kb.graph_embedding_jobs`), and graph-relationship
(`kb.graph_relationship_embedding_jobs`) queues SHALL share terminal semantics: the same max-attempts
threshold and the same dead-letter behaviour.

#### Scenario: Same max-attempts across queues

- **WHEN** a job in any of the three queues exhausts its retries
- **THEN** it is dead-lettered under the same max-attempts threshold, regardless of which queue it belongs to

#### Scenario: Same dead-letter destination semantics

- **WHEN** a terminal, over-attempt job is retired
- **THEN** it moves to `dead_letter` with the same status semantics across all three queues, never silently dropped or retried
