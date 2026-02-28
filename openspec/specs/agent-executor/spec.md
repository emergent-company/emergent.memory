# agent-executor Specification

## Purpose
TBD - created by archiving change bun-adk-session-service. Update Purpose after archive.
## Requirements
### Requirement: Resume Agent Context Restoration

The Agent Executor MUST inject the bun-backed ADK session service to seamlessly restore conversational context when resuming a paused agent run.

#### Scenario: Resuming a paused agent

- **WHEN** an administrator calls the resume API for a paused `AgentRun`
- **THEN** the Agent Executor retrieves the existing ADK session using the new database service
- **THEN** the ADK runner continues execution with the full historical context of prior messages and tool calls

### Requirement: Database Session Injection

The system MUST initialize the ADK `runner.New` with the `bun`-backed session service instead of `InMemoryService()`.

#### Scenario: Instantiating the ADK pipeline

- **WHEN** the `AgentExecutor.execute` pipeline is created
- **THEN** it connects to the PostgreSQL database to construct a persistent session service for tracking states and events

