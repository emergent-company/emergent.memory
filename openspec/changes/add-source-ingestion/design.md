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
- A sync job lifecycle (queued/running/failed/cancelled) with structured errors.
- Ingest through the existing docs → chunks → extraction → graph pipeline.
- MCP relay as the preferred transport (no vendor SDKs in the server).

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

### D2 — MCP relay is the connector bus

`domain/mcpregistry` registers MCP servers/tools; `domain/mcprelay` relays invocations;
`connector.linux/internal/mcphost` already hosts MCP servers on the OS side. A source's
`config` therefore specifies an MCP server + tool (or a generic `type` mapping to a relay
route), and the sync worker invokes it through the relay. This keeps credentials in the
relay/provider layer and keeps the server free of vendor SDKs.

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

### D5 — Sync job lifecycle reuses scheduler/job-ledger

The job lifecycle is implemented as a scheduler-backed job (or a dedicated
`kb.source_sync_jobs` table) with a state machine `queued → running → failed|cancelled|success`.
Cancellation sets a flag checked by the worker between batches. Reuse `domain/scheduler`
and the existing job-ledger reporting (`job-ledger-reporting`) rather than a new queue.

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
