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
- MCP registry's `ProxyManager` as the preferred outbound transport (no vendor SDKs in
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

### D2 — MCP registry `ProxyManager` is the outbound connector bus

`domain/mcprelay` is the **inbound** WebSocket star relay for NAT'd local connectors: it
requires a registered live `(projectID, instanceID)` session to route a call and **cannot
reach SaaS**. So the relay is not the SaaS ingestion transport.

The correct outbound client is `domain/mcpregistry`'s `ProxyManager`
(`proxy.go:85-154`): `CallToolOnServer`/`CallTool` over stdio/sse/http, plus
`DiscoverTools`. It is the component that actually invokes an MCP server's tools. A
source's `config` therefore specifies an MCP **server + tool** registered in
`mcpregistry`; the sync worker invokes it through `ProxyManager`. `domain/mcprelay` is
used only for OS-level local-node connectors (`connector.linux/internal/mcphost` hosts
local tools **to** Memory — it does not pull SaaS content). This keeps credentials in the
provider/registry layer and keeps the server free of vendor SDKs.

### D3 — Cursor-based incremental sync

`kb.sources.sync_state` holds an opaque cursor (external id / watermark / etag) + last
sync status. The sync worker reads the cursor, fetches "items after cursor" via the
transport, and advances the cursor after a successful batch. Idempotency is by source +
external item identity: re-ingesting the same external id updates rather than duplicates.
This avoids the removed design's whole-source re-read.

### D4 — Migration path from `00089` (do not resurrect)

The dropped `kb.data_source_integrations` was coupled to column-based sync state on
`kb.documents` (`data_source_integration_id`, `external_source_id`, `sync_version`,
`integration_metadata` — all removed in `00089`). The new `kb.sources` is deliberately
different: it stores sync state **on the source** (not on documents), treats MCP as the
transport, and keeps documents clean (source attribution via a single nullable
`source_id`, added separately). The proposal documents this difference so the framework
does not silently reintroduce the removed coupling.

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
embedding. `kb.source_sync_jobs` supplies the source-specific columns (cursor, error
jsonb) on top of the shared queue mechanics; no new queue is written.

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
