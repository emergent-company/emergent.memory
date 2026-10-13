# work-pickup-signals

## Why

Object-driven agent work wakes an agent only on a single, implicit signal: the
object being **created**. There is no way to wake an agent when an object
**moves into a status** it cares about, when a specific **property changes**, or
when the object is **assigned to it** — so rework, reassignment, and
edit-driven follow-up all require ad-hoc re-enqueue logic scattered across the
work path (and, in the request-changes case, a direct re-enqueue that competes
with the event bus and can double-dispatch).

This change makes the work-pickup signal **configurable** on an agent's work
config: an agent declares the object type it listens to and the one signal that
wakes it — `created`, `updated`, `status_changed`, or `assigned` — and the
dispatcher, reconciler, and claim path honour that declaration.

## What Changes

- **Four pickup signals** — `created` (unchanged), `updated` (wakes when the
  changed property paths intersect the agent's declared `properties`),
  `status_changed` (wakes when the object transitions into one of the agent's
  `triggerStatuses`, defaulting to `ready`), and `assigned` (wakes when the new
  assignee equals the agent's name). `deleted` is rejected as a work signal.
- **New event kinds** — `entity.status_changed` and `entity.assigned`, emitted
  **after commit** (never inside the transaction) from the work-transition and
  reassign paths, carrying status / previous status / assignee so dispatch does
  not need a second graph read.
- **Trigger statuses** — `AgentWorkConfig.triggerStatuses` (default `["ready"]`)
  widens the reconciler/reaper scan to the union of ready and trigger statuses as
  a safety net for stranded objects.
- **Generalized claim CAS** — `status_changed` claims from the matched status,
  `assigned` claims from the object's current HEAD status, and `created`/`updated`
  claim from ready; a lost race still skips the run without consuming the budget.
- **Explicit rework, plus a signal for other consumers** — request-changes
  enqueues the rework run directly, so it carries the full feedback history and
  is recognised as a rework run; the review→revision transition additionally
  emits `status_changed`, and a work agent that also declares the revision status
  as a trigger status is protected from double-dispatch by its required
  `concurrencyStrategy: skip`.
- **Validation** — agent create/update and blueprint apply reject a work agent
  that declares more than one object type, more than one signal, `deleted`, or
  `concurrencyStrategy: parallel`.

## Impact

- Server (`apps/server/domain/agents/`): `entity.go` (signal set, `Properties`,
  `triggerStatuses`), `triggers.go` (4-way match + claim metadata), `worker_pool.go`
  (claim-source dispatch), `work_maintenance.go` (listener index + status union),
  `work_actions.go` (request-changes), new `work_pickup_validation.go`.
- Server (`apps/server/domain/graph/`): `work_object.go` (post-commit emit),
  `service.go` (update payload carries changed property paths).
- Server (`apps/server/domain/events/`): `status_changed` / `assigned` event types
  and emit helpers.
- Server (`apps/server/domain/blueprints/apply.go`): blueprint work-agent validation.
- No migration: `triggerStatuses` defaults to `["ready"]` in code, so existing
  work agents keep the current `created`/`ready` behaviour.

## Non-Goals

- Multi-signal agents: a work agent still declares exactly one pickup signal.
- Web UI / board changes (handled in a separate lane).
- A general event-condition DSL — the four signals are fixed, purpose-built lanes.
