## MODIFIED Requirements

### Requirement: Show the project info

The project settings page SHALL display the project record, including the project name, project info, chat prompt template, auto-extract objects, and auto-merge extraction branches. The monthly budget is not shown here; it lives on the Budget settings page.

#### Scenario: Project info present

- **WHEN** the project settings page loads
- **THEN** the project name, project info, chat prompt template, auto-extract objects, and auto-merge extraction branches are shown

#### Scenario: Optional fields absent

- **WHEN** an optional field (project info or chat prompt template) is not set
- **THEN** that field is shown as a clear "not set" state

### Requirement: Edit the project info

The project settings page SHALL allow the user to update the project name, project info, chat prompt template, auto-extract objects, and auto-merge extraction branches. The monthly budget is edited on the Budget settings page.

#### Scenario: Save project changes

- **WHEN** the user submits the project info form with valid values
- **THEN** the changes persist and the page shows a success message

#### Scenario: Project save rejected

- **WHEN** the user submits the project info form with an empty project name
- **THEN** the page shows an error and does not persist the change
