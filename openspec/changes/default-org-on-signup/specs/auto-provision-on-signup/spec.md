## ADDED Requirements

### Requirement: OIDC name claims are propagated to user profile on first login
The system SHALL extract `given_name`, `family_name`, and `name` from the Zitadel OIDC introspection and userinfo responses and populate `first_name`, `last_name`, and `display_name` on the `core.user_profiles` row when a new profile is created. The system SHALL NOT overwrite these fields on subsequent logins if they are already set.

#### Scenario: New user with full OIDC name claims
- **WHEN** a user authenticates for the first time with Zitadel OIDC token containing `given_name: "Jane"`, `family_name: "Doe"`, and `name: "Jane Doe"`
- **THEN** the system creates a `core.user_profiles` row with `first_name = "Jane"`, `last_name = "Doe"`, and `display_name = "Jane Doe"`

#### Scenario: New user with partial OIDC name claims
- **WHEN** a user authenticates for the first time with Zitadel OIDC token containing `name: "Jane Doe"` but no `given_name` or `family_name`
- **THEN** the system creates a `core.user_profiles` row with `display_name = "Jane Doe"` and `first_name` and `last_name` remain NULL

#### Scenario: Existing user re-authenticates
- **WHEN** an existing user with a populated profile (`first_name = "Jane"`) authenticates again with OIDC token containing `given_name: "Janet"`
- **THEN** the system SHALL NOT update `first_name` — the existing value is preserved

### Requirement: Default organization is auto-created on first login
The system SHALL create a default organization when a new user profile is created during first login. The organization name SHALL be derived from the user's name using the following priority: `"<FirstName> <LastName>'s Org"` → `"<DisplayName>'s Org"` → `"<email-local-part>'s Org"` → `"My Organization"`. The user SHALL be granted the `org_admin` role on the new organization.

#### Scenario: New user with first and last name
- **WHEN** a user authenticates for the first time with `given_name: "Jane"` and `family_name: "Doe"`
- **THEN** the system creates an organization named `"Jane Doe's Org"` and grants the user the `org_admin` role

#### Scenario: New user with only email available
- **WHEN** a user authenticates for the first time with no name claims and email `"jdoe@example.com"`
- **THEN** the system creates an organization named `"jdoe's Org"` and grants the user the `org_admin` role

#### Scenario: Org name conflicts with existing org
- **WHEN** a user authenticates for the first time and the derived org name `"Jane Doe's Org"` already exists
- **THEN** the system SHALL append a numeric suffix (e.g., `"Jane Doe's Org 2"`) or fall back to an email-based name to resolve the conflict

#### Scenario: Existing user logs in again
- **WHEN** an existing user (who already has a profile) authenticates
- **THEN** the system SHALL NOT create any new organization

### Requirement: Default project is auto-created within the default organization
The system SHALL create a default project within the auto-created organization. The project name SHALL be derived from the user's name using the following priority: `"<FirstName>'s Project"` → `"<DisplayName>'s Project"` → `"<email-local-part>'s Project"` → `"My Project"`. The user SHALL be granted the `project_admin` role on the new project. The `graph-query-agent` SHALL be eagerly provisioned for the new project.

#### Scenario: Successful auto-provisioning of project with first name
- **WHEN** a default organization is auto-created for a new user with `given_name: "Tom"`
- **THEN** the system creates a project named `"Tom's Project"` within that org, grants the user `project_admin` role, and provisions the `graph-query-agent`

#### Scenario: Project name fallback to email
- **WHEN** a default organization is auto-created for a new user with no name claims and email `"jdoe@example.com"`
- **THEN** the system creates a project named `"jdoe's Project"` within that org

#### Scenario: Project creation fails after org creation
- **WHEN** the default organization is created successfully but project creation fails
- **THEN** the system SHALL log the error but SHALL NOT roll back the organization creation — the user can manually create a project later

### Requirement: Auto-provisioning does not affect existing users
The system SHALL only trigger auto-provisioning when a brand-new user profile is created. Users who already have a `core.user_profiles` row (including soft-deleted and reactivated profiles) SHALL NOT trigger auto-provisioning.

#### Scenario: Reactivated user
- **WHEN** a previously soft-deleted user profile is reactivated during login
- **THEN** the system SHALL NOT create a new default organization or project

### Requirement: Auto-provisioning is decoupled via interface
The system SHALL expose auto-provisioning behind an `AutoProvisionService` interface injected into the auth middleware. The implementation SHALL call the existing `orgs.Service.Create()` and `projects.Service.Create()` methods rather than duplicating creation logic.

#### Scenario: Auth package dependency isolation
- **WHEN** the auth middleware triggers auto-provisioning
- **THEN** it calls through the `AutoProvisionService` interface without directly importing domain packages (orgs, projects)
