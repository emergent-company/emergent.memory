## MODIFIED Requirements

### Requirement: Agent Persistence

The system MUST persist agent configurations and execution history to the database.

#### Scenario: Schema Definition

- **WHEN** migrations are run on a fresh database
- **THEN** the `agents` and `agent_runs` tables SHALL exist
- **AND** `agents` SHALL have `prompt`, `cron_schedule`, and `role` columns
- **AND** `agent_runs` SHALL have additional columns: `parent_run_id` (UUID, nullable), `step_count` (INT, default 0), `max_steps` (INT, nullable), `resumed_from` (UUID, nullable), `error_message` (TEXT, nullable), `completed_at` (TIMESTAMPTZ, nullable)

#### Scenario: Agent Run Extended Fields

- **WHEN** an AgentRun is created for a sub-agent spawned by another agent
- **THEN** `parent_run_id` SHALL be set to the parent agent's run ID
- **AND** `step_count` SHALL track cumulative steps across resumes
- **AND** `max_steps` SHALL reflect the agent definition's step limit

### Requirement: Run Logging

The system MUST log every execution of an agent.

#### Scenario: Successful Run

- **WHEN** an agent executes successfully
- **THEN** an `AgentRun` record SHALL be created with `status: completed`
- **AND** `completed_at` SHALL be set
- **AND** `step_count` SHALL reflect total LLM iterations performed
- **AND** `error_message` SHALL be NULL

#### Scenario: Failed Run

- **WHEN** an agent throws an exception during execution
- **THEN** the `AgentRun` record SHALL be updated to `status: failed`
- **AND** `error_message` SHALL contain the error description
- **AND** `completed_at` SHALL be set to the failure time

#### Scenario: Paused Run

- **WHEN** an agent is stopped due to step limit or timeout
- **THEN** the `AgentRun` record SHALL be updated to `status: paused`
- **AND** `summary` SHALL contain the agent's partial progress summary
- **AND** the run SHALL be eligible for resumption via `resume_run_id`
