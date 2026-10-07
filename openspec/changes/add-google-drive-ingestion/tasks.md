## 1. Spec reconciliation + migration (TDD)

- [ ] 1.1 Add `<!-- openspec:archive-hold: superseded by add-google-drive-ingestion -->` to `openspec/changes/add-source-ingestion/tasks.md` and `openspec/changes/add-external-document-ingestion/tasks.md`, and note the supersession in each proposal.
- [ ] 1.2 New migration `apps/server/migrations/00226_document_sources_versions.sql`:
  - `kb.documents`: add `canonical_id uuid NOT NULL DEFAULT uuid_generate_v4()`, `supersedes_id uuid NULL REFERENCES kb.documents(id) ON DELETE SET NULL`, `version integer NOT NULL DEFAULT 1`, `source_id uuid NULL`, `source_external_id text NULL`, `source_revision_id text NULL`, `source_text_hash text NULL`, `source_synced_at timestamptz NULL`, `source_status text NOT NULL DEFAULT 'active'`, `source_deleted_at timestamptz NULL`; backfill `canonical_id = id`, `version = 1`, `supersedes_id = NULL`.
  - Indexes/constraints: `UNIQUE (canonical_id, version)`; partial `UNIQUE (source_id, source_external_id) WHERE source_external_id IS NOT NULL AND supersedes_id IS NULL`; index on `(project_id, source_id, source_status)`.
  - `kb.sources`: `id, project_id, provider, display_name, config jsonb, encrypted_credentials bytea, sync_policy, sync_interval_minutes, cursor jsonb, status, error_count, last_error, last_error_at, last_synced_at, created_at, updated_at`; RLS on `project_id`.
  - `kb.source_sync_jobs`: the `internal/jobs.Queue` contract (`id, status, scheduled_at, priority, started_at, updated_at, attempt_count, last_error, completed_at`) plus `project_id, source_id, trigger_type, total_items, processed_items, successful_items, failed_items, skipped_items, current_phase, status_message, error jsonb, cursor_before jsonb, cursor_after jsonb, created_at`; partial `UNIQUE (source_id) WHERE status IN ('pending','processing')`; RLS on `project_id`.
  - Migration comment documenting the difference from the `00089`-dropped design and `kb.external_sources`.
- [ ] 1.3 (TDD) Migration test: up/down round-trips; backfill sets `canonical_id=id`/`version=1`; the head partial unique index rejects a second HEAD for one `(source_id, source_external_id)` but permits many version rows; the admission partial unique index rejects a second `pending`/`processing` job for one source but permits a new one after a terminal state; deleting a source SET NULLs `documents.source_id` while keeping the document.

## 2. Document versioning + ingestion seam (TDD)

- [ ] 2.1 `domain/documents`: add version-aware repository methods — `GetHeadBySource(projectID, sourceID, externalID)`, a direct (dedup-bypassing) insert, transactional `CreateVersion` (share HEAD `canonical_id`, set `supersedes_id`, increment `version`), and history read by `canonical_id`. Extend entity/DTOs with the versioning/attribution columns.
- [ ] 2.2 Default `List`/`GetByID` read HEAD only (`supersedes_id IS NULL`) and exclude `source_status='deleted_from_source'` unless explicitly requested; add `includeHistory`/source-status filters.
- [ ] 2.3 (TDD) Versioning unit test: a changed item appends a version with the same `canonical_id` and incremented `version`; the prior row is retained; exactly one HEAD exists; the ingestion path never calls the content-hash dedup (assert no collapse when identical bytes/duplicate content exist).
- [ ] 2.4 (TDD) HEAD reads test: multi-version documents return only HEAD by default; history returns all versions ordered; deleted-from-source excluded from default listing but present in history.
- [ ] 2.5 Ingestion seam service: upsert by `(source_id, source_external_id)`; change detection by parsed-text hash; unchanged text updates only `source_revision_id`/`source_synced_at`; changed text creates a version, recreates chunks, and triggers extraction; removals mark `deleted_from_source`; re-appearance appends an active version.
- [ ] 2.6 (TDD) Seam unit test: first ingest creates HEAD; identical text re-ingest is a no-op version; changed text creates a version and schedules extraction; removal marks status and retains history; re-appearance reactivates.

## 3. Google Drive transport client (TDD)

- [ ] 3.1 New Drive client package: construct `drive/v3` service from decrypted OAuth2 credentials via `option.WithHTTPClient`; methods to list a folder subtree (`'<folder_id>' in parents and trashed=false` + MIME filter, `supportsAllDrives=true`, paginated), read `headRevisionId`, export Google-native files, and download binaries; bounded size/time caps; injectable HTTP client.
- [ ] 3.2 Credential handling: encrypt/decrypt the OAuth2 credential blob via `pkg/crypto`, following the `provider.EncryptedCredential` pattern; connection-test call.
- [ ] 3.3 (TDD) Client unit test with a mocked `http.Client`/Drive server: enumeration filters by folder + MIME and paginates; revision pre-filter skips unchanged; export/download paths return content; size/time caps abort; connection test reports success and failure.
- [ ] 3.4 Promote `google.golang.org/api` (Drive v3) from indirect to a direct dependency in `apps/server/go.mod`; `go mod tidy`.

