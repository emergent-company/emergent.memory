# object-driven-agent-work Specification

## Purpose
Graph objects as work items: any object type can carry work; a built-in `status` and `assignee` express the workflow; agents "listening" to a type are woken when a matching object is created; the run claims the object correctly against the versioned write model, ends through an explicit run-finalizing terminator, and — where the agent requires it — leaves room for human review on the main graph. Failures are classified and routed rather than lost, and a Kanban board is a projection over objects, sessions, and runs.

## Requirements

### Requirement: Work items are graph objects

A work item SHALL be a graph object of a user-defined type. Its work state SHALL be the object `status`; its lane SHALL be the object `assignee`.

#### Scenario: Status and assignee on a work item

- **WHEN** an object of a board-enabled type is created with a status and an assignee
- **THEN** both are stored as the object's built-in fields and are queryable

#### Scenario: Assignee is optional

- **WHEN** a board-enabled work object is created without an assignee
- **THEN** the object is eligible to be claimed by any listening agent for its type

### Requirement: Board-enabled types declare and validate their work status

An object type SHALL be able to be flagged board-enabled; a board-enabled type SHALL declare its allowed work status values, and a write outside that set SHALL be rejected.

#### Scenario: Invalid status rejected

- **WHEN** a board-enabled type allowing only `ready`, `in_progress`, `review`, `done` is written with `status = "shipped"`
- **THEN** the write is rejected and the object is unchanged

#### Scenario: Non-board type unconstrained

- **WHEN** an object of a type that is not board-enabled is written with any status string within the field limit
- **THEN** the write is accepted

### Requirement: A single writer owns work status

For board-enabled types, `status` SHALL be authoritative and SHALL be changed only through the platform's work path, which creates the next version and keeps `properties["status"]` consistent. A direct agent write that sets work status SHALL be rejected — covering **both** the `status` field **and** `properties["status"]`, at every write entry point (create, create-or-update, patch, bulk status update).

#### Scenario: Agent cannot set status directly

- **WHEN** an agent attempts to write `status` on a board-enabled work object through a direct graph write
- **THEN** the write is rejected and the object is unchanged

#### Scenario: Agent cannot smuggle status through properties

- **WHEN** an agent attempts to write `properties["status"]` on a board-enabled work object
- **THEN** the write is rejected and the object is unchanged

#### Scenario: Platform transition keeps both copies consistent

- **WHEN** the platform transitions a board-enabled object's status
- **THEN** the new version's `status` column and `properties["status"]` agree

### Requirement: Listening agents subscribe to a type and are deduplicated

An agent SHALL subscribe to work through a reaction trigger over an object type and the `created` event. Dispatch SHALL be deduplicated so a repeated `created` delivery for the same agent, object, and object version results in a single run. The dedup key SHALL use the object's canonical identity (`canonical_id`) so a version bump does not defeat dedup.

#### Scenario: Created object wakes a listener

- **WHEN** an object of a type an agent listens to is created
- **THEN** the agent is woken for that object

#### Scenario: Duplicate delivery deduplicated

- **WHEN** the same `created` event for the same agent and object version is delivered twice
- **THEN** only one run is created, using the existing processing-log dedup key

#### Scenario: Edits do not wake listeners

- **WHEN** a work object is updated without an explicit re-queue
- **THEN** listening agents are not woken by that update

### Requirement: Dispatch through the queue, routed by assignee

Waking a listening agent SHALL enqueue a run on the agent's queue rather than executing inline. When the object has an assignee, only the listening agent matching that assignee within the object's project SHALL be enqueued.

#### Scenario: Enqueued, not inline

- **WHEN** a work object is created and its listener has a configured queue
- **THEN** a run is enqueued on that queue and is not executed within the triggering request

#### Scenario: Assigned object routes to its assignee

- **WHEN** a work object is created with an assignee and several agents listen to its type
- **THEN** only the listening agent matching the assignee in the same project is enqueued

#### Scenario: Unroutable object is surfaced

- **WHEN** a board-enabled object has an assignee matching no listener, or its type has no listener
- **THEN** the object is surfaced as unroutable (a derived predicate) rather than left silently pending

### Requirement: Correct, crash-safe claim

Claiming SHALL transition the object from the ready status to the in-progress status under the object's advisory lock and the versioned write model; a lost race SHALL leave the run skipped without consuming the failure budget.

#### Scenario: Claim is serialized

