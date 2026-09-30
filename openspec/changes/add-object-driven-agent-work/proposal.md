## Why

Memory can already run agents on a schedule, on manual trigger, on graph-object reactions, and on webhooks, with a durable dispatch ledger (`kb.agent_run_jobs`) and per-queue workers. What it lacks is a **general, object-driven work model**: the ability to create *any* object in the graph, have an agent "listening" to that object type pick it up, decide when it is done, and — where configured — leave room for human review. Today a reaction-triggered agent runs inline, with no claim, no completion contract, no failure policy, and no way to see the work's status across a project.

This change introduces **object-driven agent work**: work items are graph objects; a built-in `status` and `assignee` express the workflow; agents subscribe to types via reaction triggers; the run claims the object atomically, completes through an explicit tool, and a per-agent review gate decides whether work is done or returns for revision. Failures are classified and routed rather than lost. A Kanban view is a **projection** over objects + sessions + runs — not a separate store.

> Revision note (post-review): this revision drops per-attempt branch isolation (reviewers found the branch machinery non-atomic and O(graph) per attempt) in favour of the **existing `needs_review`/`reviewed_by`/`reviewed_at` review primitives on the main graph**, corrects the claim to match the versioned write model, reuses the existing `kb.agent_processing_log` for dispatch dedup and the existing agent circuit breaker, and sequences behind `add-agent-review-queues` (#1304).

## What Changes

- **Work item = graph object.** Any object type can be a work item. For types flagged **board-enabled**, the built-in `status` column is the **authoritative** work state and a new built-in `assignee` column (nullable, indexed) records the lane. Status *values* are user-defined and validated per object type.
- **Single status writer.** Agents never write `status` directly; transitions flow through the platform's work path (`work_complete`/`work_block`), which creates the new version and keeps `properties["status"]` consistent so the object write path cannot clobber the claim.
- **Listening agents.** An agent subscribes via its existing reaction trigger (`objectTypes`, `events: [created]`). Creation of a matching object wakes it; edits do not (rework is explicit).
- **Dispatch through the queue with durable dedup.** A created-object event enqueues a run on the agent's queue (instead of running inline), reusing **`kb.agent_processing_log`** (`agent_id + graph_object_id + object_version + event_type`) for deduplication instead of a new key. Routing honours `assignee` (assigned object → only the matching listener, within the object's project).
- **Correct claim.** The worker claims the job, then performs the object transition under the existing per-object advisory lock (`AcquireObjectUpsertLock` → read HEAD → `CreateVersion`). A lost race makes the run `skipped` (no failure budget burn). Board-enabled work types require a non-null `key`.
- **Lease and reaper.** A claimed object's liveness is derived from its associated run; a reaper returns `in-progress` objects whose run is gone/terminal to `ready` (budget-aware) or `blocked`, and a reconciler enqueues `ready` objects with no live run.
- **Explicit completion.** `work_complete` (→ done, or review when the agent requires review) and `work_block` (→ blocked + human task) are run-finalizing terminators. A run that ends without one is a protocol violation → bounded retry → block. A run-end→item-transition table defines every mapping.
- **Review on main (no branches).** `requiresReview` sets the object's `needs_review`; approval sets `reviewed_by`/`reviewed_at` and `status=done`; a change request sets `status=revision`, records append-only feedback, and explicitly enqueues a rework run (which carries all prior feedback). A revision cap escalates to a human.
- **Failure policy (3 classes).** Retryable (transient → backoff, counts against the per-item budget), deterministic/poison (no retry → block), human (needs-input/review). Quota/rate-limit errors are agent-level and never consume the item budget. The **existing** agent circuit breaker (`ConsecutiveFailures` + auto-disable) is extended, not duplicated.
- **Human actions.** A defined API for approve / request-changes / retry / reassign / cancel, with auth tiers registered in `route-authority.yaml`; review and escalations surface through `kb.tasks`.
- **Kanban projection.** A read projection over board-enabled object types (status, assignee) joined to runs by `kb.agent_runs.subject_object_id`; columns are work status, the latest run's execution status is a badge, the card drawer shows sessions/runs/artifacts/feedback; drag sets the object status and enqueues when entering the ready lane. Unfiled sessions are excluded.

## Capabilities

### New Capabilities

- `object-driven-agent-work`: graph objects as work items (board-enabled status/assignee), listening-agent dispatch through the queue with existing dedup, advisory-lock claim with a lease and reaper, explicit run-finalizing terminators plus a run-end mapping, per-agent review on main with explicit rework, a 3-class failure policy with a per-item budget, and the Kanban projection over objects + runs.

### Modified Capabilities

<!-- none — reaction triggers, the dispatch ledger, the worker pool, and the graph write path are extended, not redefined. -->

## Impact

- Server (`apps/server/domain/agents/`): `entity.go` (agent `workConfig`), `repository.go` (enqueue-on-create, claim transition, budget, run↔subject link), `triggers.go` (enqueue instead of inline; honest routing/dedup), `worker_pool.go` (run-end mapping, extended breaker), new `work_complete`/`work_block` tools and a lease/reaper.
- Server (`apps/server/domain/graph/`): `assignee` built-in column + index; the single-work-status write path (authoritative `status`, consistent `properties["status"]`); `key` required for board-enabled types; a work-status reaper. Review uses existing `needs_review`/`reviewed_by`/`reviewed_at`.
- Server (`apps/server/domain/schemas/`): per-type board-enabled flag, allowed status values, operational flags (skip embeddings/extraction/search) — **new schema surface** (the registry does not have these today).
- Migrations: `kb.graph_objects.assignee` + index; `kb.agent_runs.subject_object_id`/`subject_object_type`; `failure_class`; numbering `00205+` **after** `add-agent-review-queues` (`00204`).
- CI surface that must be updated with any new route/model: `apps/server/route-authority.yaml`, `apps/server/internal/schemadrift/models.go`, fx `module.go` wiring.
- Web UI (`apps/web-ui/gateway/`): Kanban projection view, card drawer, Failed/Blocked surfaces, Retry/Reassign/Cancel/Approve/Request-changes actions, agent-health readout.
- CLI: `memory work-items` (list/create/move/comment/approve/request-changes/block/retry/reassign).
- No breaking change: absent `workConfig`/board configuration, existing reaction triggers, cron, webhook, and manual runs behave as today.

## Non-Goals

- **Per-attempt branch isolation / merge** (deferred; the branch machinery is non-atomic and O(graph) per attempt — see design "Alternatives").
- A separate work-item table or a workflow engine (the object is the work item; the board is a projection).
- Replacing the dispatch ledger, the worker pool, the processing log, or the agent circuit breaker.
- Work-contract validation and full compensation (later).

## Sequencing

`add-agent-review-queues` (#1304) lands first: it adds migration `00204`, the queue/priority columns, and per-queue workers — the same files this change touches, and the migration numbering depends on it.
