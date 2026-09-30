# Design — Object-driven agent work

## Context

Memory already has the pieces this builds on:

- **Reaction triggers** on agents: `ReactionConfig{ObjectTypes, Events(created|updated|deleted), ConcurrencyStrategy, IgnoreAgentTriggered, IgnoreSelfTriggered}` (`domain/agents/entity.go`). Graph mutations emit `EntityEvent`s (`domain/graph/service.go` `EmitCreated/Updated/Deleted`) which `TriggerService.onEntityEvent` maps to `HandleEvent` — but agent-originated events are ignored (`triggers.go`, `evt.Actor.ActorType == ActorAgent → return`), and `executor.Execute` runs **inline**.
- **A durable dispatch ledger** `kb.agent_run_jobs` with `queue`, `priority`, `max_attempts`, `attempt_count`, `next_run_at`, `FOR UPDATE SKIP LOCKED` claiming, and `FailJob` backoff (see `add-agent-review-queues`), plus per-queue workers.
- **Graph objects** with a built-in `Status *string` column, `properties jsonb`, `key`, versioning (`canonical_id`), `branch_id`, and `actor_type`/`actor_id`.
- **Branches** (`domain/branches`) and sandboxes for isolated workspaces.
- **Composable failure mechanics**: `FailRun`, `MarkOrphanedRunsAsError`, `MarkStaleRunsAsError`, `RequeueOrphanedQueuedRuns`, `StaleRunReaper`, the cancel-wins guard, and `ask_user` suspend/resume.

The gap is not mechanics but **policy and object semantics**: who owns a work item, how it ends, what happens when it fails, and how a human sees it.

## Goals

1. Any graph object type is a work item; creation wakes listening agents.
2. Exactly one terminator per run; the machine cannot silently "succeed".
3. Claim is atomic — no double work.
4. Review is per agent, with an explicit rework loop.
5. Failures are classified and routed: retry, block, or unassign — never lost.
6. The Kanban board is a projection, not a second source of truth.

## Non-goals

A work-item table; a workflow/DAG engine; a new agent runtime; full compensation (phase 6).

## Data model

### Built-in fields on the work object

| Field | Source |
|---|---|
| `status` | existing built-in `kb.graph_objects.status` — the work state (user-defined values) |
| `assignee` | **new** built-in `kb.graph_objects.assignee TEXT NULL` (+ index) — the lane; null = any listener |
| everything else | user `properties`, `key`, `labels` |

No new work-item table. `kb.agent_runs` gains `subject_object_id UUID NULL` (+ `subject_object_type TEXT`, index) so the board can join runs to their object; plus `failure_class TEXT NULL`.

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
  retryPolicy:
    maxAttempts:        3
    initialInterval:    10s
    backoffCoefficient: 2.0
    maxInterval:        5m
```

Defaults apply when `workConfig` is absent; a definition without it keeps today's behaviour.

### Object-type configuration (schemas registry)

Per object type: the allowed `status` values (validated on write), a `boardEnabled` flag (does it appear on the Kanban board), and `operational` flags (skip embeddings / extraction / default search).

## Dispatch and claim

1. **Create** a work object → `EmitCreated` (`actor=user`) → `onEntityEvent`.
2. `HandleEvent` matches agents whose reaction trigger lists the type and `created`; for each, **enqueue** a job on that agent's queue (this replaces the inline `executor.Execute` path) with trigger metadata `{subject_object_id, subject_object_type, version}` and **idempotency key** `agentId+canonicalId+version` (a duplicate create for the same agent+object+version is a no-op).
3. **Routing**: if the object has an `assignee`, only the listening agent matching it is enqueued; otherwise any listening agent. An assignee that matches no agent leaves the job unenqueued and the object visible as stranded.
4. **Claim** (worker): after claiming the job, perform a **compare-and-set** on the object — `UPDATE … SET status = <inProgress> WHERE canonical_id = ? AND status = <ready>` (new version). If zero rows change, the object was already taken/finished → the job completes as a no-op. `ConcurrencyStrategy: skip` is enforced here.
5. The run records `subject_object_id`; the board can now show the object's execution badge.

`update_object` returns a **new id** (versioning) — all references use `canonical_id`.

## Lifecycle and terminator

The run ends in exactly one of:

- `work_complete(summary, metadata, artifacts)` → object `status` = `done`, or **`review`** when `requiresReview`; registers produced objects, merges the attempt branch, attaches the handoff.
- `work_block(reason, kind)` → object `status` = `blocked`; a `kb.tasks` row + notification for the human (kind: `needs_input` | `capability` | `dependency` | `transient`).
- Run exits without a terminator → **protocol violation**: bounded retry, then `failure_class=agent_quality` and block.

Tools run in the agent's own process and reach the board regardless of the sandbox backend (sandbox/remote terminals); hence tools, not shell commands.

## Isolated attempts and merge

- On claim, open an **attempt context**: an isolated graph **branch** (+ sandbox workspace). Agent-produced objects are written on the branch.
- **Operational metadata stays on main** (status, assignee, failure count) so board state is truthful and never branch-shadowed.
- On success → **merge the branch to main** (after human approval when review is on). On failure/retry → **discard the branch**. On block → **retain** the branch for inspection/rework.
- This yields rollback-by-construction and keeps high-churn operational writes out of the main graph. Rework reuses the retained branch with the human's feedback.

## Review

- `work_complete` with `requiresReview` → `review`.
- Human **approve** → merge branch, `status=done`.
- Human **request changes** → `status=revision`, feedback recorded on the object, and an **explicit rework enqueue** (created-only reactions stay loop-free). A revision cap escalates to a human instead of looping.

## Failure policy

| Class | Trigger | Action |
|---|---|---|
| `transient` | provider 5xx, rate limit, timeout, sandbox flap | auto-retry with backoff; **no** budget tick |
| `agent_quality` | ran but invalid output / no terminator | bounded retry → escalate |
| `terminal` | bad credential, missing model/tool, invalid object | **no retry** → block + notify |
| `needs_input` | `work_block` / `ask_user` | `blocked`, await human |
| `capability` | wrong specialist | **unassign** → `ready` for another lane |

- **Failure budget** per work item (default 3, configurable). Exhaustion → `blocked` (dead-letter) + `kb.tasks`/notification.
- **Circuit breaker** per agent: N consecutive failures → auto-disable its trigger + surface; operator re-enables.
- **Human actions** on a failed card: Retry · Reassign · Cancel.
- **Error compaction**: the compacted failure is injected into the retry/rework context so the agent can adapt (12-factor F9), alongside deterministic retry/backoff safeguards.
- **Idempotent attempts**: produced objects are upserted by `key`, so a retry does not duplicate.

## Kanban projection

Read model over board-enabled object types:

```
kb.graph_objects (type ∈ board-enabled, status, assignee)
   ⟕ kb.agent_runs  ON agent_runs.subject_object_id = graph_objects.canonical_id
