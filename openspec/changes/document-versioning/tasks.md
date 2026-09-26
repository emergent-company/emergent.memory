## 1. Schema & Migration

- [ ] 1.1 Add migration `apps/server/migrations/00179_document_revisions.sql`: `ALTER TABLE kb.documents ADD COLUMN IF NOT EXISTS document_group_id uuid`, `version_number integer NOT NULL DEFAULT 1`, `supersedes_document_id uuid`, `is_current boolean NOT NULL DEFAULT true`
- [ ] 1.2 Backfill existing rows: `document_group_id = id`, `version_number = 1`, `is_current = true`, `supersedes_document_id = NULL`
- [ ] 1.3 Add `CREATE UNIQUE INDEX ... ON kb.documents (document_group_id) WHERE is_current` and an index on `(document_group_id, version_number)`
- [ ] 1.4 Write the `-- +goose Down` block dropping the two indexes and the four columns
- [ ] 1.5 Unit test: migration idempotency guard (re-run is a no-op) and `document_group_id` backfill assertion in a test fixture DB

## 2. Documents Domain — Revision Chain

- [ ] 2.1 Extend `Document` in `apps/server/domain/documents/entity.go` with `DocumentGroupID`, `VersionNumber`, `SupersedesDocumentID`, `IsCurrent`
- [ ] 2.2 Update list/get queries in `repository.go` to select the new columns and filter by `document_group_id`
- [ ] 2.3 Add `CreateRevision(ctx, baseDocumentID, params)` to `service.go`: resolve base, compute `version_number = max+1`, demote prior current, insert new row in one transaction
- [ ] 2.4 Add `ListRevisions(ctx, projectID, documentID)` ordered by `version_number DESC`
- [ ] 2.5 Add `DiscardRevision(ctx, projectID, revisionID)`: reject if `is_current`, else remove revision + chunks + staged objects
- [ ] 2.6 Unit test: creating a revision sets `version_number = 2`, demotes prior current, promotes new; exactly one current per group
- [ ] 2.7 Unit test: concurrent revision creation resolves to distinct increasing version numbers (no two currents)
- [ ] 2.8 Unit test: discarding the current revision is rejected; discarding a superseded one succeeds

## 3. Revision HTTP API

- [ ] 3.1 Add `POST /api/documents/:id/revisions` handler (multipart, reuses upload path) under `documents:write`
- [ ] 3.2 Add `GET /api/documents/:id/revisions` handler under `documents:read`
- [ ] 3.3 Add `DELETE /api/documents/:id/revisions/:revId` handler under `documents:delete`
- [ ] 3.4 Expose `documentGroupId`, `versionNumber`, `isCurrent` on document read responses
- [ ] 3.5 Register the routes in `apps/server/domain/documents/routes.go`
- [ ] 3.6 Update swagger annotations and regenerate `apps/server/docs/swagger`
- [ ] 3.7 Unit test: revision upload rejects an unknown base document and a disallowed file without mutating the group

## 4. Extraction Provenance (`kb.object_chunks`)

- [ ] 4.1 Thread chunk ids alongside chunk text through batch construction in `apps/server/domain/extraction/object_extraction_worker.go`
- [ ] 4.2 Write `kb.object_chunks` rows (object_id, chunk_id, extraction_job_id, confidence) for every created/updated object in `persistResults`
- [ ] 4.3 Ensure re-extraction associates the newest job's provenance with the object without duplicating stale rows
- [ ] 4.4 Unit test: each extracted object has provenance rows for each source chunk it was derived from
- [ ] 4.5 Unit test: an object present in a later extraction gets refreshed provenance for the latest job

## 5. Revision Diff

- [ ] 5.1 Promote `github.com/pmezard/go-difflib` to a direct dependency in `apps/server/go.mod`
- [ ] 5.2 Add a revision-diff service computing a unified line diff plus added/removed line counts from two revisions' parsed content
- [ ] 5.3 Implement entity-delta assembly: staged objects for the target revision matched by `(type, key)` → added/updated/removed; plus added/removed relationships
- [ ] 5.4 Compute the `removed` set from provenance (objects whose only chunks belong to the source revision)
- [ ] 5.5 Add `GET /api/documents/:id/revisions/diff?from=&to=` handler with default from=previous, to=current; reject cross-group pairs
- [ ] 5.6 Unit test: line diff reports the correct added/removed lines for a known pair of contents
- [ ] 5.7 Unit test: entity delta classifies added/updated/removed correctly, including a removal whose provenance is fully absent

## 6. Staging & Apply

- [ ] 6.1 Add a "do not auto-merge" mode to revision extraction and record `staging_branch_id` on the job
- [ ] 6.2 Add `POST /api/documents/:id/revisions/:revId/apply`: dry-run merge → execute merge → tombstone fully-removed-provenance objects; return added/updated/removed counts
- [ ] 6.3 Make apply idempotent (re-applying writes nothing) and add the graph-service tombstone path if missing
- [ ] 6.4 Extend DiscardRevision to delete the revision's staging branch and staged objects
- [ ] 6.5 Unit test: apply adds/updates staged objects and tombstones objects with fully-removed provenance
- [ ] 6.6 Unit test: apply leaves objects with surviving provenance live
- [ ] 6.7 Unit test: applying the same revision twice reports no additional changes
- [ ] 6.8 Integration test: upload v1 → extract → upload v2 → diff → apply → assert graph state matches the v2 delta

## 7. SDK

- [ ] 7.1 Add `CreateRevision` / `ListRevisions` / `GetRevisionDiff` / `ApplyRevision` / `DiscardRevision` to `apps/server/pkg/sdk/documents/client.go`
- [ ] 7.2 Unit test: SDK request/response round-trip against an httptest server

## 8. CLI

- [ ] 8.1 Add `--revision-of` to `memory documents upload`
- [ ] 8.2 Add `memory documents revisions <id>`
- [ ] 8.3 Add `memory documents diff <id> [--from] [--to]`
- [ ] 8.4 Add `memory documents apply-revision <id> --revision <n>` and `discard-revision <id> --revision <n>`
- [ ] 8.5 Unit test: flag parsing and error when `--revision` is missing on apply/discard
- [ ] 8.6 Update the CLI reference skill/docs for the new subcommands

## 9. Web UI (gateway)

- [ ] 9.1 Add a Revisions section to `apps/web-ui/gateway/documents.templ` (list, latest-first)
- [ ] 9.2 Render the revision diff + entity delta with a parsing/processing state
- [ ] 9.3 Add Apply / Discard actions with confirmation; omit Discard on the current revision
- [ ] 9.4 Add a revision upload control on the document detail page
- [ ] 9.5 Run `templ generate` and add/extend handler tests for the new sections

## 10. Verification & Spec Sync

- [ ] 10.1 `go build ./...` and `cd apps/web-ui/gateway && go build ./...`
- [ ] 10.2 `templ generate`
- [ ] 10.3 `task lint`
- [ ] 10.4 `task test` and `task test:integration`
- [ ] 10.5 Manual browser check of the Revisions section (upload → diff → apply → discard)
- [ ] 10.6 Confirm no OpenSpec drift: `openspec validate document-versioning --strict`
- [ ] 10.7 Post-merge: `openspec archive document-versioning`