## 4. Sources domain + sync worker + scheduler + REST (TDD)

- [ ] 4.1 New `domain/sources`: entities, `store.go` (source CRUD + cursor read/write), `service.go` (project scope, credential encryption, config validation, sync admission), `handler.go`, `module.go`.
- [ ] 4.2 (TDD) Sources store/service unit test: CRUD persists; cross-project source access returns not-found; credentials are stored encrypted and never returned; invalid config (missing `folder_id`) rejected.
- [ ] 4.3 Sync worker over `kb.source_sync_jobs` (reusing `internal/jobs` queue semantics): lifecycle `pending → processing → completed|failed|cancelled`, per-item error isolation, durable progress counters, cancellation checked between items, cursor/state advance only on success, `RecoverStaleJobs` on startup.
- [ ] 4.4 (TDD) Worker unit test: lifecycle transitions recorded; cancel stops fetching; per-item failure records a structured error without aborting; progress counters persist; stale job recovery keeps the single-active-job invariant.
- [ ] 4.5 (TDD) Idempotency/integration test against a fake Drive: full sync ingests in-scope items; re-sync with no changes creates nothing; a changed item versions once; a removed item is marked deleted-from-source; admission rejects a second active job.
- [ ] 4.6 Scheduler: register a cron task that admits due sources by `sync_interval_minutes` through the same admission rule.
- [ ] 4.7 (TDD) Scheduler unit test: a due source is admitted; a not-yet-due source is skipped; an active job suppresses a duplicate trigger.
- [ ] 4.8 REST handlers/routes matching `pkg/sdk/datasources/client.go`: providers, provider schema, source-types, test-config, test-connection, source CRUD, sync trigger, sync-jobs list/latest/detail/cancel; auth + project-scope middleware.
- [ ] 4.9 (TDD) Handler test: each route covers success, validation, and project-scope (404) paths; credentials/config absent from responses; sync-trigger conflict returns "already in progress"; sync-job DTO carries the full SDK field set.
- [ ] 4.10 Delegate schema discovery for a new source to `domain/discoveryjobs`; (TDD) assert a discovery job is scheduled on source creation.

## 5. SDK + gateway UI (TDD)

- [ ] 5.1 Verify `pkg/sdk/datasources` round-trips against the new routes (SDK client test or gateway fake backend).
- [ ] 5.2 Gateway client methods + JSON handlers for source CRUD, provider catalog/schema, connection test, sync trigger/monitor/cancel; handler tests cover success, validation, and memory-error paths.
- [ ] 5.3 Data Sources UI: sidebar entry + list (provider/status/last-sync badges, empty state), connect form (Drive folder + MIME types + credentials), detail/edit page with test-connection, trigger-sync, sync-job history/progress, and delete; templ render tests for empty/populated/in-progress/delete states.
- [ ] 5.4 Document version history view: render a document's versions with source status; templ render test for single- and multi-version documents.

## 6. Verification

- [ ] 6.1 `task build` (server compile) passes.
- [ ] 6.2 Server unit tests for the touched domains pass; migration test passes.
- [ ] 6.3 `templ generate` produces no diff; gateway `go build ./...` + `go vet ./...` + `go test ./...` pass.
- [ ] 6.4 `task lint` passes for the touched modules.
- [ ] 6.5 Manual browser test: connect a Drive source for a test folder + type filter, test the connection, trigger a sync, observe job progress, confirm v1 documents and chunks; re-sync (no-op); change a document's text and confirm a new version + re-extraction; remove it in Drive and confirm `deleted_from_source` with history retained.
- [ ] 6.6 Update the spec if any behavior changed during implementation (spec and implementation ship in the same PR).

## 7. Deferred (documented, NOT implemented)

- [ ] 7.1 Drive Changes API cursor as an enumeration optimization (replacing full folder scans).
- [ ] 7.2 In-app Google OAuth authorize/callback flow (first iteration takes an operator-supplied refresh token).
- [ ] 7.3 Additional first-class connectors and the generic MCP-first source bus (`add-source-ingestion`).
- [ ] 7.4 Version snapshot compaction and cross-version diff UI.
<!-- openspec:archive-hold: first-iteration implementation shipped; section 7 (§7.1 Drive Changes API cursor, §7.2 in-app OAuth authorize/callback, §7.3 additional connectors/generic MCP-first source bus, §7.4 version compaction+cross-version diff UI) is deferred to follow-ups, so this change is intentionally not archive-ready yet. -->
