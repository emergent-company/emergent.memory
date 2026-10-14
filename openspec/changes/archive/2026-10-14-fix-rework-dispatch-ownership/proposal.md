# fix-rework-dispatch-ownership

## Why

Request-changes enqueues a rework run **directly**, carrying the full feedback
history (the only carrier of `work_feedback`). The review→revision transition
then also emits `status_changed`, which wakes any work agent whose
`triggerStatuses` include `revision` — **including the same agent that the
direct rework run was enqueued for**.

The original protection relied on `concurrencyStrategy: skip`: the direct rework
run holds the object's `revision→in_progress` claim, so the later
`status_changed` dispatch loses the race and skips. That is racy. The two
enqueues are async and unordered; when the `status_changed` dispatch wins the
claim first, the owner executes the rework **without** `work_feedback` — blind,
duplicated, or lost rework.

This change makes request-changes **own** rework dispatch: the
`status_changed` event it emits carries the owning agent's identity, and the
dispatcher suppresses only that owner, so the owner cannot re-dispatch itself
while other agents that also declare the revision status still wake.

## What Changes

- `graph.WorkObjectTransition` gains `ReworkAgentID *uuid.UUID`, set only by the
  request-changes path, and threaded into the `status_changed` payload as
  `rework_agent_id`.
- `graph.Service.RequestChangesWorkObject` and
  `WorkActionService.RequestChanges` thread the owning agent's id.
- `agents.TriggerService.enqueueWorkRun` skips the enqueue when the payload's
  `rework_agent_id` equals the candidate agent's id (suppress the owner only).
- Keep emitting `status_changed` (other consumers still need it) and keep the
  explicit rework enqueue (the only carrier of `work_feedback`).

## Impact

- `apps/server/domain/graph/work_object.go` — transition field, payload, emit path.
- `apps/server/domain/agents/work_actions.go` / `work_object.go` — interface + owner id.
- `apps/server/domain/agents/triggers.go` — owner suppression guard.
- `apps/server/migrations/00240_allow_work_pickup_event_types.sql` — relax the
  `kb.agent_processing_log.event_type` CHECK to admit `status_changed`/`assigned`
  (baseline only allowed `created`/`updated`/`deleted`).
- Tests: `apps/server/domain/agents/work_rework_dispatch_test.go`.

## Non-Goals

- No change to approval, reassignment, or revision-cap behaviour.
- No Web UI / board changes.
- No removal of the `status_changed` event or the explicit rework enqueue.
