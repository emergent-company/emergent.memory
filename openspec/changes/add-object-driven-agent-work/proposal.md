## Why

Memory can already run agents on a schedule, on manual trigger, on graph-object reactions, and on webhooks, with a durable dispatch ledger (`kb.agent_run_jobs`) and per-queue workers. What it lacks is a **general, object-driven work model**: the ability to create *any* object in the graph, have an agent "listening" to that object type pick it up, decide when it is done, and — where configured — leave room for human review. Today a reaction-triggered agent runs inline with no claim, no completion contract, no failure policy, and no way to see the work's status across a project.

This change introduces **object-driven agent work**: work items are graph objects; a built-in `status` and a new built-in `assignee` express the workflow; agents subscribe to types via reaction triggers; the run claims the object atomically, completes through an explicit tool, and a per-agent review gate decides whether the work is done or returns for revision. Failures are classified and routed (retry, block, or unassign) rather than lost. A Kanban view is then a **projection** over objects + sessions + runs — not a separate store.

## What Changes

- **Work item = graph object.** Any object type can be a work item. The existing built-in `GraphObject.Status` is the work state; a new built-in `assignee` records the lane. Status *values* are user-defined and validated per object type via the schemas registry.
- **Listening agents.** An agent subscribes to a type through its existing reaction trigger (`objectTypes`, `events: [created]`). Creation of a matching object wakes it; edits do not (rework is explicit).
- **Dispatch through the queue.** A created-object event enqueues a run on the agent's queue (instead of running inline), with an idempotency key (`agentId + canonicalId + version`), bounded concurrency, retry, and backoff.
- **Atomic claim.** The worker claims the job, then transitions the object `status` from the configured ready value to the in-progress value with a compare-and-set; if the object is already taken, the claim is a no-op — no double work.
- **Explicit completion.** New tools `work_complete` (sets the configured done value, or the review value when the agent requires review) and `work_block` (sets the blocked value and surfaces to a human). A run that ends without a terminator is a protocol violation and is retried, then escalated.
- **Per-agent review gate.** `requiresReview` sends completion to the review status; a human approves (→ done) or requests changes (→ revision with structured feedback, which explicitly enqueues a rework run).
- **Failure policy.** Every failed run is classified — transient/infra (auto-retry, no budget tick), agent/quality (bounded retry then escalate), terminal/config (no retry, block + notify), needs-input (blocked), capability (unassign → ready). A per-work-item failure budget (default 3) exhausts into a blocked/dead-letter state with human notification. A per-agent circuit breaker auto-disables an agent after N consecutive failures.
- **Isolated attempts.** Agent-produced objects are written on an isolated graph branch + sandbox workspace per attempt and merged to main only on success; failed attempts are discarded, blocked attempts retained for inspection. Operational metadata (status, assignee, failure count) stays on main so the board is always truthful.
- **Kanban projection.** A read projection over board-enabled object types (status, assignee) joined to runs by `subject_object_id`: columns are work status, the latest run's execution status is a badge, and the card drawer shows sessions, runs, artifacts, and feedback. Dragging a card sets the object status and enqueues when it enters the ready lane.

## Capabilities

### New Capabilities

- `object-driven-agent-work`: graph objects as work items (built-in status/assignee), listening-agent dispatch through the queue, atomic claim, explicit completion/block, per-agent review and rework, failure classification with retry/block/unassign, isolated attempts with merge-on-success, and the Kanban projection over objects + runs.

### Modified Capabilities

<!-- none — the reaction trigger, dispatch ledger, and worker pool are extended, not redefined. -->

## Impact

- Server (`apps/server/domain/agents/`): `entity.go` (agent `workConfig`), `repository.go` (enqueue-on-create, claim CAS, failure budget, run↔subject link), `triggers.go` (enqueue instead of inline; enforce `ConcurrencyStrategy`), `worker_pool.go` (failure classification, circuit breaker), new `work_complete`/`work_block` tools, run↔object linkage, branch merge on success.
- Server (`apps/server/domain/graph/`): new built-in `assignee` column + index; status/assignee in DTOs and filters; branch-aware attempt writes.
- Server (`apps/server/domain/schemas/`): per-type allowed status values, board-enabled flag, and operational flags (skip embeddings/extraction/search).
- Migrations: `kb.graph_objects.assignee` + index; `kb.agent_runs.subject_object_id`/`subject_object_type`/`failure_class`; work-item failure-count storage.
- Web UI (`apps/web-ui/gateway/`): Kanban projection view (columns, drag = transition/enqueue), card drawer, Failed/Blocked lanes, Retry/Reassign/Cancel actions, agent-health surface.
- CLI: `memory work-items` (list/create/move/comment/approve/request-changes/block/retry/reassign).
- No breaking change: absent `workConfig`/board configuration, existing reaction triggers, cron, webhook, and manual runs behave as today; the new behavior is opt-in per agent and per object type.

## Non-Goals

- A separate work-item table or a bespoke workflow engine (the object is the work item; the board is a projection).
- Replacing the dispatch ledger or the worker pool.
- Building a new agent runtime — agents, tools, sandboxes, and branches are reused.
- Full compensation/rollback for partial effects (phase 6; phase 1 uses isolated attempts + merge-on-success).
