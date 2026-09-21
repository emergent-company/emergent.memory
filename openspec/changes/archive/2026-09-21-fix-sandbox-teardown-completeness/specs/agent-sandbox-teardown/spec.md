## Purpose

Guarantees that a provisioned agent sandbox is torn down exactly once per run, regardless of how the run ends, and that sandbox rows left without a live owner are recovered after a restart so their containers and volumes are reclaimed rather than lingering until the 30-day TTL.

## ADDED Requirements

### Requirement: Every provisioned sandbox SHALL be torn down exactly once per run

Teardown of a run's provisioned sandbox SHALL occur exactly once for every exit path of that run — normal completion, error return, context cancellation, panic, and streaming abort — without depending on the caller invoking a cleanup function. A second teardown for the same run SHALL be a safe no-op.

#### Scenario: Teardown runs when the caller never invokes cleanup
- **GIVEN** a run provisions a sandbox
- **WHEN** the run completes and the caller does not invoke any cleanup function
- **THEN** the sandbox SHALL be torn down exactly once

#### Scenario: Teardown runs on a panic
- **GIVEN** a run has provisioned a sandbox
- **AND** execution panics before returning a result
- **WHEN** the panic is recovered by the surrounding handler
- **THEN** the sandbox SHALL be torn down
- **AND** the run SHALL be recorded as failed

#### Scenario: Teardown runs on context cancellation
- **GIVEN** a run has provisioned a sandbox
- **WHEN** the run's context is cancelled and execution returns early
- **THEN** the sandbox SHALL be torn down

#### Scenario: Repeated cleanup does not double-destroy
- **GIVEN** a run's sandbox has already been torn down
- **WHEN** cleanup is invoked again for the same run
- **THEN** no second destroy SHALL be issued
- **AND** no error SHALL be surfaced to the caller

#### Scenario: A failed teardown does not mask the run result
- **GIVEN** a run finished
- **AND** destroying its sandbox fails
- **WHEN** teardown completes
- **THEN** the failure SHALL be logged
- **AND** the run's own result SHALL be unaffected

### Requirement: Share-link runs SHALL tear down their sandboxes

The share-link execution path SHALL tear down its sandbox on every exit path, including early stream termination and run failure.

#### Scenario: A served share link leaves no running sandbox
- **WHEN** a share link is served with sandboxing enabled, for both a completed and an aborted stream
- **THEN** the sandbox SHALL be torn down
- **AND** no sandbox row SHALL remain in a non-stopped state for that run

### Requirement: Sandbox rows without a live owner SHALL be recovered after startup

When startup recovery determines that a run is no longer active, the sandbox rows linked to that run SHALL be transitioned out of a non-stopped state so that their containers and volumes become eligible for reclamation. Rows linked to a live run SHALL NOT be touched.

#### Scenario: Sandbox left behind by a crashed process is recovered
- **GIVEN** a previous process exited while a sandbox row was in a non-stopped state with no active run
- **WHEN** the server starts and runs orphan recovery
- **THEN** that sandbox row SHALL be transitioned out of its non-stopped state
- **AND** the row's container and volume SHALL become eligible for reclamation
- **AND** the recovery SHALL be logged with the row's identifier and previous status

#### Scenario: A live run's sandbox is never recovered
- **GIVEN** a sandbox row is linked to a run that is still active
- **WHEN** startup recovery runs
- **THEN** the row SHALL NOT be modified

#### Scenario: Recovery is idempotent
- **GIVEN** orphan recovery has already transitioned a sandbox row
- **WHEN** the server restarts again and recovery runs
- **THEN** the already-transitioned row SHALL NOT be modified further
- **AND** no additional destroy SHALL be issued for it

### Requirement: Teardown and reaping contracts SHALL be documented as implemented

Comments and doc comments describing teardown and persistent-workspace semantics SHALL match the implemented behaviour, and SHALL NOT assert a safety mechanism that does not exist.

#### Scenario: Documented guarantee exists in code
- **WHEN** a reader follows any comment describing teardown as guaranteed
- **THEN** the described mechanism SHALL exist in code at the referenced location
