# Background Scheduler

The scheduler runs maintenance tasks on a fixed cron schedule. It has no HTTP endpoints — it is an internal subsystem that runs automatically when the server starts.

## Scheduled tasks

| Task | Schedule | Description |
|---|---|---|
| `RevisionCountRefreshTask` | Every 15 minutes | Refreshes cached revision counts on graph nodes to avoid expensive `COUNT` queries at read time |
| `TagCleanupTask` | Every 30 minutes | Removes orphaned tags (tags with no associated objects) |
| `CacheCleanupTask` | Every hour | Evicts stale entries from in-memory and DB-backed caches |
| `StaleJobCleanupTask` | Every 10 minutes | Marks jobs that have been stuck in `processing` state beyond their timeout as `failed`, recovering from worker crashes |

All tasks run with a **30-minute hard timeout**. If a task exceeds this limit it is cancelled and the error is logged; the next scheduled run will attempt it again automatically.

---

## Cron expression format

The scheduler uses `robfig/cron` with **seconds precision** (6-field expressions):

```
┌─────── second (0–59)
│ ┌───── minute (0–59)
│ │ ┌─── hour (0–23)
│ │ │ ┌─ day of month (1–31)
│ │ │ │ ┌ month (1–12)
│ │ │ │ │ ┌ day of week (0–6, Sunday=0)
│ │ │ │ │ │
* * * * * *
```

Examples:

| Expression | Runs |
|---|---|
| `0 */15 * * * *` | Every 15 minutes |
| `0 */30 * * * *` | Every 30 minutes |
| `0 0 * * * *` | Every hour, on the hour |
| `0 */10 * * * *` | Every 10 minutes |

---

## Stale job detection

The `StaleJobCleanupTask` is particularly important for reliability. If a worker process crashes while a job is in `processing` state, the job would otherwise remain stuck indefinitely. This task detects jobs that have exceeded their expected processing time and marks them as `failed`, making them eligible for retry.

The timeout threshold per job type:

| Job type | Timeout threshold |
|---|---|
| Document parsing | 30 minutes |
| Object extraction | 60 minutes |
| Chunk embedding | 15 minutes |
| Graph embedding | 15 minutes |
| Datasource sync | 60 minutes |

---

## Observability

Scheduler task runs are logged at the `INFO` level:

```
INFO  scheduler: task started name=RevisionCountRefreshTask
INFO  scheduler: task completed name=RevisionCountRefreshTask duration=1.2s
WARN  scheduler: task exceeded timeout name=TagCleanupTask duration=30m0s
ERROR scheduler: task failed name=StaleJobCleanupTask error="context canceled"
```

The scheduler itself starts and stops gracefully with the server (via `fx` lifecycle hooks). On shutdown, the scheduler waits for any in-progress task to complete or be cancelled before the process exits.
