## ADDED Requirements

### Requirement: Worker Pool Initialization

The system SHALL start a configurable worker pool on server startup that continuously polls for queued agent runs and executes them.

#### Scenario: Worker pool starts with server

- **WHEN** the server starts
- **THEN** the worker pool SHALL start N goroutines, where N is configured by `AGENT_WORKER_POOL_SIZE` (default 5)
- **AND** each worker SHALL begin polling `kb.agent_run_jobs` for pending jobs

#### Scenario: Worker pool disabled

- **WHEN** `AGENT_WORKER_POOL_SIZE` is set to `0`
- **THEN** no worker goroutines SHALL start
- **AND** queued agent runs SHALL accumulate in the DB but not execute until the pool is re-enabled

#### Scenario: Worker pool stops cleanly

- **WHEN** the server shuts down
- **THEN** the worker pool SHALL stop accepting new jobs
- **AND** in-progress workers SHALL be allowed to finish their current run (or be cancelled via context)
- **AND** all worker goroutines SHALL exit before shutdown completes

### Requirement: Job Claim with FOR UPDATE SKIP LOCKED

Each worker SHALL atomically claim one pending job from `kb.agent_run_jobs` using `FOR UPDATE SKIP LOCKED`, preventing duplicate execution by concurrent workers.

#### Scenario: Single job claimed once

- **WHEN** two workers poll simultaneously and one pending job exists
- **THEN** exactly one worker SHALL claim the job
- **AND** the other worker SHALL find no job and wait before retrying

#### Scenario: Worker polls with no jobs

- **WHEN** a worker polls and finds no pending jobs
- **THEN** the worker SHALL sleep for a configurable poll interval (default 5 seconds) before polling again

#### Scenario: Job status transition on claim

- **WHEN** a worker claims a job
- **THEN** the `kb.agent_run_jobs` row SHALL have `status` updated to `processing`
- **AND** the corresponding `kb.agent_runs` row SHALL have `status` updated to `running`
- **AND** both updates SHALL occur within the same DB transaction

### Requirement: Job Execution

After claiming a job, the worker SHALL execute the agent run by invoking the existing `executor.Execute()` path.

#### Scenario: Successful queued run

- **WHEN** a worker executes a claimed job and the agent completes successfully
- **THEN** the `kb.agent_run_jobs` row SHALL be updated to `completed`
- **AND** the `kb.agent_runs` row SHALL be updated to `success` with the result summary
- **AND** `completed_at` SHALL be set on the job row

#### Scenario: Failed queued run within retry budget

- **WHEN** a worker executes a claimed job and the agent returns an error
- **AND** `attempt_count < max_attempts` on the job row
- **THEN** the job row SHALL be updated to `pending` with `next_run_at` set to now + exponential backoff
- **AND** `attempt_count` SHALL be incremented
- **AND** the `kb.agent_runs` row SHALL remain associated with the job for the next attempt

#### Scenario: Failed queued run exceeding retry budget

- **WHEN** a worker executes a claimed job and the agent returns an error
- **AND** `attempt_count >= max_attempts`
- **THEN** the job row SHALL be updated to `failed`
- **AND** the `kb.agent_runs` row SHALL be updated to `error` with the error message

### Requirement: Startup Orphan Re-enqueue

On server startup, the system SHALL detect agent runs that were left in `queued` or `running` state and recover them.

#### Scenario: Re-enqueue orphaned queued runs

- **WHEN** the server starts and finds `kb.agent_runs` rows with `status: queued` that have no corresponding `pending` or `processing` row in `kb.agent_run_jobs`
- **THEN** the system SHALL insert a new `kb.agent_run_jobs` row for each such run
- **AND** the run's `status` SHALL remain `queued` until a worker claims it

#### Scenario: Mark orphaned running runs as error

- **WHEN** the server starts and finds `kb.agent_runs` rows with `status: running`
- **THEN** the system SHALL update those runs to `status: error` with message "server restarted during execution"
- **AND** their corresponding `kb.agent_run_jobs` rows (if any) SHALL be updated to `failed`

### Requirement: Agent Run Jobs Table

The system SHALL maintain a `kb.agent_run_jobs` table as the dispatch ledger for queued agent runs.

#### Scenario: Job row created on enqueue

- **WHEN** `trigger_agent` routes to a `queued` agent and creates an `agent_runs` row
- **THEN** a corresponding `kb.agent_run_jobs` row SHALL be created atomically in the same transaction
- **AND** the job row SHALL reference the `agent_runs` row via `run_id` foreign key

#### Scenario: Job row columns

- **WHEN** a job row is created
- **THEN** it SHALL contain: `id`, `run_id` (FK), `status` (`pending` | `processing` | `completed` | `failed`), `attempt_count` (default 0), `max_attempts`, `next_run_at`, `created_at`, `completed_at`
