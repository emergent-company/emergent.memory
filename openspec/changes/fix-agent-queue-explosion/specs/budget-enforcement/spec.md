## ADDED Requirements

### Requirement: System SHALL check budget before executing agent runs

The system SHALL check if project has exceeded its monthly budget BEFORE making any LLM API calls.

#### Scenario: Run blocked when budget exceeded
- **WHEN** project has budget_usd=$100
- **AND** current month spend is $105
- **AND** user attempts to execute agent run
- **THEN** system SHALL return error "Project has exceeded its monthly budget"
- **AND** system SHALL return HTTP 402 Payment Required status
- **AND** system SHALL NOT make any LLM API calls
- **AND** run status SHALL be set to 'error'

#### Scenario: Run allowed when budget not exceeded
- **WHEN** project has budget_usd=$100
- **AND** current month spend is $75
- **AND** user attempts to execute agent run
- **THEN** system SHALL proceed with agent execution
- **AND** system SHALL make LLM API calls as needed

#### Scenario: Run allowed when no budget set
- **WHEN** project has budget_usd=NULL
- **AND** user attempts to execute agent run
- **THEN** system SHALL proceed with agent execution
- **AND** system SHALL NOT enforce any budget limits

### Requirement: Budget check SHALL use current month spend

The budget enforcement SHALL compare current month-to-date spending against the monthly budget limit.

#### Scenario: Budget resets at start of month
- **WHEN** current date is March 1st
- **AND** February spending was $200 (exceeded budget)
- **AND** March spending is $0
- **THEN** system SHALL allow agent runs in March
- **AND** system SHALL only count March spending toward budget

#### Scenario: Mid-month budget check
- **WHEN** current date is March 15th
- **AND** spending from March 1-14 is $50
- **AND** project budget is $100/month
- **THEN** system SHALL show $50 spent of $100 budget
- **AND** system SHALL allow runs until $100 threshold reached

### Requirement: Budget enforcement SHALL be configurable

Budget enforcement SHALL be controlled by environment variable BUDGET_ENFORCEMENT_ENABLED with default value true.

#### Scenario: Budget enforcement enabled
- **WHEN** BUDGET_ENFORCEMENT_ENABLED=true
- **AND** project is over budget
- **THEN** system SHALL block agent runs with budget error

#### Scenario: Budget enforcement disabled
- **WHEN** BUDGET_ENFORCEMENT_ENABLED=false
- **AND** project is over budget
- **THEN** system SHALL allow agent runs to proceed
- **AND** system SHALL still track spending and send alerts

### Requirement: Budget check failure SHALL fail gracefully

If the budget check query fails, the system SHALL allow the run to proceed (fail-open behavior).

#### Scenario: Budget query error allows run
- **WHEN** querying project budget fails with database error
- **THEN** system SHALL log warning "failed to check budget"
- **AND** system SHALL proceed with agent execution
- **AND** system SHALL NOT block the run

#### Scenario: Usage query error allows run
- **WHEN** querying current month spending fails with error
- **THEN** system SHALL log warning "failed to check budget"
- **AND** system SHALL proceed with agent execution

### Requirement: Budget exceeded error SHALL include helpful message

When blocking a run due to budget, the error message SHALL guide user to resolution.

#### Scenario: Budget error message content
- **WHEN** run is blocked due to budget
- **THEN** error message SHALL state "Project has exceeded its monthly budget"
- **AND** error message SHALL include text "Increase budget to continue"
- **AND** error SHALL be categorized as BudgetExceededError type

### Requirement: Budget checks SHALL have low latency impact

The budget pre-flight check SHALL complete in under 100ms for typical project sizes.

#### Scenario: Budget check performance
- **WHEN** executing budget check for project with < 1M usage events
- **THEN** query SHALL use indexes on (project_id, created_at)
- **AND** query execution time SHALL be < 100ms
- **AND** query SHALL use date_trunc for month boundary calculation
