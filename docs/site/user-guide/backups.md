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

## Checksums

Each backup includes `manifestChecksum` and `contentChecksum` for integrity verification. Compare these values after downloading to confirm the archive has not been corrupted.
