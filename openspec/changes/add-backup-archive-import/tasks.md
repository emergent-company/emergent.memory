## 1. Migration — allow imported backups

- [ ] 1.1 Add `apps/server/migrations/00157_allow_imported_backups.sql`: Up drops `backups_project_id_fkey` and adds `imported boolean NOT NULL DEFAULT false`; Down deletes imported restores/backups, drops the column, re-adds the FK. Verify: `task migrate:up` then `task migrate:status` shows 00157 applied; `\d kb.backups` has `imported` and no `backups_project_id_fkey`.

## 2. Importer — parse from bytes, validate source identity

- [ ] 2.1 Extract the ZIP parse/validate body of `Importer.Load` into `Parse(data []byte) (*Archive, error)`; `Load` downloads then calls `Parse`. Verify: existing importer/restorer tests still pass.
- [ ] 2.2 In `validate`, require a non-empty parseable-UUID `manifest.project.id`, non-empty `manifest.project.name`, and `manifest.project.id == project/config.json["id"]`; reject with a clear error otherwise. Verify: new unit test covers each rejection.
- [ ] 2.3 Add `ErrInvalidArchive` sentinel; `Parse` wraps validation failures with it. Verify: `errors.Is` works in a unit test.

## 3. Service + entity — import and register

- [ ] 3.1 Add `Imported bool` to `Backup` (`bun:"imported,notnull,default:false"`). Verify: `go build ./...`.
- [ ] 3.2 Add `Service.ImportBackup(ctx, orgID, userID string, data []byte, retentionDays int) (*Backup, error)`: `Parse`, upload to `GenerateStorageKey(orgID, newBackupID)` as `application/zip`, insert the `ready` backup (field mapping per design D5). Return `ErrInvalidArchive` untouched; wrap other failures. Verify: service unit test with a synthetic valid archive.
- [ ] 3.3 Reject a non-`ErrInvalidArchive` failure path without leaving a storage object behind. Verify: unit test asserts no upload on validation failure (and no row).

## 4. Handler + route — multipart upload

- [ ] 4.1 Add `Handler.ImportBackup`: require auth; org-membership check (mirror `restoreClone`); `http.MaxBytesReader` + `c.FormFile("file")`; size check (`MaxImportArchiveSize`); read bytes; call service; 400 on `ErrInvalidArchive`, 413 on oversize, 415 on non-ZIP content. Verify: handler unit tests for success, missing file, oversize, invalid archive, non-member (403).
- [ ] 4.2 Register `org.POST("/backups/import", handler.ImportBackup)`. Verify: route present; `go build ./...`.
- [ ] 4.3 Reject overwrite restore of an imported backup in `restoreOverwrite` (`400`) . Verify: handler unit test.

## 5. Restorer — safe clone of imported archives

- [ ] 5.1 Guard `Restorer.restore`: `imported` + `overwrite` returns an error. Verify: restorer unit test.
- [ ] 5.2 In `restoreClone`, force membership filtering for imported backups (`sameOrg := backup.OrganizationID == req.TargetOrgID && !backup.Imported`). Verify: restorer unit test asserts foreign members are dropped and the importer is added.
- [ ] 5.3 Add `owner_user_id` to `chat_conversations.nullIfUnmapped`. Verify: restorer test asserts a foreign owner is nulled on clone.

## 6. Docs + verification

- [ ] 6.1 Document the import-and-restore procedure in `docs/site/user-guide/backups.md` (endpoint, multipart shape, limits, clone-only follow-up). Verify: doc reads correctly and matches the implemented route.
- [ ] 6.2 Run `go build ./...`, `go vet ./...`, `task lint`, and the backups package tests from `apps/server`. Verify: all clean.
- [ ] 6.3 Update the `backup-restore` main spec via the change's delta (no manual sync in this PR). Verify: `openspec validate add-backup-archive-import` passes.
