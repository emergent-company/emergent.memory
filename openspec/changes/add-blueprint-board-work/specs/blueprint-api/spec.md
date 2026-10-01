## ADDED Requirements

### Requirement: Blueprint manifests declare board-enabled object types

A blueprint object-type SHALL be able to declare `boardEnabled: true`, an
`allowedStatuses` list of work-status values, and the operational flags
`skipEmbeddings`, `skipExtraction`, and `excludeFromSearch`. These keys SHALL be
carried through the manifest loader and the apply path into the schema registry
unchanged.

#### Scenario: Board-enabled type applied

- **WHEN** a blueprint manifest defines an object type with `boardEnabled:
  true`, `allowedStatuses: [ready, in_progress, review, revision, blocked,
  done]`, and the three operational flags
- **THEN** the applied schema pack preserves those fields on the object type

#### Scenario: Operational flags applied

- **WHEN** a blueprint object type sets `skipEmbeddings`, `skipExtraction`, and
  `excludeFromSearch` to true
- **THEN** the applied type excludes its objects from embeddings, extraction,
  and default search

### Requirement: Blueprint manifests wire agent work and reactions

A blueprint agent definition SHALL be able to declare `workConfig`
(including `requiresReview`, `failureLimit`, and `workContract`), `triggerType`,
`reactionConfig` (object types, events, and concurrency strategy), and a
non-empty `cronSchedule`. These keys SHALL be carried through the manifest
loader and the apply path into the agent definition unchanged.

#### Scenario: Reaction-triggered worker applied

- **WHEN** a blueprint agent declares `triggerType: reaction`,
  `dispatchMode: queued`, and a `reactionConfig` with `objectTypes: [Task]`,
  `events: [created]`, and `concurrencyStrategy: skip`
- **THEN** the applied agent definition wakes on created `Task` objects,
  enqueues rather than running inline, and skips concurrent triggers for the
  same object

#### Scenario: Work config applied

- **WHEN** a blueprint agent declares a `workConfig` with `requiresReview:
  true`, a non-zero `failureLimit`, and `workContract.requireArtifacts: true`
- **THEN** the applied agent definition requires review before done, enforces
  the failure budget, and requires artifacts on completion

### Requirement: Blueprint seed objects carry an assignee

A blueprint seed object SHALL be able to declare an `assignee`. On apply, the
seed object's assignee SHALL be stored, routing the object to the matching
listening agent within its project.

#### Scenario: Assigned seed object

- **WHEN** a blueprint seed object of a board-enabled type declares
  `assignee: task-worker`
- **THEN** the applied object carries that assignee and is routed to the
  matching listener when it is `ready`
