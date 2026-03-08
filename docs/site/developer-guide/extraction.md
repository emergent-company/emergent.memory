# Extraction Pipeline

The extraction pipeline turns raw documents into structured knowledge graph objects. It runs asynchronously across multiple job queues and exposes an admin API for monitoring and control.

## How extraction works

```
Document uploaded
      │
      ▼
Document parsing job   ← chunks text into passages
      │
      ▼
Object extraction job  ← LLM extracts typed objects + relationships
      │
      ▼
Chunk embedding jobs   ← embeds document chunks for search
      │
      ▼
Graph embedding jobs   ← embeds extracted objects for vector search
```

Each stage is an independent job queue. Failures in one stage don't block others.

---

## Job types

| Job type | `jobType` value | Description |
|---|---|---|
| Full extraction | `full_extraction` | Process all chunks in a document |
| Re-extraction | `reextraction` | Re-process an already-extracted document |
| Incremental | `incremental` | Process only new/changed chunks |

---

## Job statuses

| API status | Description |
|---|---|
| `queued` | Waiting to be picked up |
| `running` | Currently processing |
| `completed` | Successfully finished |
| `failed` | Failed after max retries |
| `cancelled` | Manually cancelled |
| `requires_review` | Completed but flagged for human review |

---

## Trigger types

| Trigger | `triggerType` value | Description |
|---|---|---|
| Manual | `manual` | Created via API |
| Scheduled | `scheduled` | Created by the scheduler |
| Webhook | `webhook` | Triggered by a datasource sync event |

---

## Managing extraction jobs

### List jobs for a project

```bash
curl https://api.dev.emergent-company.ai/api/admin/extraction-jobs/projects/<projectId> \
  -H "Authorization: Bearer <token>"
```

Query parameters:

| Parameter | Description |
|---|---|
| `status` | Filter by status: `queued`, `running`, `completed`, `failed`, `cancelled` |
| `jobType` | Filter by job type |
| `limit` | Number of results (default 20) |
| `offset` | Pagination offset |

### Get a specific job

```bash
curl https://api.dev.emergent-company.ai/api/admin/extraction-jobs/<jobId> \
  -H "Authorization: Bearer <token>"
```

### Get job statistics

```bash
curl https://api.dev.emergent-company.ai/api/admin/extraction-jobs/projects/<projectId>/statistics \
  -H "Authorization: Bearer <token>"
```

Returns counts by status and job type, plus throughput metrics (jobs per hour, average duration).

### Create a job manually

```bash
curl -X POST https://api.dev.emergent-company.ai/api/admin/extraction-jobs \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "projectId": "<projectId>",
    "documentId": "<documentId>",
    "jobType": "full_extraction",
    "priority": 10
  }'
```

Higher `priority` values are processed first.

### Cancel a job

```bash
curl -X POST https://api.dev.emergent-company.ai/api/admin/extraction-jobs/<jobId>/cancel \
  -H "Authorization: Bearer <token>"
```

### Retry a failed job

```bash
curl -X POST https://api.dev.emergent-company.ai/api/admin/extraction-jobs/<jobId>/retry \
  -H "Authorization: Bearer <token>"
```

---

## Bulk operations

### Bulk cancel

```bash
curl -X POST https://api.dev.emergent-company.ai/api/admin/extraction-jobs/projects/<projectId>/bulk-cancel \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"status": "queued"}'
```

### Bulk retry

```bash
curl -X POST https://api.dev.emergent-company.ai/api/admin/extraction-jobs/projects/<projectId>/bulk-retry \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"status": "failed"}'
```

### Bulk delete

```bash
curl -X DELETE https://api.dev.emergent-company.ai/api/admin/extraction-jobs/projects/<projectId>/bulk-delete \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"status": "completed", "olderThanDays": 30}'
```

---

## Job logs

Each extraction job records detailed logs for every step, including LLM calls, token counts, and objects created.

### Get logs for a job

```bash
curl https://api.dev.emergent-company.ai/api/admin/extraction-jobs/<jobId>/logs \
  -H "Authorization: Bearer <token>"
```

### Log entry fields

| Field | Description |
|---|---|
| `operation` | Type of step (see below) |
| `message` | Human-readable description |
| `metadata` | Structured context: model name, token counts, object IDs, etc. |
| `createdAt` | Timestamp |

### Log operation types

| Operation | Description |
|---|---|
| `llm_call` | An LLM was called — `metadata` includes model, input tokens, output tokens |
| `chunk_processing` | A document chunk was processed |
| `object_creation` | A graph object was created from extracted data |
| `relationship_creation` | A graph relationship was created |
| `suggestion_creation` | A suggestion (pending review) was created |
| `validation` | Schema validation was run on extracted data |
| `error` | An error occurred during processing |

---

## Extraction job entity reference

**`ObjectExtractionJob`** — table `kb.object_extraction_jobs`

| Field | Type | Description |
|---|---|---|
| `id` | UUID | Primary key |
| `projectId` | UUID | Owning project |
| `documentId` | UUID | Source document (nullable) |
| `datasourceId` | UUID | Source datasource (nullable) |
| `jobType` | string | `full_extraction` \| `reextraction` \| `incremental` |
| `triggerType` | string | `manual` \| `scheduled` \| `webhook` |
| `status` | string | See status table above |
| `priority` | int | Processing priority (higher = first) |
| `errorMsg` | string | Last error message |
| `retryCount` | int | Number of retries so far |
| `maxRetries` | int | Maximum allowed retries |
| `startedAt` | timestamp | When processing began |
| `completedAt` | timestamp | When processing finished |
| `createdAt` | timestamp | |
| `updatedAt` | timestamp | |

---

## Embedding control (ops)

!!! warning "Internal endpoints"
    These endpoints have **no authentication**. They must be protected at the network or firewall level.

### Check embedding worker status

```bash
curl http://localhost:3012/api/embeddings/status
```

### Pause embedding worker

```bash
curl -X POST http://localhost:3012/api/embeddings/pause
```

Useful during maintenance windows or when running bulk re-indexing.

### Resume embedding worker

```bash
curl -X POST http://localhost:3012/api/embeddings/resume
```

### Update embedding config

```bash
curl -X PATCH http://localhost:3012/api/embeddings/config \
  -H "Content-Type: application/json" \
  -d '{"batchSize": 50, "concurrency": 4}'
```
