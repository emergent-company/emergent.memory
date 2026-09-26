## 1. Schema & Migration

- [ ] 1.1 Add migration `apps/server/migrations/00182_document_revisions.sql` (next unused version — `00179`–`00181` are taken): `ALTER TABLE kb.documents ADD COLUMN IF NOT EXISTS document_group_id uuid`, `ADD COLUMN IF NOT EXISTS version_number integer NOT NULL DEFAULT 1`, `ADD COLUMN IF NOT EXISTS supersedes_document_id uuid`, `ADD COLUMN IF NOT EXISTS is_current boolean NOT NULL DEFAULT true`, `ADD COLUMN IF NOT EXISTS applied_at timestamptz`
- [ ] 1.2 Backfill existing rows (`document_group_id = id`, `applied_at = created_at`), then `ALTER COLUMN document_group_id SET NOT NULL`
- [ ] 1.3 Add `CREATE UNIQUE INDEX ... ON kb.documents (document_group_id, version_number)` and `CREATE UNIQUE INDEX ... ON kb.documents (document_group_id) WHERE is_current`
- [ ] 1.4 Write the `-- +goose Down` block dropping the two indexes and the five columns
- [ ] 1.5 Unit test: migration idempotency (re-run is a no-op), `document_group_id` backfill, and that `document_group_id` is NOT NULL

## 2. Documents Domain — Revision Chain

- [ ] 2.1 Extend `Document` in `apps/server/domain/documents/entity.go` with `DocumentGroupID`, `VersionNumber`, `SupersedesDocumentID`, `IsCurrent`, `AppliedAt`
- [ ] 2.2 Initialize the new fields in every creation path — `Create` and `CreateFromUpload` in `service.go` set `document_group_id = id`, `version_number = 1`, `is_current = true`, `supersedes_document_id = NULL`, `applied_at = now()` (first revision is current and applied)
- [ ] 2.3 Update list/get queries in `repository.go` to select the new columns; default document list to current revisions only, with an `includeSuperseded` option
- [ ] 2.4 Add `CreateRevision(ctx, baseDocumentID, params)` to `service.go`: single transaction computing `version_number = max+1`, inserting the new row **pending** (`is_current = false`, `applied_at = NULL`), leaving the current revision untouched, retrying on a concurrent-insert conflict
- [ ] 2.5 Add `ListRevisions(ctx, projectID, documentID)` ordered by `version_number DESC`, exposing `isCurrent` and `appliedAt` so clients can derive current/pending/superseded
- [ ] 2.6 Add `DiscardRevision(ctx, projectID, revisionID)`: allow only pending revisions (reject current and applied); delete revision row + chunks + staging branch; re-point the successor's `supersedes_document_id` to the discarded revision's predecessor
- [ ] 2.7 Change generic delete (`Delete` / `BulkDelete`) to remove the whole revision group, so no group is left with zero current revisions
- [ ] 2.8 Add `ApplyRevisionPromotion(ctx, revisionID)` helper (or inline in apply) that, in one transaction, sets `applied_at`, promotes the revision to current, and demotes the prior current — preserving exactly one current per group
- [ ] 2.9 Unit test: creating a revision inserts a pending row (`isCurrent=false`, no `appliedAt`) and leaves the current revision and its version number unchanged
- [ ] 2.10 Unit test: concurrent revision creation yields distinct increasing version numbers and still exactly one current revision
- [ ] 2.11 Unit test: promotion sets the applied revision current and the prior current superseded, with exactly one current
- [ ] 2.12 Unit test: discarding the current revision and discarding an applied revision are both rejected; discarding a pending revision succeeds (undo) and rewires a pending successor's supersession link
- [ ] 2.13 Unit test: generic delete removes all revisions of a group and leaves no group with zero revisions

## 3. Revision HTTP API

