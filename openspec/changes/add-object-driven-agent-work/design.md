# Design — Object-driven agent work

## Context

Memory already has the pieces this builds on:

- **Reaction triggers** on agents: `ReactionConfig{ObjectTypes, Events(created|updated|deleted), ConcurrencyStrategy, IgnoreAgentTriggered, IgnoreSelfTriggered}`. Graph mutations emit `EntityEvent`s (`graph/service.go`); `TriggerService.onEntityEvent` maps them to `HandleEvent`, which **unconditionally drops agent-originated events** (`triggers.go:58-62`) and calls `executor.Execute` **inline** (`triggers.go:365,418`).
- **A durable dispatch ledger** `kb.agent_run_jobs` (status, `attempt_count`/`max_attempts`, `next_run_at`, `FOR UPDATE SKIP LOCKED`) and per-queue workers, added by `add-agent-review-queues` (#1304).
- **Graph objects** `kb.graph_objects`: **versioned** — a write never mutates in place; `CreateVersion` (`repository.go:835`) sets the old HEAD's `supersedes_id` and inserts a new row with a **new `id`**, linked by `canonical_id`. Built-in fields include **`Status *string`**, `Key *string` (**nullable**), `BranchID`, `NeedsReview *bool`, `ReviewedBy *uuid`, `ReviewedAt *time.Time`. The `status` column is **synced from `properties["status"]`** on the write path (`service.go:701-705, 1046-1054`), so there are two candidate sources of truth.
- **Per-object advisory locks**: `AcquireObjectLock` (`repository.go:1130`) and `AcquireObjectUpsertLock` (`repository.go:1178`); `FindHeadByTypeAndKey` (`repository.go:1141`). `AcquireObjectUpsertLock` is `pg_advisory_xact_lock` (transaction-scoped), so it composes atomically with `FindHeadByTypeAndKey` + `CreateVersion` inside one `bun.Tx` (verified).
- **`kb.agent_processing_log`** (`agents/entity.go`): `agent_id, graph_object_id, object_version, event_type, status, started_at, completed_at, error_message, result_summary` — an existing dedup ledger (no `run_id`).
- **Failure mechanics**: `FailRun`, `MarkOrphanedRunsAsError`, `MarkStaleRunsAsError` (keyed on `last_step_at`), `RequeueOrphanedQueuedRuns`, `StaleRunReaper` (run-keyed), job backoff, cancel-wins guard, and an **existing agent circuit breaker** (`worker_pool.go:366-401` — `Agent.ConsecutiveFailures` + auto-disable).
- **Human-in-the-loop primitives**: `ask_user` (suspend/resume) and `kb.tasks` (inbox).

> **Dormant-column warning:** `needs_review` / `reviewed_by` / `reviewed_at` are **declared and read but never written** anywhere in the tree today. This change treats them as the review substrate but must **build their write path** (tasks 3.1/3.2); "reuse existing" means reuse the columns, not an existing feature.

The gap is not mechanics but **policy and object semantics**: who owns a work item, how it ends, what happens when it fails, and how a human sees it.

## Goals

1. Any graph object type is a work item; creation wakes listening agents.
2. Exactly one terminator per run; the machine cannot silently "succeed".
3. The claim is correct against the versioned write model and crash-safe.
4. Review is per agent, on the main graph, with an explicit rework loop.
5. Failures are classified and routed: retry / block / unassign — never lost.
6. The Kanban board is a projection, not a second source of truth.

## Non-goals

A work-item table; a workflow/DAG engine; a new agent runtime; **per-attempt branch isolation** (deferred — see Alternatives); contract validation.

## Data model

### Built-in fields on the work object

| Field | Source |
|---|---|
| `status` | existing built-in `kb.graph_objects.status` — **authoritative work state for board-enabled types** |
| `assignee` | **new** built-in `kb.graph_objects.assignee TEXT NULL` (+ index) — the lane; meaningful only for board-enabled types; null = any listener |
| `needs_review` / `reviewed_by` / `reviewed_at` | **existing columns, currently dormant** (no writer) — this change adds the write path |
| `key` | existing; **required (non-null) for board-enabled types** so the versioned claim has a stable identity |
| everything else | user `properties`, `labels` |

No work-item table.

### Single status writer (resolves dual-source status)

For board-enabled types the `status` **column** is authoritative. Agents may **not** set work status through direct graph writes; the platform rejects **both** the `status` field **and** `properties["status"]` for board-enabled objects, at **every write entry point** — `Create`, `CreateOrUpdate`, `Patch`, `BulkUpdateStatus`, and bulk/merge actions. Reject (do not silently ignore) so the two copies can never diverge. All transitions go through one server-side path (invoked by `work_complete`/`work_block` and by human actions) that (a) takes the per-object advisory lock, (b) creates the next version, and (c) writes `status` and `properties["status"]` consistently (the `ContentHash` is computed from `properties+status+key+labels`, `repository.go:842`, so both must be set together).

### Agent configuration (`workConfig` on the agent definition)

```yaml
workConfig:
  status:
    ready:      "ready"
    inProgress: "in_progress"
    review:     "review"
    revision:   "revision"
    blocked:    "blocked"
    done:       "done"
  requiresReview: false
  failureLimit:   3
  retryPolicy: { maxAttempts: 3, initialInterval: 10s, backoffCoefficient: 2.0, maxInterval: 5m }
```

Defaults apply when `workConfig` is absent; a definition without it keeps today's behaviour.

### Object-type configuration (schemas registry — new surface)

Per object type (net-new on `ObjectTypeSchema`): `boardEnabled`, the allowed `status` values (validated on write), and operational flags (`skipEmbeddings`, `skipExtraction`, `excludeFromSearch`). These flags are **required** for board-enabled types, because every status transition creates a new version and would otherwise enqueue embeddings/FTS/extraction.

### Run linkage and dedup

- **Dedup**: reuse `kb.agent_processing_log` — `(agent_id, graph_object_id, object_version, event_type)` is exactly the dispatch-dedup key. `graph_object_id` MUST be the object's **`canonical_id`** (standardize this; the existing batch-trigger path may currently write the physical `id`, which would break dedup across versions).
- **Board join**: add `kb.agent_runs.subject_object_id` (+ `subject_object_type`) storing the **`canonical_id`** — the processing log has no `run_id`, so the run→object link is needed for the projection. Because the object's physical `id` changes on every version, the join keys on `canonical_id`, never `id`.

## Dispatch and claim

1. **Create** a work object (`actor=user`) → `EmitCreated` → `onEntityEvent`.
2. Match agents whose reaction trigger lists the type + `created`; for each, **dedup against `kb.agent_processing_log`**, then **enqueue** a job on that agent's queue (replacing the inline `executor.Execute`) with `subject_object_id`/`subject_object_type`. `version` is taken from the HEAD at dispatch (never from the batch payload, which omits it — `triggers.go:99-108`).
3. **Routing**: if `assignee` is set, only the listening agent matching it is enqueued (within the object's project — the project check is re-applied, never assumed). **Unroutable is derived, not persisted**: a board-enabled object whose `assignee` matches no listener, or whose type has no listener, is surfaced by the board/reconciler as *unroutable* (a query predicate), not given a new status value.
4. **Claim** (worker): after claiming the job, take `AcquireObjectUpsertLock(project, type, key)` and, in the same transaction, read HEAD (`FindHeadByTypeAndKey`) and, if `status == ready`, `CreateVersion(in_progress)`; otherwise **the run is `skipped`** (job completed, no failure-budget burn, no agent execution). `CreateVersion` does not itself assert `ready` — the caller owns that check, and it must be a defensive assert. The non-null `key` requirement guarantees `FindHeadByTypeAndKey` cannot match two objects.
5. The run records `subject_object_id`; the board shows the object's execution badge.

## Lifecycle and terminator

Terminators are **run-finalizing**: the platform ends the run when one is called; any later steps are ignored.

- `work_complete(summary, artifacts)` → `status` = `done`, or `review` + `needs_review=true` when `requiresReview`.
- `work_block(reason, kind)` → `status` = `blocked`; a `kb.tasks` row + notification for the human.

### Run-end → item-transition mapping (exhaustive)

| Run end state | Item transition | Budget |
|---|---|---|
| `success` with `work_complete` | → `done`, or → `review` if `requiresReview` | no change |
| `success` without a terminator | protocol violation: → retry while budget remains, else → `blocked` | +1 |
| `error` retryable (transient) | → back to `ready`, re-enqueue with backoff | +1 |
| `error` deterministic (poison) | → `blocked` (no retry) | +1 |
| `error` terminal/config | → `blocked`, no retry | 0 |
| quota / rate-limit error | agent-level only; item unchanged | 0 |
| `paused` (`ask_user`) | item stays `in-progress`, no wait time counted | 0 |
| `cancelled` (durable cancel) | → `ready` (or `blocked` if human-cancelled) | 0 |
| `timeout` (max runtime) | → retry once with longer backoff, else → `blocked` | +1 |

> **Net-new plumbing:** the "quota / rate-limit" row requires provider→executor **structured error typing** that does not exist today (no `ErrRateLimit`/`429` classification in `worker_pool.go`/`provider`). That classification is in scope for P2.

## Claim liveness: lease, reaper, reconciliation

Liveness is derived from the run (no new lease column). Predicates are explicit:

- **A run is "live"** if its status is `queued`, `running`, or **`paused`**. `paused` (`ask_user`) MUST be treated as live — otherwise a human waiting on a question lets the item be reclaimed and run twice.
- **A job is "live"** if its status is `pending` or `processing`.
- **Work-status reaper** (analogous to `StaleRunReaper`, over graph objects): an `in-progress` object with no **live** run past the threshold → `ready` (budget-aware) or `blocked`. It MUST coordinate with `MarkStaleRunsAsError`'s `last_step_at` threshold so it never races the run reaper, and MUST exclude `paused` runs.
- **Reconciler**: a `ready` board-enabled object with no **live** run and no **live** job → enqueue. It MUST treat `pending`+`processing` jobs as live and tolerate the window between a job completing as `skipped` and the item transition committing, so it cannot double-enqueue.

## Review (on main — no branches)

- `work_complete` with `requiresReview` → `status=review`, `needs_review=true` (produced objects are already on main).
- **Approve** → `reviewed_by`/`reviewed_at` set, `needs_review=false`, `status=done`.
- **Request changes** → `status=revision`, append-only `feedback[] {round, author, text, runId}` recorded on the item (reject requires non-empty feedback), and an **explicit rework enqueue** whose payload carries **all** prior feedback.
- **Revision cap** → escalate via `kb.tasks` (`type=work-escalation`) with the full feedback history.
- Review is synchronous on main: the item cannot be `done` while its produced objects are absent.

> **Accepted trade-off:** because there is no branch, agent-produced objects are written to main and are **searchable/embeddable before approval** — `needs_review` does not gate search or embeddings. This is the deliberate cost of cutting branch isolation (see Alternatives); deferring produced-object visibility would reintroduce isolation.

## Failure policy

| Class | Examples | Action | Item budget |
|---|---|---|---|
| Retryable | provider 5xx, transient sandbox | retry with backoff | +1 |
| Deterministic / poison | invalid tool args, `max_steps` exhaustion | no retry → `blocked` | +1 |
| Human | `work_block`, `ask_user` | `blocked`, await human | 0 |
| Quota / rate-limit | 429, provider quota | agent-level breaker only | 0 |
| Capability | wrong specialist | unassign (`assignee` cleared) → `ready`, with requeue backoff | 0 |

- **Per-item budget** (default 3) → `blocked` dead-letter + `kb.tasks`/notification. Human actions: Retry · Reassign · Cancel.
- **Unassign thrash guard**: capability re-queue applies a backoff and a reclaim cap; attempt history survives unassign.
- **Agent circuit breaker**: extend the existing `ConsecutiveFailures` + auto-disable with explicit thresholds; distinguish a **poison item** (block the item) from a **broken agent** (disable the agent). Breaker scope (global-per-agent vs per-tenant) is a configuration decision.
- **At-least-once**: the platform guarantees at-least-once execution. Side effects outside the graph are the agent's idempotency responsibility; produced objects are upserted by `key`.

## Kanban projection

```
kb.graph_objects (boardEnabled type, status, assignee)
   ⟕ kb.agent_runs  ON agent_runs.subject_object_id = graph_objects.canonical_id
```

- **Column** = object `status`; **badge** = latest run execution status.
- **Card** = object + its sessions/runs + artifacts + feedback; unfiled sessions (no object) are excluded.
- **Unroutable** cards are a derived flag (board-enabled + no matching listener) rendered as an alert, not a column.
- **Drag** = set the object status via the single-work-status path; entering `ready` enqueues, entering `revision` enqueues rework. The machine cannot move a card to `done` — only `work_complete` + approval can.

## Worked trace — "Research for new Gemini model"

```
1. User creates object: type=ResearchRequest (boardEnabled), key="gemini-model-2026",
   properties={topic:"new Gemini model"}, status=ready, assignee=researcher.
2. created event (actor=user) → researcher's reaction trigger matches.
3. Dedup against kb.agent_processing_log (researcher+canonicalId+v1+created) → new → enqueue job.
4. Worker claims; AcquireObjectUpsertLock → HEAD.status==ready (assert) → CreateVersion(in_progress); run records subject_object_id=canonicalId.
5. Researcher agent searches and writes a ResearchReport object on main.
6. work_complete(summary, artifacts=[ResearchReport]); requiresReview=true
   → status=review, needs_review=true. Run is finalized.
7. Human approves → reviewed_by/at set, status=done. (Reject → status=revision +
   feedback[] + rework enqueue; researcher re-runs with all prior feedback.)
8. Board shows the card in Done with the run badge and the report artifact.

Failure variant: step 5 hits a provider 5xx → retryable, backoff, budget +1.
Three deterministic failures → status=blocked, kb.tasks escalation; human
Retries, Reassigns (assignee cleared → ready with backoff), or Cancels.
Crash after claim → the status reaper returns the item to ready (no live run).
Paused at step 5 (ask_user) → item stays in-progress; the reaper does not touch it.
```

## Alternatives considered

- **Per-attempt branch isolation + merge-on-success (rejected for v1).** Isolation is prompt-advice only (`branch_id` is an optional tool arg; agents can write main), `ForkBranch` **eagerly clones the whole graph** per attempt (O(graph)), the fork copy is non-transactional, `MergeBranch` **can partially apply and leave conflicts** (not atomic), merge enumeration caps at 2000, and there is no branch GC. Review on main via the (currently dormant) `needs_review`/`reviewed_by`/`reviewed_at` columns is far cheaper. Branch isolation is deferred to its own change.
- **Dedicated `kb.work_items` table.** Rejected: duplicates graph state and breaks "card is the object".
- **New idempotency key + linkage.** Rejected: `kb.agent_processing_log` already keys agent+object+version+event.
- **A second per-agent circuit breaker.** Rejected: one already exists; extend it.
- **Inline reaction execution.** Rejected: no bounded concurrency, retry, reclaim, or visibility.

## Compatibility and migration

- `assignee` and `subject_object_id` are additive; existing objects, triggers, and runs are unaffected.
- Without `workConfig`, an agent behaves exactly as today.
- Single-status-writer enforcement applies only to board-enabled types.
- Migrations `00205+`, **after** `add-agent-review-queues` (#1304 / `00204`).
- New routes require `route-authority.yaml` entries; any new bun model requires registration in `internal/schemadrift/models.go`; new services/tools require fx `module.go` wiring.

## Risks

- **Version churn** on status/assignee writes — per-type operational skip-flags (R11) are mandatory for board-enabled types.
- **Event-bus lossiness** — mitigated by the reconciler.
- **Reaper vs paused/slow runs** — the liveness predicates above are mandatory or the crash-safety guarantee is false.
- **Breaker blast radius** (global-per-agent) — poison-item detection blocks the item instead.
- **`key` discipline** — board-enabled types must require a non-null key.
- **Dead config** (#1306): `IgnoreAgentTriggered`/`IgnoreSelfTriggered`/`ConcurrencyStrategy` are unenforced; this change keeps the conservative default and does not rely on relaxing it.
