# Design — Queue-backed review agents

## Context

`kb.agent_run_jobs` is already a durable, concurrency-safe dispatch ledger:

- one row per queued run (`run_id` FK → `kb.agent_runs`),
- statuses `pending | processing | completed | failed`, `attempt_count`, `max_attempts`, `next_run_at` for backoff,
- claimed by `WorkerPool` (`domain/agents/worker_pool.go`) with `SELECT … FOR UPDATE SKIP LOCKED`, transitioning job→processing and run→running in one transaction (`repository.go ClaimNextJob`),
- jobs are created by `CreateRunQueued` / `CreateRunJob`; safeguards (`fix-agent-queue-explosion`) cap depth per agent.

What it lacks is *organisation*: there is one implicit queue, routing is implicit in `run.agent_id`, priority does not exist, and concurrency is a single process-wide `AGENT_WORKER_POOL_SIZE`. There is no API to observe or configure the queue.

## Goals

1. Named queues with concurrency, priority, and enable/disable — without replacing the ledger.
2. Routing: an agent's work lands on its bound queue; claiming is queue-scoped and priority-ordered.
3. Per-queue worker pools so a slow review queue cannot starve interactive work.
4. A management/observability API (list queues + live depth).
5. First-class **review agents** that consume a queue, inspect a subject, and emit findings via configurable output actions. Pilot: security code review.

## Non-goals

- Replacing `kb.agent_run_jobs`.
- A generic workflow/DAG engine.
- Multi-tenant cross-project queues (queues are project-scoped).

## Data model

### `kb.agent_queues` (new)

| Column | Type | Notes |
|---|---|---|
| `name` | TEXT PK | queue identifier, project-unique (composite PK with project) |
| `project_id` | UUID | owning project |
| `display_name` | TEXT | label |
| `description` | TEXT | |
| `concurrency` | INT default 1 | max simultaneous `processing` jobs from this queue |
| `priority` | INT default 100 | informational ordering of the queue itself |
| `enabled` | BOOL default true | disabled queues are not polled |
| `created_at` / `updated_at` | TIMESTAMPTZ | |

PK = `(project_id, name)`. A `default` queue is seeded per project on first use (lazily) and by the migration for existing projects.

### `kb.agent_run_jobs` (modified)

Add:

- `queue TEXT NOT NULL DEFAULT 'default'` — the queue this job is claimed from.
- `priority INT NOT NULL DEFAULT 100` — lower = claimed earlier.
- Index `(queue, status, priority, next_run_at) WHERE status='pending'` for the claim query (replaces the single-queue poll index for the new path; the old index is retained or superseded).

### `kb.agent_definitions` (modified)

Add `default_queue TEXT NOT NULL DEFAULT 'default'`. A runtime `Agent` may override via `Config["queue"]`.

### `kb.review_findings` (new, phase 2)

| Column | Notes |
|---|---|
| `id` UUID PK | |
| `project_id` UUID | |
| `run_id` UUID → `kb.agent_runs` | producing run |
| `queue` TEXT | queue the run was claimed from |
| `subject_type` TEXT | `pull_request` \| `commit` \| `document` \| `graph_object` \| `manual` |
| `subject_ref` TEXT | PR number, SHA, object key, … |
| `severity` TEXT | `info` \| `low` \| `medium` \| `high` \| `critical` |
| `category` TEXT | e.g. `injection`, `authz`, `secret`, `ux` |
| `title` TEXT | |
| `detail` TEXT | |
| `status` TEXT | `open` \| `acknowledged` \| `resolved` \| `wont-fix` |
| `created_at` / `updated_at` | |

## Routing resolution

A run's queue and priority are resolved at enqueue time:

```
run.queue    = agent.Config["queue"]            (runtime override, if set)
             → definition.DefaultQueue          (binding)
             → "default"

run.priority = agent.Config["priority"]         (optional int)
             → 100
```

