# project-settings-ui Specification

## Purpose
A gateway web UI page where a user can view and edit project-level Memory configuration: the project record, per-agent definition overrides, the remember pipeline config, and the entity-create dedup threshold.
## Requirements
### Requirement: Navigate to project settings

The gateway sidebar SHALL include a Project Settings entry in the Settings group that opens the project settings page.

#### Scenario: Sidebar entry present

- **WHEN** the sidebar loads
- **THEN** a Project Settings link is shown in the Settings group and opens the project settings page

### Requirement: Show the project info

The project settings page SHALL display the project record, including the project name, project info, chat prompt template, auto-extract objects, auto-merge extraction branches, and budget.

#### Scenario: Project info present

- **WHEN** the project settings page loads
- **THEN** the project name, project info, chat prompt template, auto-extract objects, auto-merge extraction branches, and budget are shown

#### Scenario: Optional fields absent

- **WHEN** an optional field (project info, chat prompt template, or budget) is not set
- **THEN** that field is shown as a clear "not set" state

### Requirement: Edit the project info

The project settings page SHALL allow the user to update the project name, project info, chat prompt template, auto-extract objects, auto-merge extraction branches, and budget.

#### Scenario: Save project changes

- **WHEN** the user submits the project info form with valid values
- **THEN** the changes persist and the page shows a success message

#### Scenario: Project save rejected

- **WHEN** the user submits the project info form with an empty project name
- **THEN** the page shows an error and does not persist the change

### Requirement: Show agent definition overrides

The project settings page SHALL list agent definition overrides, each showing the agent name it applies to and its overridden fields (system prompt, model, tools, max steps, sandbox config).

#### Scenario: Overrides present

- **WHEN** the project settings page loads and agent overrides exist
- **THEN** each override is listed with its agent name and overridden fields

#### Scenario: No overrides

- **WHEN** the project settings page loads and no agent overrides exist
- **THEN** a clear "no overrides" state is shown

### Requirement: Edit agent definition overrides

The project settings page SHALL allow the user to add, update, and remove agent definition overrides.

#### Scenario: Update an override

- **WHEN** the user edits an override's fields and saves
- **THEN** the override persists and the page shows a success message

#### Scenario: Remove an override

- **WHEN** the user removes an override
- **THEN** the override is deleted and the page shows a success message

### Requirement: Show the remember and dedup config

The project settings page SHALL display the remember pipeline config (which agent definition powers `remember`) and the entity-create dedup threshold.

#### Scenario: Remember and dedup config present

- **WHEN** the project settings page loads
- **THEN** the remember agent name and the dedup similarity threshold are shown, or a clear "not set" state where absent

### Requirement: Edit the remember and dedup config

The project settings page SHALL allow the user to set the remember agent name and the entity-create dedup similarity threshold.

#### Scenario: Save remember and dedup changes

- **WHEN** the user updates the remember agent name or the dedup threshold and saves
- **THEN** the values persist and the page shows a success message

#### Scenario: Invalid dedup threshold

- **WHEN** the user submits a dedup threshold outside the valid 0.0–1.0 range
- **THEN** the page shows an error and does not persist the value

### Requirement: Handle an unreachable memory service

The project settings page SHALL show an error state when the Memory service cannot be reached, without crashing the rest of the UI.

#### Scenario: Memory unreachable

- **WHEN** the project settings page loads and the Memory service is unreachable
- **THEN** an error message is shown and the page does not render partial data as authoritative
