## Why

Project backups can be exported and downloaded, but there is no supported path to import an archive into a deployment that did not create it. Every restore request carries only a `backupId`, the row is resolved from the calling deployment's own database, and the archive is fetched from that deployment's own `documents` bucket. Moving a project to another host therefore requires hand-placing the object at the exact expected storage key and hand-inserting a matching `kb.backups` row — the unsupported workaround forced by #592 (issue #603).

## What Changes

- Add `POST /api/v1/organizations/:orgId/backups/import` (multipart `file`): accepts an archive, validates manifest + checksums, stores it under a fresh backup key, and registers a `ready` backup row so the existing clone-restore path can consume it.
- Mark imported backups with a new `kb.backups.imported` flag, because their `project_id` is a foreign source project that has no local `kb.projects` row.
- Drop the `kb.backups.project_id` foreign key: the column records the archive's source project, which intentionally may live in another deployment. `project_id` stays `NOT NULL` (always set to the manifest's source project id).
- Restrict imported backups to clone restore (overwrite requires a local project that matches the source id) and make clone handle foreign identities safely (membership filtering, nulling foreign user references).
- Document the import-and-restore procedure.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `backup-restore`: add self-service archive import; define imported-backup semantics (clone-only, foreign project id).

## Impact

- `apps/server/domain/backups/`: `routes.go`, `handler.go`, `service.go`, `importer.go`, `restorer.go`, `entity.go`.
- `apps/server/migrations/00157_allow_imported_backups.sql`.
- `docs/site/user-guide/backups.md`.
- No CLI or gateway surface in this change; the API is the self-service path.
