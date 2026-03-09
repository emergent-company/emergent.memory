## ADDED Requirements

### Requirement: Structured trigger_agent Input

The `trigger_agent` tool SHALL accept a JSON object as its message parameter instead of a plain string. The object SHALL have two optional fields: `instructions` (freeform string describing what the agent should do) and `task_id` (advisory reference to a graph Task object). Both fields are optional. `task_id` is a hint passed through to the agent's initial message — the system does not validate it or perform any lookup; the agent's prompt decides how to use it.

#### Scenario: Instructions only

- **WHEN** an agent calls `trigger_agent` with `{ "instructions": "Research Go 1.22 release date" }`
- **THEN** the system SHALL pass the instructions string as the agent's initial user message

#### Scenario: Instructions with task_id

- **WHEN** an agent calls `trigger_agent` with `{ "instructions": "Research Go 1.22 release date", "task_id": "task_abc123" }`
- **THEN** the system SHALL construct an initial message that includes both the instructions and the task_id hint
- **AND** the sub-agent's prompt SHALL determine how to use the task_id (e.g. query the graph for that task's details)

#### Scenario: Empty message object

- **WHEN** an agent calls `trigger_agent` with `{}`  (no fields set)
- **THEN** the system SHALL use the agent definition's default prompt as the initial message
- **AND** execution SHALL proceed normally

### Requirement: Get Run Status Tool

The system SHALL provide a `get_run_status` MCP tool that returns the current status and result of an agent run by ID.

#### Scenario: Run is still queued or running

- **WHEN** an agent calls `get_run_status` with a `run_id` that is in `status: queued` or `status: running`
- **THEN** the tool SHALL return `{ "run_id": "<id>", "status": "queued" | "running" }`
- **AND** no `result` field SHALL be included

#### Scenario: Run completed successfully

- **WHEN** an agent calls `get_run_status` with a `run_id` that is in `status: success`
- **THEN** the tool SHALL return `{ "run_id": "<id>", "status": "success", "result": "<summary>" }`

#### Scenario: Run failed

- **WHEN** an agent calls `get_run_status` with a `run_id` that is in `status: error`
- **THEN** the tool SHALL return `{ "run_id": "<id>", "status": "error", "error": "<error_message>" }`

#### Scenario: Run not found

- **WHEN** an agent calls `get_run_status` with a `run_id` that does not exist in `kb.agent_runs`
- **THEN** the tool SHALL return an error result indicating the run was not found

#### Scenario: Cross-project isolation

- **WHEN** an agent calls `get_run_status` with a `run_id` belonging to a different project
- **THEN** the tool SHALL return an error result indicating the run was not found
- **AND** SHALL NOT expose that the run exists in another project

## MODIFIED Requirements

### Requirement: Spawn Agents Tool

The system SHALL provide a `spawn_agents` tool that enables a parent agent to execute one or more sub-agents in parallel.

#### Scenario: Parallel sub-agent execution

- **WHEN** an agent calls `spawn_agents` with an array of spawn requests, each specifying `agent_name` and `task`
- **THEN** the system SHALL look up each agent by name in `kb.agent_definitions`
- **AND** spawn each sub-agent as a separate goroutine
- **AND** wait for all sub-agents to complete (using `sync.WaitGroup`)
- **AND** return an array of SpawnResult objects

#### Scenario: Spawn result structure

- **WHEN** sub-agents complete execution
- **THEN** each SpawnResult SHALL include: `run_id` (for potential resumption), `status` (completed/paused/failed/cancelled), `summary` (final text output), and `steps` (total steps executed)

#### Scenario: Mixed agent types in single spawn

- **WHEN** `spawn_agents` is called with requests for different agent types (e.g., one `research-assistant` and one `paper-summarizer`)
- **THEN** each sub-agent SHALL be built from its own AgentDefinition
- **AND** each sub-agent SHALL get its own tools (from its own definition, not the parent's)

#### Scenario: Spawn with timeout

- **WHEN** a spawn request includes a `timeout` parameter
- **THEN** the sub-agent's execution SHALL be bounded by that timeout
- **AND** the timeout SHALL override the agent definition's `default_timeout`

#### Scenario: Spawn with resume

- **WHEN** a spawn request includes a `resume_run_id` parameter referencing a paused AgentRun
- **THEN** the system SHALL load the prior AgentRun and its persisted messages
- **AND** reconstruct the LLM conversation from the persisted AgentRunMessages
- **AND** append a continuation message ("Continue your work...")
- **AND** the step counter SHALL carry forward cumulatively from the prior run
- **AND** the new execution SHALL have a fresh step budget (max_steps applies per resume)

#### Scenario: Invalid agent name

- **WHEN** a spawn request references an `agent_name` that does not exist in `kb.agent_definitions`
- **THEN** that specific spawn request SHALL fail with an error in its SpawnResult
- **AND** other spawn requests in the same call SHALL NOT be affected

#### Scenario: Queued agents not spawnable inline

- **WHEN** `spawn_agents` is called with an `agent_name` whose `dispatch_mode` is `queued`
- **THEN** the system SHALL return an error for that spawn request indicating that queued agents cannot be spawned inline
- **AND** the caller SHALL be directed to use `trigger_agent` instead
