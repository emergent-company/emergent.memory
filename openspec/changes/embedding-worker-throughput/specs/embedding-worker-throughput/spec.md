## Purpose

The graph embedding worker drains the `kb.graph_embedding_jobs` queue. Its throughput must not be limited by the slowest single embedding request, and every claimed job must still reach a well-defined terminal or retryable state.

## ADDED Requirements

### Requirement: Batch admission is decoupled from per-job completion
The graph embedding worker SHALL claim jobs only when there is free in-flight capacity and SHALL dispatch them asynchronously. It MUST NOT wait for a claimed tranche to finish before claiming the next one. A slow embedding request MUST hold only its own chunk's in-flight slots and MUST NOT delay the completion of jobs in other chunks.

#### Scenario: A slow request does not gate the batch
- **WHEN** one claimed job's embedding request is slow and other claimed jobs could complete
- **THEN** the poll loop (`processBatch`) returns without waiting for the slow job, and the other jobs reach `completed` while the slow job is still `processing`

#### Scenario: The slow job is not lost
- **WHEN** the slow job's request finally returns
- **THEN** the job is marked `completed` and the object's embedding is stored — it is neither failed nor abandoned for being slow

### Requirement: In-flight work is bounded by the configured concurrency
The worker SHALL NOT claim more jobs than the effective concurrency allows. The number of jobs in flight MUST be at most `WorkerConcurrency` (or the adapted value when the system-health scaler is enabled).

#### Scenario: Admission never overshoots
- **WHEN** the configured concurrency is 2 and 5 jobs are queued
- **THEN** at most 2 jobs are claimed (`processing`) and the remaining 3 stay `pending`

#### Scenario: Capacity is reclaimed
- **WHEN** a claimed chunk finishes
- **THEN** its jobs are released and the freed capacity is available to claim the next jobs without waiting for the next poll interval

### Requirement: Every claimed job reaches exactly one terminal or retryable state
Each claimed job SHALL be completed, failed, moved to dead_letter, requeued for retry, or deleted because its object no longer exists. A job MUST NOT be lost, double-processed, or reaped as stale merely because its embedding request is slow. Transient failures SHALL be requeued with backoff and permanent failures SHALL not be retried, as before.

#### Scenario: Transient failure is requeued
- **WHEN** a multi-object embedding request fails with a transient error
- **THEN** each job in the group is requeued as `pending` with backoff, or moved to `dead_letter` once `MaxAttempts` is reached

#### Scenario: Permanent failure is not retried
- **WHEN** an embedding request fails with a permanent error (invalid model, credentials, or missing model configuration)
- **THEN** each affected job is marked `failed` without a retry

#### Scenario: Missing object drops the job
- **WHEN** a claimed job's graph object no longer exists
- **THEN** the job is deleted rather than retried

#### Scenario: Slow does not mean stale
- **WHEN** a job's embedding request is slow but within the stale-job threshold
- **THEN** nothing in the worker's admission or completion path marks that job stale

### Requirement: Objects are embedded in multi-object requests grouped per project
Where the embedding service supports multi-object requests, the worker SHALL embed the objects of a chunk with a single request per project, because embedding credentials and budgets are resolved per project. The number of objects per request SHALL be configurable (`EmbeddingRequestBatchSize`, default 100). Where the embedding service does not support multi-object requests, the worker SHALL issue one request per job and MUST NOT serialize jobs behind one another.

#### Scenario: One request per project
- **WHEN** a chunk contains objects from two projects and the embedding service supports multi-object requests
- **THEN** one multi-object request is issued per project, each carrying that project's objects, and no single-object request is issued

#### Scenario: Single-object fallback is concurrent
- **WHEN** the embedding service does not support multi-object requests
- **THEN** every job is embedded by its own request in its own chunk, so one job's request cannot block another's

#### Scenario: Request batch size is configurable
- **WHEN** `GRAPH_EMBEDDING_REQUEST_BATCH_SIZE` (or `GraphEmbeddingConfig.EmbeddingRequestBatchSize`) is set
- **THEN** no embedding request carries more than that many objects

### Requirement: Per-project budget and usage semantics are preserved
The worker SHALL apply the embedding budget check per project and SHALL reschedule budget-exceeded jobs with a 5-minute delay without incrementing their attempt count. Embedding usage SHALL be recorded per project with the token usage reported by the provider.

#### Scenario: Budget exceeded reschedules the group
- **WHEN** budget enforcement is enabled and a project has exceeded its monthly budget
- **THEN** that project's claimed jobs are rescheduled to `pending` with `last_error = 'budget_exceeded'` and a 5-minute delay, and no embedding request is issued for them
