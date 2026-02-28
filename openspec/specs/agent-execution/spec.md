# agent-execution Specification

## Purpose
TBD - created by archiving change multi-agent-coordination. Update Purpose after archive.
## Requirements
### Requirement: Agent Pipeline Construction

The system SHALL build an ADK-Go pipeline from an AgentDefinition, creating a Gemini model via Vertex AI and wiring the agent's resolved tools into the pipeline.

#### Scenario: Single-flow agent execution

- **WHEN** an AgentDefinition with `flow_type: "single"` is executed with an input string
- **THEN** the system SHALL create a Gemini model using the definition's model config (provider, name, temperature)
- **AND** resolve the agent's tools via ResolveTools
- **AND** build a single ADK agent with the definition's system prompt
- **AND** execute the pipeline against the input
- **AND** return the LLM's final text output as the run summary

#### Scenario: Sequential-flow agent execution

- **WHEN** an AgentDefinition with `flow_type: "sequential"` is executed
- **THEN** the system SHALL wrap the agent in a sequential pipeline that processes multiple steps in order

#### Scenario: Loop-flow agent execution

- **WHEN** an AgentDefinition with `flow_type: "loop"` is executed
- **THEN** the system SHALL wrap the agent in a loop pipeline with a configurable max iteration count

### Requirement: Agent Run Lifecycle Tracking

The system SHALL create an AgentRun record before execution begins and update it upon completion or failure.

#### Scenario: Successful execution

- **WHEN** an agent executes successfully
- **THEN** an AgentRun record SHALL be created with `status: running` before execution
- **AND** updated to `status: completed` after execution
- **AND** `summary` SHALL contain the agent's final text output
- **AND** `duration` SHALL reflect the wall-clock execution time
- **AND** `completed_at` SHALL be set to the completion timestamp

#### Scenario: Failed execution

- **WHEN** an agent execution encounters an error
- **THEN** the AgentRun SHALL be updated to `status: failed`
- **AND** `error_message` SHALL contain the error description
- **AND** `duration` and `completed_at` SHALL still be set

#### Scenario: Cancelled execution

- **WHEN** an agent execution is cancelled via context cancellation
- **THEN** the AgentRun SHALL be updated to `status: cancelled`
- **AND** `summary` SHALL contain any partial output produced before cancellation

### Requirement: Step Limit Enforcement

The system SHALL enforce step limits on agent execution, using soft stop followed by hard stop.

#### Scenario: Sub-agent default step limit

- **WHEN** a sub-agent is spawned without an explicit `max_steps` in its definition
- **THEN** the system SHALL apply a default of 50 steps

#### Scenario: Top-level agent unlimited steps

- **WHEN** a top-level agent (not spawned by another agent) has `max_steps: nil` in its definition
- **THEN** the system SHALL allow unlimited LLM iterations

#### Scenario: Soft stop at step limit

- **WHEN** an agent reaches its `max_steps` limit
- **THEN** the system SHALL inject a system message instructing the LLM to summarize progress and stop
- **AND** the LLM SHALL be given one final iteration to produce a summary
- **AND** tools SHALL be disabled for that final iteration

#### Scenario: Hard stop after soft stop ignored

- **WHEN** an agent reaches its step limit and the LLM attempts a tool call after the soft stop message
- **THEN** the system SHALL refuse to execute the tool call
- **AND** the AgentRun SHALL be updated to `status: paused`
- **AND** the run's partial summary SHALL be preserved

### Requirement: Timeout Enforcement

The system SHALL enforce execution timeouts using Go context deadlines with a grace period for soft stop.

#### Scenario: Timeout with graceful shutdown

- **WHEN** an agent's execution time exceeds its timeout (from definition's `default_timeout` or spawn parameter)
- **THEN** the system SHALL inject a "time's up, summarize" message
- **AND** wait up to 30 seconds for the LLM to produce a response
- **AND** if the LLM responds within the grace period, the AgentRun SHALL be `status: paused` with the summary

#### Scenario: Hard timeout after grace period

- **WHEN** an agent does not respond within the 30-second grace period after timeout
- **THEN** the system SHALL cancel the Go context (hard stop)
- **AND** the AgentRun SHALL be updated to `status: paused`
- **AND** partial results SHALL be preserved

#### Scenario: Spawn timeout overrides definition timeout

- **WHEN** a sub-agent is spawned with an explicit `timeout` parameter
- **THEN** the spawn timeout SHALL take precedence over the agent definition's `default_timeout`

### Requirement: Doom Loop Detection

The system SHALL detect and break agent doom loops where the same tool is called with identical arguments repeatedly.

#### Scenario: Detection threshold

- **WHEN** an agent makes 3 consecutive tool calls with the same tool name and identical argument hash
- **THEN** the system SHALL inject an error message instead of executing the third call
- **AND** the error message SHALL instruct the agent to try a different approach

#### Scenario: Hard stop after continued looping

- **WHEN** an agent continues to make identical tool calls after the doom loop warning (reaching 5 consecutive identical calls)
- **THEN** the system SHALL hard-stop the agent
- **AND** the AgentRun SHALL be updated to `status: failed`
- **AND** `error_message` SHALL indicate doom loop termination

#### Scenario: Non-identical calls reset counter

- **WHEN** an agent makes a tool call with a different tool name or different arguments after a doom loop warning
- **THEN** the consecutive counter SHALL reset to 1
- **AND** execution SHALL continue normally

### Requirement: Trigger Endpoint Activation

The existing agent trigger endpoint SHALL execute agents using the AgentExecutor instead of marking runs as skipped.

#### Scenario: Manual trigger via API

- **WHEN** a POST request is made to `/api/projects/:id/agents/:agentId/trigger`
- **THEN** the system SHALL build an ADK-Go pipeline from the agent's definition
- **AND** execute it with the request body as input
- **AND** return the AgentRun record with execution results
- **AND** the run SHALL NOT have `status: skipped`