- **WHEN** two runs are dispatched for the same object
- **THEN** only one transition to in-progress succeeds and the other run is skipped

#### Scenario: Skipped claim does not burn budget

- **WHEN** a run's claim finds the object already taken
- **THEN** the run is marked skipped, the job completes, and the item's failure budget is unchanged

#### Scenario: Board-enabled types require a stable key

- **WHEN** a board-enabled work object is created without a key
- **THEN** the write is rejected, because the claim requires a stable identity

### Requirement: Claimed work is reclaimed when its run dies

A claim whose run is missing or terminal beyond a threshold SHALL be reclaimed, and ready board-enabled objects with no live run SHALL be reconciled and enqueued. A run paused for human input SHALL count as live and SHALL NOT be reclaimed.

#### Scenario: Stranded in-progress is released

- **WHEN** an object is in-progress but its run is missing or terminal past the threshold
- **THEN** a reaper returns the object to the ready status (budget-aware) or to blocked

#### Scenario: Missed enqueue is reconciled

- **WHEN** a ready board-enabled object has no live run or job
- **THEN** it is enqueued by the reconciler

### Requirement: Explicit, run-finalizing terminator

A run SHALL end by calling `work_complete` (→ the done status, or the review status when the agent requires review) or `work_block` (→ the blocked status). A terminator call SHALL end the run; subsequent steps SHALL be ignored.

#### Scenario: Complete sets done

- **WHEN** an agent that does not require review calls `work_complete`
- **THEN** the object's status becomes done and the run ends

#### Scenario: Complete routes to review

- **WHEN** an agent that requires review calls `work_complete`
- **THEN** the object's status becomes review, its `needs_review` is set, and the run ends

#### Scenario: Block surfaces to a human

- **WHEN** an agent calls `work_block` with a reason
- **THEN** the object's status becomes blocked and a human-facing item is created

#### Scenario: Missing terminator

- **WHEN** a run ends without a terminator
- **THEN** it is retried while the failure budget remains and otherwise the object is blocked as a protocol violation

### Requirement: Run end maps explicitly to an item transition

Every run end state SHALL map to a defined item transition and a defined failure-budget effect.

#### Scenario: Retryable failure re-queues

- **WHEN** a run ends with a retryable error
- **THEN** the item returns to ready and is re-enqueued with backoff, and the budget is incremented

#### Scenario: Quota error does not consume the item budget

- **WHEN** a run ends because of a provider quota or rate-limit error
- **THEN** the item is unchanged and the item budget is not incremented

#### Scenario: Paused run does not advance the item

- **WHEN** a run pauses for human input
- **THEN** the item stays in-progress and wait time is not counted

### Requirement: Per-agent review on the main graph, with explicit rework

Review SHALL use the object's `needs_review`/`reviewed_by`/`reviewed_at` columns on the main graph; this change builds the write path for those (currently dormant) fields. Approval SHALL finalize; a change request SHALL record append-only feedback and explicitly enqueue a rework run that carries all prior feedback; a revision cap SHALL escalate to a human.

#### Scenario: Approval finalizes

- **WHEN** a human approves an object in review
- **THEN** the object's `reviewed_by`/`reviewed_at` are set, `needs_review` is cleared, and its status becomes done

#### Scenario: Change request requires feedback and re-opens the work

- **WHEN** a human requests changes with non-empty feedback
- **THEN** the object's status becomes revision, the feedback is appended to its feedback history, and a rework run is enqueued carrying all prior feedback

#### Scenario: Empty feedback rejected

- **WHEN** a change request is submitted with empty feedback
- **THEN** the request is rejected

#### Scenario: Revision cap escalates

- **WHEN** an object has been returned for rework beyond the configured cap
- **THEN** it is escalated to a human and is not re-enqueued again

### Requirement: Human actions are defined and authorized

The system SHALL expose approve, request-changes, retry, reassign, and cancel actions for work items, behind the project-membership auth tier, and SHALL register the corresponding routes.

#### Scenario: Retry a failed item

- **WHEN** a human retries a blocked or failed work item
- **THEN** the item returns to ready and is enqueued

#### Scenario: Reassign an item

- **WHEN** a human changes or clears the assignee of a work item
- **THEN** the item is routed according to the new assignee (or to any listener)

#### Scenario: Cancel an item

- **WHEN** a human cancels a work item
- **THEN** the item is closed and any in-flight run is cancelled

