## 1. Migration — `kb.sources` + sync jobs (TDD)

- [ ] 1.1 New migration `apps/server/migrations/<n>_create_sources.sql`: `kb.sources` (id, project_id, type, config jsonb, auth_ref, sync_state jsonb, created_at, updated_at) + `kb.source_sync_jobs` (id, project_id, source_id, status, cursor_before/after, error jsonb, created_at, started_at, finished_at), with RLS scoping to `project_id` (matching prior job tables).
- [ ] 1.2 (TDD) Migration test: up/down round-trips; sync_state jsonb shape round-trips; FK cascade from source to jobs.
- [ ] 1.3 Document in the migration comment how this differs from the removed `kb.data_source_integrations` (sync state on source, not documents; MCP transport) and that `kb.external_sources` is the closer prior art.

## 2. Sources domain — CRUD + auth (TDD)

- [ ] 2.1 New `domain/sources` package: entities, `store.go` (CRUD + cursor read/write), `service.go` (project-scope checks, auth-ref validation), `handler.go`, `module.go`.
- [ ] 2.2 (TDD) Store unit test: CRUD persists; cursor read/write round-trips; delete removes jobs.
- [ ] 2.3 (TDD) Service unit test: cross-project source access rejected; auth stores a credential reference, not a raw token; invalid auth-ref rejected.

## 3. Sync lifecycle + cursor (TDD)

- [ ] 3.1 Sync worker over `apps/server/internal/jobs` (`NewQueue("kb.source_sync_jobs", ...)`, atomic `Dequeue`, `MarkCompleted`/`MarkFailed`, `RecoverStaleJobs`): state machine `pending → processing → completed|failed|cancelled` (+`dead_letter`), cancellation flag checked between batches, structured error recorded on failure.
- [ ] 3.2 (TDD) Unit test: lifecycle transitions recorded; cancel sets `cancelled` and stops fetching; failure records structured error; partial failure leaves cursor at last-success and ingested items intact.
- [ ] 3.3 (TDD) Idempotency test: re-syncing identical external ids produces no duplicate documents/chunks/graph objects (updates in place).

## 4. Ingestion path → docs/chunks/graph (TDD)

- [ ] 4.1 Generic ingestion path: source item → document (create/update by source + external id) → existing chunking → `domain/extraction` → `domain/graph`, attributing outputs to the source.
- [ ] 4.2 (TDD) Unit test: an ingested item creates a document and chunks; extraction output graph objects are attributable to the source.
- [ ] 4.3 (TDD) Unit test: re-ingesting an updated item updates the document in place without duplicating chunks.

## 5. MCP transport + discovery (TDD)

- [ ] 5.1 Wire source transport through `domain/mcpregistry`'s `ProxyManager` (`CallToolOnServer`/`CallTool`) using `mcpregistry` server/tool lookup; no vendor SDK in the server. (`mcprelay` is the OS-connector/local-node transport only, NOT SaaS.)
- [ ] 5.2 (TDD) Unit test with a fake MCP tool: a source configured via MCP fetches items through `ProxyManager`.
- [ ] 5.3 Delegate new-source schema auto-discovery to `domain/discoveryjobs`.
- [ ] 5.4 (TDD) Unit test: creating a source schedules a discovery job through `discoveryjobs`.

## 6. Verify + deferred scope

- [ ] 6.1 `task build` (server compile).
- [ ] 6.2 `task lint` for the touched modules.
- [ ] 6.3 Deferred (documented, NOT implemented): the 1-2 highest-value first-class connectors; per-source ACL integration (`add-resource-acl`).
- [ ] 6.4 Cross-check: no overlap with `add-data-sources` (gateway connect/sync surface) or `add-integrations` (GitHub App + general UX) — this change touches server framework only.
