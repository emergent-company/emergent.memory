## Purpose

Graph objects as work items: any object type can carry work; a built-in `status` and `assignee` express the workflow; agents "listening" to a type are woken when a matching object is created; the run claims the object atomically, ends through an explicit terminator, and — where the agent requires it — leaves room for human review. Failures are classified and routed rather than lost, and a Kanban board is a projection over objects, sessions, and runs.

## ADDED Requirements

### Requirement: Work items are graph objects with built-in status and assignee

A work item SHALL be a graph object of a user-defined type. Its work state SHALL be the built-in object `status`; its lane SHALL be the built-in object `assignee`.

#### Scenario: Assignee and status set on creation

- **WHEN** an object is created with a status and an assignee
- **THEN** both are stored as the object's built-in fields and are queryable

#### Scenario: Assignee is optional

- **WHEN** a work object is created without an assignee
- **THEN** the object is eligible to be claimed by any listening agent for its type

### Requirement: Status values validated per object type

An object type MAY declare the set of allowed work status values, and a write that sets a status outside that set SHALL be rejected.

#### Scenario: Invalid status rejected

- **WHEN** an object of a type that allows only `ready`, `in_progress`, `review`, `done` is written with `status = "shipped"`
- **THEN** the write is rejected and the object is unchanged

#### Scenario: Unconstrained type

- **WHEN** an object type declares no allowed status values
- **THEN** any status string within the field limit is accepted

### Requirement: Listening agents subscribe to an object type

An agent SHALL subscribe to work by declaring a reaction trigger over an object type and the `created` event.

#### Scenario: Created object wakes a listener

- **WHEN** an object of a type an agent listens to is created
- **THEN** the agent is woken for that object

#### Scenario: Edits do not wake listeners

- **WHEN** an existing work object is updated without being explicitly re-queued
- **THEN** listening agents are not woken by that update alone

### Requirement: Dispatch through the work queue with idempotency

Waking a listening agent SHALL enqueue a run on the agent's queue rather than executing it inline. A duplicate dispatch for the same agent, object, and version SHALL be a no-op.

#### Scenario: Enqueued, not inline

- **WHEN** a work object is created and its listener has a configured queue
- **THEN** a run is enqueued on that queue and is not executed within the triggering request

#### Scenario: Duplicate create is deduplicated

- **WHEN** the same created event is delivered twice for the same agent and object version
- **THEN** only one run is enqueued

#### Scenario: Assigned object routes to its assignee

- **WHEN** a work object is created with an assignee, and several agents listen to its type
- **THEN** only the listening agent matching the assignee is enqueued

### Requirement: Atomic claim prevents double work

A claimed run SHALL transition the object from the configured ready status to the in-progress status with a compare-and-set, and SHALL do no work if the object is no longer claimable.

#### Scenario: Second claimant is a no-op

- **WHEN** two runs are dispatched for the same object and the first claims it
- **THEN** the second claim finds the object already in progress and completes without executing the agent

### Requirement: Explicit completion terminator

A run SHALL end by calling a terminator: `work_complete`, which sets the done status (or the review status when the agent requires review), or `work_block`, which sets the blocked status.

#### Scenario: Complete sets done

- **WHEN** an agent that does not require review calls `work_complete`
- **THEN** the object's status becomes the configured done value and the run succeeds

#### Scenario: Complete routes to review

- **WHEN** an agent that requires review calls `work_complete`
- **THEN** the object's status becomes the configured review value and the run succeeds

#### Scenario: Block surfaces to a human

- **WHEN** an agent calls `work_block` with a reason
- **THEN** the object's status becomes the configured blocked value and a human-facing item is created

### Requirement: Missing terminator is a protocol violation

A run that ends without calling a terminator SHALL be treated as a protocol violation and SHALL NOT leave the object in the done status.

#### Scenario: No terminator

- **WHEN** a run exits without `work_complete` or `work_block`
- **THEN** the run is retried up to the retry bound and, if it still fails, the object is blocked with an agent-quality failure

### Requirement: Per-agent review gate with explicit rework

When an agent requires review, completion SHALL await a human decision; approval finalizes the object, and a change request SHALL return the object for rework with structured feedback.

#### Scenario: Approval finalizes

- **WHEN** a human approves an object in review
- **THEN** the object's status becomes done

#### Scenario: Change request re-opens the work

- **WHEN** a human requests changes with feedback on an object in review
- **THEN** the object's status becomes the revision value, the feedback is recorded, and a rework run is explicitly enqueued

#### Scenario: Revision cap

- **WHEN** an object has been returned for rework beyond the configured cap
- **THEN** it is escalated to a human instead of being re-queued again

### Requirement: Failure classification

Every failed run SHALL be classified as transient, agent-quality, terminal, needs-input, or capability, and the class SHALL determine the response.

#### Scenario: Transient failure retries without budget

- **WHEN** a run fails with a transient (infrastructure) error
- **THEN** it is retried with backoff and the work item's failure budget is not consumed

#### Scenario: Terminal failure blocks without retry

- **WHEN** a run fails with a terminal (configuration) error
- **THEN** it is not retried and the object is blocked with a human notification

#### Scenario: Capability failure unassigns

- **WHEN** a run fails because the assigned agent cannot perform the work
- **THEN** the object's assignee is cleared and its status returns to the ready value

### Requirement: Per-item failure budget and dead-letter

Each work item SHALL track a failure count, and exhausting the configured budget SHALL move the item to a blocked, human-visible dead-letter state.

#### Scenario: Budget exhaustion blocks

- **WHEN** a work item reaches the configured failure budget
- **THEN** it is blocked with the last failure recorded and a human-facing item is created

#### Scenario: Human recovery

- **WHEN** a human retries, reassigns, or cancels a dead-lettered item
- **THEN** the item returns to the ready state, is reassigned, or is closed respectively

### Requirement: Agent circuit breaker

An agent that fails consecutively beyond a threshold SHALL be automatically disabled for triggering and surfaced to an operator.

#### Scenario: Breaker opens

- **WHEN** an agent exceeds the consecutive-failure threshold
- **THEN** its trigger is disabled and its unhealthy state is surfaced, and it resumes only when an operator re-enables it

### Requirement: Isolated attempts merged on success

Agent-produced objects SHALL be written on an isolated branch per attempt; the branch SHALL be merged to main only on success and discarded on failure.

#### Scenario: Success merges

- **WHEN** a run completes successfully
- **THEN** its branch is merged to main

#### Scenario: Failure discards

- **WHEN** a run fails and is retried
- **THEN** its attempt branch is discarded and does not affect main

#### Scenario: Blocked retains

- **WHEN** a run blocks
- **THEN** its attempt branch is retained for inspection and rework

#### Scenario: Operational state stays on main

- **WHEN** a work item's status or assignee changes
- **THEN** the change is visible on main regardless of any in-flight attempt branch

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
- **THEN** the object's status is updated and a run is enqueued

#### Scenario: Machine cannot self-complete

- **WHEN** a card has not been completed or approved
- **THEN** it is not shown in the done column

#### Scenario: Unfiled sessions excluded

- **WHEN** a session has no associated work object
- **THEN** it does not appear on the board
