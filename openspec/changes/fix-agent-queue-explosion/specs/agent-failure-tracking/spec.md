## ADDED Requirements

### Requirement: System SHALL track consecutive failures per agent

The system SHALL maintain a counter of consecutive run failures for each agent in kb.agents.consecutive_failures column.

#### Scenario: Failure counter increments on run failure
- **WHEN** agent run completes with status='error'
- **THEN** system SHALL increment kb.agents.consecutive_failures by 1 for that agent
- **AND** system SHALL persist the updated counter to database

#### Scenario: Failure counter resets on run success
- **WHEN** agent run completes with status='success'
- **THEN** system SHALL reset kb.agents.consecutive_failures to 0 for that agent
- **AND** system SHALL persist the reset counter to database

#### Scenario: Failure counter persists across runs
- **WHEN** agent fails 3 times consecutively
- **THEN** system SHALL show consecutive_failures=3
- **WHEN** agent succeeds once
- **THEN** system SHALL show consecutive_failures=0
- **WHEN** agent fails again
- **THEN** system SHALL show consecutive_failures=1

### Requirement: System SHALL auto-disable agents after consecutive failure threshold

The system SHALL automatically disable an agent when its consecutive_failures counter reaches or exceeds the configured threshold.

#### Scenario: Agent auto-disabled at threshold
- **WHEN** agent has consecutive_failures=4
- **AND** agent run fails (incrementing to 5)
- **AND** AGENT_CONSECUTIVE_FAILURE_THRESHOLD=5
- **THEN** system SHALL set kb.agents.enabled=false for that agent
- **AND** system SHALL log "auto-disabled agent after 5 consecutive failures"
- **AND** system SHALL include agent_id and failure count in log

#### Scenario: Agent not disabled below threshold
- **WHEN** agent has consecutive_failures=4
- **AND** AGENT_CONSECUTIVE_FAILURE_THRESHOLD=5
- **THEN** system SHALL NOT disable the agent
- **AND** agent SHALL remain enabled=true

#### Scenario: Disabled agent cannot be cron-triggered
- **WHEN** agent is auto-disabled due to failures
- **AND** cron schedule fires for that agent
- **THEN** system SHALL NOT create a new run
- **AND** system SHALL remove agent from cron scheduler

#### Scenario: Disabled agent can be manually re-enabled
- **WHEN** agent is auto-disabled
- **AND** admin sets enabled=true via API
- **THEN** system SHALL re-enable the agent
- **AND** system SHALL reset consecutive_failures to 0
- **AND** system SHALL re-register cron trigger if trigger_type=schedule

### Requirement: Failure threshold SHALL be configurable

The consecutive failure threshold SHALL be configurable via environment variable AGENT_CONSECUTIVE_FAILURE_THRESHOLD with default value 5.

#### Scenario: Custom failure threshold
- **WHEN** AGENT_CONSECUTIVE_FAILURE_THRESHOLD=10
- **AND** agent fails 9 times consecutively
- **THEN** system SHALL NOT auto-disable the agent
- **WHEN** agent fails 10th time
- **THEN** system SHALL auto-disable the agent

#### Scenario: Default failure threshold
- **WHEN** AGENT_CONSECUTIVE_FAILURE_THRESHOLD is not set
- **THEN** system SHALL use default value of 5 as the threshold

### Requirement: System SHALL store disable reason

When auto-disabling an agent, the system SHALL record the reason for audit and debugging purposes.

#### Scenario: Disable reason recorded
- **WHEN** agent is auto-disabled after 5 failures
- **THEN** system SHALL log disable reason as "auto-disabled after 5 consecutive failures"
- **AND** reason SHALL include failure count
- **AND** reason SHALL be available in agent logs

### Requirement: Failure tracking SHALL apply to all failure types

The consecutive failure counter SHALL increment for any run failure, including LLM errors, budget errors, and execution timeouts.

#### Scenario: LLM error increments counter
- **WHEN** agent run fails with LLM API error
- **THEN** system SHALL increment consecutive_failures

#### Scenario: Budget exceeded error increments counter
- **WHEN** agent run fails with budget exceeded error
- **THEN** system SHALL increment consecutive_failures

#### Scenario: Timeout error increments counter
- **WHEN** agent run fails with timeout error
- **THEN** system SHALL increment consecutive_failures

#### Scenario: Non-retryable errors increment counter
- **WHEN** agent run fails with RESOURCE_EXHAUSTED error (non-retryable)
- **THEN** system SHALL increment consecutive_failures
- **AND** agent SHALL be disabled if threshold reached
