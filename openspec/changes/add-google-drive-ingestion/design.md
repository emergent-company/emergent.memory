## Context

`apps/server/domain/documents` supports upload/list/get/delete and drives the existing
chunking → extraction → graph pipeline, but has no source-sync path and no version history.
`kb.documents` carries `source_type` and `source_url`, neither populated. The committed SDK
client `apps/server/pkg/sdk/datasources/client.go` targets `/api/data-source-integrations/*`,
but no server domain or routes exist. The old datasource framework (`kb.data_source_integrations`
with column-based sync state on documents, including `sync_version`) was dropped in
`migrations/00089`. Reusable primitives: `internal/jobs` (PG queue with `Dequeue` /
`MarkCompleted` / `MarkFailed` / `RecoverStaleJobs`), `domain/scheduler` (robfig/cron),
`domain/discoveryjobs`, `pkg/xberg` (HTML/binary → text), `pkg/crypto`, and the
`provider.EncryptedCredential` encrypted-credential pattern.

Two in-flight spec-only changes conflict on document attribution and neither implements
versioning. This change reconciles and implements.

## Goals / Non-Goals

**Goals**

- One reconciled document-attribution schema on `kb.documents`.
- First-class document versioning (supersedes chain) with HEAD reads and history retrieval.
- Versioning driven by real content change (parsed-text hash), not raw byte/etag churn.
- A durable, project-scoped source model and a monitored sync-job lifecycle.
- A working, folder- and type-scoped Google Drive connector feeding the existing pipeline.
- Deleted-from-source items retain history and are marked, not destroyed.
- The server REST surface matches the committed SDK contract so `add-data-sources` can build UI on it.

**Non-Goals**

- The generic MCP-first source-transport bus and the full `kb.sources`/`source_sync_jobs`
  framework spec'd by `add-source-ingestion` (superseded). Drive is a native first-class connector.
