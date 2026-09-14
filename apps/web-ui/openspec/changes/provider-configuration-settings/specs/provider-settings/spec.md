## Purpose

A Project Settings panel that lists the LLM providers configured for a project, their models, and each model's rate — showing whether the rate is automatic (retail) or a custom override, and letting the user set or remove an override.

## ADDED Requirements

### Requirement: List configured providers in settings

The Providers settings panel SHALL list every provider configured for the current project, each with its models.

#### Scenario: Providers listed

- **WHEN** an authenticated user opens the Providers settings panel and providers are configured
- **THEN** each configured provider is listed with its models

#### Scenario: No providers configured

- **WHEN** no providers are configured for the project
- **THEN** the panel shows an empty state indicating no providers are configured

### Requirement: Show each model's rate

The Providers panel SHALL display, for each model, its current rate (input and output price per 1M tokens) and whether it is automatic or custom.

#### Scenario: Automatic rate shown

- **WHEN** a model has no override
- **THEN** the model's retail (automatic) rate is shown and marked automatic

#### Scenario: Custom rate shown

- **WHEN** a model has a project override
- **THEN** the override rate is shown and marked custom (overridden)

#### Scenario: No rate available

- **WHEN** no retail price and no override exist for a model
- **THEN** the panel shows the rate as unknown rather than an error

### Requirement: Override a model's rate

The Providers panel SHALL let the user set a custom input and output price (USD per 1M tokens) for a model, which SHALL be persisted as a project pricing override.

#### Scenario: Override saved

- **WHEN** the user submits a custom rate for a model
- **THEN** the override is persisted and the panel reflects the custom rate

#### Scenario: Invalid rate rejected

- **WHEN** the user submits a non-numeric or negative rate
- **THEN** the input is rejected with a clear validation message and no override is written

### Requirement: Remove an override

The Providers panel SHALL let the user remove a model's override, reverting it to the automatic retail rate.

#### Scenario: Override removed

- **WHEN** the user removes a model's override
- **THEN** the override is deleted and the panel shows the automatic rate again

### Requirement: Surface load failures without crashing

The Providers panel SHALL show a clear error state when a backend fetch or write fails and SHALL NOT render a broken page.

#### Scenario: Backend fetch fails

- **WHEN** loading providers or pricing fails
- **THEN** the panel shows an error message while the rest of Settings remains usable
