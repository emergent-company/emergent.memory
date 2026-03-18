## Context

The Memory server processes three categories of background LLM workload:

1. **Chunk embeddings** — `ChunkEmbeddingWorker` calls `EmbedQueryWithUsage()` per chunk text
2. **Graph embeddings** — `GraphEmbeddingWorker` calls `EmbedQueryWithUsage()` per graph object
3. **Object extraction** — `ObjectExtractionWorker` runs an ADK `ExtractionPipeline` that calls `GenerateContent()` via `ModelFactory`

All three use real-time Vertex AI / Google AI endpoints and are billed at full pricing. None require sub-second latency — they run as background jobs polled on configurable intervals with semaphore-limited concurrency.

Google Vertex AI offers **Batch Prediction Jobs** — an asynchronous API that processes `generateContent` / embedding requests at a 50% cost reduction. The trade-off is latency: results arrive within 24 hours instead of seconds. This is acceptable for our background workers.

**Current architecture relevant to this design:**

- **Credential resolution hierarchy**: project config → org config → env vars (`provider/service.go:101`)
- **Embedding client dispatch**: `embeddings.Service.resolveClient()` creates transient Vertex or GenAI clients per request (`embeddings/module.go:186`)
- **Generation dispatch**: `adk.ModelFactory.CreateModelWithName()` resolves credentials and creates a `gemini.Model` (`adk/model.go:81`)
- **Usage tracking**: `TrackingModel` intercepts `GenerateContent` responses, fires async `LLMUsageEvent` to `UsageService` channel (`provider/tracking_model.go:58`)
- **Job infrastructure**: Bun-backed job queues with poll-based workers, configurable concurrency, health-aware scaling

**Vertex AI Batch Prediction constraints:**
- Vertex AI only (not available on Google AI / API key auth)
- Input: JSONL file on GCS, max 200k requests, max 1GB
- Output: JSONL on GCS
- Turnaround: up to 24h, 72h queue timeout
- No streaming, no explicit caching, no RAG
- Auth: service account / ADC (no API key)

## Goals / Non-Goals

**Goals:**
- Reduce LLM costs by ~50% for background embedding and extraction workloads via Vertex AI Batch Prediction
- Make batch mode opt-in at the project/org configuration level — no disruption to existing users
- Maintain the same eventual outcome: chunks get embeddings, graph objects get embeddings, extraction produces entities
- Track batch job costs in `LLMUsageEvent` the same as real-time calls
- Graceful degradation: if batch jobs fail or time out, fall back to real-time processing

**Non-Goals:**
- Supporting batch for Google AI (API key) — the Batch Prediction API is Vertex AI only; Google AI configs silently use real-time
- Batch for interactive / user-facing LLM calls (chat, agent runs) — those need real-time latency
- Sub-hour turnaround guarantees — batch jobs may take hours
- Multi-cloud batch support (e.g., AWS Bedrock batch) — Vertex AI only for now
- GCS bucket lifecycle management — the user provides a bucket; we don't create or manage retention policies

## Decisions

### D1: Accumulate-and-submit model (not request-per-job)

**Decision:** Accumulate pending embedding/generation requests in a Postgres staging table, then periodically flush them as a single Vertex AI `BatchPredictionJob`. A poller retrieves results when the job completes.

**Why not submit per-request:** Vertex AI batch jobs have overhead (GCS upload, job creation, polling). Submitting one job per embedding request would be slower and more expensive than real-time. Batching hundreds/thousands of requests into one job maximizes the cost benefit.

**Why not in-memory accumulation:** We already have Postgres-backed job queues. A staging table (`kb.batch_prediction_requests`) is crash-safe, observable, and consistent with existing patterns.

**Alternatives considered:**
- *Redis queue*: adds an infrastructure dependency we don't have; Postgres is simpler and crash-safe
- *File-based accumulation*: fragile on multi-replica deploys; not crash-safe

### D2: Two-phase worker architecture

**Decision:** Split the batch path into two workers:

1. **BatchSubmitWorker** — runs on a timer (e.g., every 5 minutes). Queries staged requests, builds JSONL, uploads to GCS, creates `BatchPredictionJob`, records job ID.
2. **BatchPollWorker** — runs on a timer (e.g., every 2 minutes). Polls active `BatchPredictionJob`s for completion. On success: downloads output JSONL, parses results, writes embeddings/extraction results, records `LLMUsageEvent`s, cleans up.

**Why two workers:** Submission and result retrieval are independent lifecycles. The submit worker runs once to create a job; the poll worker checks multiple in-flight jobs. Separating them keeps logic simple and testable.

### D3: Conditional routing in existing workers

