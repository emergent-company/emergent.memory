# embedding-sweep-ceilings Specification

## Purpose
The embedding sweep worker backfills objects and relationships that never received an embedding, and the chunk and relationship embedding workers drain their own job queues. Sweep admission and worker intake must not be capped by a fixed per-sweep or per-tranche constant, and every claimed job or attempted relationship must still reach a well-defined, retryable state.

## Requirements

### Requirement: Object admission is keyed to available queue depth
The sweep worker SHALL admit objects into `kb.graph_embedding_jobs` in proportion to available queue capacity, not a fixed per-sweep constant. Admission per sweep SHALL be `max(0, ObjectQueueTargetDepth − active)` where `active` counts `pending`|`processing` graph embedding jobs. If the active-job count cannot be read, the sweep SHALL admit nothing for that sweep rather than risk a flood. Admission SHALL remain idempotent: an object with an active job, or a project without an embedding model, SHALL NOT be enqueued.

#### Scenario: Admission is not capped by the constant batch size
- **WHEN** the target depth is 500, the queue is empty, and 600 objects lack embeddings
- **THEN** the sweep enqueues 500 objects (not the constant 200), and 100 remain for a later sweep

#### Scenario: A full queue is not flooded
- **WHEN** the active job count already equals the target depth
- **THEN** the sweep enqueues nothing

#### Scenario: Active jobs reduce admission
- **WHEN** the target depth is 10 and 6 jobs are already active
- **THEN** the sweep admits at most 4 objects

#### Scenario: A failed depth read admits nothing
- **WHEN** the active-job count query fails
- **THEN** the sweep enqueues no objects for that sweep

### Requirement: Relationships are embedded in multi-object requests grouped per project
Where the embedding service supports multi-object requests, the sweep worker SHALL embed a project's relationships with `EmbedDocumentsWithUsage`, split into `RelationshipRequestBatchSize` sub-batches (default 100), because embedding credentials and budgets are resolved per project. It SHALL apply the budget check once per project. Where the service has no multi-object method, or the batch size is negative, the worker SHALL keep the one-request-per-relationship path.

#### Scenario: One request per project
- **WHEN** two projects each have two relationships with a missing embedding and the service supports multi-object requests
- **THEN** two multi-object requests are issued (one per project, two relationships each) and no single-object request is issued

#### Scenario: Sub-batches respect the configured request size
- **WHEN** a project has more relationships than `RelationshipRequestBatchSize`
- **THEN** no embedding request carries more than that many relationships

#### Scenario: A mismatched batch response writes nothing
- **WHEN** a multi-object request returns a different number of vectors than documents
- **THEN** every relationship in that sub-batch is counted as an error and none is updated

#### Scenario: Single-object fallback
- **WHEN** the embedding service does not support multi-object requests
- **THEN** every relationship is embedded by its own request and stored independently

### Requirement: A failed relationship embedding is retryable, never partially written
Relationships have no job queue: each relationship SHALL independently end the sweep either updated with its embedding or left with `embedding IS NULL` for a later sweep. A wrong-length vector MUST NOT be written. A budget-exceeded project's relationships SHALL be skipped (not counted as errors) and left NULL. Each relationship SHALL count toward exactly one of embedded, errored, or skipped.

#### Scenario: A failed batch leaves rows for retry
- **WHEN** every embedding request fails
- **THEN** each attempted relationship is counted as an error and all embeddings remain NULL

#### Scenario: Budget-exceeded project is skipped
- **WHEN** a project has exceeded its monthly budget and enforcement is enabled
- **THEN** none of that project's relationships is embedded and none is counted as an error

### Requirement: Chunk and relationship worker admission is decoupled from per-job completion
`ChunkEmbeddingWorker` and `GraphRelationshipEmbeddingWorker` SHALL claim jobs only when there is free in-flight capacity and SHALL dispatch them asynchronously. They MUST NOT wait for a claimed tranche to finish before claiming the next one. In-flight jobs SHALL be bounded by the effective concurrency (after the system-health scaler, when enabled). Capacity SHALL be reclaimed as soon as a job finishes, without waiting for the next poll interval.

#### Scenario: A slow request does not gate the batch
- **WHEN** one claimed job's embedding request is slow and other claimed jobs could complete
- **THEN** `processBatch` returns without waiting for the slow job, the other jobs reach `completed`, and the slow job is still `processing`

#### Scenario: The slow job is not lost
- **WHEN** the slow job's request finally returns
- **THEN** the job is completed and its embedding is stored — it is neither failed nor abandoned for being slow

#### Scenario: Admission never overshoots
- **WHEN** the configured concurrency is 2 and 5 jobs are queued
- **THEN** at most 2 jobs are claimed (`processing`) and the remaining 3 stay `pending`

### Requirement: Claimed jobs reach exactly one terminal or retryable state on shutdown too
Each claimed chunk or relationship job SHALL be completed, failed, moved to `dead_letter`, requeued for retry, or deleted because its entity no longer exists. On shutdown the worker SHALL drain in-flight jobs within the caller's context so a claimed job is not abandoned in `processing`. A job MUST NOT be lost, double-processed, or reaped as stale merely because its embedding request is slow.

#### Scenario: Shutdown drains in-flight jobs
- **WHEN** the worker is stopped while jobs are in flight
- **THEN** it waits for those jobs to finish (within the stop context) rather than returning with them still `processing`

#### Scenario: Transient vs permanent failure unchanged
- **WHEN** an embedding request fails with a transient error
- **THEN** the job is requeued with backoff; a permanent error marks it failed without retry, exactly as before