```

- **Column** = the object's `status` (work state).
- **Badge** = the latest run's execution status (queued/running/succeeded/failed).
- **Card** = object + its sessions/runs + artifacts + feedback; **unfiled sessions** (no object) are excluded.
- **Drag** = set the object status (transition); entering the ready lane enqueues; entering revision enqueues rework; the machine cannot move a card to `done` — only `work_complete`/human approval can.

## Worked trace — "Research for new Gemini model"

```
1. User creates object: type=ResearchRequest, key="gemini-model-2026",
   properties={topic:"new Gemini model"}, status=ready, assignee=researcher.
2. created event (actor=user) → researcher's reaction trigger matches.
3. Enqueue job on researcher's queue; idempotency = researcher+canonicalId+v1.
4. Worker claims; CAS status ready→in_progress; open branch research/gemini.
5. Researcher agent searches, writes a ResearchReport object on the branch.
6. work_complete(summary, artifacts=[ResearchReport]) — requiresReview=true
   → status=review (branch staged, not merged).
7. Human reviews the card, approves → branch merged to main, status=done.
   (Reject → status=revision + feedback → rework enqueue; researcher re-runs,
    reusing the branch.)
8. Board shows the card in Done with the run badge and the report artifact.

Failure variant: step 5 hits a provider 5xx → transient, retried, no budget
tick. Three agent_quality failures → status=blocked, task + notification;
human either Retries, Reassigns (clear assignee → ready), or Cancels.
```

## Alternatives considered

- **Dedicated `kb.work_items` table.** Rejected: duplicates graph state, needs sync, breaks the "card is the object" mental model.
- **Status as a user-defined property instead of the built-in column.** Rejected: `Status` already exists and is indexed/versioned; reusing it avoids property-name drift.
- **Assignee as a property.** Rejected: cross-cutting and needed for routing; a built-in indexed column is uniform and fast.
- **Inline reaction execution.** Rejected: no bounded concurrency, retry, reclaim, or visibility.
- **Assigning by graph CAS instead of the queue.** The queue gives concurrency, backoff, and reclaim for free; CAS is layered on top for the object.
- **Retry the whole item vs the failed attempt.** Retry the attempt; a branch per attempt makes retries safe.

## Compatibility and migration

- `assignee` and `subject_object_id` are additive; existing objects, triggers, and runs are unaffected.
- Without `workConfig`, an agent behaves exactly as today (inline reaction, no claim, no terminator).
- The inline→enqueue change is gated on `workConfig` (or a `dispatchMode`) so it is opt-in.
- Migration numbering follows `add-agent-review-queues` (that change adds `00204`); this change's migrations are `00205+`.

## Risks

- **Branch-per-attempt coupling**: verify `domain/branches` merge semantics and cost before building the merge path (P1 spike).
- **Version churn**: status/assignee writes create versions; operational objects should be excluded from embeddings/extraction/search.
- **Wildcard listeners** can fan out wide; admission control is required.
- **Dead config**: `IgnoreAgentTriggered`/`IgnoreSelfTriggered`/`ConcurrencyStrategy` are declared but unenforced today; P1 wires `ConcurrencyStrategy` and the rest is tracked.