- [ ] 3.1 Add `POST /api/documents/:id/revisions` handler (multipart, reuses upload path) under `documents:write`
- [ ] 3.2 Add `GET /api/documents/:id/revisions` handler under `documents:read`
- [ ] 3.3 Add `DELETE /api/documents/:id/revisions/:revId` handler under `documents:delete`
- [ ] 3.4 Expose `documentGroupId`, `versionNumber`, `isCurrent`, `appliedAt`, `supersedesDocumentId` on document read responses; document ids address revisions directly (no implicit current-resolution)
- [ ] 3.5 Register the routes in `apps/server/domain/documents/routes.go`
- [ ] 3.6 Update swagger annotations and regenerate `apps/server/docs/swagger`
- [ ] 3.7 Unit test: revision upload rejects an unknown base document and a disallowed file without mutating the group; list and delete enforce the new scopes

## 4. Extraction Provenance (`kb.object_chunks`)

- [ ] 4.1 Thread chunk ids alongside chunk text through batch construction in `apps/server/domain/extraction/object_extraction_worker.go`
- [ ] 4.2 Write `kb.object_chunks` rows (object_id, chunk_id, extraction_job_id, confidence) for every created/updated object in `persistResults`, at batch scope (every chunk that fed the batch → every object from that batch)
- [ ] 4.3 On re-extraction, ensure an object produced again gains provenance rows for the latest job's chunks
- [ ] 4.4 Unit test: each extracted object has provenance rows for each chunk that fed its batch, with the extraction job id
- [ ] 4.5 Unit test: re-extraction of the same object adds provenance for the new job's chunks

## 5. Revision Diff

- [ ] 5.1 Promote `github.com/pmezard/go-difflib` to a direct dependency in `apps/server/go.mod`
- [ ] 5.2 Add a revision-diff service computing a unified line diff plus added/removed line counts from two revisions' parsed content
- [ ] 5.3 Implement entity-delta assembly by comparing the staged extraction's `(type, key)` set and content against main-graph heads → added/updated; plus added/removed relationships
- [ ] 5.4 Compute the `removed` set via provenance: main objects with provenance from an earlier revision of the group and none from the target revision's chunks, absent from the staged set
- [ ] 5.5 Add `GET /api/documents/:id/revisions/diff?from=&to=` handler defaulting `to` to the newest revision (pending if present) and `from` to its predecessor; reject cross-group pairs
- [ ] 5.6 Unit test: line diff reports the correct added/removed lines for a known pair of contents
- [ ] 5.7 Unit test: entity delta classifies added/updated/removed correctly, including a removal attributable only to a superseded revision

## 6. Staging & Apply

- [ ] 6.1 Add a revision extraction mode that makes staging-branch creation and recording mandatory and aborts extraction on failure (no fallback to writing main); record `staging_branch_id` on the job before writing objects
- [ ] 6.2 Add a graph-service reconciliation method that applies staged branch objects to main by `(type, key)` — create when absent, new version when content differs, no-op when identical — and rewrites provenance to the final main object ids. Do **not** use `MergeBranch` (canonical-id based)
- [ ] 6.3 Add the removal pass: tombstone main objects attributable only to superseded revisions of the group (provenance from an earlier revision, none from the applied revision, absent from staged set)
- [ ] 6.4 Add `POST /api/documents/:id/revisions/:revId/apply`: reconcile + remove atomically, mark the revision applied, promote it to current (demote the prior current), delete the staging branch; idempotent on re-apply
- [ ] 6.5 Extend DiscardRevision to delete the revision's staging branch and staged objects
- [ ] 6.6 Unit test: apply creates added objects and versions updated objects (no duplicate rows for an existing `(type, key)`), and promotes the revision to current
- [ ] 6.7 Unit test: apply tombstones objects with provenance only from superseded revisions and leaves unchanged/shared objects live
- [ ] 6.8 Unit test: staging-branch creation failure aborts revision extraction and writes nothing to main
- [ ] 6.9 Unit test: applying the same revision twice reports no additional changes
- [ ] 6.10 Integration test: upload v1 → extract → upload v2 (pending) → diff → apply v2 → assert graph matches the v2 delta, v2 is current, v1 superseded, and removed objects are tombstoned
- [ ] 6.11 Integration test: upload v2 (pending) then discard it — graph and current revision are unchanged (undo)

