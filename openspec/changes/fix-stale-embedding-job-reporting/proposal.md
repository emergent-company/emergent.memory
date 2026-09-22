## Why

The stale-job sweep (`domain/scheduler.StaleJobCleanupTask`) terminal-fails jobs
by stamping `status = 'failed'` plus a fixed error text. On 2026-09-20 that sweep
mis-classified healthy queued work and mass-failed 97,129 jobs across the job
tables (issue #705). The sweep itself was fixed, but two problems remain:

1. **Reporting is permanently wrong.** Every status aggregate counts a row as
   `failed` purely on `status = 'failed'`, so stale-sweep reaps are presented as
   current failures forever. The embeddings page for a fully-embedded project
   (107,233/107,233 objects) shows ~96,898 failed jobs and looks broken, even
   though nothing is failing and nothing is missing.
2. **Retention does not reclaim all of them.** The embedding job purge task only
   covered three tables and its age predicate assumed an `updated_at` column that
   `kb.email_jobs` does not have, so terminal rows in three of the five swept
   tables were never reclaimed.

## What Changes

- Introduce a single canonical stale-sweep marker constant (`internal/jobs.StaleJobMessage`)
  used by both the sweep and the reporting queries.
- Split per-queue job statistics into **genuine** failures (`failed`) and
  **stale-sweep terminal** rows (`staleFailed`/`stale_failed`) across every
  swept job table, so historical cleanup is never presented as current breakage.
  `failed` is narrowed to genuine failures; the new counter is additive, so the
  change is backwards compatible.
- Apply the split consistently to `GET /api/embeddings/progress`,
  `GET /api/projects/:id/embeddings/progress`, and `GET /api/metrics/jobs`, and to
  the web-ui embeddings page that consumes the progress response.
- Widen the terminal-job purge task to cover all five swept tables
  (`kb.document_parsing_jobs`, `kb.chunk_embedding_jobs`,
  `kb.graph_embedding_jobs`, `kb.object_extraction_jobs`, `kb.email_jobs` plus
  `kb.graph_relationship_embedding_jobs`), with correct per-table terminal
  statuses and age column (`kb.email_jobs` uses `COALESCE(processed_at, created_at)`
  and treats `sent` as terminal).
- Document (but do not execute) the one-off SQL that reclaims the existing
  ~97k rows, with size measurement and bloat-reclaim guidance.

## Capabilities

### New Capabilities

- `job-ledger-reporting`: Server-side job-queue reporting that separates
  stale-sweep terminal failures from genuine failures, and retention that
  reclaims terminal rows across every job table the sweep touches.

### Modified Capabilities

- `embedding-status`: The queue-progress section additionally shows a stale-failed
  count, and the failed count means genuine failures only.

## Impact

### Code Changes

- `apps/server/internal/jobs/stale.go` (new): canonical `StaleJobMessage`.
- `apps/server/domain/scheduler/tasks.go`: sweep marker sourced from the shared constant.
- `apps/server/domain/scheduler/embedding_job_purge_task.go`: per-table purge config covering all six tables.
- `apps/server/domain/extraction/graph_embedding_jobs.go`,
  `graph_relationship_embedding_jobs.go`, `chunk_embedding_jobs.go`,
  `embedding_control_handler.go`: stale/genuine failure split.
- `apps/server/domain/health/metrics_handler.go`: `stale_failed` counter across all five tables.
- `apps/server/domain/email/jobs.go`,
  `apps/server/domain/extraction/document_parsing_jobs.go`,
  `apps/server/domain/extraction/object_extraction_jobs.go`,
  `apps/server/domain/superadmin/{repository,dto}.go`: apply the same
  failed/stale split to the remaining server aggregates. `withErrors`-style
  ledger counts are intentionally left counting rows that carry error text
  (including stale rows) and are documented in code.
- `apps/cli/internal/cmd/embeddings.go`, `apps/cli/internal/cmd/auth.go`: surface
  stale-failed in `memory embeddings progress` (counted in the total so the
  percentage reconciles) and in `memory auth status`.
- `apps/server/pkg/sdk/health/client.go`, `apps/server/pkg/sdk/superadmin/client.go`:
  add the additive stale-failure field to the SDK response types.
- `apps/server/docs/swagger/{docs.go,swagger.json,swagger.yaml}`: regenerated.
- `apps/web-ui/gateway/embeddings.go`, `embeddings.templ`, `embeddings_test.go`: consume and render `staleFailed`.

### API Changes

- `GET /api/embeddings/progress`: each queue gains `staleFailed`; `failed` now excludes stale-sweep rows. Additive and backwards compatible.
- `GET /api/projects/:id/embeddings/progress`: each queue gains `staleFailed`; `failed` narrowed likewise.
- `GET /api/metrics/jobs`: each queue gains `stale_failed`; `failed` narrowed likewise.

### Database Changes

- None. No migration: retention widening is code-only and the one-off reclaim is documented, not shipped as a migration.

### Out of scope

- Backfill/throughput work (issue #743).
