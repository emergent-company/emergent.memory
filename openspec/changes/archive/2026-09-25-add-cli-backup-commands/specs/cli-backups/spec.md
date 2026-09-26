# Spec: cli-backups

## Purpose

CLI surface for creating, listing, downloading, importing, deleting, and restoring project backups via `memory backups` and `memory restores`.

## ADDED Requirements

### Requirement: Backup create with optional wait

`memory backups create --project <id|name>` SHALL create a full backup for the target project and print the resulting backup. When `--wait` is supplied the command SHALL poll the backup status until it reaches `ready` or `failed`, and a `failed` backup SHALL return its `errorMessage` as an error.

#### Scenario: Create returns immediately

- **WHEN** `memory backups create --project <id>` runs against an authenticated server
- **THEN** the command exits 0 and prints a backup with status `creating`

#### Scenario: Create with wait polls to ready

- **WHEN** `memory backups create --project <id> --wait` runs and the backup eventually reaches `ready`
- **THEN** the command exits 0 and prints the `ready` backup

#### Scenario: Create with wait surfaces failure

- **WHEN** `memory backups create --project <id> --wait` runs and the backup reaches `failed`
- **THEN** the command exits non-zero and reports the backup's `errorMessage`

#### Scenario: Wait timeout

- **WHEN** `memory backups create --project <id> --wait --timeout 10m` does not reach a terminal status within the timeout
- **THEN** the command exits non-zero with a timeout error

### Requirement: Backup list with org and project scoping

`memory backups list` SHALL list backups for the resolved organization, optionally filtered by project and paginated via `--limit` and `--cursor`.

#### Scenario: List scoped to org

- **WHEN** `memory backups list --org-id <id>` runs
- **THEN** the command exits 0 and lists the organization's backups

#### Scenario: List filtered by project

- **WHEN** `memory backups list --project <id>` runs
- **THEN** the command lists only that project's backups

#### Scenario: List with pagination

- **WHEN** `memory backups list --limit 20 --cursor <c>` runs
- **THEN** the command passes `limit` and `cursor` to the server

### Requirement: Backup get

`memory backups get <backupId>` SHALL fetch and print a single backup, resolving the organization via `--org-id` or the first organization.

#### Scenario: Get prints backup

- **WHEN** `memory backups get <backupId> --org-id <id>` runs against an authenticated server
- **THEN** the command exits 0 and prints the backup details

### Requirement: Backup download

`memory backups download <backupId> --out <path>` SHALL stream the backup archive to `--out` and print the bytes written. The archive SHALL be streamed, not buffered, so large backups do not exhaust memory.

#### Scenario: Download writes file

- **WHEN** `memory backups download <backupId> --out backup.zip` runs against a `ready` backup
- **THEN** the command exits 0, writes the archive to `backup.zip`, and prints the number of bytes written

#### Scenario: Download of non-ready backup fails

- **WHEN** `memory backups download <backupId> --out backup.zip` runs against a non-`ready` backup
- **THEN** the command exits non-zero with an error

### Requirement: Backup delete

`memory backups delete <backupId>` SHALL delete the backup, requiring `--force` when the target is not confirmed.

#### Scenario: Delete with force

- **WHEN** `memory backups delete <backupId> --force` runs
- **THEN** the command exits 0 and the backup is deleted

#### Scenario: Delete without force prompts

- **WHEN** `memory backups delete <backupId>` runs without `--force`
- **THEN** the command requires explicit confirmation before deleting

### Requirement: Backup import

`memory backups import <archive.zip>` SHALL upload a backup archive (multipart field `file`, optional `retentionDays`) to the resolved organization, streaming the archive rather than buffering it.

#### Scenario: Import succeeds

- **WHEN** `memory backups import archive.zip --org-id <id>` runs against an authenticated server
- **THEN** the command exits 0 and prints the imported `ready` backup

#### Scenario: Import rejects oversized or non-ZIP archive

- **WHEN** `memory backups import` uploads a non-ZIP or oversized archive
- **THEN** the command exits non-zero with the server's error

### Requirement: Clone restore create with optional wait

`memory restores create --backup <backupId>` SHALL create a clone restore into the resolved organization (the server forces clone mode). When `--wait` is supplied the command SHALL poll restore status until `completed` or `failed`, and print the `targetProjectId`.

#### Scenario: Create clone restore

- **WHEN** `memory restores create --backup <backupId> --org-id <id>` runs
- **THEN** the command exits 0 and prints a restore with status `pending`

#### Scenario: Create with wait polls to completed

- **WHEN** `memory restores create --backup <backupId> --wait` runs and the restore completes
- **THEN** the command exits 0 and prints the completed restore with its `targetProjectId`

#### Scenario: Create with wait surfaces failure

- **WHEN** `memory restores create --backup <backupId> --wait` runs and the restore fails
- **THEN** the command exits non-zero and reports the restore's `errorMessage`

### Requirement: Restore get

`memory restores get <restoreId>` SHALL fetch and print a single restore job status.

#### Scenario: Get prints restore

- **WHEN** `memory restores get <restoreId>` runs against an authenticated server
- **THEN** the command exits 0 and prints the restore status, progress, and target project

### Requirement: Retention-day validation

The CLI SHALL validate `--retention-days` to the inclusive range 1..365 before issuing a request.

#### Scenario: Invalid retention rejected

- **WHEN** `memory backups create --retention-days 0` or `--retention-days 400` runs
- **THEN** the command exits non-zero with a validation error and makes no request
