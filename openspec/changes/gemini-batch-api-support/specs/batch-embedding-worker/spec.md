## ADDED Requirements

### Requirement: Chunk and graph embedding workers stage requests when batch API is enabled
When `use_batch_api=true` in the resolved project credentials and the provider is Vertex AI, `ChunkEmbeddingWorker` and `GraphEmbeddingWorker` SHALL NOT call the real-time embedding API. Instead, they SHALL insert a row into `kb.batch_prediction_requests` with `request_type='embed'`, `source_job_type` set to `'chunk_embedding'` or `'graph_embedding'`, the source job ID, and the text payload. The source job SHALL be marked `batch_pending` and left in the queue.

#### Scenario: Chunk embedding job routed to batch staging
- **WHEN** `ChunkEmbeddingWorker.processJob` runs for a project with `use_batch_api=true` (Vertex AI)
- **THEN** a row is inserted into `kb.batch_prediction_requests` with the chunk text and `source_job_id`
- **AND** the chunk embedding job is marked `batch_pending` (not `completed` or `failed`)
- **AND** no real-time embedding API call is made

#### Scenario: Graph embedding job routed to batch staging
- **WHEN** `GraphEmbeddingWorker.processJob` runs for a project with `use_batch_api=true` (Vertex AI)
- **THEN** a row is inserted into `kb.batch_prediction_requests` with the extracted text and `source_job_id`
- **AND** the graph embedding job is marked `batch_pending`

#### Scenario: Real-time path used when batch is disabled
- **WHEN** the project has `use_batch_api=false` (or no config)
- **THEN** the existing real-time embedding call is made and the job completes normally

#### Scenario: Real-time path used for Google AI provider
- **WHEN** the project uses a Google AI (API key) provider even if `use_batch_api=true`
- **THEN** the real-time embedding call is made (batch is not supported for Google AI)

### Requirement: BatchSubmitWorker flushes staged embed requests into a Vertex AI batch job
A `BatchSubmitWorker` SHALL run on a configurable interval (default 5 minutes, derived from `batch_flush_interval_seconds`). On each cycle it SHALL:
1. Query `kb.batch_prediction_requests` for rows in `staged` status grouped by `(project_id, org_id)`
2. For each group, resolve Vertex AI credentials, build a JSONL input file, upload to GCS, and create a `BatchPredictionJob`
3. Insert a row in `kb.batch_prediction_jobs` recording the Vertex job name, GCS URIs, and request count
4. Update the staged request rows to `submitted` status with the `batch_job_id` foreign key
5. Mark the corresponding source jobs as `batch_submitted`

#### Scenario: Flush cycle with staged requests
- **WHEN** `BatchSubmitWorker` runs and finds 150 staged embed requests for one project
- **THEN** one `BatchPredictionJob` is created on Vertex AI with 150 items
- **AND** a `kb.batch_prediction_jobs` row is inserted with `status='submitted'` and `request_count=150`
- **AND** all 150 `kb.batch_prediction_requests` rows are updated to `submitted`

#### Scenario: No staged requests — cycle is a no-op
- **WHEN** `BatchSubmitWorker` runs and finds no staged requests
- **THEN** no GCS uploads or Vertex API calls are made

#### Scenario: Flush interval is respected
- **WHEN** `batch_flush_interval_seconds=300`
- **THEN** `BatchSubmitWorker` does not flush more frequently than every 300 seconds per project group

### Requirement: BatchPollWorker retrieves completed embed results and writes embeddings
A `BatchPollWorker` SHALL run on a configurable interval (default 2 minutes). On each cycle it SHALL:
1. Query `kb.batch_prediction_jobs` for rows in `submitted` or `running` status
2. Call `vertexbatch.Client.Poll` for each job
3. For jobs in `succeeded` state: download output JSONL, parse each `BatchResponse`, and for each successful embedding result write the vector to the corresponding `kb.chunks` or `kb.graph_objects` row, mark the source job `completed`, and record an `LLMUsageEvent` with `is_batch=true` and `operation='embed'`
4. For jobs in `failed` state: reset staged requests to `staged`, reset source jobs to `pending`, and mark the batch job `failed`
5. For jobs that have been in-flight longer than 24 hours without succeeding: treat as failed (see above)

#### Scenario: Successful batch embed job — embeddings written
- **WHEN** a `BatchPredictionJob` completes with all items succeeded
- **THEN** each chunk's `embedding` column (or graph object's `embedding_v2` column) is updated with the returned vector
- **AND** the corresponding source jobs are marked `completed`
- **AND** `LLMUsageEvent` rows are inserted with `is_batch=true`, `operation='embed'`, and correct token counts

#### Scenario: Partial success — successful items written, failed items retried
- **WHEN** a batch job completes but some items have per-item errors
- **THEN** successful items have their embeddings written and jobs marked `completed`
- **AND** failed items' source jobs are reset to `pending` for real-time retry

#### Scenario: Batch job failure triggers fallback to real-time
- **WHEN** a `BatchPredictionJob` reaches `JOB_STATE_FAILED`
- **THEN** all staged requests for that job are reset to `staged`
- **AND** all source jobs are reset to `pending`
- **AND** the `kb.batch_prediction_jobs` row is marked `failed`
- **AND** the real-time embedding workers pick up the reset jobs on their next poll

#### Scenario: 24-hour timeout triggers failure handling
- **WHEN** a batch job has been in `submitted` or `running` status for more than 24 hours
- **THEN** it is treated as failed and the fallback path executes

### Requirement: Usage events for batch embeddings apply the batch cost discount
`LLMUsageEvent` rows created by `BatchPollWorker` for embedding results SHALL have `is_batch=true`. The `UsageService.calculateCost` method SHALL apply a `0.5` multiplier to the computed cost when `is_batch=true`.

#### Scenario: Cost is halved for batch embedding events
- **WHEN** a batch embedding `LLMUsageEvent` is persisted with token counts `T` and retail price `P`
- **THEN** `estimated_cost_usd = T * P / 1_000_000 * 0.5`
