## Purpose

A gateway web UI page for managing project backups: list, create, download, and delete backups for the active project, and surface each backup's status, progress, size, statistics, and integrity checksums.

## ADDED Requirements

### Requirement: Navigate to backups

The gateway SHALL include a Backups entry in the sidebar that opens the backups page for the active project.

#### Scenario: Sidebar entry present

- **WHEN** the sidebar loads
- **THEN** a Backups link is shown and opens the backups page

### Requirement: List backups

The gateway SHALL list the active project's backups, most recent first, each showing its status, progress, size, and creation time, and linking to the dedicated details page. Checksums are NOT shown in the list.

#### Scenario: Backups listed

- **WHEN** the backups page loads and backups exist for the active project
- **THEN** the backups are listed ordered by recency with their status, progress, size, and creation time

#### Scenario: Row links to details

- **WHEN** a backup row renders
- **THEN** its title links to the backup details page at `/backups/:id`, and no checksums are shown in the list

#### Scenario: No backups

- **WHEN** the backups page loads and no backups exist for the active project
- **THEN** a clear "no backups" state is shown, not an error

### Requirement: Create a backup

The gateway SHALL allow a user to create a full backup of the active project, and MUST surface the asynchronous result (the backup is returned with a `creating` status and progress is tracked by polling).

#### Scenario: Backup created

- **WHEN** the user submits the create-backup form
- **THEN** the gateway creates the backup and shows it as `creating`, updating to `ready` or `failed` as progress completes

#### Scenario: Backup create rejected

- **WHEN** the create-backup request is rejected by the Memory service (e.g. an invalid retention value)
- **THEN** the page shows an error and does not show a phantom backup

### Requirement: View backup details page

The gateway SHALL serve a dedicated backup details page at `/backups/:id` showing: an overview (project name, full backup id, storage key, status, progress, size, created/completed/expires timestamps, and the creator when present); the archive contents statistics (`documents`, `chunks`, `graphObjects`, `graphRelationships`, `chatConversations`, `chatMessages`, `extractionJobs`, `projectMemberships`, `files`, and `totalSizeBytes`) with absent keys omitted; the current include settings (documents, chunks, graph, chat, journal, deleted items) as clear on/off rows; retention/expiry and backup type; a static archive-format explainer (ZIP at the storage key containing `manifest.json`, a database dump, and a `files/` tree); and the integrity checksums. The page MUST degrade gracefully for a `creating` or `failed` backup (partial stats, no checksums).

#### Scenario: Details page renders

- **WHEN** the user opens a backup's details page
- **THEN** the overview, contents statistics, include settings, archive-format explainer, and (when available) checksums are shown, and the list is not required to show checksums

#### Scenario: Missing backup

- **WHEN** the requested backup cannot be loaded (missing, org unresolved, or Memory unreachable)
- **THEN** the page renders an error state with the same style as the list page, not a 500

#### Scenario: Creating backup

- **WHEN** the backup is still in the `creating` state
- **THEN** progress is shown with a progress bar, available statistics are shown, and no checksums are presented

#### Scenario: Failed backup

- **WHEN** the backup is in the `failed` state
- **THEN** its error message is shown prominently and its checksums are not presented as valid

### Requirement: Show backup details and checksums

The gateway SHALL display a backup's integrity checksums (`manifestChecksum` and `contentChecksum`) in full (untruncated, selectable) on the details page when the backup is `ready` and the checksums are present. Checksums are shown on the details page, not in the list.

#### Scenario: Ready backup details

- **WHEN** a `ready` backup has checksums
- **THEN** both checksums are shown in full on its details page

#### Scenario: Failed backup details

- **WHEN** a backup is in the `failed` state
- **THEN** its error message is shown and its checksums are not presented as valid

### Requirement: Download a backup

The gateway SHALL allow downloading a backup archive when it is `ready`, and MUST NOT offer a download for a backup that is not `ready`.

#### Scenario: Download ready backup

- **WHEN** the user requests a download of a `ready` backup
- **THEN** the browser downloads the backup archive

#### Scenario: Download unavailable

- **WHEN** a backup is not `ready` (still `creating` or `failed`)
- **THEN** the download action is not offered

### Requirement: Delete a backup

The gateway SHALL allow a user to permanently delete a backup, and MUST confirm the destructive action before deleting.

#### Scenario: Delete a backup

- **WHEN** the user confirms deletion of a backup
- **THEN** the backup is deleted and disappears from the list

### Requirement: Restore is not yet available

The gateway MUST NOT present a working restore action: the Memory service returns "not implemented" for restore, so the gateway SHALL either hide restore or surface it as unavailable rather than attempt and fail.

#### Scenario: Restore surfaced as unavailable

- **WHEN** the backups page renders
- **THEN** restore is either hidden or marked as unavailable, and no restore request that would fail is issued

### Requirement: Authenticated, project-scoped access

The backups page and its backing calls SHALL require the same session/API-key trust boundary as the rest of the gateway, and MUST be scoped to the signed-in user's active project and organization.

#### Scenario: Unauthenticated request

- **WHEN** a client requests backups without a valid session or API key
- **THEN** the gateway returns an unauthorized response and no backup data

### Requirement: Handle an unreachable memory service

The backups page SHALL show an error state when the Memory service cannot be reached, without crashing the rest of the UI.

#### Scenario: Memory unreachable

- **WHEN** the backups page loads and the Memory service is unreachable
- **THEN** an error message is shown and no partial data is presented as authoritative
