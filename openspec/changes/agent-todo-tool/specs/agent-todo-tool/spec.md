## ADDED Requirements

### Requirement: Todo Update Tool

The system SHALL provide a `todo_update` ADK tool that allows an agent to replace its entire task list for the current run.

#### Scenario: Agent updates todos with a valid list

- **WHEN** an agent calls `todo_update` with a `todos` array of `[{content, status, priority}]`
- **THEN** the system SHALL delete all existing todo rows for the current `run_id`
- **AND** insert the new list with `position` set to the array index (0-based)
- **AND** return a success result containing the count of todos written

#### Scenario: Agent clears the todo list

- **WHEN** an agent calls `todo_update` with an empty `todos` array
- **THEN** the system SHALL delete all existing todo rows for the current `run_id`
- **AND** return a success result indicating zero todos

#### Scenario: Invalid todo item fields

- **WHEN** an agent calls `todo_update` with a todo item missing `content`
- **THEN** the tool SHALL return an error result describing the missing field
- **AND** the existing todo list SHALL remain unchanged

#### Scenario: Valid status values

- **WHEN** an agent provides todo items with `status`
- **THEN** the system SHALL accept exactly: `pending`, `in_progress`, `completed`, `cancelled`
- **AND** reject any other value with an error result

#### Scenario: Valid priority values

- **WHEN** an agent provides todo items with `priority`
- **THEN** the system SHALL accept exactly: `high`, `medium`, `low`
- **AND** reject any other value with an error result

### Requirement: Todo Read Tool

The system SHALL provide a `todo_read` ADK tool that returns the current task list for the run.

#### Scenario: Agent reads current todos

- **WHEN** an agent calls `todo_read` with no parameters
- **THEN** the system SHALL return all todo rows for the current `run_id`, ordered by `position` ascending
- **AND** each item SHALL include `content`, `status`, and `priority`

#### Scenario: No todos exist

- **WHEN** an agent calls `todo_read` and no todos have been set for the run
- **THEN** the system SHALL return an empty list

### Requirement: Todo Tool Opt-In

The todo tools SHALL only be injected into an agent's ADK pipeline when the agent definition explicitly opts in.

#### Scenario: Agent opts in via tools list

- **WHEN** an agent definition's `tools` array contains `"todo_update"`
- **THEN** both `todo_update` and `todo_read` SHALL be added to the agent's resolved tool set
- **AND** the system prompt SHALL be augmented with a `## Task Management` guidance section

#### Scenario: Agent does not opt in

- **WHEN** an agent definition's `tools` array does not contain `"todo_update"` or `"todo_read"`
- **THEN** neither tool SHALL appear in the agent's ADK pipeline
- **AND** no task management section SHALL be added to the system prompt

#### Scenario: Opt-in with either name activates both

- **WHEN** an agent definition's `tools` array contains `"todo_read"` but not `"todo_update"`
- **THEN** both `todo_update` and `todo_read` SHALL still be added to the agent's resolved tool set

### Requirement: Todo Persistence

The system SHALL persist agent run todos in a dedicated `kb.agent_run_todos` table.

#### Scenario: Todos stored per run

- **WHEN** `todo_update` is called during a run
- **THEN** the system SHALL persist rows in `kb.agent_run_todos` keyed by `(run_id, position)`
- **AND** deleting a run SHALL cascade-delete its todos

#### Scenario: Transactional replace

- **WHEN** `todo_update` executes the full-replace
- **THEN** the delete and re-insert SHALL occur within a single database transaction
- **AND** a partial failure SHALL leave the prior todo list intact

### Requirement: Todo Read API Endpoint

The system SHALL expose a read-only HTTP endpoint to retrieve the todo list for a given agent run.

#### Scenario: Authenticated read of run todos

- **WHEN** a client sends `GET /agents/runs/:runID/todos` with a valid JWT
- **THEN** the system SHALL return `200 OK` with a JSON array of `{content, status, priority, position}` objects ordered by position
- **AND** the endpoint SHALL require the caller to have access to the project the run belongs to

#### Scenario: Run not found

- **WHEN** a client requests todos for a `runID` that does not exist
- **THEN** the system SHALL return `404 Not Found`

#### Scenario: Run with no todos

- **WHEN** a client requests todos for a valid run that has no todos
- **THEN** the system SHALL return `200 OK` with an empty JSON array

### Requirement: Agent System Prompt Guidance

When todo tools are active, the system SHALL augment the agent's system prompt with guidance on how to use the tools.

#### Scenario: Guidance injected for opted-in agents

- **WHEN** an agent definition opts in to todo tools
- **THEN** the executor SHALL append a `## Task Management` section to the system instruction before the run starts
- **AND** the section SHALL instruct the agent to use `todo_update` proactively, keep only one item `in_progress` at a time, and mark items `completed` immediately when done

#### Scenario: Full-replace rule communicated

- **WHEN** the `todo_update` tool description is sent to the LLM
- **THEN** the description SHALL explicitly state that the agent must send the complete updated list on every call
