## Why

A generic datasource/integration framework once existed in
`migrations/00001_baseline.sql` and was permanently removed in
`migrations/00089_remove_integrations_datasource_githubapp.sql`. Only OS-level
connectors remain (`apps/connector.linux`, `apps/connector.mac`). There is no SaaS
ingestion: no path to pull email, Drive, GitHub, or ClickUp content into the graph.

Two existing OpenSpec changes already describe the *connection surfaces*: `add-data-sources`
and `add-integrations` (gateway connect/configure/test/sync UX). What neither covers is the
**server-side ingestion framework** — the durable `kb.sources` model, auth/credentials, the
sync job lifecycle, and the pipeline that turns source content into documents → chunks →
extraction → graph. This change is that framework, deliberately framework-first rather than
50 hand-written connectors.

## What Changes

- Add `kb.sources (type, config, auth, sync_state)` and a generic ingestion path feeding
  the **existing** pipeline: docs → chunks → extraction → graph.
- Prefer consuming SaaS sources via **MCP**: Memory already has `domain/mcpregistry`,
  `domain/mcprelay`, and `connector.linux/internal/mcphost`. The MCP relay is the
  connector bus — a source declares an MCP server/tool as its transport rather than
  hard-coding a vendor SDK.
- Reuse `discoveryjobs` for schema auto-discovery on new sources.
- A hand-written first-class connector is documented as future work for only the 1-2
  highest-value targets; **not** implemented in this change.
- Requirements cover source CRUD + auth config, incremental sync with state/cursor, sync
  job lifecycle (queued/running/failed/cancelled), idempotent re-sync, error surfacing,
  and that ingested content lands in documents/chunks/graph.

## Capabilities

### New Capabilities

- `source-ingestion`: a durable source model, auth/credential storage, incremental sync
  with cursor/state, and a sync job lifecycle that feeds the existing documents → chunks →
  extraction → graph pipeline, with MCP as the preferred transport.

### Modified Capabilities

<!-- None: this change consumes existing pipelines but writes no delta against an
existing capability spec. -->

### Related / Consumed Capabilities

- `document-extraction` (consumed): ingested source documents flow through the existing
  extraction pipeline unchanged.
- `mcp-connector` (consumed): the MCP relay acts as the source transport bus.

## Impact

- **DB** (`apps/server/migrations/`): `kb.sources` (reintroduced as a durable model, not
  the removed `kb.data_source_integrations`) + `kb.source_sync_jobs` with sync
  state/cursor.
- **Server** (`apps/server/domain/`): new `sources` domain (CRUD + auth config + sync
  orchestration); reuse `domain/discoveryjobs` (schema auto-discovery), `domain/mcpregistry`
  + `domain/mcprelay` (transport), and the existing ingestion pipeline
  (`domain/documents` → chunking → `domain/extraction` → `domain/graph`).
- **Migration path from `00089`**: document how the reintroduced model differs from the
  dropped `data_source_integrations` (durable, MCP-first, cursor-based) so we do not
  resurrect the dropped design.

## Relationship to existing changes (not duplicated here)

- `add-data-sources` (capability `data-sources`) = the gateway connect/sync **surface**.
- `add-integrations` (capability `integrations`) = GitHub App + general integration **UX**.
- This change = the server-side **ingestion framework** those surfaces call. The three are
  complementary; this proposal references both in Impact and does not re-specify their UI.

## Dependency

Relates to `add-resource-acl` for per-source permissions (a source's ingested resources
should inherit the source's ACL). Not blocked by it; ingestion ships first and ACL is
applied at read time.
