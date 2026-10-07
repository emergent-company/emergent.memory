<!-- openspec:archive-hold: superseded by add-google-drive-ingestion (attribution schema reconciled; URL ingestion dropped) -->
## 1. Source schema and dedup

- [ ] 1.1 Add a migration for `source_provider`, `source_external_id`, `source_synced_at`, `source_content_hash` and a uniqueness constraint on `(project_id, source_provider, source_external_id)` (`source_url` already exists); verify with a migration test
- [ ] 1.2 Add a unit test for upsert-by-source that creates on first call and updates on the second, then implement the store operation
- [ ] 1.3 Add a unit test that concurrent duplicate ingestion yields exactly one document, then implement conflict-safe upsert

## 2. SSRF guard and safe fetching

- [ ] 2.1 Build a new SSRF guard (no shared guard exists); add unit tests that loopback, private, link-local, and cloud-metadata targets are rejected
- [ ] 2.2 Add unit tests for redirect re-validation (public → private rejected), bounded redirect count, max response size (default 10 MiB), and timeout (default 30s), then implement the bounding
- [ ] 2.3 Add a unit test that an aborted fetch leaves no partial storage object, then implement cleanup-on-abort
- [ ] 2.4 Add unit tests that a non-success HTTP status, an unsupported content type, and an unreachable URL each return an error and create no document, then implement

## 3. Ingestion service

- [ ] 3.1 Add a unit test that a fetched URL becomes a document with `source_url` set under the `web` provider, then implement URL ingestion through the existing parse pipeline
- [ ] 3.2 Add unit tests for URL normalization (host case, default port, fragment) that equivalent URLs deduplicate, then implement normalization
- [ ] 3.3 Add a unit test that re-ingesting unchanged bytes only updates `source_synced_at`, then implement `source_content_hash` change detection
- [ ] 3.4 Add a unit test that changed bytes update the document and schedule re-extraction, then implement re-sync
- [ ] 3.5 Add a unit test that an external `(provider, external_id)` item is ingested and isolated per project, then implement the provider-agnostic entry point

## 4. API, SDK, CLI

- [ ] 4.1 Add unit tests for the ingest-by-URL endpoint (success, SSRF rejection, redirect rejection, unreachable), then implement the handler and route
- [ ] 4.2 Add unit tests for refresh (sourced document reports change; unsourced document rejected), then implement the refresh handler and route
- [ ] 4.3 Add a unit test that an unauthenticated call to the ingestion endpoints is rejected, then implement auth enforcement
- [ ] 4.4 Add SDK (`pkg/sdk/documents`) methods `IngestURL` and `Refresh`, and the gateway client method; verify with fake-backend tests
- [ ] 4.5 Add CLI commands for ingest-url and refresh; verify with command output tests

## 5. Gateway surface

- [ ] 5.1 Add an "ingest from URL" control on the documents page wired to the client method; verify with a render test
- [ ] 5.2 Add a refresh action for sourced documents wired to the client method; verify with a render test

## 6. Verification

- [ ] 6.1 `go build ./...` + `go vet ./...` + `go test ./...` in `apps/server` pass
- [ ] 6.2 `task lint` passes; `templ generate` produces no diff if gateway UI changed
- [ ] 6.3 Manual browser test: ingest a public URL, see the document and its source, refresh unchanged and changed, confirm no duplicates
