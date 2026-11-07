# Backups

Memory supports project-level backups. A backup captures the complete state of a project — graph objects, relationships, documents, and metadata — and stores it for later restoration.

---

## Creating a Backup

```http
POST /api/v1/projects/{projectId}/backups
Content-Type: application/json

{
  "backupType": "full"
}
```

Backup creation is **asynchronous**. The response returns a backup record with `status: "creating"`. Poll to check progress.

### Backup types

| Type | Description |
|---|---|
| `full` | Complete snapshot of all project data |
| `incremental` | Only the changes since the last backup (`parentBackupId` required) |

---

## Scheduled backups

A project can run its backups automatically. Each project has at most one
schedule: enable it, set a cadence, and the server starts a normal backup every
time the cadence fires.

```http
PUT /api/v1/projects/{projectId}/backup-schedule
Content-Type: application/json

{
  "enabled": true,
  "cronSchedule": "0 2 * * *",
  "retentionDays": 30,
  "includeChat": false,
  "includeJournal": false,
  "includeDeleted": false
}
```

| Field | Description |
|---|---|
| `enabled` | Whether the schedule runs. Defaults to `true` when omitted on save. |
| `cronSchedule` | Standard **5-field** cron (`minute hour day-of-month month day-of-week`) evaluated in **UTC**. |
| `retentionDays` | Retention applied to every backup the schedule creates. Default 30, range 1–365. |
| `includeChat` / `includeJournal` / `includeDeleted` | What each scheduled archive includes. |

Reading, and removing, the schedule:

```http
GET    /api/v1/projects/{projectId}/backup-schedule   # 404 when no schedule is configured
DELETE /api/v1/projects/{projectId}/backup-schedule   # 204
```

Rules:

- **Minimum interval is 15 minutes.** A cron expression that fires more often is
  rejected with `400`. Common cadences: `0 * * * *` (hourly), `0 */6 * * *
  (every 6 hours), `0 2 * * *` (daily 02:00 UTC), `0 2 * * 0` (weekly, Sunday).
- **Schedules are evaluated in UTC**, so they do not follow the viewer's
  timezone or daylight-saving shifts.
- **Editing recomputes the next run immediately** — a new cadence applies at once
  instead of waiting out the old expression.
- **Missed fires collapse into one catch-up run.** If the server was down across
  several slots, the schedule runs once and then resumes at the next slot rather
  than firing a burst.
- **No pile-up.** A due fire is skipped when the project already has a backup in
  progress (`status: "creating"`).
- Scheduled backups are attributed to the user who saved the schedule, and the
  `lastBackupId` on the schedule points at the most recent one.

The schedule's response also carries the runner's bookkeeping:

| Field | Description |
|---|---|
| `nextRunAt` | Next time the schedule will fire (UTC) |
| `lastRunAt` | When the runner last fired it |
| `lastBackupId` | The backup the last fire started |

### Retention

`expiresAt` is written at creation (`createdAt + retentionDays`, default 30
days). A daily sweep enforces it:

1. A backup whose `expiresAt` has passed is **soft-deleted** — it disappears from
   the backups list but its row and archive are kept.
2. After a **14-day grace window**, the archive object and row are purged.

The grace window is deliberate: an over-aggressive `retentionDays` costs a
support request, not the data.

### Managing schedules from the CLI

```bash
memory backups schedule set --cron "0 2 * * *"                 # create or replace
memory backups schedule set --cron "0 */6 * * *" --retention-days 14 --include-chat
memory backups schedule set --cron "0 2 * * *" --disabled      # save, but do not run
memory backups schedule get                                     # "No backup schedule configured." when unset
memory backups schedule delete
```

The same controls are on the **Backups** page in the web UI, in the *Scheduled
backups* card.

---

## Backup status fields

| Field | Description |
|---|---|
| `status` | `creating` · `ready` · `failed` · `deleted` |
| `progress` | Integer 0–100 indicating completion percentage |
| `sizeBytes` | Size of the backup archive in bytes |
| `errorMessage` | Present if status is `failed` |
| `completedAt` | Timestamp when the backup finished |
| `expiresAt` | Expiry after which the backup is auto-deleted (set from the create request's `retentionDays`, default 30 days) |
| `stats` | Object counts and other summary metrics |

---

## Listing Backups

```http
GET /api/v1/organizations/{orgId}/backups
```

---

## Getting a Backup

```http
GET /api/v1/organizations/{orgId}/backups/{backupId}
```

---

## Downloading a Backup

```http
GET /api/v1/organizations/{orgId}/backups/{backupId}/download
```

Returns a signed download URL or streams the backup archive directly.

---

## Restoring a Backup

!!! warning "Destructive operation"
    Restoring a backup **overwrites** the current project state. This cannot be undone. Create a fresh backup before restoring if you want to preserve the current state.

```http
POST /api/v1/projects/{projectId}/restore
Content-Type: application/json

{
  "backupId": "<backup-id>"
}
```

Monitor the restore job:

```http
GET /api/v1/projects/{projectId}/restores/{restoreId}
```

---

## Deleting a Backup

```http
DELETE /api/v1/organizations/{orgId}/backups/{backupId}
```

---

## Importing an archive from another deployment

Backups are self-describing ZIP archives (`manifest.json`, `project/config.json`, `database/*.ndjson`, `files/*`). You can import an archive produced by another deployment so it can be restored locally.

```http
POST /api/v1/organizations/{orgId}/backups/import
Content-Type: multipart/form-data

file: <backup.zip>
retentionDays: 30   # optional, 1-365 (default 30)
```

Requirements and limits:

| Constraint | Value |
|---|---|
| Field | `file` (multipart/form-data) |
| Maximum size | 1 GiB |
| Format | ZIP only |
| Authorization | Must be a member of the target organization |
| Manifest | Must pass structure, project-id, and checksum validation |

On success the endpoint returns `201 Created` with a backup record that has `status: "ready"` and `imported: true`. Its `projectId` records the archive's *source* project — a foreign project that does not exist in this deployment.

Imported backups are **clone-only**. They cannot be used for an overwrite restore (overwrite requires a local project matching the source project id). To restore an imported backup, clone it into the target organization:

```http
POST /api/v1/organizations/{orgId}/restore
Content-Type: application/json

{
  "backupId": "<imported-backup-id>",
  "targetProjectName": "Restored project"
}
```

The clone creates a new project with a new project id, drops foreign user references, and adds the restoring user to the new project.

---

## Checksums

Each backup includes `manifestChecksum` and `contentChecksum` for integrity verification. Compare these values after downloading to confirm the archive has not been corrupted.
