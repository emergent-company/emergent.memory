## Purpose

The server-side guarantee that every agent run records the orchestration root it
belongs to: a top-level run roots to itself, a spawned sub-agent inherits its
delegator's root, and that root survives a queue hop. Root-based grouping is what
lets a consumer reassemble a delegation tree — the in-flight
`agent-run-preview` capability groups runs by this identifier and expects all
runs in a tree to share it.

The guarantee holds regardless of whether tracing is enabled, because the root is
a logical relationship between runs rather than a tracing artifact. Persisting it
must also never fail a run: a missed write costs observability, not execution.

## ADDED Requirements

### Requirement: Every run records its orchestration root

Each agent run SHALL persist the orchestration root it belongs to: a run with no
caller-supplied root and no previously stored root SHALL root to its own run id,
and a run executed on behalf of another SHALL inherit that caller's root
unchanged.

#### Scenario: Top-level run roots to itself

- **WHEN** a top-level run executes with no caller-supplied root
- **THEN** its persisted orchestration root is its own run id

#### Scenario: Spawned child inherits the delegator's root

- **WHEN** an agent run spawns a sub-agent through the delegation tool
- **THEN** the child's persisted orchestration root is the delegator's root, unchanged

#### Scenario: Caller override wins

- **WHEN** a caller supplies an orchestration root for a run that also has a stored root
- **THEN** the caller-supplied root is the one persisted

### Requirement: Root linkage does not depend on tracing

Root persistence SHALL NOT be conditional on an active tracing span. A run
executed while tracing is disabled SHALL still record its orchestration root,
while the trace identifier remains absent when there is no span.

#### Scenario: Root is written with tracing disabled

- **WHEN** a run executes with tracing disabled and therefore has no valid span
- **THEN** its orchestration root is persisted and its trace identifier is recorded as absent

#### Scenario: Root and trace are both written with tracing enabled

- **WHEN** a run executes with a valid span
- **THEN** both its orchestration root and its trace identifier are persisted

#### Scenario: A failed linkage write does not fail the run

- **WHEN** persisting the run's orchestration root fails
- **THEN** the failure is logged and the run continues

### Requirement: Root linkage survives queued execution

A run created through the queue SHALL be recorded with its orchestration root at
creation when one is known, and a run that is re-enqueued SHALL keep its original
orchestration root rather than rooting to its new run id.

#### Scenario: Queued run is created with its root

- **WHEN** a queued run is created while the caller's orchestration root is known
- **THEN** the created run row already carries that root

#### Scenario: Re-enqueued run keeps its original root

- **WHEN** a queued run is re-enqueued after its parent completes
- **THEN** the re-enqueued run's orchestration root is the original root, not the re-enqueued run's own id

#### Scenario: Shared root across a delegation tree

- **WHEN** a delegator run and the runs it spawned through a queue hop are read back
- **THEN** every run in that tree reports the same orchestration root
