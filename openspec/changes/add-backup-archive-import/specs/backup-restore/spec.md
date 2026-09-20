## Purpose

Extend the backup restore pipeline with a self-service archive import path so an archive produced by another deployment can be registered and restored.

## ADDED Requirements

### Requirement: Import a backup archive into an organization
The system SHALL expose `POST /api/v1/organizations/{orgId}/backups/import` accepting a `multipart/form-data` upload with a `file` field containing a backup archive. The endpoint SHALL validate the archive, store it under a backup storage key scoped to the target organization, and register a `ready` backup record. The calling user SHALL be authenticated and a member of the target organization.

#### Scenario: Successful import
- **WHEN** a member of organization X uploads a valid backup archive
- **THEN** the server SHALL validate the manifest and checksums
- **THEN** the server SHALL store the archive under `backups/{orgId}/{backupId}/backup.zip`
- **THEN** the server SHALL return the registered backup with status `ready` and `imported` set to true
- **THEN** the backup SHALL be restorable via the existing clone restore endpoint

#### Scenario: Non-member is rejected
- **WHEN** a user who is not a member of the target organization uploads an archive
- **THEN** the server SHALL reject the request with `403 Forbidden`
- **THEN** no archive SHALL be stored and no backup record SHALL be created

#### Scenario: Invalid archive is rejected
- **WHEN** an uploaded file is not a ZIP archive, is missing `manifest.json`, or fails a checksum
- **THEN** the server SHALL reject the request with `400 Bad Request`
- **THEN** no backup record SHALL be created

#### Scenario: Oversized archive is rejected
- **WHEN** an uploaded archive exceeds the server's import size limit
- **THEN** the server SHALL reject the request with `413 Request Entity Too Large`
- **THEN** the server SHALL NOT buffer the whole upload beyond the limit

### Requirement: Imported backups record their foreign source project
An imported backup SHALL record the source project id and name from the archive manifest, and SHALL be marked as imported. The system SHALL NOT require the source project or source organization to exist in the importing deployment.

#### Scenario: Imported backup keeps the source project id
- **WHEN** an archive whose manifest source project does not exist locally is imported
- **THEN** the backup record SHALL store the manifest's source project id and name
- **THEN** the import SHALL succeed without creating a project row

#### Scenario: Archive without a source project identity is rejected
- **WHEN** an archive's manifest has an empty or invalid source project id, or a source project id that disagrees with `project/config.json`
- **THEN** the import SHALL fail with a validation error

### Requirement: Imported backups support clone restore only
Restore of an imported backup SHALL be limited to clone mode. Overwrite restore SHALL be rejected for imported backups because overwrite requires a local project matching the archive's source project.

#### Scenario: Clone an imported backup
- **WHEN** a user requests a clone restore of an imported backup into an organization
- **THEN** a new project SHALL be created from the archive with a new project id
- **THEN** foreign membership references SHALL be filtered to members of the target organization
- **THEN** the restoring user SHALL be added to the new project

#### Scenario: Overwrite of an imported backup is rejected
- **WHEN** a user requests an overwrite restore of an imported backup
- **THEN** the server SHALL reject the request with `400 Bad Request`

### Requirement: Imported clones do not copy foreign user references
A clone restore of an imported backup SHALL NOT insert user identifiers that do not exist in the target deployment into foreign-keyed columns.

#### Scenario: Foreign chat owner is dropped
- **WHEN** an imported archive contains a chat conversation whose owner is not a user in the target deployment
- **THEN** the restored conversation SHALL have a null owner rather than a dangling foreign user id