`CreateRunQueued` / `CreateRunJob` persist `queue` + `priority` on the job row so the claim query needs no joins.

## Claim semantics

New method `ClaimNextJobInQueue(ctx, queue string)`:

```sql
SELECT * FROM kb.agent_run_jobs
WHERE status='pending' AND next_run_at <= now()
  AND ($1 = '' OR queue = $1)
ORDER BY priority ASC, next_run_at ASC
LIMIT 1
FOR UPDATE SKIP LOCKED
```

- Empty `queue` preserves today's behaviour (claim from any queue) — the legacy `ClaimNextJob` delegates with `""`.
- The existing run-status guard (cancel wins) is preserved unchanged.

Concurrency safety stays with `SKIP LOCKED`; per-queue limits are enforced by how many workers poll a given queue (see below), not by the claim query.

## Worker pool → supervisor

`WorkerPool` becomes a supervisor:

- On `Start`, and every `refreshInterval`, load enabled queues (with `concurrency`).
- Maintain a worker set per queue: `concurrency` goroutines each calling `ClaimNextJobInQueue(queue)`.
- `default` queue concurrency defaults to `AGENT_WORKER_POOL_SIZE` when configured (>0) to preserve current sizing; other queues use their row's `concurrency`.
- Queues created/edited at runtime are picked up on the next refresh; removed/disabled queues have their workers drained.
- `size=0` still disables the pool entirely (unchanged).

This makes "agents working in a queue" concrete: each queue has its own workers and its own bounded parallelism.

## Review agents & producers

- **Definition**: a review agent binds to a queue (`defaultQueue`) and carries a system prompt that defines the review contract (subject, severity scale, finding shape, rubric).
- **Subject**: carried in run `trigger_metadata` (`subjectType`, `subjectRef`, plus provider-specific fields such as `prNumber`, `headSha`).
- **Producers**:
  1. *Manual / CLI / API* — `POST /api/projects/:projectId/agent-queues/:name/enqueue` creates a queued run bound to the queue's agent (or an explicit `agentId`), with `triggerMetadata`.
  2. *Graph reaction* — existing `reaction` trigger: a graph object event enqueues a run; queue resolved from the agent binding.
  3. *Cron* — existing `schedule` trigger with queue binding.
  4. *GitHub (phase 2)* — a GitHub webhook (`pull_request.opened/synchronize`) enqueues a security-review run; a follow-up action posts the review back. Consumes `core.github_app_config`; no new App provisioning.

## Findings & output actions (phase 2)

- Review agents record findings either as graph objects (phase 1, using existing graph-write tools) or as `kb.review_findings` rows via a new write tool (phase 2).
- After a run completes, an **action dispatcher** reads the run's findings and executes the definition's configured `outputActions` in order:
  - `create_task` — insert a `kb.tasks` row for human triage.
  - `notify` — emit a notification/event.
  - `github_comment` — post an inline review comment (phase 2, GitHub App token).
  - `graph_write` — write finding objects into the graph.
- Actions are best-effort and independently error-isolated; a failed action does not fail the run.

## Alternatives considered

- **Separate queue table** (not extending `agent_run_jobs`). Rejected: duplicates the claim/retry/backoff machinery and splits observability.
- **Priority only via `next_run_at`.** Rejected: conflates scheduling delay with priority and breaks backoff semantics.
- **Global pool with an in-memory queue filter.** Rejected: no per-queue concurrency enforcement, and behaviour diverges across instances.
- **Findings in the graph by default.** Rejected for phase 1: review findings are high-volume operational data; Postgres keeps them queryable and cheap, with graph mirroring as an opt-in action.

## Migration & compatibility

- New `queue`/`priority` columns default to `default`/`100`; all existing rows and producers keep working.
- `ClaimNextJob` (any-queue) is retained; existing tests keep passing.
- `AGENT_WORKER_POOL_SIZE` continues to size the `default` queue.
- Rollback: drop the new columns/tables and restore the single-pool loop.
