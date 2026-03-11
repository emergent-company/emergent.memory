## MODIFIED Requirements

### Requirement: Agent Run Lifecycle Tracking

The system SHALL create an AgentRun record before execution begins and update it upon completion or failure.

#### Scenario: Successful execution

- **WHEN** an agent executes successfully
- **THEN** an AgentRun record SHALL be created with `status: running` before execution
- **AND** updated to `status: success` after execution
- **AND** `summary` SHALL contain the agent's final text output
- **AND** `duration` SHALL reflect the wall-clock execution time
- **AND** `completed_at` SHALL be set to the completion timestamp

#### Scenario: Failed execution

- **WHEN** an agent execution encounters an error
- **THEN** the AgentRun SHALL be updated to `status: error`
- **AND** `error_message` SHALL contain the error description
- **AND** `duration` and `completed_at` SHALL still be set

#### Scenario: Cancelled execution

- **WHEN** an agent execution is cancelled via context cancellation
- **THEN** the AgentRun SHALL be updated to `status: cancelled`
- **AND** `summary` SHALL contain any partial output produced before cancellation

#### Scenario: Queued run lifecycle

- **WHEN** `trigger_agent` routes to an agent with `dispatch_mode: queued`
- **THEN** an AgentRun record SHALL be created with `status: queued` immediately
- **AND** a corresponding `kb.agent_run_jobs` row SHALL be inserted atomically
- **AND** the run SHALL transition to `status: running` when a worker claims the job
- **AND** the run SHALL transition to `status: success` or `status: error` when the worker finishes

#### Scenario: Budget-exceeded execution

- **WHEN** an agent run is aborted by budget enforcement in `beforeModelCb`
- **THEN** the AgentRun SHALL be updated to `status: budget_exceeded`
- **AND** `duration` and `completed_at` SHALL be set
- **AND** any partial summary produced before the budget check SHALL be preserved in `summary`
- **AND** the run response DTO SHALL include `budgetExceeded: true`

## ADDED Requirements

### Requirement: Pre-Call Budget Enforcement
The `beforeModelCb` hook in `runPipeline` SHALL check accumulated run usage from `kb.llm_usage_events` against the agent definition's budget limits before each LLM invocation. If either limit is exceeded, the hook SHALL abort the run with status `budget_exceeded`.

#### Scenario: Cost budget check passes
- **WHEN** `beforeModelCb` is called and the agent definition has `MaxCostUSD` set
- **AND** `GetRunTokenUsage` returns an `EstimatedCostUSD` below `MaxCostUSD`
- **THEN** `beforeModelCb` SHALL return without error and the LLM call SHALL proceed

#### Scenario: Cost budget check fails
- **WHEN** `beforeModelCb` is called and `GetRunTokenUsage` returns an `EstimatedCostUSD` >= `MaxCostUSD`
- **THEN** `beforeModelCb` SHALL update the AgentRun to `status: budget_exceeded`
- **AND** return an error that aborts the pipeline without making the LLM call

#### Scenario: Token budget check fails
- **WHEN** `beforeModelCb` is called and `GetRunTokenUsage` returns `TotalInputTokens + TotalOutputTokens` >= `MaxTotalTokens`
- **THEN** `beforeModelCb` SHALL update the AgentRun to `status: budget_exceeded`
- **AND** return an error that aborts the pipeline without making the LLM call

#### Scenario: No budget configured
- **WHEN** `beforeModelCb` is called and the agent definition has `Budget == nil`
- **THEN** the system SHALL skip the budget check entirely and proceed normally
