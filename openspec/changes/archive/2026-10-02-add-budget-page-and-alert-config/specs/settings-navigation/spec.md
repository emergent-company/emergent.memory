## MODIFIED Requirements

### Requirement: Show the settings sub-navigation

The Settings area SHALL show a vertical sub-navigation rail listing its sections: General, Assistant, Overrides, Providers, Voice, Devices, and Budget.

#### Scenario: Sub-navigation present

- **WHEN** a Settings page loads
- **THEN** the sub-navigation shows General, Assistant, Overrides, Providers, Voice, Devices, and Budget links

#### Scenario: Active section highlighted

- **WHEN** a Settings page loads
- **THEN** the sub-navigation highlights the link for the current section

### Requirement: Navigate to each settings section

Each sub-navigation link SHALL open its own Settings sub-page.

#### Scenario: Open General

- **WHEN** the user selects the General link
- **THEN** the General settings page opens, showing the project info and remember & dedup panels

#### Scenario: Open Assistant

- **WHEN** the user selects the Assistant link
- **THEN** the Assistant settings page opens, showing the assistant agent selection

#### Scenario: Open Overrides

- **WHEN** the user selects the Overrides link
- **THEN** the Agent overrides settings page opens, showing the per-agent definition overrides

#### Scenario: Open Providers

- **WHEN** the user selects the Providers link
- **THEN** the Providers settings page opens, showing the configured LLM providers and per-model rates

#### Scenario: Open Voice

- **WHEN** the user selects the Voice link
- **THEN** the Voice settings page opens, showing the voice configuration

#### Scenario: Open Devices

- **WHEN** the user selects the Devices link
- **THEN** the Devices settings page opens, showing the iOS setup and devices panels

#### Scenario: Open Budget

- **WHEN** the user selects the Budget link
- **THEN** the Budget settings page opens at `/settings/budget`, showing the monthly budget cap and the budget alert threshold
