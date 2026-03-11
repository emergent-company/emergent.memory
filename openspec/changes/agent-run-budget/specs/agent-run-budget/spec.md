## ADDED Requirements

### Requirement: Per-Run Budget Configuration
An `AgentDefinition` MAY include an optional `budget` block within its `model` configuration specifying a `maxCostUSD` (float, USD) and/or `maxTotalTokens` (integer) limit. Both fields are independently optional; a nil/absent value means "no limit". When both are present, either limit being reached SHALL stop the run.

#### Scenario: Budget block with cost limit only
- **WHEN** an agent definition is created with `model.budget.maxCostUSD: 0.50` and no `maxTotalTokens`
- **THEN** the system SHALL store the budget configuration and enforce only the cost limit at runtime

#### Scenario: Budget block with token limit only
- **WHEN** an agent definition is created with `model.budget.maxTotalTokens: 10000` and no `maxCostUSD`
- **THEN** the system SHALL store the budget configuration and enforce only the token limit at runtime

#### Scenario: Budget block with both limits
- **WHEN** an agent definition is created with both `model.budget.maxCostUSD` and `model.budget.maxTotalTokens`
- **THEN** the system SHALL enforce whichever limit is reached first

#### Scenario: No budget block
- **WHEN** an agent definition has no `model.budget` block
- **THEN** the agent SHALL execute with no budget constraints (existing behavior, no change)

### Requirement: Budget Enforcement Before Each LLM Call
Before each LLM invocation in `runPipeline`, the system SHALL query accumulated token usage and estimated cost for the current run from `kb.llm_usage_events`. If the run has exceeded any configured budget limit, the system SHALL abort the run with terminal status `budget_exceeded`.

#### Scenario: Cost limit reached before next call
- **WHEN** an agent run's accumulated `EstimatedCostUSD` (from `kb.llm_usage_events`) meets or exceeds `maxCostUSD` before the next LLM call
- **THEN** the system SHALL abort the run without making the LLM call
- **AND** the AgentRun SHALL be updated to `status: budget_exceeded`
- **AND** any partial summary produced so far SHALL be preserved in the `summary` field

#### Scenario: Token limit reached before next call
- **WHEN** an agent run's accumulated total tokens (`TotalInputTokens + TotalOutputTokens`) meets or exceeds `maxTotalTokens` before the next LLM call
- **THEN** the system SHALL abort the run without making the LLM call
- **AND** the AgentRun SHALL be updated to `status: budget_exceeded`
- **AND** any partial summary produced so far SHALL be preserved in the `summary` field

#### Scenario: Budget not yet reached
- **WHEN** an agent run's accumulated usage is below all configured limits before the next LLM call
- **THEN** the system SHALL proceed with the LLM call normally

#### Scenario: Budget check on first call
- **WHEN** an agent run begins and the first LLM call is about to be made
- **THEN** the system SHALL perform the budget check before the first call
- **AND** if usage is already at or above the limit (e.g., from a previous run's data — which cannot happen since usage is per-run), the run SHALL be aborted

### Requirement: budget_exceeded Terminal Run Status
The system SHALL recognize `budget_exceeded` as a distinct terminal run status alongside `success`, `error`, `cancelled`, `paused`, `queued`, and `failed`.

#### Scenario: budget_exceeded in run response
- **WHEN** a run is stopped due to budget enforcement
- **THEN** the AgentRun record SHALL have `status: budget_exceeded`
- **AND** the run response DTO SHALL include `budgetExceeded: true`

#### Scenario: budget_exceeded is terminal
- **WHEN** an AgentRun has `status: budget_exceeded`
- **THEN** the run SHALL NOT be resumable or re-queued
- **AND** it SHALL be treated the same as `error` for lifecycle purposes (completed_at and duration set)

### Requirement: Blueprint YAML Budget Support
The CLI blueprint loader SHALL support a `budget:` key under each agent's `model:` block in blueprint YAML files, mapping to the `Budget` field on the server's `AgentDefinition.ModelConfig`.

#### Scenario: Blueprint with budget fields
- **WHEN** a blueprint YAML contains an agent with:
  ```yaml
  model:
    budget:
      maxCostUSD: 1.00
      maxTotalTokens: 50000
  ```
- **THEN** the CLI loader SHALL map these fields to `AgentDefinition.ModelConfig.Budget.MaxCostUSD` and `.MaxTotalTokens`
- **AND** apply them when creating or updating the agent definition

#### Scenario: Blueprint without budget fields
- **WHEN** a blueprint YAML contains an agent with no `budget:` key under `model:`
- **THEN** the CLI loader SHALL create the agent definition with `Budget: nil`
- **AND** the agent SHALL run without budget constraints

### Requirement: Async Usage Flush Acknowledgement
The system SHALL document that budget enforcement operates on data from `kb.llm_usage_events`, which is written asynchronously. A small lag may exist between a completed LLM call's usage being committed and the next pre-call budget check reading it.

#### Scenario: Usage lag between calls
- **WHEN** an LLM call completes and the next call is about to begin within a very short interval
- **THEN** the budget check MAY not yet reflect the most recent call's usage due to async flush
- **AND** the run SHALL still be stopped at the next pre-call check once the usage is committed
- **AND** this means a run MAY exceed a budget limit by at most one LLM call's worth of usage
