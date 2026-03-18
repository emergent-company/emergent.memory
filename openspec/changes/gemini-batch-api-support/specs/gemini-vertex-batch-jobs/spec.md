## ADDED Requirements

### Requirement: Submit a batch prediction job to Vertex AI
The system SHALL provide a `pkg/vertexbatch.Client` that accepts a list of embedding or generation requests, serialises them to JSONL, uploads the file to a caller-supplied GCS bucket, and creates a Vertex AI `BatchPredictionJob` via the `cloud.google.com/go/aiplatform/apiv1` SDK. The client SHALL return a `BatchJob` record containing the Vertex AI job resource name and the GCS input/output URIs.

#### Scenario: Successful submission of embedding requests
- **WHEN** `Client.Submit(ctx, requests, cfg)` is called with a non-empty list of embed requests and valid Vertex AI credentials
- **THEN** a JSONL file is written to `gs://<bucket>/batch-jobs/<job-id>/input.jsonl`
- **AND** a `BatchPredictionJob` is created on Vertex AI pointing at that file
- **AND** the returned `BatchJob` has status `submitted` and a non-empty `VertexJobName`

#### Scenario: Empty request list is rejected
- **WHEN** `Client.Submit` is called with an empty request slice
- **THEN** an error is returned and no GCS upload or Vertex job is created

#### Scenario: GCS upload failure aborts submission
- **WHEN** the GCS upload returns an error
- **THEN** `Client.Submit` returns that error and does NOT create a `BatchPredictionJob`

#### Scenario: Vertex job creation failure is surfaced
- **WHEN** the `CreateBatchPredictionJob` RPC returns an error
- **THEN** `Client.Submit` returns that error with context

### Requirement: Poll a batch prediction job for completion
The system SHALL provide a `Client.Poll(ctx, vertexJobName)` method that calls `GetBatchPredictionJob` on Vertex AI and returns the current `BatchJobStatus` (`pending`, `running`, `succeeded`, `failed`, `cancelled`).

#### Scenario: Job is still running
- **WHEN** `Client.Poll` is called for a job in `JOB_STATE_RUNNING`
- **THEN** `BatchJobStatus` `running` is returned with no error

#### Scenario: Job succeeds
- **WHEN** `Client.Poll` is called for a job in `JOB_STATE_SUCCEEDED`
- **THEN** `BatchJobStatus` `succeeded` is returned with the GCS output URI prefix populated

#### Scenario: Job fails on Vertex side
- **WHEN** `Client.Poll` is called for a job in `JOB_STATE_FAILED`
- **THEN** `BatchJobStatus` `failed` is returned along with the error detail from the Vertex API response

### Requirement: Read results from completed batch job output
The system SHALL provide a `Client.ReadResults(ctx, gcsOutputURI)` method that downloads the JSONL prediction output from GCS and returns a slice of `BatchResponse` items, each containing the original request index, the response payload (embedding vector or generation text), token usage counts, and an optional per-item error.

#### Scenario: Output file parsed correctly
- **WHEN** `Client.ReadResults` is called with a valid GCS URI pointing to a completed batch job output
- **THEN** each line in the JSONL is parsed into a `BatchResponse`
- **AND** items with successful predictions have `Embedding` or `GenerationText` populated
- **AND** items with per-item errors have `Error` populated and `Embedding`/`GenerationText` empty

#### Scenario: Missing output file returns error
- **WHEN** the GCS object does not exist
- **THEN** `Client.ReadResults` returns a not-found error

#### Scenario: Malformed JSONL line is skipped with error annotation
- **WHEN** a line in the output JSONL cannot be parsed
- **THEN** that line is returned as a `BatchResponse` with `Error` set and parsing continues for subsequent lines

### Requirement: Cancel an in-flight batch job
The system SHALL provide a `Client.Cancel(ctx, vertexJobName)` method that calls `CancelBatchPredictionJob` on Vertex AI. Cancellation is best-effort and does not guarantee the job stops immediately.

#### Scenario: Cancel request is issued
- **WHEN** `Client.Cancel` is called with a valid job name
- **THEN** the Vertex AI cancel RPC is called and any error is returned to the caller

### Requirement: Batch client is constructed from Vertex AI credentials
The system SHALL provide a `NewClient(ctx, cfg BatchClientConfig)` constructor that accepts a GCP project, location, service account JSON (or relies on ADC), and GCS bucket name. The constructor SHALL return an error if the Vertex AI `JobClient` cannot be initialised.

#### Scenario: Valid service account JSON builds client successfully
- **WHEN** `NewClient` is called with valid SA JSON and project/location
- **THEN** a `*Client` is returned with no error

#### Scenario: Missing GCP project returns error
- **WHEN** `NewClient` is called with an empty `GCPProject`
- **THEN** an error is returned before any network call is made