### Requirement: Failure classification and per-item budget

A failed run SHALL be classified as retryable, deterministic, or human; only retryable and deterministic failures SHALL consume the per-item failure budget, and exhausting the budget SHALL move the item to a human-visible blocked state. Quota and rate-limit failures SHALL be handled at the agent level and SHALL NOT consume the item budget.

#### Scenario: Budget exhaustion blocks

- **WHEN** a work item reaches its configured failure budget
- **THEN** it is blocked with the last failure recorded and a human-facing item is created

#### Scenario: Capability failure unassigns with backoff

- **WHEN** a run fails because the assigned agent cannot perform the work
- **THEN** the assignee is cleared, the item returns to ready with a requeue backoff, and the attempt history is retained

### Requirement: Agent circuit breaker is extended, not duplicated

The existing per-agent consecutive-failure breaker SHALL be extended with explicit thresholds, and SHALL distinguish a poison item (which blocks the item) from a broken agent (which disables the agent for triggering).

#### Scenario: Poison item does not disable the agent

- **WHEN** the same work item repeatedly and deterministically fails
- **THEN** the item is blocked and the agent remains enabled

#### Scenario: Broken agent is disabled

- **WHEN** an agent's consecutive failures across distinct items exceed the threshold
- **THEN** its trigger is disabled and its unhealthy state is surfaced until an operator re-enables it

### Requirement: Kanban projection

The board SHALL be a read projection over board-enabled object types joined to their runs, with no separate work-item store.

#### Scenario: Columns from work status

- **WHEN** board-enabled objects exist with different statuses
- **THEN** they are grouped into columns by status

#### Scenario: Execution badge

- **WHEN** an object has associated runs
- **THEN** the latest run's execution status is shown on its card

#### Scenario: Dragging executes

- **WHEN** a card is moved to the ready lane
- **THEN** the object's status is updated through the work path and a run is enqueued

#### Scenario: Machine cannot self-complete

- **WHEN** a card has not been completed or approved
- **THEN** it is not shown in the done column

#### Scenario: Unfiled sessions excluded

- **WHEN** a session has no associated work object
- **THEN** it does not appear on the board

### Requirement: Operational objects do not pollute the knowledge pipelines

A board-enabled type SHALL declare whether its objects are excluded from embeddings, extraction, and default search, and those flags SHALL be honoured by the respective pipelines.

#### Scenario: Excluded from embeddings

- **WHEN** an operational work object changes status
- **THEN** no embedding job is enqueued for the new version

#### Scenario: Excluded from extraction

- **WHEN** an operational work object is written
- **THEN** it is not scheduled for extraction

### Requirement: Work contract validates required deliverables before completion

An agent MAY declare a work contract on its work config. When the contract is non-empty, `work_complete` SHALL validate it before transitioning the item to done: `requireArtifacts` SHALL require a non-empty summary or artifacts, and each type in `requiredDeliverableTypes` SHALL be satisfied by at least one declared deliverable of that type that resolves to an existing object in the project. A rejected completion SHALL return an error tool result and SHALL NOT finalize the run or change the item, so the agent may continue in the same run; it SHALL NOT consume the per-item failure budget. An empty or unset contract SHALL leave `work_complete` behaviour unchanged.

#### Scenario: Contract satisfied completes the item

- **WHEN** an agent whose contract requires a deliverable of a type declares such a deliverable that resolves to an existing object and calls `work_complete`
- **THEN** the item is completed and the run ends

#### Scenario: Missing required deliverable rejects completion

- **WHEN** an agent whose contract requires a deliverable of a type calls `work_complete` without declaring a deliverable of that type
- **THEN** completion is rejected with an error tool result, the run is not finalized, and the item is unchanged

#### Scenario: Declared deliverable that does not exist rejects completion

- **WHEN** an agent declares a required deliverable whose type and key do not resolve to an existing object
- **THEN** completion is rejected, the run is not finalized, and the item is unchanged

#### Scenario: Required artifacts absent rejects completion

- **WHEN** an agent whose contract sets `requireArtifacts` calls `work_complete` with neither artifacts nor a summary
- **THEN** completion is rejected, the run is not finalized, and the item is unchanged

#### Scenario: Empty contract leaves behaviour unchanged

- **WHEN** an agent has no work contract (or an empty one) and calls `work_complete`
- **THEN** the item is completed exactly as if no contract were configured
