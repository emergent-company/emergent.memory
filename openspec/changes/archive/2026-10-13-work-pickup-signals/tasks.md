# Tasks — Configurable work-pickup signals

## 1. Signal set + config

- [x] 1.1 Add `EventTypeStatusChanged` / `EventTypeAssigned` to `ReactionEventType` in `domain/agents/entity.go`.
- [x] 1.2 Add `ReactionConfig.Properties []string` (RFC 6901 paths) for the `updated` signal.
- [x] 1.3 Add `AgentWorkConfig.TriggerStatuses []string` + `TriggerStatusValues()` (default `["ready"]`) and fold into `IsZero()`.

## 2. Events

- [x] 2.1 Add `entity.status_changed` / `entity.assigned` to `domain/events/types.go`.
- [x] 2.2 Add `EmitStatusChanged` / `EmitAssigned` to `domain/events/service.go`.
- [x] 2.3 Emit `status_changed` (status moved) and `assigned` (reassign) **after commit** from `transitionWorkObject`; payload carries new status, previous status, assignee.
- [x] 2.4 `emitObjectUpdated` payload carries changed property paths (`ChangeSummary["paths"]`).

## 3. Dispatch

- [x] 3.1 Map the new event kinds in `onEntityEvent`.
- [x] 3.2 4-way match in `enqueueWorkRun` (`workPickupMatch`): created always; updated by property intersection; status_changed by trigger status; assigned by assignee == name.
- [x] 3.3 Record claim-source metadata (`work_trigger_event` + `work_claim_status`) at enqueue.

## 4. Claim + reconciliation

- [x] 4.1 Generalize `claimWorkObject`: status_changed CASes from the matched status; assigned CASes from the HEAD's current status; created/updated claim from ready.
- [x] 4.2 `reactionSubscribesToWork` (created | status_changed) for the listener index + `FindReactionAgentsForType`.
- [x] 4.3 `listWorkAgentStatuses` unions trigger statuses into the ready scan; reconciler enqueue records the claim source.

## 5. Rework + validation

- [x] 5.1 Keep the explicit rework enqueue in `RequestChanges` — it carries the full feedback history and marks the run as a rework run; the review→revision transition additionally emits `status_changed`, and double-dispatch is prevented by the required `concurrencyStrategy: skip`.
- [x] 5.2 `ValidateWorkPickup` (single object type, single signal, no `deleted`, no `parallel`); wire into `CreateAgent`/`UpdateAgent` and blueprint `ensureRuntimeAgent`.

## 6. Tests

- [x] 6.1 Unit: validation, trigger statuses, 4-way match, trigger metadata, claim-source dispatch, subscribe-to-work, statuses union.
- [x] 6.2 Graph integration: status_changed/assigned emitted after commit, lost CAS emits nothing, same-status transition emits nothing.

## 7. Verify

- [x] 7.1 `go build ./...`, `go vet`, `go test` (unit + integration where DB is available).
- [x] 7.2 `openspec validate work-pickup-signals`.
