## Why

`StaleJobCleanupTask` (`apps/server/domain/scheduler/tasks.go`) sweeps non-terminal jobs across `kb.document_parsing_jobs`, `kb.chunk_embedding_jobs`, `kb.graph_embedding_jobs`, `kb.object_extraction_jobs` and `kb.email_jobs`. The old predicate terminal-failed **pending jobs that never started** once their `created_at` aged past the stale window:

```sql
WHERE status IN ('pending', 'processing', 'running')
AND (
    (started_at IS NOT NULL AND started_at < ?)
    OR (started_at IS NULL AND created_at < ?)   -- never attempted, still failed
)
```

That makes no distinction between a *stuck* job and a job merely *queued behind a large backlog*. On dev, a single sweep marked **97,129 healthy `kb.graph_embedding_jobs` rows** `failed` with `last_error = 'Job marked as stale during cleanup'`, while 84,933 other pending rows drained to zero on their own and completed successfully — proof that the reaped rows were healthy and queued, not stale (issue #705).

Partial fix already landed on `main` (commit `6ed32ba4f`) for the four tables that have `started_at`: the `started_at IS NULL AND created_at < ?` disjunct was removed. Two gaps remain:

1. `kb.email_jobs` has **no `started_at`**, so it still sweeps `status IN ('pending', 'processing', 'running') AND created_at < ?` and still terminal-fails never-started pending email jobs. This change gives `kb.email_jobs` a `started_at` column (migration 00173) stamped at dequeue, so it uses the same stale-start rule as the other four tables and no longer terminal-fails pending OR active jobs.
2. A mass reap is **silent**: there is no metric or log distinguishing a routine handful of genuinely-stuck in-flight jobs from a 97k-row mis-classification.

## What Changes

- **Never terminal-fail never-started jobs, generically.** The sweep only reaps jobs that actually started: `processing`/`running` with a stale `started_at`. `kb.email_jobs` now gets a `started_at` column stamped at dequeue (migration 00173), so it uses the same stale-start rule as the other four tables; `created_at` remains only a defensive fallback for a swept table that has no `started_at` column (none currently). `pending` rows are never candidates for the failing sweep, for every swept table.
- **Keep current reaping semantics for `processing`/`running` rows** with a stale `started_at` — those genuinely need reaping when a worker died mid-job.
- **Make mass reaps visible.** Add `StaleJobMassReapThreshold` (env `STALE_JOB_MASS_REAP_THRESHOLD`, default 1000). When a single sweep terminal-fails more than the threshold in one table, emit an `ERROR` log carrying the stable `alert=mass_stale_reap` marker, plus a counter `scheduler_stale_jobs_reaped_total{table}`.
- **Extract a pure query builder** (`cleanupStaleJobsQuery`) so predicate selection is unit-testable without a live database.

Chosen option: **exclude** never-started pending jobs rather than re-queue them. Re-queueing (pushing `scheduled_at`/`next_retry_at` with backoff) adds queue churn and per-table column variance for no correctness benefit; exclusion already guarantees a never-started job is never dropped, and the mass-reap alert surfaces a queue that is not draining. Flagged in the PR body.

## Capabilities

### New Capabilities

- `scheduler-stale-job-cleanup`: semantics of the periodic stale-job sweep — which jobs may be terminal-failed, and the visibility guarantees when it reaps an unexpectedly large batch.

### Modified Capabilities

- None.

## Impact

### Code Changes

- `apps/server/domain/scheduler/tasks.go`: extract `cleanupStaleJobsQuery`; only `processing`/`running` rows are reaped (all tables, including `kb.email_jobs`); add `massReapThreshold` + `SetMassReapThreshold`/`GetMassReapThreshold`; emit mass-reap alert log and metric.
- `apps/server/domain/scheduler/metrics.go` (new): `scheduler_stale_jobs_reaped_total` counter, labelled by table.
- `apps/server/domain/scheduler/config.go`: new `StaleJobMassReapThreshold` field / `STALE_JOB_MASS_REAP_THRESHOLD` env var.
- `apps/server/domain/scheduler/module.go`: wire the configured threshold into the task.
- `apps/server/domain/scheduler/stale_job_cleanup_test.go`: DB-free predicate and mass-reap tests.
- `apps/server/migrations/00173_add_email_jobs_started_at.sql`: add `started_at` to `kb.email_jobs`.
- `apps/server/domain/email/jobs.go`: stamp `started_at = now()` on dequeue.
- `apps/server/domain/DOMAIN_GUIDE.md`: document the corrected sweep behavior.

### Configuration

- New env var: `STALE_JOB_MASS_REAP_THRESHOLD` (default: `1000`).

### Operational Impact

- Never-started `pending` jobs in all five swept tables are no longer terminal-failed, so a slow queue no longer loses work.
- Genuinely stuck `processing`/`running` jobs are still reaped.
- A sweep reaping more than the threshold in a table logs `alert=mass_stale_reap` and increments `scheduler_stale_jobs_reaped_total`, making a mass-fail visible.

### Testing Requirements

- Unit tests for predicate selection (`cleanupStaleJobsQuery`) for tables with and without `started_at`, asserting `pending` is never targeted.
- Unit test that a sweep above the threshold emits the `mass_stale_reap` alert and one below it does not.
- Unit tests for threshold get/set clamping and per-table stale-minute override resolution.
