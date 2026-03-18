## Why

LLM calls for offline workloads (chunk embedding, graph embedding, extraction) are billed at full real-time pricing even though they have no latency requirements. Google Vertex AI's Batch Prediction API offers a **50% discount** on these calls by processing requests asynchronously (up to 24 h turnaround), making it significantly cheaper for bulk, background workloads.

## What Changes

- Add a **Vertex AI Batch Prediction** submission and polling client to the embeddings and generation layer.
- Add a **batch job queue** (backed by the existing Bun/Postgres job infrastructure) that accumulates embedding and generation requests and submits them as Vertex AI `BatchPredictionJob`s.
- Expose a project/org-level **config flag** (`use_batch_api: bool`) that opts a project into the batch path for background workers (embedding & extraction).
- Implement **result retrieval**: poll job state, read JSONL output from GCS, and write results back (embeddings stored, extracted entities enqueued downstream).
- Update **usage tracking** so batch job costs are recorded as `LLMUsageEvent`s (with a `source: batch` tag) after job completion.
- The existing real-time path is **unchanged and remains the default** — batch is opt-in.

## Capabilities

### New Capabilities

- `gemini-vertex-batch-jobs`: Submit, poll, and retrieve results for Vertex AI `BatchPredictionJob`s. Covers the full lifecycle: JSONL input construction, GCS upload, job creation, status polling, output JSONL download, and result dispatch.
- `batch-embedding-worker`: A background worker that drains the chunk/graph embedding queue via the Vertex AI Batch Prediction API instead of real-time calls, controlled by the `use_batch_api` flag on the project/org config.
- `batch-llm-generation`: Route extraction agent `GenerateContent` calls through Vertex AI batch jobs when the batch flag is enabled, with results written back to the job queue for downstream processing.
- `batch-api-config`: Project- and org-level configuration to enable/disable batch API usage, including GCS bucket configuration (required for Vertex AI batch I/O) and batch size / flush interval tuning.

### Modified Capabilities

- `llm-provider`: `ProjectProviderConfig` and `OrgProviderConfig` entities gain `use_batch_api` (bool) and `batch_gcs_bucket` (string) fields. Credential resolution exposes these to workers.

## Impact

**Code touched:**
- `pkg/embeddings/vertex/client.go` — add `BatchEmbedDocuments()` using Vertex AI Batch Prediction
- `pkg/embeddings/genai/client.go` — add `BatchEmbedDocuments()` stub (Gemini API has no batch; delegates to real-time)
- `pkg/embeddings/client.go` — extend `Client` interface with optional `BatchEmbedDocuments()`
- `pkg/adk/model.go` — add `CreateBatchModel()` factory that returns a batch-capable LLM wrapper
- `domain/provider/entity.go` — add `use_batch_api`, `batch_gcs_bucket` columns
- `domain/provider/service.go` — expose batch config in `ResolvedCredential`
- `domain/extraction/chunk_embedding_worker.go` — conditional batch path
- `domain/extraction/graph_embedding_worker.go` — conditional batch path
- `domain/extraction/object_extraction_worker.go` — conditional batch path for GenerateContent
- New package: `pkg/vertexbatch/` — `JobClient`, `InputBuilder`, `OutputReader`, `Poller`

**Dependencies:**
- `cloud.google.com/go/aiplatform` Go SDK (new dependency) — for `JobClient.CreateBatchPredictionJob`
- `cloud.google.com/go/storage` (likely already in tree) — for GCS JSONL upload/download

**Infrastructure:**
- A GCS bucket is required per Vertex AI org/project config for batch I/O.
- Vertex AI batch is **asynchronous** — results arrive hours later; existing job-queue infrastructure handles the async gap.
- Batch path is **Vertex AI only** — the Google AI (API key) provider falls back to real-time automatically.
- No breaking changes to existing APIs or real-time paths.
