# agent-coordination-tools Specification

## Purpose
TBD - created by archiving change multi-agent-coordination. Update Purpose after archive.
## Requirements
### Requirement: List Available Agents Tool

The system SHALL provide a `list_available_agents` tool that agents can call to discover other agents in the project's catalog.

#### Scenario: Agent catalog query

- **WHEN** an agent calls `list_available_agents` with no parameters
- **THEN** the tool SHALL return all agent definitions for the current project from `kb.agent_definitions`
- **AND** each result SHALL include: name, description, tools list, flow_type, and visibility
- **AND** the result SHALL NOT include full system prompts (to keep context windows manageable)

#### Scenario: All visibility levels included

- **WHEN** an agent calls `list_available_agents`
- **THEN** agents with visibility `external`, `project`, AND `internal` SHALL all be included
- **AND** visibility level SHALL be indicated in the response for each agent

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

### Requirement: Context Propagation for Cancellation

The system SHALL propagate Go context cancellation to all spawned sub-agents.

#### Scenario: Parent cancellation cascades

- **WHEN** a parent agent's context is cancelled (e.g., due to timeout)
- **THEN** all of its spawned sub-agents SHALL also receive context cancellation
- **AND** sub-agents SHALL stop execution and persist their partial state

#### Scenario: Individual sub-agent timeout

- **WHEN** a single sub-agent's timeout fires while other sub-agents are still running
- **THEN** only the timed-out sub-agent SHALL be stopped
- **AND** other sub-agents SHALL continue until completion or their own timeout

