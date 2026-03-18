## 1. Database Migrations

- [ ] 1.1 Create Goose migration: add `use_batch_api` (bool default false), `batch_gcs_bucket` (text default ''), `batch_flush_interval_seconds` (int default 300) columns to `kb.org_provider_configs` and `kb.project_provider_configs`
- [ ] 1.2 Create Goose migration: create `kb.batch_prediction_jobs` table (id, project_id, org_id, vertex_job_name, gcs_input_uri, gcs_output_uri, status, request_count, completed_count, failed_count, error_message, submitted_at, completed_at, created_at, updated_at)
- [ ] 1.3 Create Goose migration: create `kb.batch_prediction_requests` table (id, project_id, org_id, request_type, source_job_type, source_job_id, input_payload JSONB, batch_job_id, status, created_at, updated_at)
- [ ] 1.4 Create Goose migration: add `is_batch` (bool default false) column to `kb.llm_usage_events`

## 2. Provider Config — Entity and Credential Updates

- [ ] 2.1 Add `UseBatchAPI bool`, `BatchGCSBucket string`, `BatchFlushIntervalSeconds int` fields to `OrgProviderConfig` and `ProjectProviderConfig` structs in `domain/provider/entity.go` with correct Bun column tags
- [ ] 2.2 Add the same three fields to `ProviderConfigResponse` and `ProjectProviderConfigResponse` response types in `domain/provider/entity.go`
- [ ] 2.3 Add `UseBatchAPI bool`, `BatchGCSBucket string`, `BatchFlushIntervalSeconds int` to `ResolvedCredential` in `pkg/adk/credentials.go`
- [ ] 2.4 Add `UseBatchAPI bool`, `BatchGCSBucket string`, `BatchFlushIntervalSeconds int` to `ResolvedEmbeddingCredential` in `pkg/embeddings/credentials.go`
- [ ] 2.5 Update `CredentialService.decryptOrgConfig` and `decryptProjectConfig` in `domain/provider/service.go` to populate the three new batch fields on `ResolvedCredential`
- [ ] 2.6 Update the embedding `EmbeddingResolver` adapter in `domain/provider/embeddings_adapter.go` to populate batch fields on `ResolvedEmbeddingCredential`
- [ ] 2.7 Add `IsBatch bool` field to `LLMUsageEvent` struct in `domain/provider/entity.go` with Bun column tag `is_batch`

## 3. Provider Config — Validation and API

- [ ] 3.1 Add `UseBatchAPI bool`, `BatchGCSBucket string`, `BatchFlushIntervalSeconds int` to `UpsertProviderConfigRequest` in `domain/provider/entity.go`
- [ ] 3.2 In `CredentialService.UpsertOrgConfig` and `UpsertProjectConfig`, add validation: if `UseBatchAPI=true` and provider is `google-vertex` and `BatchGCSBucket` is empty, return `400 Bad Request`
- [ ] 3.3 In `CredentialService.UpsertOrgConfig` and `UpsertProjectConfig`, add GCS write-test validation: when `UseBatchAPI=true` and a bucket is provided for Vertex AI, attempt to write and delete a test object; return `422 Unprocessable Entity` on failure
- [ ] 3.4 Update handler(s) in `domain/provider/handler.go` to pass batch fields from request body to `UpsertProviderConfigRequest`

## 4. Usage Service — Batch Cost Discount

- [ ] 4.1 Update `UsageService.calculateCost` in `domain/provider/usage_service.go` to apply a `0.5` multiplier when `event.IsBatch == true`
- [ ] 4.2 Update `TrackingModel.recordUsage` in `domain/provider/tracking_model.go` to set `IsBatch: false` explicitly (ensures default is clear for real-time events)

## 5. New Package: `pkg/vertexbatch/`

