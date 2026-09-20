# Backups

Emergent Memory supports project-level backups. A backup captures the complete state of a project — graph objects, relationships, documents, and metadata — and stores it for later restoration.

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

## Backup status fields

| Field | Description |
|---|---|
| `status` | `creating` · `ready` · `failed` · `deleted` |
| `progress` | Integer 0–100 indicating completion percentage |
| `sizeBytes` | Size of the backup archive in bytes |
| `errorMessage` | Present if status is `failed` |
| `completedAt` | Timestamp when the backup finished |
| `expiresAt` | Optional expiry after which the backup is auto-deleted |
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