**Decision:** Modify `ChunkEmbeddingWorker`, `GraphEmbeddingWorker`, and `ObjectExtractionWorker` to check a `use_batch_api` flag from the resolved project/org config. If enabled and the provider is Vertex AI, instead of calling the real-time API, insert a row into `kb.batch_prediction_requests` and mark the job as "pending-batch". The existing job stays in a `batch_pending` state until the `BatchPollWorker` writes results and marks it complete.

**Why not a separate "batch worker":** The existing workers already handle job dequeuing, error handling, and retry logic. Adding a conditional branch is less code than duplicating the full worker pipeline.

**Alternatives considered:**
- *Separate batch-only workers*: duplication of job queue logic; harder to maintain
- *Middleware/interceptor on embedding client*: would need to intercept the call, buffer it, and return a placeholder — overly complex for the embedding path where we need the result written to the DB

### D4: New `pkg/vertexbatch/` package

**Decision:** Create a new package `pkg/vertexbatch/` with:
- `client.go` — wraps `cloud.google.com/go/aiplatform/apiv1.JobClient` for `CreateBatchPredictionJob`, `GetBatchPredictionJob`, `CancelBatchPredictionJob`
- `input.go` — builds JSONL from staged requests, uploads to GCS
- `output.go` — downloads output JSONL from GCS, parses responses
- `types.go` — shared types (`BatchJob`, `BatchRequest`, `BatchResponse`)

**Why a dedicated package:** Separates Vertex AI batch protocol details from domain logic. The domain workers interact with a clean Go API (`Submit`, `Poll`, `ReadResults`) without knowing about GCS paths or JSONL formats.

**Why `cloud.google.com/go/aiplatform` over raw HTTP:** The official SDK handles auth, retries, gRPC transport, and proto (de)serialization. It's well-maintained and matches how we use `google.golang.org/genai` elsewhere.

### D5: Config stored in existing provider config entities

**Decision:** Add three fields to `OrgProviderConfig` and `ProjectProviderConfig`:

| Field | Type | Default | Description |
|---|---|---|---|
| `use_batch_api` | `bool` | `false` | Enable batch path for background workers |
| `batch_gcs_bucket` | `string` | `""` | GCS bucket for batch I/O (required when `use_batch_api=true`) |
| `batch_flush_interval_seconds` | `int` | `300` | How often the submit worker flushes staged requests |

**Why not a separate config table:** These are provider-level settings that follow the same resolution hierarchy (project -> org). Adding columns to the existing entities keeps the resolution logic unified.

### D6: New staging table `kb.batch_prediction_requests`

**Decision:** Create a table to stage individual requests before they are bundled into a batch job:

```sql
CREATE TABLE kb.batch_prediction_requests (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID NOT NULL,
    org_id          UUID NOT NULL,
    request_type    TEXT NOT NULL,          -- 'embed' or 'generate'
    source_job_type TEXT NOT NULL,          -- 'chunk_embedding', 'graph_embedding', 'object_extraction'
    source_job_id   UUID NOT NULL,          -- FK to the original job queue entry
    input_payload   JSONB NOT NULL,         -- the request body (text for embed, prompt for generate)
    batch_job_id    UUID,                   -- FK to kb.batch_prediction_jobs once submitted
    status          TEXT NOT NULL DEFAULT 'staged', -- staged, submitted, completed, failed
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

And a jobs table `kb.batch_prediction_jobs`:

```sql
CREATE TABLE kb.batch_prediction_jobs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id          UUID NOT NULL,
    org_id              UUID NOT NULL,
    vertex_job_name     TEXT,               -- Vertex AI resource name
    gcs_input_uri       TEXT,
    gcs_output_uri      TEXT,
    status              TEXT NOT NULL DEFAULT 'pending', -- pending, submitted, running, succeeded, failed, cancelled
    request_count       INT NOT NULL DEFAULT 0,
    completed_count     INT NOT NULL DEFAULT 0,
    failed_count        INT NOT NULL DEFAULT 0,
    error_message       TEXT,
    submitted_at        TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### D7: Usage tracking for batch results

**Decision:** When the `BatchPollWorker` processes completed results, it creates `LLMUsageEvent` records with `Operation = "embed"` or `"generate"` and a new field indicating batch source. Cost calculation uses the same `ProviderPricing` lookup — the 50% discount is inherent in what Google charges, not something we apply manually.

Add an `is_batch bool` field on `LLMUsageEvent` and apply a 0.5x multiplier in `calculateCost` when true, since retail pricing tables reflect full price but batch is billed at 50%.

### D8: Fallback to real-time on failure

**Decision:** If a batch job fails (Vertex returns `JOB_STATE_FAILED`) or times out (no completion within 24h), the `BatchPollWorker`:
1. Marks the batch job as failed
2. Resets all associated `batch_prediction_requests` to `staged` status
3. Marks the original source jobs (chunk/graph/extraction) back to `pending`
4. The existing real-time workers pick them up on their next poll cycle

This ensures no work is lost. If batch repeatedly fails, an org admin can disable `use_batch_api` to revert to real-time entirely.

