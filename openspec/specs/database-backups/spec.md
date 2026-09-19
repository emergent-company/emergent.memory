# database-backups Specification

## Purpose
Defines the superadmin scheduled full-database backup: PostgreSQL client/server major compatibility, the version preflight that makes a mismatch actionable, storage and retention of `pg_dump` artifacts, cleanup of failed dumps, and the health signal that makes a failing backup observable.

## Requirements

### Requirement: Packaged pg_dump client matches the database image major

The server runtime image SHALL install a PostgreSQL client whose major version matches the major version of the database image the stock self-hosted compose files pair it with. The client major SHALL be selectable through a `PG_CLIENT_MAJOR` build argument (default `17`) rather than hard-coded, restricted to majors available in the base image's package repositories, and the build entry points (`build.sh`, `docker-compose.local.yml`) SHALL pass that argument through.

#### Scenario: Stock image installs the matched client
- **WHEN** the server image is built with the default build arguments
- **THEN** the installed `pg_dump` client major SHALL equal the major of `pgvector/pgvector:pg17` (17)
- **THEN** running `pg_dump --version` inside the image SHALL report major 17

#### Scenario: Available client major is overridable
- **WHEN** the image is built with a `PG_CLIENT_MAJOR` whose client package exists in the base image's repositories (e.g. 16 on `alpine:3.21`)
- **THEN** the installed client package SHALL be the PostgreSQL client of that major

#### Scenario: Unavailable client major fails the build
- **WHEN** the image is built with a `PG_CLIENT_MAJOR` whose client package does not exist in the base image's repositories (e.g. 18 on `alpine:3.21`)
- **THEN** the image build SHALL fail at package install rather than producing an image with a mismatched client
- **THEN** the operator SHALL bump the base image to one whose repositories ship that major

#### Scenario: Compose files document the coupling
- **WHEN** an operator reads `deploy/self-hosted/docker-compose.yml`
- **THEN** the database image SHALL carry a comment stating that its major must stay in sync with `PG_CLIENT_MAJOR`

### Requirement: Scheduled backup preflights client/server version compatibility

Before attempting to dump, the scheduled database backup SHALL verify that the `pg_dump` binary exists and that its major version is at least the database server's major version. A version mismatch SHALL fail the run with an actionable error naming both majors and the `PG_CLIENT_MAJOR` build argument, and SHALL be persisted on the backup record rather than only logged. The preflight SHALL run after the `running` record is inserted so the failure is part of the backup history.

#### Scenario: Older client fails loudly and is recorded
- **WHEN** the image ships a `pg_dump` major older than the database server major
- **THEN** the scheduled run SHALL fail before invoking the dump
- **THEN** the backup row SHALL have status `failed` and an error message naming both majors and `PG_CLIENT_MAJOR`
- **THEN** the failure SHALL be visible through `GET /api/superadmin/database-backups` without reading server logs

#### Scenario: Missing pg_dump fails the run
- **WHEN** no `pg_dump` binary is present on `PATH`
- **THEN** the run SHALL fail with an error identifying the missing binary rather than an opaque exec error

#### Scenario: Matching or newer client proceeds
- **WHEN** the `pg_dump` major is equal to or newer than the server major
- **THEN** the preflight SHALL succeed and the dump SHALL proceed

### Requirement: Backup retention applies regardless of run outcome

Retention of database backup records and their stored objects SHALL run after every backup attempt, including attempts that fail. Retention failures SHALL be logged and SHALL NOT fail the task or mask the backup outcome.

#### Scenario: Failed runs do not accumulate forever
- **WHEN** database backups fail repeatedly for longer than the retention window
- **THEN** records and objects older than the retention window SHALL be deleted as they are on the success path

#### Scenario: Retention failure does not mask a successful backup
- **WHEN** a backup completes and retention then fails
- **THEN** the task SHALL report success and log the retention error

### Requirement: Failed dumps never leave untracked objects

When `pg_dump` fails, the backup SHALL abort the in-flight upload rather than allowing a truncated or empty object to be finalized, and any object that nevertheless landed under the run's key SHALL be deleted. The reported error SHALL remain the `pg_dump` failure including its stderr, not a cleanup error.

#### Scenario: Aborted dump leaves no object
- **WHEN** `pg_dump` exits non-zero before producing output
- **THEN** the upload SHALL be aborted
- **THEN** no object SHALL remain in the `database-backups` bucket under that run's key
- **THEN** the recorded error SHALL be the `pg_dump` failure with its stderr

#### Scenario: Cleanup failure does not mask the dump error
- **WHEN** the upload reports success despite a failed `pg_dump` and the defensive deletion itself fails
- **THEN** the run SHALL still report the `pg_dump` failure
- **THEN** the cleanup failure SHALL be logged as a warning

### Requirement: Backup health is observable

The service health endpoint SHALL report the status of the most recent scheduled database backup as a named check, derived from the newest `kb.database_backups` record. The check SHALL be classified as optional, so a failing backup degrades the health response without returning a service-unavailable status. A missing table or unreadable status SHALL NOT be reported as unhealthy.

#### Scenario: Latest backup failed
- **WHEN** the newest backup record has status `failed`
- **THEN** the `database_backup` check SHALL be `unhealthy` with the stored error message
- **THEN** the overall health status SHALL be `degraded` with HTTP 200

#### Scenario: Latest backup completed
- **WHEN** the newest backup record has status `completed`
- **THEN** the `database_backup` check SHALL be `healthy`
- **THEN** the overall health status SHALL be unaffected by this check

#### Scenario: Stale running backup
- **WHEN** the newest backup record is `running` or `pending` and started more than six hours ago
- **THEN** the `database_backup` check SHALL be `unhealthy` with a message naming the start time

#### Scenario: No backups recorded yet
- **WHEN** `kb.database_backups` contains no rows
- **THEN** the `database_backup` check SHALL be `healthy` with a message indicating no backups have run yet

#### Scenario: Status unavailable does not manufacture an outage
- **WHEN** reading the newest backup record fails (for example the table is absent)
- **THEN** the `database_backup` check SHALL be `healthy` with a message explaining the status is unavailable
- **THEN** the overall health status SHALL NOT become `unhealthy` because of this check
