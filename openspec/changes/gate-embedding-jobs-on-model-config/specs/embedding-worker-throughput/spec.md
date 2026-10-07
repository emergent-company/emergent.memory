## Purpose

Align the graph embedding worker's terminal-vs-transient classification with the new typed embedding error
kinds, make the static noop fallback terminal, and bring the graph-relationship queue to the same
max-attempts / dead-letter semantics as the graph-object and chunk queues.

## MODIFIED Requirements

### Requirement: Every claimed job reaches exactly one terminal or retryable state

Each claimed job SHALL be completed, failed, moved to dead_letter, requeued for retry, or deleted because its
object no longer exists. A job MUST NOT be lost, double-processed, or reaped as stale merely because its
embedding request is slow. Transient failures SHALL be requeued with backoff and permanent failures SHALL not
be retried. Terminal vs transient SHALL be classified from the typed embedding error kinds in
`pkg/embeddings` via `errors.Is`/`errors.As` — never by substring matching on the error message. The static
`NoopClient` fallback is a terminal not-configured outcome, not a transient failure.

#### Scenario: Transient failure is requeued

- **WHEN** a multi-object embedding request fails with a typed transient error
- **THEN** each job in the group is requeued as `pending` with backoff, or moved to `dead_letter` once `MaxAttempts` is reached

#### Scenario: Permanent failure is not retried

- **WHEN** an embedding request fails with a typed terminal error (not-configured, model-denied/status, or dimension-mismatch)
- **THEN** each affected job is marked `failed` without a retry

#### Scenario: Noop fallback is terminal

- **WHEN** no usable credential resolves and the embeddings module would otherwise return an empty vector via the noop fallback
- **THEN** the job is marked `failed` as a terminal not-configured outcome, never requeued

#### Scenario: Missing object drops the job

- **WHEN** a claimed job's graph object no longer exists
- **THEN** the job is deleted rather than retried

#### Scenario: Slow does not mean stale

- **WHEN** a job's embedding request is slow but within the stale-job threshold
- **THEN** nothing in the worker's admission or completion path marks that job stale

## ADDED Requirements

### Requirement: The graph-relationship queue shares the terminal contract

The graph-relationship queue (`kb.graph_relationship_embedding_jobs`) SHALL apply the same max-attempts
threshold and the same dead-letter semantics as the graph-object (`kb.graph_embedding_jobs`) and chunk
(`kb.chunk_embedding_jobs`) queues, so terminal and over-attempt relationship jobs retire identically.

#### Scenario: Relationship job terminal semantics match the object queue

- **WHEN** a relationship embedding request fails with a typed terminal error, or a relationship job exhausts `MaxAttempts`
- **THEN** the job is marked `failed` or moved to `dead_letter` under the same rules as a graph-object job
