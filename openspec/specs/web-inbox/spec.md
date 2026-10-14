# web-inbox Specification

## Purpose
Define the web notification inbox — an account-scope view spanning every project the user belongs to plus a single-project scope — so each row attributes its originating project and the account scope can be filtered by project.

## Requirements

### Requirement: Account-inbox rows attribute their project

Account-inbox rows SHALL show the originating project's name when the
notification carries a `projectId` and the name resolves. The chip SHALL be
omitted when the project is unknown (best-effort attribution) or the
notification has no project.

#### Scenario: Row shows the originating project

- **GIVEN** an account-scope notification with a `projectId` that resolves to a project name
- **WHEN** the account inbox lists it
- **THEN** the row renders a chip with the project name

#### Scenario: Unknown project omits the chip

- **GIVEN** an account-scope notification whose `projectId` does not resolve
- **WHEN** the account inbox lists it
- **THEN** the row renders without a project chip

#### Scenario: Project scope does not repeat the project label

- **WHEN** the project inbox lists a notification
- **THEN** no project chip is rendered (the scope switch already names the project)

### Requirement: Account project filter

The Account inbox SHALL offer a project filter listing "All projects" plus the
caller's projects, and SHALL narrow the list and counts to the selected project.
The filter SHALL NOT render for the Project scope, which already narrows to one
project.

#### Scenario: Filter narrows the account inbox

- **WHEN** the user selects a project from the account inbox filter
- **THEN** the list and counts are narrowed to that project's account events

#### Scenario: Unknown filter value is ignored

- **WHEN** the inbox URL carries a `project` query value that is not one of the caller's projects
- **THEN** the filter is ignored and the account inbox lists all projects

#### Scenario: Project scope has no filter

- **WHEN** the Project scope is shown
- **THEN** no project filter is rendered
