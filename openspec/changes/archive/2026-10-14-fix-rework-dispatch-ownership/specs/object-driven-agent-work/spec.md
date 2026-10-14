## MODIFIED Requirements

### Requirement: Per-agent review on the main graph, with explicit rework

Review SHALL use the object's `needs_review`/`reviewed_by`/`reviewed_at` columns on the main graph; this change builds the write path for those (currently dormant) fields. Approval SHALL finalize; a change request SHALL record append-only feedback and transition the object to revision, and SHALL enqueue a rework run directly that carries the full feedback history (so the worker recognises it as a rework run and sees why it is re-running). The review→revision transition SHALL still emit a `status_changed` event for other consumers, and that event SHALL carry the owning agent's identity; the dispatcher SHALL NOT re-dispatch the owning agent from that event, so a work agent that declares the revision status as a trigger status receives the rework run only through the direct rework enqueue and cannot win the `revision→in_progress` claim without the feedback — while other agents that also declare the revision status still wake. A revision cap SHALL escalate to a human.

#### Scenario: Approval finalizes

- **WHEN** a human approves an object in review
- **THEN** the object's `reviewed_by`/`reviewed_at` are set, `needs_review` is cleared, and its status becomes done

#### Scenario: Change request requires feedback and re-opens the work

- **WHEN** a human requests changes with non-empty feedback
- **THEN** the object's status becomes revision, the feedback is appended to its feedback history, and exactly one rework run is enqueued carrying that feedback history

#### Scenario: Empty feedback rejected

- **WHEN** a change request is submitted with empty feedback
- **THEN** the request is rejected

#### Scenario: Owning agent is not re-dispatched by its own status_changed event

- **WHEN** request-changes transitions an object to revision and the owning agent also declares `revision` as a trigger status
- **THEN** the owning agent is not woken by the `status_changed` event that request-changes emitted, and receives exactly the one feedback-carrying rework run

#### Scenario: Another work agent still wakes on the revision trigger status

- **WHEN** request-changes transitions an object to revision and a different work agent declares `revision` as a trigger status
- **THEN** that agent is woken by the `status_changed` event

#### Scenario: Revision cap escalates

- **WHEN** an object has been returned for rework beyond the configured cap
- **THEN** it is escalated to a human and is not re-enqueued again
