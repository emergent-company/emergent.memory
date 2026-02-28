# agent-state-persistence Specification

## Purpose
TBD - created by archiving change multi-agent-coordination. Update Purpose after archive.
## Requirements
### Requirement: Agent Run Message Persistence

The system SHALL persist every LLM message exchanged during an agent run to the `kb.agent_run_messages` table.

#### Scenario: Message persistence during execution

- **WHEN** an LLM message is sent or received during agent execution (system, user, assistant, or tool_result role)
- **THEN** an AgentRunMessage record SHALL be created with: run_id, role, content (as JSONB), step_number, and created_at
- **AND** the record SHALL be persisted during execution (not after completion) for crash recovery

#### Scenario: Message content format

- **WHEN** an assistant message containing tool calls is persisted
- **THEN** the `content` JSONB field SHALL preserve the complete message structure including tool call IDs, function names, and arguments
- **AND** the format SHALL be sufficient to reconstruct the full LLM conversation for resumption

### Requirement: Agent Run Tool Call Persistence

The system SHALL persist every tool invocation during an agent run to the `kb.agent_run_tool_calls` table.

#### Scenario: Successful tool call recording

- **WHEN** a tool is invoked and completes successfully during agent execution
- **THEN** an AgentRunToolCall record SHALL be created with: run_id, message_id (linking to the assistant message that triggered it), tool_name, input (JSONB), output (JSONB), status ("completed"), duration, step_number, and created_at

#### Scenario: Failed tool call recording

- **WHEN** a tool invocation returns an error
- **THEN** the AgentRunToolCall record SHALL have `status: "error"`
- **AND** the `output` JSONB SHALL contain the error details

#### Scenario: Tool call duration tracking

- **WHEN** a tool call is recorded
- **THEN** `duration` SHALL reflect the wall-clock time of the tool execution (not including LLM time)

### Requirement: Sub-Agent Resumption

The system SHALL support resuming paused agent runs with full conversation context preservation.

#### Scenario: Resume a paused run

- **WHEN** `spawn_agents` is called with `resume_run_id` referencing a run with `status: paused`
- **THEN** the system SHALL load all AgentRunMessages for that run ordered by step_number
- **AND** reconstruct the LLM conversation exactly as it was before the pause
- **AND** append a new user message instructing the agent to continue
- **AND** the new AgentRun SHALL have `resumed_from` set to the original run ID

#### Scenario: Cumulative step counter across resumes

- **WHEN** a paused run with `step_count: 45` is resumed
- **THEN** the new execution SHALL start its step count at 46
- **AND** `StepCount` on the AgentRun SHALL reflect the cumulative total across all resumes

#### Scenario: Fresh step budget per resume

- **WHEN** a run is resumed with `max_steps: 50`
- **THEN** the agent SHALL have 50 new steps available for this resume session
- **AND** the cumulative step count SHALL continue incrementing

#### Scenario: Max total steps enforcement

- **WHEN** an agent run's cumulative step count reaches MaxTotalStepsPerRun (500)
- **THEN** the system SHALL refuse to resume the run
- **AND** return an error indicating the maximum total steps have been exceeded

#### Scenario: Resume non-paused run rejected

- **WHEN** `spawn_agents` is called with `resume_run_id` referencing a run with `status: completed` or `status: failed`
- **THEN** the system SHALL return an error indicating only paused runs can be resumed

### Requirement: Agent Run History API

The system SHALL provide a progressive-disclosure API for inspecting agent runs with cursor-based pagination.

#### Scenario: List agent runs

- **WHEN** a GET request is made to `/api/projects/:id/agent-runs`
- **THEN** the system SHALL return a paginated list of AgentRun records
- **AND** support cursor-based pagination (not offset-based)
- **AND** support filters: status, agent_id, parent_run_id
- **AND** each record SHALL include: id, agent_id, status, step_count, duration, created_at, completed_at

#### Scenario: Get agent run detail

- **WHEN** a GET request is made to `/api/projects/:id/agent-runs/:runId`
- **THEN** the system SHALL return the full AgentRun record including summary, error_message, max_steps, parent_run_id, and resumed_from

#### Scenario: List messages for a run

- **WHEN** a GET request is made to `/api/projects/:id/agent-runs/:runId/messages`
- **THEN** the system SHALL return AgentRunMessage records with cursor-based pagination
- **AND** messages SHALL be ordered by step_number ascending

#### Scenario: Get single message

- **WHEN** a GET request is made to `/api/projects/:id/agent-runs/:runId/messages/:msgId`
- **THEN** the system SHALL return the full AgentRunMessage including complete content JSONB

#### Scenario: List tool calls for a run

- **WHEN** a GET request is made to `/api/projects/:id/agent-runs/:runId/tool-calls`
- **THEN** the system SHALL return AgentRunToolCall records with cursor-based pagination
- **AND** support filters: tool_name, status
- **AND** tool calls SHALL be ordered by step_number ascending

#### Scenario: Get single tool call

- **WHEN** a GET request is made to `/api/projects/:id/agent-runs/:runId/tool-calls/:callId`
- **THEN** the system SHALL return the full AgentRunToolCall including complete input and output JSONB

### Requirement: Extended Agent Run Schema

The system SHALL extend the existing `kb.agent_runs` table with additional columns for multi-agent coordination.

#### Scenario: New columns on agent_runs

- **WHEN** the database migration runs
- **THEN** `kb.agent_runs` SHALL have new nullable columns: `parent_run_id` (UUID FK), `step_count` (INT DEFAULT 0), `max_steps` (INT), `resumed_from` (UUID FK), `error_message` (TEXT), `completed_at` (TIMESTAMPTZ)
- **AND** existing rows SHALL not be affected (all new columns are nullable or have defaults)

#### Scenario: Parent-child run relationship

- **WHEN** a sub-agent is spawned by a parent agent
- **THEN** the sub-agent's AgentRun SHALL have `parent_run_id` set to the parent's run ID
- **AND** querying runs by `parent_run_id` SHALL return all child runs

