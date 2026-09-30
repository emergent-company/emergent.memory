## Purpose

Named, priority-aware agent work queues on top of the existing `kb.agent_run_jobs` dispatch ledger: define a queue with its own concurrency, bind agents to it, route each run's dispatch job to the bound queue, claim queue-scoped, and observe live depth. Absent any configuration every run resolves to the `default` queue and behaves as today.

## ADDED Requirements

### Requirement: Define named queues

The system SHALL allow a project to define named agent work queues, each with a display name, optional description, a concurrency limit, a queue priority, and an enabled flag.

#### Scenario: Create a queue

- **WHEN** an operator creates a queue with a name and a concurrency of 3
- **THEN** the queue exists for the project with concurrency 3 and is enabled by default

#### Scenario: Duplicate queue name

- **WHEN** an operator creates a queue whose name already exists in the project
- **THEN** the system rejects the request with a conflict error and leaves the existing queue unchanged

#### Scenario: Invalid concurrency

- **WHEN** an operator creates or updates a queue with a concurrency below 1
- **THEN** the system rejects the request and the queue's concurrency is unchanged

### Requirement: Default queue always exists

The system SHALL ensure every project has a `default` queue. A run whose agent is not bound to any queue MUST be routed to `default`.

#### Scenario: Unbound agent routes to default

- **WHEN** a run is enqueued for an agent with no queue binding
- **THEN** the run's dispatch job is placed on the `default` queue

#### Scenario: Default queue is auto-created

- **WHEN** a project has no queues and a run is first enqueued
- **THEN** a `default` queue is created for the project and the run is placed on it

### Requirement: Bind an agent to a queue

The system SHALL allow an agent definition to declare a default queue, and SHALL allow a runtime agent to override that binding.

#### Scenario: Definition binding used

- **WHEN** an agent definition declares `defaultQueue = security-review` and its runtime agent sets no override
- **THEN** runs of that agent are enqueued on `security-review`

#### Scenario: Runtime override wins

- **WHEN** a runtime agent's config sets `queue = triage` while its definition declares `defaultQueue = security-review`
- **THEN** runs of that runtime agent are enqueued on `triage`

### Requirement: Route a run to its queue with priority

The system SHALL persist the resolved queue and priority on the run's dispatch job at enqueue time, so claiming requires no joins.

#### Scenario: Queue persisted on the job

- **WHEN** a run bound to `security-review` is enqueued
- **THEN** its `kb.agent_run_jobs` row records `queue = security-review`

#### Scenario: Default priority

- **WHEN** a run is enqueued with no priority configured
- **THEN** its dispatch job records the default priority

### Requirement: Claim jobs queue-scoped and priority-ordered

The system SHALL claim a pending dispatch job from a specified queue, choosing the lowest priority value first and otherwise the earliest `next_run_at`, while retaining the existing atomic job→processing and run→running transition and cancel-wins guard.

#### Scenario: Highest priority claimed first

- **WHEN** a queue holds two ready pending jobs with priorities 10 and 100
- **THEN** the worker claims the priority-10 job first

#### Scenario: Claim restricted to the queue

- **WHEN** a worker for queue A polls and queue B has pending jobs
- **THEN** the worker never claims a queue-B job

#### Scenario: Any-queue claim preserved

- **WHEN** a caller claims a job without specifying a queue
- **THEN** the earliest ready pending job across all queues is returned

### Requirement: Per-queue concurrency

The system SHALL run a bounded number of concurrent workers per enabled queue equal to that queue's concurrency, so a backed-up queue cannot consume the capacity of another.

#### Scenario: Concurrency respected

- **WHEN** a queue is configured with concurrency 2 and many jobs are pending
- **THEN** at most two jobs from that queue are processed simultaneously

#### Scenario: Separate queues do not starve each other

- **WHEN** queue A is saturated with long-running jobs and queue B receives a new job
- **THEN** queue B's job is still picked up by queue B's own workers

### Requirement: List queues with live depth

The system SHALL list a project's queues with their configuration and current depth (pending and processing job counts).

#### Scenario: Depth reported

- **WHEN** an operator lists queues and one queue has 3 pending and 1 processing jobs
- **THEN** that queue is reported with a pending count of 3 and a processing count of 1

### Requirement: Update and delete a queue

The system SHALL allow an operator to update a queue's concurrency, priority, enabled flag, display name, and description, and to delete a queue that has no pending or processing jobs.

#### Scenario: Disable a queue

- **WHEN** an operator disables a queue
- **THEN** its workers stop claiming new jobs and its pending jobs remain pending until it is re-enabled

#### Scenario: Delete a queue with work in flight

- **WHEN** an operator deletes a queue that still has pending or processing jobs
- **THEN** the system rejects the delete with a conflict error and the queue is left intact

### Requirement: Enqueue a work item onto a queue

The system SHALL provide an endpoint that creates a queued agent run on a specified queue, with an optional runtime agent/id selection and arbitrary trigger metadata.

#### Scenario: Enqueue with metadata

- **WHEN** a caller enqueues a work item on `security-review` with subject metadata
- **THEN** a queued run is created on `security-review` carrying that metadata

#### Scenario: Enqueue on a disabled queue

- **WHEN** a caller enqueues a work item on a disabled queue
- **THEN** the run is created but remains pending until the queue is re-enabled
