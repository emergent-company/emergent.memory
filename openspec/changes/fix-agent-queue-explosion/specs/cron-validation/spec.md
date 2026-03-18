## ADDED Requirements

### Requirement: System SHALL validate cron schedule minimum interval

The system SHALL validate that cron schedules have a minimum interval of 15 minutes between executions.

#### Scenario: Reject cron with 1-minute interval
- **WHEN** user creates agent with cron_schedule="* * * * *" (every minute)
- **THEN** system SHALL return validation error
- **AND** error message SHALL state "cron interval 1m is below minimum 15m"
- **AND** system SHALL NOT create the agent

#### Scenario: Reject cron with 5-minute interval
- **WHEN** user creates agent with cron_schedule="*/5 * * * *" (every 5 minutes)
- **THEN** system SHALL return validation error
- **AND** error SHALL indicate interval is below 15-minute minimum

#### Scenario: Accept cron with 15-minute interval
- **WHEN** user creates agent with cron_schedule="*/15 * * * *" (every 15 minutes)
- **THEN** system SHALL accept the cron schedule
- **AND** system SHALL create the agent successfully

#### Scenario: Accept cron with 30-minute interval
- **WHEN** user creates agent with cron_schedule="*/30 * * * *" (every 30 minutes)
- **THEN** system SHALL accept the cron schedule

#### Scenario: Accept cron with hourly interval
- **WHEN** user creates agent with cron_schedule="0 * * * *" (every hour)
- **THEN** system SHALL accept the cron schedule

#### Scenario: Accept cron with daily interval
- **WHEN** user creates agent with cron_schedule="0 9 * * *" (daily at 9am)
- **THEN** system SHALL accept the cron schedule

### Requirement: Validation SHALL use cron parser to calculate interval

The system SHALL parse cron expressions and simulate next executions to determine actual interval.

#### Scenario: Calculate interval from next two executions
- **WHEN** validating cron schedule
- **THEN** system SHALL parse cron expression using cron parser library
- **AND** system SHALL calculate next execution time from current time
- **AND** system SHALL calculate second next execution time
- **AND** system SHALL compute interval as difference between executions
- **AND** system SHALL compare interval against minimum threshold

### Requirement: Minimum interval SHALL be configurable

The minimum cron interval SHALL be configurable via environment variable AGENT_MIN_CRON_INTERVAL_MINUTES with default value 15.

#### Scenario: Custom minimum interval
- **WHEN** AGENT_MIN_CRON_INTERVAL_MINUTES=30
- **AND** user creates agent with 20-minute interval cron
- **THEN** system SHALL reject the cron schedule
- **WHEN** user creates agent with 30-minute interval cron
- **THEN** system SHALL accept the cron schedule

#### Scenario: Default minimum interval
- **WHEN** AGENT_MIN_CRON_INTERVAL_MINUTES is not set
- **THEN** system SHALL use default value of 15 minutes

### Requirement: Validation SHALL apply to both create and update operations

Cron schedule validation SHALL be enforced when creating new agents and when updating existing agents.

#### Scenario: Validation on agent creation
- **WHEN** user creates new agent via POST /api/agents
- **AND** cron_schedule has interval < 15 minutes
- **THEN** system SHALL return HTTP 400 Bad Request
- **AND** agent SHALL NOT be created

#### Scenario: Validation on agent update
- **WHEN** user updates existing agent via PATCH /api/agents/:id
- **AND** new cron_schedule has interval < 15 minutes
- **THEN** system SHALL return HTTP 400 Bad Request
- **AND** agent SHALL NOT be updated
- **AND** existing cron schedule SHALL remain unchanged

### Requirement: Validation error SHALL include helpful message

Cron validation errors SHALL clearly explain the interval requirement and show detected interval.

#### Scenario: Error message includes detected interval
- **WHEN** user provides cron with 5-minute interval
- **THEN** error message SHALL include text "cron interval 5m"
- **AND** error message SHALL include text "below minimum 15m"
- **AND** error message SHALL guide user to fix their cron expression

### Requirement: Invalid cron expressions SHALL be rejected before interval check

The system SHALL validate cron expression syntax before checking interval requirements.

#### Scenario: Syntax error detected first
- **WHEN** user provides malformed cron expression "not a cron"
- **THEN** system SHALL return error "invalid cron expression"
- **AND** system SHALL NOT attempt interval validation
- **AND** error SHALL include syntax parsing details

#### Scenario: Valid syntax enables interval check
- **WHEN** user provides syntactically valid cron "* * * * *"
- **THEN** system SHALL parse cron successfully
- **AND** system SHALL proceed to interval validation
- **AND** system SHALL reject due to interval < 15 minutes
