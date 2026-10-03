## Context

`migrations/00001_baseline.sql` defined `kb.data_source_integrations` and related tables;
`migrations/00089_remove_integrations_datasource_githubapp.sql` dropped them permanently.
Only OS-level connectors (`apps/connector.linux`, `apps/connector.mac`) ingest anything,
and they are agent-side, not a server SaaS ingestion path. Two in-flight OpenSpec changes
(`add-data-sources`, `add-integrations`) cover the connection **surface**; this change is
the server **framework** underneath.

## Goals / Non-Goals

**Goals**

- A durable, project-scoped source model with auth (credential reference) and sync state.
- Incremental sync with a persisted cursor; idempotent re-sync.
- A sync job lifecycle (`pending/processing/completed/failed/cancelled`) with structured
  errors.
- Ingest through the existing documents → chunking → extraction → graph pipeline.
- MCP registry's **service layer** as the preferred outbound transport (no vendor SDKs in
  the server).

**Non-Goals**

- 50 hand-written connectors (explicitly deferred; 1-2 highest-value targets documented
  as future work).
- Re-implementing chunking/extraction/graph — all reused.
- Re-introducing the dropped `data_source_integrations` design (see D4).

## Decisions

### D1 — Framework-first, not connector breadth

Onyx's value is its connector breadth; but re-implementing 50 connectors is a trap that
would block on auth, pagination, and vendor quirks. This change builds the framework
(`kb.sources` + sync lifecycle + ingestion path) with MCP as the escape hatch: any SaaS
source that has an MCP server is consumable immediately, with zero server code. First-class
connectors are limited to the 1-2 targets whose MCP story is weak (documented future work).

### D2 — MCP registry service layer is the outbound connector bus

`domain/mcprelay` is the **inbound** WebSocket star relay for NAT'd local connectors: it
requires a registered live `(projectID, instanceID)` session to route a call and **cannot
reach SaaS**. So the relay is not the SaaS ingestion transport.

The correct outbound entry is **`mcpregistry.Service.CallToolOnServer`**
(`apps/server/domain/mcpregistry/service.go:601`), the policy-enforcing layer: it resolves
the server by id scoped to the project, enforces **server and tool enablement**, the
**share-instance tool allowlist** (deny-by-default), builtin **per-tool authority**
(`AuthorizeToolCall`), and an **invocation timeout** (`mcpToolCallTimeout`, 30s). It then
dispatches external (stdio/sse/http) servers through `ProxyManager.CallToolOnServer` as the
raw transport leaf.

`ProxyManager.CallToolOnServer` (`proxy.go:124-154`) is **not** the ingestion entry: it
takes an already-resolved `*MCPServer` and connects directly, bypassing the service layer's
enablement/allowlist/authority/timeout checks. A source's `config` therefore specifies an
MCP **server + tool** registered in `mcpregistry` (a registry server row, not a relay
instance), and the sync worker invokes it through the **service** layer.

Because the sync worker is an internal caller with no API token, it SHALL use a defined
**internal service identity**: either a dedicated internal invoke method on
`mcpregistry.Service` (e.g. `CallToolOnServerInternal`) that skips only the API-token-scoped
allowlist resolution while still enforcing server/tool enablement, a deny-by-default
internal tool policy, and the timeout — or `Service.CallToolOnServer` invoked with an
explicit trusted-internal policy (nil/empty `apiTokenID` + a documented scope set) that the
resolver treats as an internal, deny-by-default caller. `domain/mcprelay` is used only for
OS-level local-node connectors (`connector.linux/internal/mcphost` hosts local tools **to**
Memory — it does not pull SaaS content). This keeps credentials in the provider/registry
layer and keeps the server free of vendor SDKs.

### D3 — Cursor-based incremental sync

`kb.sources.sync_state` holds an opaque cursor (external id / watermark / etag) + last
sync status. The sync worker reads the cursor, fetches "items after cursor" via the
transport, and advances the cursor after a successful batch. Idempotency is by source +
external item identity: re-ingesting the same external id updates rather than duplicates.
This avoids the removed design's whole-source re-read.

**Incremental semantics are scoped to cursor-capable transports.** The normative "resume
from the persisted cursor and fetch only later items" applies to sources whose transport
exposes a stable watermark/cursor/etag. A transport with **no** cursor does **not** get a
synthesized watermark in this change; instead it uses **full-sync idempotency** — re-fetch
the whole source and deduplicate by `(source_id, external_item_id)`, so repeated syncs
still do not duplicate. The source's `sync_state` records which mode it uses, and the
worker branches on it rather than assuming every source is incremental. (The fallback in
Risks is this full-sync path, not a violation of the cursor rule.)

### D4 — Migration path from `00089` (do not resurrect)

The dropped `kb.data_source_integrations` was coupled to column-based sync state on
`kb.documents` (`data_source_integration_id`, `external_source_id`, `sync_version`,
`integration_metadata` — all removed in `00089`). The new `kb.sources` is deliberately
different: it stores sync state **on the source** (not on documents), treats MCP as the
transport, and attributes documents with a single nullable `source_id` **added in this
change** — NOT a resurrection of the removed `external_source_id`/`data_source_integration_id`
columns. Document attribution needs a durable identity key: this change adds to
`kb.documents` a nullable `source_id` (FK → `kb.sources`, `ON DELETE SET NULL`) plus a
nullable `external_item_id`, with a partial unique index
`UNIQUE (source_id, external_item_id) WHERE external_item_id IS NOT NULL` so the same
external item under one source is a single row (the idempotency key for D3). Deleting a
source sets `source_id` to NULL (documents are kept — see `add-data-sources` D6 — and lose
their attribution rather than being cascaded), which the unique index accommodates because
`source_id IS NULL` rows are not constrained. The proposal documents this difference so the
framework does not silently reintroduce the removed coupling.

