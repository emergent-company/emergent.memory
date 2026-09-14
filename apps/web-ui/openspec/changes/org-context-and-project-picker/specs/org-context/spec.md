## Purpose

Defines organization as a first-class navigation context in the web console: an org-scoped sidebar, an org landing view, org member and tool-settings management, and session-level switching between no-org, org, and project contexts.

## ADDED Requirements

### Requirement: Organization navigation context

The gateway SHALL support an organization as a navigation context distinct from a project context, stored in the session as an active organization with no active project.

#### Scenario: Activate organization context
- **WHEN** a signed-in user opens an organization from the picker via its header or cogwheel
- **THEN** the session records that organization as active and clears any active project

#### Scenario: Clear context entirely
- **WHEN** the gateway needs a state with neither an active organization nor an active project
- **THEN** the session has both the active-organization and active-project fields cleared

### Requirement: Organization sidebar

The gateway SHALL render an organization-scoped sidebar whenever an organization is the active context and no project is selected.

#### Scenario: Org sidebar shown
- **WHEN** the active context is an organization
- **THEN** the sidebar lists Projects, Members, Invites, Tool settings, and Delete for that organization

#### Scenario: Project navigation hidden
- **WHEN** the active context is an organization
- **THEN** the project-scoped navigation (Agents, Chat, Sessions, Memory Browser, Settings) is not rendered

### Requirement: Organization landing view

The gateway SHALL render the active organization's projects as the landing content for the org context.

#### Scenario: Projects listed
- **WHEN** the org context is active and the organization has projects
- **THEN** the console lists the organization's projects

#### Scenario: Empty organization
- **WHEN** the org context is active and the organization has no projects
- **THEN** the console shows an empty state with a call to action to create a project in that organization

### Requirement: Create a project in the active organization

The gateway SHALL let the user create a project scoped to the active organization without re-selecting it.

#### Scenario: Pre-scoped creation
- **WHEN** the user creates a project from within the org context
- **THEN** the creation form is pre-bound to the active organization

### Requirement: No-organization onboarding wizard

The gateway SHALL show only an organization-creation wizard when the signed-in user belongs to no organization.

#### Scenario: Wizard shown
- **WHEN** the signed-in user has zero organizations
- **THEN** the console shows only the create-organization wizard and no project or organization navigation

#### Scenario: Wizard completes
- **WHEN** the user submits a valid organization name in the wizard
- **THEN** the organization is created and becomes the active context

### Requirement: List organization members

The gateway SHALL list the members of the active organization with their identities and roles.

#### Scenario: Members listed
- **WHEN** a user opens the Members view in the org context
- **THEN** the console lists each organization member with identity and role

#### Scenario: Single member
- **WHEN** the organization has only the current user as a member
- **THEN** the console lists that member and no error

### Requirement: Manage organization tool settings

The gateway SHALL list, update, and remove the active organization's tool-level settings.

#### Scenario: Settings listed
- **WHEN** the user opens the Tool settings view in the org context
- **THEN** the console lists the organization's tool settings

#### Scenario: Setting updated
- **WHEN** the user changes a tool setting
- **THEN** the console persists the change for the organization

#### Scenario: Setting removed
- **WHEN** the user removes a tool setting
- **THEN** the console reverts that tool to its global default

### Requirement: Delete organization

The gateway SHALL let an authorized user delete the active organization after confirmation.

#### Scenario: Delete confirmed
- **WHEN** the user confirms deletion of the organization
- **THEN** the organization is deleted and the console returns to the wizard or picker

#### Scenario: Delete rejected for non-admin
- **WHEN** a user without administrative rights attempts to delete the organization
- **THEN** the console rejects the action and shows an authorization error