## 7. Upload Auto-Detection

- [ ] 7.1 Add `DetectUploadTarget(ctx, projectID, filename, fileHash, externalSourceID)` to the documents service: evaluate signals in priority order (external source id → file hash → normalized filename vs current revisions), project-scoped, returning matched / ambiguous-candidates / none
- [ ] 7.2 Accept and persist an optional `externalSourceId` on uploads; include it in detection
- [ ] 7.3 Wire the upload handler: explicit `revisionOf` or the revisions endpoint bypasses detection; otherwise a confident single match creates a pending revision, ambiguity/none creates a standalone document (ambiguous returns suggested candidate ids)
- [ ] 7.4 Add a per-request detection opt-out (`?detect=false`) and honour `--no-detect` from the CLI
- [ ] 7.5 Record the matched signal and detected document id on the created revision (metadata)
- [ ] 7.6 Unit test: file-hash match and external-id match auto-link; single filename match auto-links; no match is standalone
- [ ] 7.7 Unit test: multiple filename matches create a standalone document and return suggestions without linking
- [ ] 7.8 Unit test: detection is skipped when `revisionOf` is supplied, when the revisions endpoint is used, and when `detect=false`; matches never cross projects
- [ ] 7.9 Integration test: upload `notes.md`, edit it, re-upload `notes.md` → a pending revision of the first document is created

## 8. SDK

- [ ] 8.1 Add `CreateRevision` / `ListRevisions` / `GetRevisionDiff` / `ApplyRevision` / `DiscardRevision` to `apps/server/pkg/sdk/documents/client.go`
- [ ] 8.2 Add `NoDetect` / detection-outcome fields to the SDK upload types and `UploadWithOptions`
- [ ] 8.3 Unit test: SDK request/response round-trip against an httptest server

## 9. CLI

- [ ] 9.1 Add `--revision-of` and `--no-detect` to `memory documents upload`
- [ ] 9.2 Report the detection outcome (linked / standalone / suggested) in the upload output
- [ ] 9.3 Add `memory documents revisions <id>`
- [ ] 9.4 Add `memory documents diff <id> [--from] [--to]`
- [ ] 9.5 Add `memory documents apply-revision <id> --revision <n>` and `discard-revision <id> --revision <n>`
- [ ] 9.6 Unit test: flag parsing, precedence of `--revision-of` over detection, and error when `--revision` is missing on apply/discard
- [ ] 9.7 Update the CLI reference skill/docs for the new subcommands and flags

## 10. Web UI (gateway)

- [ ] 10.1 Add a Revisions section to `apps/web-ui/gateway/documents.templ` (list newest-first; single-version state when the group has one revision)
- [ ] 10.2 Render the revision diff + entity delta with a parsing/processing state
- [ ] 10.3 Add Apply / Discard actions with confirmation; omit Discard on current and applied revisions
- [ ] 10.4 Add a revision upload control on the document detail page
- [ ] 10.5 Report the upload detection outcome (linked / standalone / suggested) on the documents page, with a force-standalone control
- [ ] 10.6 Run `templ generate` and add/extend handler tests for the new sections

## 11. Verification & Spec Sync

- [ ] 11.1 Compile: `(cd apps/server && go build ./...)`, `(cd apps/web-ui/gateway && go build ./...)`, `(cd apps/cli && go build ./...)`
- [ ] 11.2 `templ generate`
- [ ] 11.3 `task lint`
- [ ] 11.4 `task build`, `task test`, and `task test:integration` from the repo root
- [ ] 11.5 Manual browser check of the Revisions section (upload → diff → apply → discard) and the auto-detect upload path
- [ ] 11.6 Confirm no OpenSpec drift: `openspec validate document-versioning --strict`
- [ ] 11.7 Post-merge: `openspec archive document-versioning`
