## ADDED Requirements

### Requirement: System SHALL perform budget check before LLM calls

Before executing any LLM API call in an agent run, the system SHALL check if the project has exceeded its monthly budget.

#### Scenario: Budget check occurs before first LLM call
- **WHEN** agent run begins execution
- **AND** run reaches point of making first LLM API call
- **THEN** system SHALL query project budget status BEFORE calling LLM provider
- **AND** system SHALL block LLM call if budget exceeded

#### Scenario: Budget check prevents retries when over budget
- **WHEN** LLM call fails with transient error
- **AND** system would normally retry
- **AND** project is over budget
- **THEN** system SHALL NOT retry the LLM call
- **AND** system SHALL fail run with budget exceeded error

### Requirement: RESOURCE_EXHAUSTED errors SHALL be treated as non-retryable

When an LLM API call returns RESOURCE_EXHAUSTED or spending cap error, the system SHALL treat it as a permanent failure after one retry.

#### Scenario: RESOURCE_EXHAUSTED retries once then disables
- **WHEN** LLM API returns error containing "RESOURCE_EXHAUSTED"
- **AND** this is the first attempt
- **THEN** system SHALL wait 5 seconds and retry once
- **WHEN** retry also returns RESOURCE_EXHAUSTED
- **THEN** system SHALL NOT retry again
- **AND** system SHALL fail the run with error
- **AND** system SHALL disable the agent
- **AND** system SHALL log "spending cap exceeded, agent disabled"

#### Scenario: Spending cap error disables agent immediately after retry
- **WHEN** LLM API returns "spending cap exceeded" error
- **AND** retry also returns spending cap error
- **THEN** system SHALL call repo.DisableAgent with reason "spending cap exceeded"
- **AND** agent SHALL be marked as enabled=false
- **AND** consecutive_failures SHALL increment

#### Scenario: Other errors continue normal retry logic
- **WHEN** LLM API returns 503 Service Unavailable
- **THEN** system SHALL retry up to maxAttempts times with backoff
- **AND** system SHALL NOT treat as permanent failure

### Requirement: Budget exceeded SHALL return specific error type

When budget is exceeded, the system SHALL return BudgetExceededError that can be distinguished from other errors.

#### Scenario: BudgetExceededError includes project context
- **WHEN** pre-flight budget check fails
- **THEN** system SHALL return error of type BudgetExceededError
- **AND** error SHALL include projectID field
- **AND** error message SHALL state "Project has exceeded its monthly budget"

### Requirement: Agent executor SHALL pass org ID for usage tracking

The agent executor SHALL resolve and pass orgID to the tracking model for correct LLM usage attribution.

#### Scenario: OrgID resolved from project
- **WHEN** executing agent run
- **THEN** system SHALL call repo.GetOrgIDByProjectID using agent's projectID
- **AND** system SHALL pass resolved orgID to ExecuteRequest
- **AND** tracking model SHALL attribute LLM usage to that orgID
