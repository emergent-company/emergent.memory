## ADDED Requirements

### Requirement: Superadmin User Management

The system SHALL provide superadmin endpoints at `/api/superadmin/users` for managing all users across organizations.

#### Scenario: List all users

- **WHEN** superadmin requests GET /api/superadmin/users
- **THEN** system returns paginated list of all users across all organizations

#### Scenario: Get user details

- **WHEN** superadmin requests GET /api/superadmin/users/:id
- **THEN** system returns detailed user information including organization memberships

#### Scenario: Update user

- **WHEN** superadmin sends PATCH /api/superadmin/users/:id
- **THEN** system updates user and returns updated record

#### Scenario: Non-superadmin denied

- **WHEN** non-superadmin user requests any /api/superadmin/\* endpoint
- **THEN** system returns 403 Forbidden

### Requirement: Superadmin Organization Management

The system SHALL provide superadmin endpoints at `/api/superadmin/organizations` for managing all organizations.

#### Scenario: List all organizations

- **WHEN** superadmin requests GET /api/superadmin/organizations
- **THEN** system returns paginated list of all organizations

#### Scenario: Get organization details

- **WHEN** superadmin requests GET /api/superadmin/organizations/:id
- **THEN** system returns detailed organization information including member counts

### Requirement: Superadmin Project Management

The system SHALL provide superadmin endpoints at `/api/superadmin/projects` for viewing all projects.

#### Scenario: List all projects

- **WHEN** superadmin requests GET /api/superadmin/projects
- **THEN** system returns paginated list of all projects across all organizations

### Requirement: Superadmin Email Job Management

The system SHALL provide superadmin endpoints at `/api/superadmin/email-jobs` for managing email jobs.

#### Scenario: List email jobs

- **WHEN** superadmin requests GET /api/superadmin/email-jobs
- **THEN** system returns list of email jobs with status and timestamps

#### Scenario: Retry failed email job

- **WHEN** superadmin sends POST /api/superadmin/email-jobs/:id/retry
- **THEN** system requeues the email job and returns updated status

### Requirement: Superadmin View-As

The system SHALL provide endpoint for superadmins to impersonate other users at `/api/superadmin/view-as`.

#### Scenario: Get impersonation token

- **WHEN** superadmin sends POST /api/superadmin/view-as with target user ID
- **THEN** system returns short-lived token that allows acting as that user

#### Scenario: View-as actions are audited

- **WHEN** superadmin uses view-as functionality
- **THEN** system logs the impersonation event with superadmin ID and target user ID

### Requirement: Agent Batch Triggers

The system SHALL provide admin endpoints at `/api/admin/agents` for managing reaction agent batch triggers.

#### Scenario: List agents

- **WHEN** admin requests GET /api/admin/agents
- **THEN** system returns list of configured agents

#### Scenario: Get pending events

- **WHEN** admin requests POST /api/admin/agents/:id/pending-events
- **THEN** system returns list of events pending processing by the agent

#### Scenario: Trigger batch processing

- **WHEN** admin sends POST /api/admin/agents/:id/batch-trigger
- **THEN** system queues batch processing job and returns job ID
