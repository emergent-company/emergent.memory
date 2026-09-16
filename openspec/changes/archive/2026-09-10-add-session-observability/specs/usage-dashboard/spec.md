## Purpose

A gateway web UI that displays LLM token usage over time — daily tokens consumed, sessions created, total tokens, and current-month spend — backed by Memory's usage-summary and usage-time-series data, in the style of a model-provider usage dashboard.

## ADDED Requirements

### Requirement: Navigate to the usage dashboard

The gateway navigation SHALL provide a link to the usage dashboard, and the dashboard SHALL render for an authenticated user.

#### Scenario: Open the usage dashboard

- **WHEN** an authenticated user opens the usage dashboard
- **THEN** the dashboard loads and displays usage metrics for the current project

### Requirement: Show daily token consumption

The usage dashboard SHALL display token consumption per day as a chart over a selectable time range.

#### Scenario: Daily consumption chart

- **WHEN** the usage dashboard loads with usage data present
- **THEN** a chart shows tokens consumed for each day in the selected range, in chronological order

#### Scenario: No data in range

- **WHEN** the selected range has no usage records
- **THEN** the chart shows an empty state with no data points, not an error

### Requirement: Show daily session counts

The usage dashboard SHALL display the number of sessions created per day alongside token consumption.

#### Scenario: Session count series

- **WHEN** the usage dashboard loads with session data present
- **THEN** a chart shows the number of sessions created for each day in the selected range

### Requirement: Show total usage summary

The usage dashboard SHALL display summary metrics for the selected range: total tokens consumed and, when available, current-month spend.

#### Scenario: Summary metrics shown

- **WHEN** the usage dashboard loads
- **THEN** the total tokens consumed in the selected range and the current-month spend (when the backend reports it) are displayed

#### Scenario: Spend unavailable

- **WHEN** the backend returns no spend figure
- **THEN** the dashboard omits the spend metric without failing the rest of the page

### Requirement: Show per-model usage

The usage dashboard SHALL display token usage broken down by model when the backend provides model-level data.

#### Scenario: Model breakdown present

- **WHEN** the usage dashboard loads and model-level usage is available
- **THEN** token usage is shown grouped by model

#### Scenario: Model breakdown absent

- **WHEN** the backend returns no model-level breakdown
- **THEN** the per-model section is omitted, and the rest of the dashboard remains usable

### Requirement: Select a time range

The usage dashboard SHALL let the user choose a time range for the displayed metrics.

#### Scenario: Change the time range

- **WHEN** the user selects a different time range (e.g. last 7, 30, or 90 days)
- **THEN** the charts and summary metrics refresh to reflect the selected range

### Requirement: Surface load failures without crashing

The usage dashboard SHALL show a clear error state when a backend fetch fails and SHALL NOT render a broken page.

#### Scenario: Backend unreachable

- **WHEN** a required backend request fails
- **THEN** the affected section shows an error message while the rest of the page remains usable
