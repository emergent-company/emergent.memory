# settings-navigation Specification

## Purpose
A vertical sub-navigation rail that organizes the Settings area into focused pages so users can find and edit project configuration without scrolling one long page.

## Requirements

### Requirement: Show the settings sub-navigation

The Settings area SHALL show a vertical sub-navigation rail listing its sections: General, Assistant, Overrides, Providers, Voice, and Devices.

#### Scenario: Sub-navigation present

- **WHEN** a Settings page loads
- **THEN** the sub-navigation shows General, Assistant, Overrides, Providers, Voice, and Devices links

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

### Requirement: Keep section save behavior intact

Each settings sub-page SHALL preserve its existing save and error behavior; moving a section to its own page SHALL NOT change how its settings are saved or how errors are reported.

#### Scenario: Save on a sub-page

- **WHEN** the user saves a valid change on any settings sub-page
- **THEN** the change persists and the page shows a success message, as before the split

#### Scenario: Error on a sub-page

- **WHEN** a backend request for a settings sub-page fails
- **THEN** that sub-page shows a clear error state without breaking the sub-navigation