## Risks / Trade-offs

**[Risk] Latency increase for background jobs** -> Mitigation: Batch is opt-in. Users who need fast embedding turnaround keep the default real-time path. Documentation will clearly state the 24h SLA.

**[Risk] GCS dependency** -> Mitigation: GCS is required only when batch is enabled. The bucket must be pre-created by the user and accessible by the Vertex AI service account. We validate GCS access on config save (similar to the existing credential live-test).

**[Risk] Partial batch job failures** -> Mitigation: Vertex AI batch jobs can partially succeed (some requests fail, others succeed). The `BatchPollWorker` processes each output line independently — successful results are written, failed ones are retried.

**[Risk] New `cloud.google.com/go/aiplatform` dependency** -> Mitigation: This is Google's official SDK, actively maintained, and already used widely in the Go ecosystem. It shares auth infrastructure with our existing `google.golang.org/genai` dependency.

**[Risk] Staging table growth** -> Mitigation: Completed requests are deleted after results are processed. A retention cleanup job (daily) removes completed batch jobs older than 7 days.

**[Risk] Multi-project batching** -> Mitigation: Batch jobs are scoped per project+org. The submit worker groups staged requests by `(project_id, org_id)` and creates separate jobs per group, ensuring credentials and cost tracking are isolated.

**[Risk] GCS bucket region mismatch** -> The bucket MUST be in the same region (or multi-region) as the Vertex AI location stored in the provider config. Google enforces this as a hard requirement — a mismatched bucket blocks the job entirely (not just an egress cost). Memory SHOULD warn on config save if the detected bucket location does not match the configured Vertex AI `location`. The user-facing setup guide must make this requirement explicit.

**[Risk] Test-write validation does not prove Vertex AI SA access** -> The GCS I/O for Batch Prediction is performed by Google's managed Vertex AI Service Agent (`service-{PROJECT_NUMBER}@gcp-sa-aiplatform.iam.gserviceaccount.com`), NOT the user's service account stored in Memory. A test write using Memory's SA credentials confirms Memory can reach the bucket, but does NOT confirm that the Vertex AI SA has access. The bucket validation on config save must document this limitation clearly. The user-facing setup guide must include the exact `gcloud` commands to grant the Vertex AI SA the required role on the bucket.

**[Trade-off] Complexity vs. cost savings**: The batch path adds two new workers, two new tables, a new package, and conditional logic in three existing workers. This is justified only if the org has meaningful LLM spend on background workloads. Small-volume projects won't benefit and should leave batch disabled.

## Migration Plan

1. **Database migration**: Add columns to `kb.org_provider_configs` and `kb.project_provider_configs`. Create `kb.batch_prediction_requests` and `kb.batch_prediction_jobs` tables. All additive — no breaking changes.
2. **Code deployment**: Deploy new package and workers. Batch is disabled by default (`use_batch_api = false`), so existing behavior is unchanged.
3. **Rollback**: Set `use_batch_api = false` on any affected configs. Staged requests revert to real-time processing. Drop new tables if full rollback needed.
4. **Validation**: Enable batch on a test project, verify embeddings arrive (with delay), verify usage events record correctly with batch discount.

## Open Questions

1. **Should batch flush be time-based or count-based?** Current design uses a timer (every N seconds). Alternative: flush when staged request count exceeds a threshold (e.g., 500). Could support both with "flush on whichever comes first."
2. **GCS bucket per-org or per-project?** Current design: per-org/project config field. Simpler alternative: one bucket per org with project-prefixed paths.
3. **Should we support batch for embeddings only (not generation)?** Embedding batches are simpler (uniform input/output shape). Generation batches are more complex (varied prompts, structured output schemas). Could ship embeddings-only in v1.

## Resolved: GCS Bucket Constraints (from research)

- **Same project not required.** The bucket can live in any GCP project, as long as the Vertex AI Service Agent from the job's project is granted access.
- **Same region IS required.** The bucket must be in the same region or multi-region as the Vertex AI `location` in the provider config. This is a hard Vertex AI enforcement, not just a cost concern.
- **Vertex AI uses its own managed SA for GCS I/O.** The SA is `service-{PROJECT_NUMBER}@gcp-sa-aiplatform.iam.gserviceaccount.com`. The user's SA (stored in Memory) is NOT used for batch GCS access. Required grant: `roles/storage.objectAdmin` (or at minimum `objectViewer` + `objectCreator`) on the bucket for that SA.
- **Memory's test-write uses the user's SA**, not the Vertex AI SA — so it validates bucket reachability for Memory, but not for Vertex AI. The setup guide must tell users to grant the Vertex AI SA explicitly.
- **Bucket setup is always user-owned ("bring your own bucket").** Memory does not create or manage buckets. A `gcloud`-based setup guide will be provided.
