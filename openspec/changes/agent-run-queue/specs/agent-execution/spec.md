## MODIFIED Requirements

### Requirement: Agent Run Lifecycle Tracking

The system SHALL create an AgentRun record before execution begins and update it upon completion or failure.

#### Scenario: Successful execution

- **WHEN** an agent executes successfully
- **THEN** an AgentRun record SHALL be created with `status: running` before execution
- **AND** updated to `status: success` after execution
- **AND** `summary` SHALL contain the agent's final text output
- **AND** `duration` SHALL reflect the wall-clock execution time
- **AND** `completed_at` SHALL be set to the completion timestamp

#### Scenario: Failed execution

- **WHEN** an agent execution encounters an error
- **THEN** the AgentRun SHALL be updated to `status: error`
- **AND** `error_message` SHALL contain the error description
- **AND** `duration` and `completed_at` SHALL still be set

#### Scenario: Cancelled execution

- **WHEN** an agent execution is cancelled via context cancellation
- **THEN** the AgentRun SHALL be updated to `status: cancelled`
- **AND** `summary` SHALL contain any partial output produced before cancellation

#### Scenario: Queued run lifecycle

- **WHEN** `trigger_agent` routes to an agent with `dispatch_mode: queued`
- **THEN** an AgentRun record SHALL be created with `status: queued` immediately
- **AND** a corresponding `kb.agent_run_jobs` row SHALL be inserted atomically
- **AND** the run SHALL transition to `status: running` when a worker claims the job
- **AND** the run SHALL transition to `status: success` or `status: error` when the worker finishes

## ADDED Requirements

### Requirement: Orphan Recovery for Queued Runs

On server startup, the system SHALL recover agent runs that were left in a transient state due to server restart.

#### Scenario: Running runs marked as error on startup

- **WHEN** the server starts and finds `kb.agent_runs` rows with `status: running`
- **THEN** the system SHALL update those rows to `status: error` with `error_message: "server restarted during execution"`

#### Scenario: Queued runs re-enqueued on startup

- **WHEN** the server starts and finds `kb.agent_runs` rows with `status: queued` that have no active job in `kb.agent_run_jobs`
- **THEN** the system SHALL insert a new `kb.agent_run_jobs` row for each orphaned queued run so the worker pool can pick them up