**Closer prior art is `kb.external_sources`** (`00001_baseline.sql:798-819`), which is
still live: it already models `project_id`, `provider_type`, `external_id`, `original_url`,
`normalized_url`, `sync_policy`, `last_etag` (a cursor), `last_synced_at`, `status`, and
`error_*`. `kb.sources` generalizes that shape (arbitrary transport/config + richer cursor
jsonb + a dedicated sync-job table) rather than resurrecting `data_source_integrations`.

### D5 — Sync job lifecycle: scheduler triggers, `kb.source_sync_jobs` owns state, `internal/jobs` runs it

`domain/scheduler` is a `robfig/cron` periodic-task runner — it only *triggers* syncs
(cron) or an on-demand request triggers a sync directly. The sync **state machine** lives
in the dedicated `kb.source_sync_jobs` table (status
`pending/processing/completed/failed/cancelled` + `dead_letter` for poison items, richer
cursor/error jsonb, `project_id` + `created_at` for RLS and per-project listing).

The worker **reuses `apps/server/internal/jobs`**, the generic table-agnostic PG queue
(`NewQueue(db, QueueConfig, logger)`, with the table/entity supplied via
`DefaultQueueConfig(tableName, entityIDColumn)`; atomic `Dequeue` with `FOR UPDATE SKIP LOCKED`,
`MarkCompleted`/`MarkFailed`, `RecoverStaleJobs`, `GetStats`) already used by chunk/graph
embedding. `kb.source_sync_jobs` supplies the source-specific columns on top of the shared
queue mechanics; no new queue is written.

**Queue column contract.** `internal/jobs.Queue` reads/writes a fixed column set, so
`kb.source_sync_jobs` MUST carry every one of them or `Dequeue`/`MarkCompleted`/
`MarkFailed`/`RecoverStaleJobs` will fail: `id`, `status`, `scheduled_at` (Dequeue
filter + retry backoff), `priority` (Dequeue ordering), `started_at`, `updated_at`
(every mutation), `attempt_count` (MarkFailed retries), `last_error` (MarkFailed error
text), and `completed_at` (MarkCompleted). The earlier draft's `finished_at` is wrong —
the queue writes `completed_at`. The source-specific columns (`source_id`, `cursor_before/
after`, `error` jsonb, and the progress fields below) sit alongside, not instead of, these.

**Progress persistence for the gateway.** The `add-data-sources` gateway `SyncJob` DTO
requires durable, restart-surviving progress: `total_items`, `processed_items`,
`successful_items`, `failed_items`, `skipped_items` (counters), `current_phase`,
`status_message` (phase/message), `trigger_type` (scheduled vs manual), and
`retry_count`/`max_retries` (mapped from the queue's `attempt_count` and
`QueueConfig.MaxAttempts`). All are columns on `kb.source_sync_jobs`, updated by the worker
as it advances; the sync-job read routes report them directly so progress is accurate after
a restart without a live in-memory state.

### D6 — Atomic single-active-job admission per source

A slow periodic sync and a manual trigger can race cursor updates if both run
concurrently for the same source. Admission SHALL enforce **at most one active job per
source**: a partial unique index
`UNIQUE (source_id) WHERE status IN ('pending','processing')` on `kb.source_sync_jobs` makes
the "one queued|running job per source" rule a database invariant, and the trigger path
SHALL insert the job under that constraint (an `ON CONFLICT`/unique-violation maps to a
"sync already in progress" result rather than enqueuing a second job).

**Stale recovery does not clear the slot by requeue.** `internal/jobs.RecoverStaleJobs`
sets a stale `processing` row back to `pending` (and resets `started_at`), and `pending`
STILL occupies the partial unique index. So a requeued job **remains** the single active
job for its source: a trigger arriving during that window SHALL return/reject the in-flight
job rather than enqueuing a new one. The admission slot is cleared **only** by a terminal
transition (`completed`, `failed`, or `cancelled`). If the source sync policy chooses to
terminal-fail a repeatedly-stale job (e.g. `MarkFailed` at `MaxAttempts`, or an explicit
`failed`/`cancelled` on dead-letter), that terminal transition — not the requeue — is what
permits the next new job.

## Risks / Trade-offs

- **Framework without a killer connector lands weak.** Mitigation: MCP-first (D2) means the
  framework is useful the day it ships for any MCP-exposed source.
- **Cursor semantics vary by source.** Some sources have no cursor. Mitigation: cursor is
  opaque; sources without cursors fall back to full-sync idempotency (still non-duplicating).
- **Credential handling.** Storing source auth must not put raw tokens in `config`.
  Mitigation: `auth` is a credential reference resolved via provider/configuration
  management.
- **Overlap with `add-data-sources`/`add-integrations`.** Those changes own the gateway UX;
  this change owns the server framework. Mitigation: explicit Impact cross-reference; this
  proposal does not re-specify their UI, and its tasks do not touch gateway routes.
