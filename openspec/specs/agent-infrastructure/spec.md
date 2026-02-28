# agent-infrastructure Specification

## Purpose
TBD - created by archiving change add-agent-system. Update Purpose after archive.
## Requirements
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

### Requirement: Dynamic Scheduling

The system MUST automatically schedule jobs based on the `agents` table configuration.

#### Scenario: Bootstrap Scheduling

Given an enabled agent in the database with schedule `*/5 * * * *`
When the application starts
Then a CronJob should be registered in the `SchedulerRegistry`
And it should execute the agent logic every 5 minutes.

#### Scenario: Dynamic Update

Given a running agent
When the admin updates the `cron_schedule` via API
Then the old CronJob should be stopped
And a new CronJob with the new schedule should be started.

#### Scenario: Reaction Agent Bootstrap

- **GIVEN** an enabled reaction agent in the database
- **WHEN** the application starts
- **THEN** the `ReactionDispatcherService` SHALL subscribe to graph events
- **AND** events matching the agent's configuration SHALL trigger execution

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

### Requirement: Agent UI Trigger Types

The system SHALL allow project administrators to select the `webhook` trigger type for an agent in the Admin UI.

#### Scenario: Agent Creation

- **WHEN** an admin creates or edits an agent
- **THEN** they can select `webhook` from the trigger type dropdown.

### Requirement: Webhook Management UI

The system SHALL display a webhook hook management interface on the agent detail page.

#### Scenario: Webhook Details

- **WHEN** an admin navigates to the agent detail page for a `webhook` triggered agent
- **THEN** a section displaying all configured webhook hooks is rendered, including creation, listing, and deletion actions.

### Requirement: Reaction Trigger Type

The system MUST support a `reaction` trigger type that executes agents in response to graph object events.

#### Scenario: Agent Configuration with Reaction Trigger

- **WHEN** an agent is created with `trigger_type: 'reaction'`
- **THEN** the agent MUST have a valid `reaction_config` specifying which object types and events to react to

#### Scenario: Event Filtering by Object Type

- **GIVEN** a reaction agent configured for object type `Person`
- **WHEN** a `Company` object is created
- **THEN** the agent SHALL NOT be triggered
- **AND WHEN** a `Person` object is created
- **THEN** the agent SHALL be triggered

#### Scenario: Event Filtering by Event Type

- **GIVEN** a reaction agent configured for `created` events only
- **WHEN** an object is updated
- **THEN** the agent SHALL NOT be triggered
- **AND WHEN** an object is created
- **THEN** the agent SHALL be triggered

### Requirement: Actor Tracking

The system MUST track who made changes to graph objects to support loop prevention.

#### Scenario: User-Initiated Change

- **WHEN** a user creates or updates a graph object via the API
- **THEN** the `actor_type` SHALL be set to `user`
- **AND** the `actor_id` SHALL be set to the user's ID

#### Scenario: Agent-Initiated Change

- **WHEN** an agent creates or updates a graph object
- **THEN** the `actor_type` SHALL be set to `agent`
- **AND** the `actor_id` SHALL be set to the agent's ID

#### Scenario: Self-Trigger Prevention

- **GIVEN** a reaction agent with `ignoreSelfTriggered: true`
- **WHEN** that agent creates or updates an object
- **THEN** the same agent SHALL NOT be triggered by that change

### Requirement: Agent Processing Log

The system MUST track which objects have been processed by each agent to avoid duplicate processing.

#### Scenario: Processing Log Entry Creation

- **WHEN** a reaction agent is triggered for an object
- **THEN** a processing log entry SHALL be created with status `pending`
- **AND** the entry SHALL include the `agent_id`, `graph_object_id`, `object_version`, and `event_type`

#### Scenario: Processing Log Status Updates

- **WHEN** agent processing begins
- **THEN** the log entry status SHALL be updated to `processing`
- **AND WHEN** processing completes successfully
- **THEN** the status SHALL be updated to `completed`
- **AND WHEN** processing fails
- **THEN** the status SHALL be updated to `failed` with an error message

#### Scenario: Skip Duplicate Processing

- **GIVEN** a reaction agent with `concurrencyStrategy: 'skip'`
- **AND** the agent is currently processing object version 5
- **WHEN** a new event arrives for the same object at version 5
- **THEN** the new event SHALL be skipped

#### Scenario: Stuck Job Recovery

- **GIVEN** a processing log entry with status `processing`
- **AND** the entry has been in that status for more than 5 minutes
- **WHEN** the stuck job detector runs
- **THEN** the entry status SHALL be updated to `abandoned`

### Requirement: Execution Modes

The system MUST support different execution modes for reaction agents.

#### Scenario: Suggest Mode

- **GIVEN** a reaction agent with `execution_mode: 'suggest'`
- **WHEN** the agent determines a change should be made
- **THEN** the system SHALL create a task for human review instead of directly modifying the graph
- **AND** the task SHALL include the suggested changes and reasoning

#### Scenario: Execute Mode

- **GIVEN** a reaction agent with `execution_mode: 'execute'`
- **WHEN** the agent determines a change should be made
- **THEN** the system SHALL directly apply the change to the graph

#### Scenario: Hybrid Mode

- **GIVEN** a reaction agent with `execution_mode: 'hybrid'`
- **WHEN** the agent determines a change should be made
- **THEN** the system SHALL choose between suggesting or executing based on agent logic

### Requirement: Agent Capabilities

The system MUST enforce capability restrictions on what agents can do.

#### Scenario: Capability Restriction

- **GIVEN** a reaction agent with `capabilities.canDeleteObjects: false`
- **WHEN** the agent attempts to delete an object
- **THEN** the operation SHALL be rejected
- **AND** an error SHALL be logged

#### Scenario: Object Type Restriction

- **GIVEN** a reaction agent with `capabilities.allowedObjectTypes: ['Person', 'Company']`
- **WHEN** the agent attempts to create a `Document` object
- **THEN** the operation SHALL be rejected

### Requirement: Suggestion Task Resolution

The system MUST support approving and rejecting agent suggestions.

#### Scenario: Approve Suggestion

- **GIVEN** a pending suggestion task created by a reaction agent
- **WHEN** a user approves the suggestion
- **THEN** the suggested changes SHALL be applied to the graph
- **AND** the task status SHALL be updated to `completed`
- **AND** the change SHALL be attributed to the original agent

#### Scenario: Reject Suggestion

- **GIVEN** a pending suggestion task created by a reaction agent
- **WHEN** a user rejects the suggestion
- **THEN** no changes SHALL be made to the graph
- **AND** the task status SHALL be updated to `rejected`

