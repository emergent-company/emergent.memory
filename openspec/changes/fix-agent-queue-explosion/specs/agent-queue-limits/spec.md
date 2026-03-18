## ADDED Requirements

### Requirement: System SHALL enforce maximum pending jobs per agent

The system SHALL reject creation of new queued agent runs when the agent already has 10 or more pending jobs in the queue.

#### Scenario: New run rejected when queue full
- **WHEN** agent has 10 pending jobs in kb.agent_run_jobs with status='pending'
- **AND** user attempts to create a new queued run for that agent
- **THEN** system SHALL return error "agent has 10 pending jobs (max 10)"
- **AND** system SHALL NOT create a new agent_run or agent_run_jobs record

#### Scenario: New run accepted when queue has capacity
- **WHEN** agent has 9 pending jobs
- **AND** user attempts to create a new queued run
- **THEN** system SHALL create the new run successfully
- **AND** system SHALL increment pending job count to 10

#### Scenario: Cron trigger skips execution when queue full
- **WHEN** cron schedule fires for an agent
- **AND** agent has 10 pending jobs
- **THEN** system SHALL log "skipping cron trigger, queue full"
- **AND** system SHALL NOT create a new agent run
- **AND** system SHALL NOT execute the agent

#### Scenario: Parent re-enqueue skips when parent queue full
- **WHEN** child agent completes successfully
- **AND** parent agent has 10 pending jobs
- **THEN** system SHALL log "skipping parent re-enqueue, queue full" with child result
- **AND** system SHALL NOT create a new queued run for parent
- **AND** child run SHALL still complete with status=success

### Requirement: System SHALL count only pending and processing jobs

The queue depth check SHALL only count jobs with status='pending' or status='processing', excluding completed and failed jobs.

#### Scenario: Completed jobs do not count toward limit
- **WHEN** agent has 15 completed jobs and 5 pending jobs
- **THEN** system SHALL report queue depth as 5
- **AND** system SHALL allow new runs to be created

#### Scenario: Failed jobs do not count toward limit
- **WHEN** agent has 20 failed jobs and 8 pending jobs
- **THEN** system SHALL report queue depth as 8
- **AND** system SHALL allow new runs to be created

### Requirement: Queue depth limit SHALL be configurable

The maximum pending jobs limit SHALL be configurable via environment variable AGENT_MAX_PENDING_JOBS with default value 10.

#### Scenario: Custom queue depth limit
- **WHEN** AGENT_MAX_PENDING_JOBS environment variable is set to 25
- **AND** agent has 24 pending jobs
- **THEN** system SHALL allow new run creation
- **WHEN** agent has 25 pending jobs
- **THEN** system SHALL reject new run creation

#### Scenario: Default queue depth limit
- **WHEN** AGENT_MAX_PENDING_JOBS is not set
- **THEN** system SHALL use default value of 10 as the maximum pending jobs

### Requirement: System SHALL provide efficient queue depth queries

The system SHALL use an indexed query to count pending jobs for an agent in O(1) time complexity.

#### Scenario: Query uses index on agent_run_jobs
- **WHEN** counting pending jobs for an agent
- **THEN** system SHALL query kb.agent_run_jobs with WHERE clause on (agent_id via join, status)
- **AND** query SHALL use composite index for performance
- **AND** query execution time SHALL be < 50ms for agents with < 10,000 total jobs
