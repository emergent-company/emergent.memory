# superadmin-role-enforcement

## Purpose

Defines authorization enforcement for superadmin roles, distinguishing between `superadmin_full` (read and write operations) and `superadmin_readonly` (read operations only) privileges.

## ADDED Requirements

### Requirement: Superadmin role stored in database
The system SHALL store a `role` column in the `core.superadmins` table with allowed values `superadmin_full` and `superadmin_readonly`. The column SHALL be non-null with a default value of `superadmin_full`. The system SHALL enforce valid role values via a database check constraint.

#### Scenario: Superadmin grant includes role
- **WHEN** a superadmin record is created in `core.superadmins` table with `role = 'superadmin_readonly'`
- **THEN** the record is stored successfully

#### Scenario: Invalid role rejected by database
- **WHEN** an insert or update attempts to set `role` to a value other than `superadmin_full` or `superadmin_readonly`
- **THEN** the database rejects the operation with a check constraint violation

#### Scenario: Default role applied
- **WHEN** a superadmin record is created without specifying a `role`
- **THEN** the role defaults to `superadmin_full`

### Requirement: Full role required for write operations
The system SHALL reject requests from users with `superadmin_readonly` role when accessing write endpoints. Write endpoints include DELETE requests and POST requests that modify data (delete, cancel, retry operations). The system SHALL respond with HTTP 403 Forbidden.

#### Scenario: Readonly superadmin blocked from deleting user
- **WHEN** a user with `superadmin_readonly` role calls `DELETE /api/superadmin/users/:id`
- **THEN** the server responds with HTTP 403 Forbidden

#### Scenario: Readonly superadmin blocked from deleting organization
- **WHEN** a user with `superadmin_readonly` role calls `DELETE /api/superadmin/organizations/:id`
- **THEN** the server responds with HTTP 403 Forbidden

#### Scenario: Readonly superadmin blocked from deleting project
- **WHEN** a user with `superadmin_readonly` role calls `DELETE /api/superadmin/projects/:id`
- **THEN** the server responds with HTTP 403 Forbidden

#### Scenario: Readonly superadmin blocked from bulk job deletion
- **WHEN** a user with `superadmin_readonly` role calls `POST /api/superadmin/embedding-jobs/delete`
- **THEN** the server responds with HTTP 403 Forbidden

#### Scenario: Readonly superadmin blocked from job cancellation
- **WHEN** a user with `superadmin_readonly` role calls `POST /api/superadmin/extraction-jobs/cancel`
- **THEN** the server responds with HTTP 403 Forbidden

#### Scenario: Full superadmin allowed to perform write operations
- **WHEN** a user with `superadmin_full` role calls `DELETE /api/superadmin/users/:id`
- **THEN** the server processes the deletion request normally

### Requirement: Both roles allowed for read operations
The system SHALL allow users with either `superadmin_full` or `superadmin_readonly` role to access read-only endpoints. Read-only endpoints include GET requests that retrieve lists or details without modifying data.

#### Scenario: Readonly superadmin can list users
- **WHEN** a user with `superadmin_readonly` role calls `GET /api/superadmin/users`
- **THEN** the server returns the paginated user list

#### Scenario: Readonly superadmin can list organizations
- **WHEN** a user with `superadmin_readonly` role calls `GET /api/superadmin/organizations`
- **THEN** the server returns the paginated organization list

#### Scenario: Readonly superadmin can list projects
- **WHEN** a user with `superadmin_readonly` role calls `GET /api/superadmin/projects`
- **THEN** the server returns the paginated project list

#### Scenario: Readonly superadmin can view job queues
- **WHEN** a user with `superadmin_readonly` role calls `GET /api/superadmin/embedding-jobs`
- **THEN** the server returns the paginated job list with stats

#### Scenario: Readonly superadmin can view email job preview
- **WHEN** a user with `superadmin_readonly` role calls `GET /api/superadmin/email-jobs/:id/preview-json`
- **THEN** the server returns the email job template data

#### Scenario: Full superadmin can access read operations
- **WHEN** a user with `superadmin_full` role calls `GET /api/superadmin/users`
- **THEN** the server returns the paginated user list

### Requirement: Me endpoint returns role
The system SHALL return the superadmin role in the `GET /api/superadmin/me` response. The response SHALL include a `role` field with the value from `core.superadmins.role` when the user is a superadmin. If the user is not a superadmin, the endpoint SHALL return null.

#### Scenario: Full superadmin sees role in me response
- **WHEN** a user with `superadmin_full` role calls `GET /api/superadmin/me`
- **THEN** the response is `{"isSuperadmin": true, "role": "superadmin_full"}`

#### Scenario: Readonly superadmin sees role in me response
- **WHEN** a user with `superadmin_readonly` role calls `GET /api/superadmin/me`
- **THEN** the response is `{"isSuperadmin": true, "role": "superadmin_readonly"}`

#### Scenario: Non-superadmin receives null
- **WHEN** a user without superadmin privileges calls `GET /api/superadmin/me`
- **THEN** the response is `null`

### Requirement: Backward compatible migration
The system SHALL provide a database migration that adds the `role` column to `core.superadmins` without breaking existing superadmin grants. The migration SHALL backfill all existing rows with `role = 'superadmin_full'` before adding the NOT NULL constraint.

#### Scenario: Existing superadmin grants become full access
- **WHEN** the migration runs on a database with existing `core.superadmins` rows
- **THEN** all existing rows receive `role = 'superadmin_full'`
- **AND** no superadmin user loses access

#### Scenario: New grants after migration specify role
- **WHEN** a new superadmin grant is created after migration
- **THEN** the grant must specify a role (or use the default `superadmin_full`)
