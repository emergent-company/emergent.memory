# agent-infrastructure Specification Delta

## ADDED Requirements

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

## MODIFIED Requirements

### Requirement: Agent Persistence

The system MUST persist agent configurations and execution history to the database.

#### Scenario: Schema Definition

Given a fresh database
When migrations are run
Then the `agents` and `agent_runs` tables should exist
And `agents` should have `prompt`, `cron_schedule`, `role`, `trigger_type`, `reaction_config`, `execution_mode`, and `capabilities` columns.

#### Scenario: Reaction Agent Persistence

- **GIVEN** a reaction agent is created
- **WHEN** the agent configuration is saved
- **THEN** the `reaction_config` JSONB column SHALL contain the object types, events, and concurrency settings
- **AND** the `execution_mode` column SHALL contain the execution mode
- **AND** the `capabilities` JSONB column SHALL contain the capability restrictions

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
