## Context

Backup archives are self-describing ZIPs: `manifest.json`, `project/config.json`, `database/*.ndjson`, `files/*`. `Importer.validate` already verifies the manifest self-checksum plus the database and files checksums, so an uploaded archive can be validated without trusting its origin. Restore always resolves a `kb.backups` row and downloads the archive from `documents/backups/{orgId}/{backupId}/backup.zip`.

The blocker is schema-level: `kb.backups.project_id UUID NOT NULL REFERENCES kb.projects(id)` and `kb.restores.backup_id UUID NOT NULL REFERENCES kb.backups(id)`. A foreign archive's source project (and org) do not exist in the target deployment, so no valid backup row can be inserted.

## Goals / Non-Goals

**Goals**
- Self-service import of an externally produced archive over HTTP.
- Validate size, ZIP structure, manifest, and checksums before anything is persisted.
- Reuse the existing clone-restore pipeline unchanged in its core remap logic.
- Keep local (same-deployment) backups working exactly as before.

**Non-Goals**
- CLI or gateway UI for import (follow-up).
- Automatic project creation during import (clone does that).
- Rewriting or normalizing the archive bytes (archives are checksum-immutable).
- Cross-deployment overwrite restore.

## Decisions

### D1 — Register imported archives as first-class `kb.backups` rows
The restore job requires a `backup_id` FK, so the import must persist a row. Store the uploaded archive at `GenerateStorageKey(targetOrgID, newBackupID)` and insert a `ready` backup. The existing clone endpoint then works with no special casing.

### D2 — `project_id` records the source project; drop its FK
`project_id` semantically means "the project this snapshot came from". For an imported archive that is the foreign UUID from the manifest, so the FK to `kb.projects` is wrong for this case. Drop the constraint (keep `NOT NULL`, always populated) and add an explicit `imported BOOLEAN NOT NULL DEFAULT false` marker for disambiguation.

- Alternative (rejected): add `source_project_id` + make `project_id` nullable. `project_id` already is the source id; the split forces `coalesce` in the restorer and a NULL-handling path in overwrite for no benefit.
- Alternative (rejected): a separate import table. `restores.backup_id` would become polymorphic — disproportionate complexity.

### D3 — Imported backups are clone-only
Overwrite restore requires `backup.project_id == URL projectId` and a local project row. An imported archive's source project is foreign, so overwrite is rejected at the handler and defensively in the restorer.

### D4 — Foreign identities are handled at restore time, never by rewriting the archive
Clone remap already rewrites the project id. Additionally, an imported clone must not copy foreign `core.user_profiles` references:
- membership filtering is forced for imported backups (foreign user ids are dropped; the importer is added to the new project);
- `chat_conversations.owner_user_id` is nulled when unmapped.

### D5 — Field mapping for an imported backup row
`organization_id` = target org (exists); `project_id`/`project_name` = manifest source project (both validated non-empty); `status` = `ready`; `size_bytes` = uploaded length; `backup_type` = manifest `backupType` (default `full`); `expires_at` = now + 30d; `created_at`/`completed_at` = import time (not the manifest's foreign `createdAt`, which would break cursor ordering); checksums copied from the manifest; `includes.imported` = true.

## Risks / Trade-offs

- **Dropped FK on `kb.backups.project_id`** — weakening a constraint globally. Backups are immutable snapshots and local rows never dangle in practice (soft delete + expiry). The down migration deletes imported rows and restores the constraint.
- **Whole archive buffered in memory** — the existing importer already `io.ReadAll`s the archive, so import inherits that; a server-side `MaxBytesReader` cap bounds it.
- **Foreign ids in non-constrained uuid columns** — harmless; only the three constrained references (project, membership user, chat owner) are handled.

## Migration Plan

`00157_allow_imported_backups.sql`:
- Up: `DROP CONSTRAINT IF EXISTS backups_project_id_fkey`; `ADD COLUMN imported boolean NOT NULL DEFAULT false`.
- Down: delete restores + backups where `imported`; drop the column; re-add the FK.

## Open Questions

- Should import expose `retentionDays`? Implemented with a fixed 30-day default to match `CreateBackup`; can be parameterized later.