- URL/web-page ingestion and its SSRF guard (`add-external-document-ingestion`'s URL half; superseded).
- Hand-written connectors beyond Google Drive (Drive is the single target here).
- Cross-version diffing/merge UI; version history is read-only.
- Per-source ACLs (`add-resource-acl`) — noted as a follow-up.

## Decisions

### D1 — One reconciled attribution schema; do not resurrect `00089`

The two prior proposals disagree. This change picks a single set of new `kb.documents` columns:
`source_id` (FK → `kb.sources`, `ON DELETE SET NULL`), `source_external_id`,
`source_revision_id`, `source_text_hash`, `source_synced_at`, `source_status`,
`source_deleted_at`. This is deliberately **not** the removed `external_source_id` /
`data_source_integration_id` / `sync_version` design: sync state lives on the source, attribution
is one `source_id` + provider item id, and versioning is the new supersedes chain (D2), not
`sync_version`.

- Why: two competing schemas cannot both ship; a partial unique index on
  `(source_id, source_external_id) WHERE source_external_id IS NOT NULL AND supersedes_id IS NULL`
  gives exactly one HEAD row per external item (the idempotency key) while allowing many version rows.
- Alternative: keep `add-external-document-ingestion`'s `source_provider`/`source_external_id`
  pair — rejected because it splits identity across two columns and drops the source FK needed for
  cursor/credential ownership.

### D2 — Versioning is a supersedes chain on `kb.documents`

`kb.documents` gains `canonical_id` (stable identity across versions), `supersedes_id`
(self-FK, `ON DELETE SET NULL`), and `version`. Following the existing graph-object convention
(`domain/graph`: HEAD is `supersedes_id IS NULL` = highest version), `supersedes_id` is a
**forward pointer**: a change inserts a **new row** with the HEAD's `canonical_id`,
`supersedes_id = NULL` (it becomes HEAD), and `version = <old version> + 1`, and in the same
transaction sets the **prior HEAD's** `supersedes_id` to the new row's id, retiring it. HEAD is
the row whose `supersedes_id IS NULL`; the self-FK is `DEFERRABLE INITIALLY DEFERRED` so the
prior head can point at the not-yet-inserted new row before it is inserted.

- Why: mirrors the existing graph-object versioning (`canonical_id` / `supersedes_id` / `version`),
  so extraction over a new document version naturally produces new graph-object versions linked by
  the same canonical identity. Old document versions stay queryable.
- Why not an append-only `kb.document_versions` side table (the `kb.chunk_revisions` pattern):
  chunks, extraction jobs, and graph attribution are already keyed by `document_id`; a new document
  row reuses those paths unchanged, whereas a side table would need a parallel chunk/extraction
  identity.
- Consequence: a new version re-chunks and re-extracts; the head swap and re-chunk run in one
  transaction before the extraction trigger.
- The existing `Service.Create` content-hash dedup (`GetByContentHash`) MUST NOT be on the
  ingestion path — it would collapse versions. Ingestion uses dedicated repository methods.

### D3 — Versioning is triggered by parsed-text difference

A provider revision id (`headRevisionId`) is the cheap pre-filter: if the HEAD's
`source_revision_id` equals the current one and the item is not otherwise dirty, the sync skips
it. If it differs, the item is fetched and parsed, and a new version is created **only when the
hash of the parsed text differs** from the HEAD's `source_text_hash`. When the text is identical,
the sync updates only `source_revision_id` and `source_synced_at` (no new version, no re-extract,
no duplicate graph objects).

- Why: export/format churn (Docs/Slides export bytes, PDF metadata) changes raw bytes and
  revision ids without changing meaning; keying on extracted text avoids version noise and
  duplicate extraction cost.
- `source_revision_id` and `source_text_hash` are both stored on the HEAD only; older versions
  keep the values they were created with.

### D4 — Native Google Drive transport, folder- and type-scoped

Auth is an OAuth2 refresh token (client id/secret + refresh token) encrypted at rest via
`pkg/crypto`, following the `provider.EncryptedCredential` pattern; no raw secret is stored in
`config`. The transport uses `google.golang.org/api/drive/v3` with an injected `*http.Client`
(`option.WithHTTPClient`) so tests run against a mock. `config` holds `folder_id` (required),
optional `shared_drive_id`, `mime_types` (allowlist), and `recursive`.

Each sync enumerates the configured folder subtree with `files.list`
(`'<folder_id>' in parents and trashed = false` plus the MIME filter, `supportsAllDrives=true`),
paginated, and applies per-item change detection (D3). Google-native docs are fetched with
`files.export` (text/plain or an office format the pipeline can parse); binaries are fetched with
`files.get(alt=media)`. Fetches enforce size and time caps.

- Why folder enumeration rather than the Drive Changes API: the configured scope is a folder
  subtree, and per-item revision pre-filtering makes re-enumeration cheap and correct without
  owning a changes cursor; the idempotent `(source_id, source_external_id)` key makes full
  re-scans non-duplicating. A Changes-API optimization is a documented follow-up, not required.
- Why native rather than MCP: Drive's MCP story is weak and the connector needs OAuth credential
  and export semantics that a generic tool-call bus cannot express; the MCP bus remains the
  documented path for future providers.

### D5 — Deleted-from-source is a status, not a delete

When a change reports an in-scope item removed/trashed, the HEAD document is marked
`source_status = 'deleted_from_source'` and stamped `source_deleted_at`; the row, its chunk tree,
its version history, and its graph objects are retained. Default document listing and retrieval
exclude `deleted_from_source`; version history still shows it. If the item re-appears in scope,
a new active version is appended and `source_status` returns to `active`.

- Why: the user explicitly wants history retained; hard-deleting would destroy the audit trail
  and orphan graph attribution.
- Deleting the **source** (not an item) is different: it SET NULLs `documents.source_id` and keeps
  documents, matching the FK contract.

### D6 — Sync lifecycle reuses `internal/jobs`; one active job per source

`kb.source_sync_jobs` carries the full `internal/jobs.Queue` column contract (`id`, `status`,
`scheduled_at`, `priority`, `started_at`, `updated_at`, `attempt_count`, `last_error`,
`completed_at`) plus source/progress columns. A partial unique index
`UNIQUE (source_id) WHERE status IN ('pending','processing')` makes "at most one active job per
source" a database invariant; the trigger maps a unique violation to "sync already in progress".
Stale `processing` rows are recovered by `RecoverStaleJobs` and remain the single active job until
a terminal transition. `domain/scheduler` (cron) triggers due sources by `sync_interval_minutes`;
a manual REST trigger admits the same way.

- Why: concurrency between a slow periodic sync and a manual trigger would otherwise race the
  cursor and duplicate work; the DB constraint is the only reliable guard across processes.

### D7 — REST matches the committed SDK; gateway UI ships in this change

The server implements the `google_drive` subset of `/api/data-source-integrations/*` exactly as
`apps/server/pkg/sdk/datasources/client.go` defines it (providers, provider schema, source-types,
test-config, test-connection, CRUD, sync trigger, sync-jobs list/latest/detail/cancel). This keeps
the existing SDK/gateway contract authoritative and lets `add-data-sources` build on a real server.
The gateway Data Sources UI and document version-history view ship in this change so the feature is
usable end-to-end.

- Why not invent a new `/api/sources` surface: the SDK client is already committed; matching it
  avoids divergent contracts and rework.

## Risks / Trade-offs

- **[OAuth refresh-token acquisition is out of band]** → The first iteration accepts an
  operator-supplied refresh token (encrypted at rest); an in-app authorize/callback flow is a
  documented follow-up. Connect UI documents exactly which values are required.
- **[Version rows duplicate stored content]** → Accepted for a correct audit trail; storage growth
  is bounded by real text changes (D3) and can be revisited with snapshot compaction later.
- **[Full folder enumeration each sync can be costly at scale]** → Per-item revision pre-filter
  avoids fetching unchanged content; metadata listing is paginated. Changes-API cursor is a
  documented optimization.
- **[Docs/Slides export churn]** → Mitigated by text-hash versioning (D3), which ignores
  representation-only changes.
- **[Drive rate limits / large files]** → Bounded concurrency, size/time caps, structured per-item
  errors that do not abort the whole sync.
- **[Existing `Service.Create` dedup could collapse versions]** → Ingestion uses dedicated
  version-aware repository methods and never the dedup path; covered by an explicit test.
- **[RLS / project isolation]** → `kb.sources` and `kb.source_sync_jobs` carry `project_id` and
  match the existing job-table RLS; cross-project ids return 404.
