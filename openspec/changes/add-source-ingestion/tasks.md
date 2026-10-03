<!-- openspec:archive-hold: spec-only change; implementation intentionally deferred (PR #1410) -->
## 1. Migration — `kb.sources` + sync jobs (TDD)

- [ ] 1.1 New migration `apps/server/migrations/<n>_create_sources.sql`: `kb.sources` (id, project_id, type, config jsonb, auth_ref, sync_state jsonb, created_at, updated_at) + `kb.source_sync_jobs` carrying **both** the `internal/jobs.Queue` column contract and the gateway progress fields: `id, project_id, source_id, status, scheduled_at, priority, started_at, updated_at, attempt_count, last_error, completed_at` (the queue reads/writes every one of these — `completed_at`, not `finished_at`) plus `cursor_before/after jsonb, error jsonb, total_items, processed_items, successful_items, failed_items, skipped_items, current_phase, status_message, trigger_type, created_at`, with RLS scoping to `project_id` (matching prior job tables). `retry_count` is served from `attempt_count`; `max_retries` from `QueueConfig.MaxAttempts` (not a column). Also add document identity/attribution columns to `kb.documents`: nullable `source_id` (FK → `kb.sources` ON DELETE SET NULL) + nullable `external_item_id`, with a partial unique index `UNIQUE (source_id, external_item_id) WHERE external_item_id IS NOT NULL` (the idempotency key). Add the single-active-job admission index on `kb.source_sync_jobs`: `UNIQUE (source_id) WHERE status IN ('pending','processing')`.
- [ ] 1.2 (TDD) Migration test: up/down round-trips; sync_state jsonb shape round-trips; FK from `documents.source_id` → `kb.sources` SET NULL on source delete (documents kept, attribution cleared); the `(source_id, external_item_id)` partial unique index rejects a duplicate external item under one source but permits NULL `external_item_id`; the admission partial unique index rejects a second `pending`/`processing` job for the same source but permits a new job after the prior one is `completed`/`failed`/`cancelled`. Also exercise the **queue operations** end-to-end against the table: `Dequeue` (reads `scheduled_at`/`priority`, sets `status='processing'` + `started_at`/`updated_at`), `MarkCompleted` (writes `completed_at`/`updated_at`), `MarkFailed` (writes `attempt_count`/`last_error`/`scheduled_at`), and `RecoverStaleJobs` — asserting every queue-required column exists and round-trips.
- [ ] 1.3 Document in the migration comment how this differs from the removed `kb.data_source_integrations` (sync state on source, not documents; MCP transport; single `source_id` + `external_item_id` attribution vs the removed `data_source_integration_id`/`external_source_id`/`sync_version` columns) and that `kb.external_sources` is the closer prior art.

## 2. Sources domain — CRUD + auth (TDD)

- [ ] 2.1 New `domain/sources` package: entities, `store.go` (CRUD + cursor read/write), `service.go` (project-scope checks, auth-ref validation), `handler.go`, `module.go`.
- [ ] 2.2 (TDD) Store unit test: CRUD persists; cursor read/write round-trips; delete removes jobs.
- [ ] 2.3 (TDD) Service unit test: cross-project source access rejected; auth stores a credential reference, not a raw token; invalid auth-ref rejected.

## 3. Sync lifecycle + cursor (TDD)

- [ ] 3.1 Sync worker over `apps/server/internal/jobs` (`NewQueue(db, jobs.DefaultQueueConfig("kb.source_sync_jobs", "source_id"), logger)`, atomic `Dequeue`, `MarkCompleted`/`MarkFailed`, `RecoverStaleJobs`): state machine `pending → processing → completed|failed|cancelled` (+`dead_letter`), cancellation flag checked between batches, structured error recorded on failure. Trigger/admission enforces the single-active-job invariant (D6): insert under the `UNIQUE (source_id) WHERE status IN ('pending','processing')` index and map a unique violation to "sync already in progress".
- [ ] 3.2 (TDD) Unit test: lifecycle transitions recorded; cancel sets `cancelled` and stops fetching; failure records structured error; partial failure leaves cursor at last-success and ingested items intact.
- [ ] 3.3 (TDD) Idempotency test: re-syncing identical external ids produces no duplicate documents/chunks/graph objects (updates in place, keyed on `(source_id, external_item_id)`).
- [ ] 3.4 (TDD) Admission test: a second trigger while a job is `pending`/`processing` is rejected (or returns the existing job) rather than enqueued — including a slow periodic + manual trigger race over the same source.
- [ ] 3.5 (TDD) Stale-job recovery test: a `processing` job whose worker died is requeued to `pending` by `RecoverStaleJobs` and **remains the single active job** — a trigger during that window returns/rejects the in-flight job rather than enqueuing a second; only a terminal transition (`completed`/`failed`/`cancelled`) clears the admission slot so the next trigger for that source is admitted.
- [ ] 3.6 (TDD) Progress persistence: the worker updates the durable counters and phase on `kb.source_sync_jobs` as it advances — `total_items`, `processed_items`, `successful_items`, `failed_items`, `skipped_items`, `current_phase`, `status_message`, and `trigger_type` — so the sync-job read routes report accurate, restart-surviving progress. Test that a job interrupted mid-run then reported after a restart still shows the persisted totals/phase (not zero).

## 4. Ingestion path → docs/chunks/graph (TDD)

- [ ] 4.1 Generic ingestion path: source item → document (create/update keyed on `(source_id, external_item_id)` via the partial unique index) → existing chunking → `domain/extraction` → `domain/graph`, attributing outputs to the source.
- [ ] 4.2 (TDD) Unit test: an ingested item creates a document and chunks; extraction output graph objects are attributable to the source.
- [ ] 4.3 (TDD) Unit test: re-ingesting an updated item updates the document in place without duplicating chunks (exercises the `(source_id, external_item_id)` upsert path).
- [ ] 4.4 (TDD) Source-deletion test: deleting a source SET NULLs `documents.source_id` (documents kept), and re-ingesting the same external item under a new source creates a distinct document (no unique-index collision).

## 5. MCP transport + discovery (TDD)

- [ ] 5.1 Wire source transport through `domain/mcpregistry`'s **`Service.CallToolOnServer`** (the policy-enforcing layer at `service.go:601`: server/tool enablement, share-instance tool allowlist, builtin per-tool authority, 30s invocation timeout), resolving the registry server by id from the source `config`. `ProxyManager.CallToolOnServer` (raw `*MCPServer`, no enablement/allowlist/timeout) SHALL be used only as the transport leaf underneath the service layer, never as the ingestion entry. A source targets a **registry server** (not a relay instance). `domain/mcprelay.Service.CallTool` (targets a live relay `instanceID`, no registry row) is the OS-connector/local-node transport only, NOT SaaS — excluded from this path.
- [ ] 5.2 (TDD) Unit test: a source configured via an MCP registry server fetches items through the **service layer** with a defined internal identity (a nil/empty `apiTokenID` + documented scope set, or a dedicated `CallToolOnServerInternal`) — and SHALL NOT route through `mcprelay` (assert the relay is never called) nor through a raw `ProxyManager.CallToolOnServer` that skips enablement/allowlist/timeout (assert the service-layer checks fire: a disabled server/tool and a denied tool are rejected).
- [ ] 5.3 Delegate new-source schema auto-discovery to `domain/discoveryjobs`.
- [ ] 5.4 (TDD) Unit test: creating a source schedules a discovery job through `discoveryjobs`.

## 6. Server REST surface the gateway (`add-data-sources`) calls (TDD)

The `add-data-sources` gateway change already assumes a `data-source-integrations` REST
surface (its SDK client `apps/server/pkg/sdk/datasources/client.go` targets
`/api/data-source-integrations/*`). This framework SHALL own the server side of that
contract, exposing `kb.sources` and `kb.source_sync_jobs` behind those routes and the
DTO shapes the gateway expects (provider catalog, provider config schema, connection
testing, sync trigger/monitor/cancel with job progress phases + item counts, and
discovery). Naming reconciliation: the server entity is `source` (`kb.sources`); the
REST resource is `data-source-integrations` (matching the gateway SDK), and `domain/sources`
maps one to the other.

- [ ] 6.1 Provider catalog + schema routes: `GET /api/data-source-integrations/providers`, `GET /api/data-source-integrations/providers/:providerType/schema`, and `GET /api/data-source-integrations/source-types` return the provider list, the JSON config schema (`{type, properties, required[]}`), and supported source types, respectively.
- [ ] 6.2 Connection-test routes: `POST /api/data-source-integrations/test-config` (unsaved config) and `POST /api/data-source-integrations/:id/test-connection` (stored config) validate credentials against the transport and return a `TestConnectionResponse`.
- [ ] 6.3 Source CRUD routes: `GET/POST /api/data-source-integrations`, `GET/PATCH/DELETE /api/data-source-integrations/:id`, with `auth`/`config` write-only on responses (secrets never round-tripped) and a project-scoped 404 for cross-project ids.
- [ ] 6.4 Sync routes: `POST /api/data-source-integrations/:id/sync` (trigger — enforces the single-active-job admission rule and returns "already in progress" on conflict), `GET /api/data-source-integrations/:id/sync-jobs`, `/sync-jobs/latest`, `/sync-jobs/:jobId` (each `SyncJob` carries status phase + progress counts, e.g. ingested/processed/failed item counts, for the gateway's polling UI), and `POST /api/data-source-integrations/:id/sync-jobs/:jobId/cancel`.
- [ ] 6.5 Discovery routes: `POST /discovery-jobs/projects/{projectId}/start` (document_ids payload), plus status/list/finalize, delegated to `domain/discoveryjobs` (already consumed in §5) with DTOs matching the gateway's `StartDiscoveryJob`/`FinalizeDiscoveryJob` calls.
- [ ] 6.6 (TDD) Handler tests: each route covers success, validation, and project-scope (404) paths; config/auth are absent from responses; sync-trigger conflict returns the documented "already in progress" result; sync-job DTOs include the full gateway `SyncJob` shape — `status`, `totalItems`, `processedItems`, `successfulItems`, `failedItems`, `skippedItems`, `currentPhase`, `statusMessage`, `errorMessage`, `triggerType`, `retryCount` (from `attempt_count`), `maxRetries`, `startedAt`, `completedAt` — mapped from the persisted `kb.source_sync_jobs` columns so the values survive a restart.

## 7. Verify + deferred scope

- [ ] 7.1 `task build` (server compile).
- [ ] 7.2 `task lint` for the touched modules.
- [ ] 7.3 Deferred (documented, NOT implemented): the 1-2 highest-value first-class connectors; per-source ACL integration (`add-resource-acl`).
- [ ] 7.4 Cross-check: the server REST surface (§6) satisfies the `add-data-sources` gateway DTO/client assumptions (providers, schema, test-connection, sync trigger/monitor/cancel with job progress, discovery); `add-data-sources` owns gateway UX, `add-integrations` owns GitHub App + general UX — this change touches the server framework + the server side of that REST surface only.