- [ ] 5.1 Create `apps/server/pkg/vertexbatch/types.go`: define `BatchClientConfig`, `BatchRequest` (RequestType, Model, InputPayload, SourceJobID), `BatchJob` (ID, VertexJobName, GCSInputURI, GCSOutputURI, Status), `BatchJobStatus` (string enum: pending/running/succeeded/failed/cancelled), `BatchResponse` (Index, SourceJobID, Embedding, GenerationText, TokensInput, TokensOutput, Error)
- [ ] 5.2 Create `apps/server/pkg/vertexbatch/client.go`: implement `NewClient(ctx, cfg)` constructing `aiplatform.JobClient` with SA JSON or ADC; validate non-empty `GCPProject`; implement `Submit(ctx, requests, cfg)`, `Poll(ctx, vertexJobName)`, `Cancel(ctx, vertexJobName)`
- [ ] 5.3 Create `apps/server/pkg/vertexbatch/input.go`: implement `BuildInputJSONL(requests)` serialising each request into a `{"request": {...}}` JSONL line per Vertex AI batch format; implement `UploadToGCS(ctx, bucket, path, data, creds)` using `cloud.google.com/go/storage`
- [ ] 5.4 Create `apps/server/pkg/vertexbatch/output.go`: implement `ReadResults(ctx, gcsOutputURI, creds)` downloading output JSONL from GCS and parsing each line into `BatchResponse`; tolerate and annotate malformed lines
- [ ] 5.5 Add `cloud.google.com/go/aiplatform` and `cloud.google.com/go/storage` to `apps/server/go.mod` via `go get`
- [ ] 5.6 Write unit tests for `BuildInputJSONL` and `ReadResults` output parsing in `pkg/vertexbatch/`

## 6. Batch Infrastructure — DB Store

- [ ] 6.1 Create `domain/extraction/batch_store.go` (or `domain/batchprediction/store.go`): Bun ORM entities `BatchPredictionRequest` and `BatchPredictionJob`; implement `InsertRequest`, `GetStagedByProject`, `MarkRequestsSubmitted`, `GetActiveJobs`, `MarkJobSucceeded`, `MarkJobFailed`, `ResetJobRequests` (back to staged), `DeleteCompletedRequests` (cleanup)
- [ ] 6.2 Add `batch_pending` and `batch_submitted` status values to the chunk embedding and graph embedding job status enums/constants (wherever job statuses are defined)

## 7. Batch Submit Worker

- [ ] 7.1 Create `domain/extraction/batch_submit_worker.go`: `BatchSubmitWorker` struct with `batchStore`, `credService`, `vertexbatchClient` factory, `log`; implement `Start(ctx)` with ticker at `batch_flush_interval_seconds`
- [ ] 7.2 Implement `BatchSubmitWorker.flush(ctx)`: query staged requests grouped by `(project_id, org_id)`, skip groups with no Vertex AI config or `UseBatchAPI=false`
- [ ] 7.3 Implement per-group submission: resolve credentials, group requests by model, call `vertexbatch.Client.Submit` for each model group, insert `kb.batch_prediction_jobs` row, update request rows to `submitted`, mark source jobs `batch_submitted`
- [ ] 7.4 Handle submit errors: if GCS upload or Vertex job creation fails, log and leave requests in `staged` for next flush cycle (do not mark failed immediately)

## 8. Batch Poll Worker

- [ ] 8.1 Create `domain/extraction/batch_poll_worker.go`: `BatchPollWorker` struct with `batchStore`, `credService`, `vertexbatchClient` factory, `db`, `usageService`, `log`; implement `Start(ctx)` with 2-minute ticker
- [ ] 8.2 Implement `BatchPollWorker.poll(ctx)`: query `kb.batch_prediction_jobs` for `submitted`/`running` status; for each job call `vertexbatch.Client.Poll`; update job status to `running` if Vertex says running
- [ ] 8.3 Implement succeeded path: call `vertexbatch.Client.ReadResults`, iterate responses; for `request_type='embed'` write vector to `kb.chunks.embedding` or `kb.graph_objects.embedding_v2`; mark source job `completed`; call `usageService.RecordAsync` with `IsBatch=true`
- [ ] 8.4 Implement succeeded path for `request_type='generate'`: deserialise JSON response per stored response schema; call `graph.Service` to persist entities and relationships; mark extraction source job `completed`; call `usageService.RecordAsync` with `IsBatch=true`
- [ ] 8.5 Implement partial-success handling: on per-item error in `BatchResponse`, reset that item's source job to `pending`; log per-item error with job ID and source job ID
- [ ] 8.6 Implement failure path: on `JOB_STATE_FAILED`, call `batchStore.ResetJobRequests` (staged), reset all source jobs to `pending`, mark batch job `failed`
- [ ] 8.7 Implement 24-hour timeout: in poll cycle, if `submitted_at` is >24h ago and job is not succeeded/failed/cancelled, treat as failed (same failure path as 8.6)

