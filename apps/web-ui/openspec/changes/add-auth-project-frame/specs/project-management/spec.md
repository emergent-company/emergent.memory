## Purpose

Lets a signed-in user list, switch between, and create Emergent Memory projects, scoping all subsequent work to the selected project.

## ADDED Requirements

### Requirement: List projects

The gateway SHALL list the projects the signed-in user can access.

#### Scenario: List projects

- **WHEN** a signed-in user opens the project list
- **THEN** the gateway shows the user's accessible projects

#### Scenario: No projects

- **WHEN** a signed-in user has no accessible projects
- **THEN** the gateway shows an empty state prompting project creation

### Requirement: Switch active project

The gateway SHALL let the user select a project as active for the session, scoping subsequent agents, chat, documents, and graph views to it.

#### Scenario: Switch project

- **WHEN** a signed-in user selects a different project
- **THEN** the session's active project changes and all subsequent Memory-scoped views use the new project

### Requirement: Create a project

The gateway SHALL let the user create a project, supplying a name and selecting an organization.

#### Scenario: Create project with an organization

- **WHEN** a signed-in user creates a project with a name and a chosen organization
- **THEN** the gateway creates the project in Memory and selects it as active

#### Scenario: Create without an organization

- **WHEN** a user creates a project and has not chosen an organization
- **THEN** the gateway prompts for an organization and does not create the project

### Requirement: Organization context

The gateway SHALL list the organizations the signed-in user belongs to, to supply the organization required when creating a project.

#### Scenario: List organizations

- **WHEN** a signed-in user opens project creation
- **THEN** the gateway lists the user's organizations for selection
