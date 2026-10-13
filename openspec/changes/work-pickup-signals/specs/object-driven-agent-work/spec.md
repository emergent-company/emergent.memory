## MODIFIED Requirements

### Requirement: Listening agents subscribe to a type and are deduplicated

An agent SHALL subscribe to work through a reaction trigger over an object type and **one** pickup signal: `created`, `updated`, `status_changed`, or `assigned`. A work agent (a definition carrying a work config) SHALL declare exactly one object type and exactly one pickup signal; `deleted` SHALL be rejected as a work signal, and a work agent SHALL require `concurrencyStrategy: skip` (empty and `parallel` both mean no concurrency control and SHALL be rejected). The `status_changed` signal SHALL be scoped to the agent's `triggerStatuses` (defaulting to the built-in `ready`), the `updated` signal SHALL match when the changed property paths (RFC 6901 JSON Pointers) intersect the agent's declared `properties` — or, when the agent declares no properties, on any emitted content change — and the `assigned` signal SHALL match when the new assignee equals the agent's name. Dispatch SHALL be deduplicated so a repeated delivery for the same agent, object, and object version results in a single run. The dedup key SHALL use the object's canonical identity (`canonical_id`) so a version bump does not defeat dedup.

#### Scenario: Created object wakes a listener

- **WHEN** an object of a type an agent listens to is created
- **THEN** the agent is woken for that object

#### Scenario: Duplicate delivery deduplicated

- **WHEN** the same pickup event for the same agent and object version is delivered twice
- **THEN** only one run is created, using the existing processing-log dedup key

#### Scenario: Status transition wakes a status_changed listener

- **WHEN** an object transitions into a status listed in an agent's `triggerStatuses`
- **THEN** a `status_changed` event is emitted after the transition commits and the agent is woken for that object

#### Scenario: Status transition outside the trigger set is ignored

- **WHEN** an object transitions into a status not listed in the agent's `triggerStatuses`
- **THEN** the agent is not woken by that transition

#### Scenario: Property edit wakes an updated listener on a declared path

- **WHEN** an object is updated and its changed property paths intersect the agent's declared `properties`
- **THEN** the agent is woken for that object

#### Scenario: Updated listener with no declared properties wakes on any change

- **WHEN** an object is updated (a real content change) and the listening agent declares no `properties`
- **THEN** the agent is woken for that object

#### Scenario: Property edit outside the declared paths is ignored

- **WHEN** an object is updated and none of its changed property paths intersect the agent's declared `properties`
- **THEN** the agent is not woken by that update

#### Scenario: Edits do not wake listeners

- **WHEN** a work object is updated and the listening agent does not subscribe to `updated` (or the changed paths do not intersect its declared `properties`)
- **THEN** the agent is not woken by that update

#### Scenario: Assignment wakes the assigned listener

- **WHEN** an object's assignee is changed to an agent's name and that agent listens via `assigned`
- **THEN** an `assigned` event is emitted after the reassignment commits and that agent is woken for the object

#### Scenario: Invalid work signal is rejected

- **WHEN** a work agent declares `deleted`, more than one signal, more than one object type, or a concurrency strategy other than `skip`
- **THEN** the agent create/update (and blueprint apply) is rejected

### Requirement: Correct, crash-safe claim

Claiming SHALL transition the object to the in-progress status under the object's advisory lock and the versioned write model. The claim's source status SHALL depend on the pickup signal: `created` and `updated` claims SHALL transition from the ready status; a `status_changed` claim SHALL transition from the matched trigger status; an `assigned` claim SHALL transition from the object's current HEAD status. A lost race SHALL leave the run skipped without consuming the failure budget.

#### Scenario: Claim is serialized

- **WHEN** two runs are dispatched for the same object
- **THEN** only one transition to in-progress succeeds and the other run is skipped

#### Scenario: Skipped claim does not burn budget

- **WHEN** a run's claim finds the object already taken
- **THEN** the run is marked skipped, the job completes, and the item's failure budget is unchanged

#### Scenario: status_changed claim CASes from the matched status

- **WHEN** a `status_changed` run claims an object that has since moved off the matched status
- **THEN** the claim loses the race and the run is skipped

#### Scenario: Board-enabled types require a stable key

- **WHEN** a board-enabled work object is created without a key
- **THEN** the write is rejected, because the claim requires a stable identity

#### Scenario: Subgraph creation rejects a keyless board-enabled object

- **WHEN** a board-enabled work object is created via the subgraph/bulk path (`POST /api/graph/subgraph`) without a key
- **THEN** the request is rejected with `work_status_key_required` and no object or relationship is created

### Requirement: Per-agent review on the main graph, with explicit rework

Review SHALL use the object's `needs_review`/`reviewed_by`/`reviewed_at` columns on the main graph; this change builds the write path for those (currently dormant) fields. Approval SHALL finalize; a change request SHALL record append-only feedback and transition the object to revision, and SHALL enqueue a rework run directly that carries the full feedback history (so the worker recognises it as a rework run and sees why it is re-running); the review→revision transition SHALL still emit a `status_changed` event for other consumers, and a work agent that also declares the revision status as a trigger status is protected from double-dispatch by its required `concurrencyStrategy: skip`. A revision cap SHALL escalate to a human.

#### Scenario: Approval finalizes

- **WHEN** a human approves an object in review
- **THEN** the object's `reviewed_by`/`reviewed_at` are set, `needs_review` is cleared, and its status becomes done

#### Scenario: Change request requires feedback and re-opens the work

- **WHEN** a human requests changes with non-empty feedback
- **THEN** the object's status becomes revision, the feedback is appended to its feedback history, and exactly one rework run is enqueued carrying that feedback history

#### Scenario: Empty feedback rejected

- **WHEN** a change request is submitted with empty feedback
- **THEN** the request is rejected

#### Scenario: Revision cap escalates

- **WHEN** an object has been returned for rework beyond the configured cap
- **THEN** it is escalated to a human and is not re-enqueued again