## 9. Existing Worker — Conditional Batch Routing

- [ ] 9.1 Update `ChunkEmbeddingWorker.processJob` in `domain/extraction/chunk_embedding_worker.go`: after resolving credentials, check `cred.UseBatchAPI && cred.IsVertexAI`; if true, call `batchStore.InsertRequest` and mark job `batch_pending`; otherwise execute existing real-time path
- [ ] 9.2 Update `GraphEmbeddingWorker.processJob` in `domain/extraction/graph_embedding_worker.go`: same conditional batch routing as 9.1 for `source_job_type='graph_embedding'`
- [ ] 9.3 Update `ObjectExtractionWorker.processJob` in `domain/extraction/object_extraction_worker.go`: after resolving credentials and loading document/schemas, check `cred.UseBatchAPI && cred.IsVertexAI`; if true, serialise prompt payload to `batchStore.InsertRequest` and mark job `batch_pending`; otherwise run `ExtractionPipeline` as today

## 10. fx Wiring and Lifecycle

- [ ] 10.1 Add `BatchSubmitWorker` and `BatchPollWorker` to the extraction domain's fx module (`domain/extraction/module.go`): provide constructors, register `Start`/`Stop` as fx lifecycle hooks
- [ ] 10.2 Provide `batchStore` (batch DB store) as an fx singleton in the module
- [ ] 10.3 Provide `vertexbatch.Client` factory (function that builds a client from resolved credentials) as a dependency injected into both workers

## 11. Cleanup Job

- [ ] 11.1 Add a daily cleanup task (using the existing scheduler infrastructure) that deletes `kb.batch_prediction_requests` rows with `status='completed'` older than 7 days and `kb.batch_prediction_jobs` rows with `status` in `(succeeded, failed, cancelled)` older than 7 days


## 12. User-Facing Setup Guide

The guide lives at `docs/guides/vertex-ai-batch-setup.md` and covers two sequential parts:
**Part 1 — GCS bucket setup (gcloud)** and **Part 2 — Memory configuration (memory CLI)**.

- [ ] 12.1 Write guide introduction: what batch API is, why use it (~50% cost reduction), and the two-SA model (Memory SA vs Vertex AI SA) explained in plain language
- [ ] 12.2 Write Part 1 — GCS bucket setup using `gcloud`:
  - Set shell variables (`PROJECT_ID`, `REGION`, `BUCKET`)
  - `gcloud storage buckets create` with `--location` matching Memory's Vertex AI location
  - `gcloud projects describe` to get project number and construct the Vertex AI SA email
  - `gcloud storage buckets add-iam-policy-binding` granting `roles/storage.objectAdmin` to the Vertex AI SA
  - `gcloud storage buckets get-iam-policy` to verify
  - Callout box: region must match Memory's Vertex AI location exactly; include `gcloud ai locations list` tip
- [ ] 12.3 Write Part 2 — Memory configuration using the `memory` CLI:
  - `memory provider update --use-batch-api --batch-gcs-bucket=<BUCKET> --batch-flush-interval=300`
  - Explain what Memory validates on save (bucket write test using Memory's SA) and what it cannot validate (Vertex AI SA access — must be done in Part 1)
  - Explain the flush interval: how often requests are bundled and submitted to Google
- [ ] 12.4 Write "What to expect" section: up to 24h turnaround, automatic fallback if a batch job fails, no user action needed day-to-day
- [ ] 12.5 Write "Turning it off" section: `memory provider update --no-use-batch-api`; real-time processing resumes immediately
- [ ] 12.6 Write "Troubleshooting" section covering: region mismatch symptoms, Vertex AI SA missing permission (jobs fail after submission), Memory SA missing permission (config save fails), and how to check batch job status
- [ ] 12.7 Add region reference table: common Vertex AI locations (`us-central1`, `us-east1`, `europe-west4`, `asia-northeast1`, etc.) and their valid GCS bucket locations