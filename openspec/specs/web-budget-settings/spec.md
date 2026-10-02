# web-budget-settings Specification

## Purpose
A dedicated Settings sub-page at `/settings/budget` where a project member can set the monthly LLM spend cap and choose when budget alerts are sent, without the General settings page. Alerts are produced by the existing usage service from the same `kb.projects` budget columns.

## Requirements

### Requirement: Budget settings page

The gateway SHALL expose a Budget settings page at `/settings/budget`, reachable from the settings sub-navigation, that shows the project's monthly budget cap and budget alert threshold.

#### Scenario: Page loads

- **WHEN** a project member opens `/settings/budget`
- **THEN** the page renders the monthly budget cap control and the budget alert threshold control, framed by the settings sub-navigation with Budget highlighted

#### Scenario: No active project

- **WHEN** the page is opened with no active project in the session
- **THEN** the user is redirected to the project/org picker rather than shown a broken page

#### Scenario: Memory unreachable

- **WHEN** the project fetch fails
- **THEN** the page shows an error state and does not render budget values as authoritative

### Requirement: Edit the monthly budget cap

The Budget settings page SHALL allow the user to set or clear the project's monthly LLM spend cap, saving inline and leaving the cap unchanged when submitted empty.

#### Scenario: Save a cap

- **WHEN** the user enters a non-negative amount and the field change commits
- **THEN** the project's `budget_usd` is updated and a success message is shown

#### Scenario: Empty cap clears the limit

- **WHEN** the user submits the budget field empty
- **THEN** the request leaves the stored budget unchanged and reports success

### Requirement: Configure when budget alerts are sent

The Budget settings page SHALL let the user set the budget alert threshold as a whole percentage of the monthly budget, and the threshold SHALL be persisted as the fraction `percentage / 100` in the existing project budget column.

#### Scenario: Save a percentage

- **WHEN** the user submits a threshold of `80` percent
- **THEN** the stored `budget_alert_threshold` fraction is `0.8` and a success message is shown

#### Scenario: Read back as a percentage

- **WHEN** the page renders a stored threshold fraction of `0.8`
- **THEN** the threshold control shows `80`

#### Scenario: Threshold absent

- **WHEN** the project read returns no threshold
- **THEN** the control shows the schema default of `80` percent rather than an empty or zero value

### Requirement: Validate the alert threshold

The gateway and the server SHALL reject a budget alert threshold that is not a positive fraction no greater than 1. A percentage that is empty, non-numeric, zero, negative, or greater than 100 SHALL be rejected with an error and SHALL NOT be persisted. The server SHALL reject a fraction `<= 0` or `> 1`.

#### Scenario: Zero threshold rejected

- **WHEN** the user submits a threshold of `0` percent
- **THEN** the page shows an error, the value is not persisted, and no alert can be configured to fire at zero spend

#### Scenario: Negative threshold rejected

- **WHEN** the user submits a negative threshold
- **THEN** the page shows an error and the value is not persisted

#### Scenario: Above-100 threshold rejected

- **WHEN** the user submits a threshold greater than `100` percent
- **THEN** the page shows an error and the value is not persisted

#### Scenario: Positive fraction accepted

- **WHEN** the user submits a threshold strictly between `0` and `100` percent inclusive
- **THEN** the value is persisted and the page shows a success message

#### Scenario: Server rejects a bad fraction

- **WHEN** an API client patches `budget_alert_threshold` with a value `<= 0` or `> 1`
- **THEN** the server responds with a validation error and does not persist the value

### Requirement: Preserve budget alert semantics

Moving the budget configuration SHALL NOT change how alerts are produced: a single threshold per project, evaluated against the current-month spend, with the existing once-per-project-per-month durable deduplication.

#### Scenario: Breach still alerts once

- **WHEN** a project's spend reaches `budget * threshold` in a month
- **THEN** the existing producer emits one budget-alert notification for that project and month, and repeated evaluations do not duplicate it

#### Scenario: Zero spend emits no alert

- **WHEN** a project has a positive threshold and no spend
- **THEN** no budget-alert notification is emitted
