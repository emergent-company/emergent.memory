## ADDED Requirements

### Requirement: Object extraction worker stages generation requests when batch API is enabled
When `use_batch_api=true` in the resolved project credentials and the provider is Vertex AI, `ObjectExtractionWorker` SHALL NOT invoke the real-time `ExtractionPipeline`. Instead, it SHALL serialise each `GenerateContent` request (prompt text, system instruction, response schema) into `kb.batch_prediction_requests` with `request_type='generate'` and `source_job_type='object_extraction'`, mark the extraction job `batch_pending`, and return.

#### Scenario: Extraction job routed to batch staging
- **WHEN** `ObjectExtractionWorker.processJob` runs for a project with `use_batch_api=true` (Vertex AI)
- **THEN** the document text and extraction prompt are serialised into a `kb.batch_prediction_requests` row
- **AND** the extraction job is marked `batch_pending`
- **AND** no real-time `GenerateContent` call is made

#### Scenario: Real-time path used when batch is disabled
- **WHEN** the project has `use_batch_api=false`
- **THEN** `ExtractionPipeline.Run` is called normally and the extraction job completes in real-time

#### Scenario: Real-time path used for Google AI provider
- **WHEN** the project uses a Google AI provider (API key) with `use_batch_api=true`
- **THEN** the real-time extraction pipeline is executed (batch unsupported for Google AI)

### Requirement: BatchSubmitWorker includes generation requests in batch jobs
The `BatchSubmitWorker` SHALL include `request_type='generate'` rows in the JSONL payload alongside embed requests, constructing each line from the stored prompt payload. Generation requests from the same project group SHALL be submitted in the same `BatchPredictionJob` as embed requests, or in a separate job if the mixed format is unsupported by Vertex AI for a given model.

#### Scenario: Generation requests are included in JSONL input
- **WHEN** staged requests include both embed and generate types for the same project
- **THEN** the JSONL file includes all requests and a single `BatchPredictionJob` is created

#### Scenario: Mixed model types require separate jobs
- **WHEN** embed requests use `gemini-embedding-001` and generate requests use `gemini-2.5-flash`
- **THEN** `BatchSubmitWorker` creates two separate `BatchPredictionJob`s, one per model

### Requirement: BatchPollWorker processes generation results and persists entities
When a `BatchPredictionJob` containing generation responses completes, `BatchPollWorker` SHALL:
1. Parse each generation `BatchResponse` from the output JSONL
2. Deserialise the JSON response according to the stored response schema
3. Pass the deserialized entities and relationships to `graph.Service` to persist them (same as `ObjectExtractionWorker.persistResults`)
4. Mark the source extraction job `completed`
5. Record an `LLMUsageEvent` with `is_batch=true`, `operation='generate'`, and correct token counts

#### Scenario: Successful generation result persisted
- **WHEN** a batch generation response is parsed successfully
- **THEN** entities and relationships are created in the knowledge graph
- **AND** the extraction source job is marked `completed`
- **AND** an `LLMUsageEvent` is inserted with `is_batch=true` and `operation='generate'`

#### Scenario: Malformed JSON generation response triggers retry
- **WHEN** the batch response for a generation request cannot be parsed as valid JSON matching the schema
- **THEN** the source extraction job is reset to `pending` for real-time retry
- **AND** the error is logged with the job ID and document ID

#### Scenario: Batch generation job failure triggers fallback
- **WHEN** a `BatchPredictionJob` for generation requests reaches `JOB_STATE_FAILED`
- **THEN** all associated extraction source jobs are reset to `pending`
- **AND** the real-time `ObjectExtractionWorker` picks them up on its next poll cycle

### Requirement: Usage events for batch generation apply the batch cost discount
`LLMUsageEvent` rows created by `BatchPollWorker` for generation results SHALL have `is_batch=true` and the `0.5` cost multiplier SHALL be applied in `UsageService.calculateCost`.

#### Scenario: Cost is halved for batch generation events
- **WHEN** a batch generation `LLMUsageEvent` is persisted with input tokens `I`, output tokens `O`, and retail prices `Pi`/`Po`
- **THEN** `estimated_cost_usd = (I * Pi / 1_000_000 + O * Po / 1_000_000) * 0.5`
