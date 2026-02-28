# task-dispatcher Specification

## Purpose
TBD - created by archiving change multi-agent-coordination. Update Purpose after archive.
## Requirements
### Requirement: Task DAG Walking

The system SHALL implement a TaskDispatcher that polls for available tasks and dispatches agents to execute them.

#### Scenario: Available task query

- **WHEN** the TaskDispatcher polls for available tasks
- **THEN** it SHALL query SpecTask objects in the knowledge graph where: status is "pending", no incoming "blocks" relationship from an incomplete task exists, and no agent is currently assigned
- **AND** tasks SHALL be returned ordered by priority (critical > high > medium > low)

#### Scenario: Concurrent task dispatch

- **WHEN** multiple tasks are available and execution slots are open
- **THEN** the TaskDispatcher SHALL dispatch up to `maxConcurrent` tasks in parallel
- **AND** each task SHALL be executed in its own goroutine

#### Scenario: Task completion unlocks dependents

- **WHEN** a task completes successfully
- **THEN** tasks that were blocked by it (via `blocks` relationships) SHALL become available on the next poll
- **AND** the TaskDispatcher SHALL detect newly available tasks within one poll interval

#### Scenario: All tasks complete

- **WHEN** all SpecTask objects in the DAG have `status: completed` or `status: skipped`
- **THEN** the TaskDispatcher SHALL stop polling and return success

### Requirement: Agent Selection

The system SHALL select appropriate agents for tasks using a hybrid strategy (code rules + LLM fallback).

#### Scenario: Code-based agent selection

- **WHEN** a task has properties that match a configured selection rule (e.g., task type → agent name mapping)
- **THEN** the CodeSelector SHALL return the matched agent definition
- **AND** no LLM call SHALL be made

#### Scenario: LLM-based agent selection fallback

- **WHEN** no code-based selection rule matches a task
- **THEN** the HybridSelector SHALL fall back to the LLMSelector
- **AND** the LLM SHALL receive the task description and available agent summaries
- **AND** the LLM SHALL return the most suitable agent name
- **AND** a fast model (e.g., Gemini Flash) SHALL be used for selection decisions

### Requirement: Task Execution Lifecycle

The system SHALL manage the full lifecycle of task execution: assignment, execution, completion, and failure handling.

#### Scenario: Task assignment

- **WHEN** the TaskDispatcher dispatches a task to an agent
- **THEN** the SpecTask's `status` SHALL be updated to `in_progress`
- **AND** `assigned_agent` SHALL be set to the selected agent's name

#### Scenario: Task context from predecessors

- **WHEN** a task is dispatched for execution
- **THEN** the system SHALL gather context from completed predecessor tasks (via incoming `blocks` relationships)
- **AND** inject predecessor summaries into the agent's input context

#### Scenario: Successful task completion

- **WHEN** an agent completes a task successfully
- **THEN** the SpecTask's `status` SHALL be updated to `completed`
- **AND** `metrics.completion_time` and `metrics.duration_seconds` SHALL be recorded

### Requirement: Retry with Failure Context

The system SHALL retry failed tasks with context from previous failures injected into the agent's input.

#### Scenario: Retry with failure context

- **WHEN** a task fails and `retry_count < max_retries`
- **THEN** the SpecTask's `status` SHALL be reset to `pending`
- **AND** `retry_count` SHALL be incremented
- **AND** `failure_context` SHALL contain the previous error and agent output
- **AND** `assigned_agent` SHALL be cleared (allow re-selection)

#### Scenario: Max retries exceeded

- **WHEN** a task fails and `retry_count >= max_retries`
- **THEN** the SpecTask's `status` SHALL be updated to `failed`
- **AND** `failure_context` SHALL include all retry attempt details

#### Scenario: Failure context injection

- **WHEN** a previously-failed task is retried
- **THEN** the agent's input context SHALL include the `failure_context` from the previous attempt
- **AND** the system prompt SHALL instruct the agent to learn from the previous failure

### Requirement: Task DAG Validation

The system SHALL validate task DAGs to prevent cycles and invalid structures.

#### Scenario: Cycle detection on creation

- **WHEN** a new `blocks` relationship is created between SpecTask objects
- **THEN** the system SHALL verify that the resulting graph is a valid DAG (no cycles)
- **AND** if a cycle would be created, the relationship creation SHALL fail with a descriptive error

#### Scenario: Topological ordering

- **WHEN** the TaskDispatcher initializes for a DAG
- **THEN** it SHALL compute a topological ordering of all tasks
- **AND** verify that the ordering is valid (no cycles detected)

### Requirement: Health Monitoring

The system SHALL monitor active task executions and handle stale or timed-out runs.

#### Scenario: Stale run detection

- **WHEN** an active agent run has been running longer than its timeout
- **THEN** the health monitor SHALL cancel the run
- **AND** treat it as a failure (triggering retry logic if applicable)

#### Scenario: Active runs tracking

- **WHEN** the TaskDispatcher is running
- **THEN** it SHALL maintain a map of active task-to-run assignments
- **AND** remove entries when runs complete, fail, or are cancelled

