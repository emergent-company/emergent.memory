## Why

Memory can ingest documents only by manual upload or raw text. The documents layer already
carries the placeholders for sourced documents (`kb.documents.source_type`, `source_url`) but
`source_url` is never populated and there is no synchronization path, no change detection, and
no version history. The committed SDK client `apps/server/pkg/sdk/datasources/client.go`
describes a `/api/data-source-integrations/*` REST surface, but no server domain or routes back
it. The generic datasource framework that once existed was dropped in
`migrations/00089_remove_integrations_datasource_githubapp.sql`.

Two spec-only OpenSpec changes already describe overlapping fragments of a solution and
**conflict** on document attribution: `add-source-ingestion` proposes
`documents.source_id` + `external_item_id`, while `add-external-document-ingestion` proposes
`documents.source_provider` + `source_external_id` + `source_content_hash`. Neither is
implemented. This change reconciles them into one document-attribution schema, adds
first-class document versioning (which neither change specifies — both say "update in place"),
and ships **Google Drive** as the first concrete source: folder- and type-scoped, monitored,
and versioned whenever the extracted text changes.

## What Changes

- **Reconcile document attribution into one schema.** One set of `kb.documents` columns:
  `source_id` (FK to a durable source), `source_external_id` (the provider's item id),
  `source_revision_id`, `source_text_hash`, `source_synced_at`, `source_status`,
  `source_deleted_at`. Supersedes both conflicting proposals; explicitly not a resurrection of
  the `external_source_id` / `data_source_integration_id` / `sync_version` columns dropped in
  `00089`.
- **Document versioning via a supersedes chain.** `kb.documents` gains
  `canonical_id` / `supersedes_id` / `version` (mirroring the existing graph-object versioning
  pattern). HEAD is `supersedes_id IS NULL`; a content change appends a new row version and
  retains the prior rows. Reads default to HEAD; version history is retrievable.
- **Versioning triggered by extracted-text difference.** A provider revision id (Drive
  `headRevisionId`) is the cheap pre-filter; on change the item is fetched and parsed, and a new
  version is created only when the hash of the **parsed text** differs from the HEAD's
  `source_text_hash`. Unchanged text updates only the revision pointer and `source_synced_at`.
- **A lean source model** `kb.sources` (project, provider, config, encrypted credentials,
  sync policy/interval, cursor, status) and a sync-job table `kb.source_sync_jobs` carrying the
  `internal/jobs.Queue` column contract plus durable progress.
- **A first-class Google Drive connector**: OAuth2 refresh-token auth (encrypted at rest),
  scoped to a configured folder subtree and a MIME-type allowlist, fetching Google-native
  documents via export and binaries via media download, feeding the existing xberg → chunking →
  extraction pipeline.
- **Source monitoring**: a sync-job lifecycle (`pending → processing → completed|failed|cancelled`)
  with at most one active job per source, cron scheduling, cancellation, structured errors, and
  restart-surviving progress.
- **Deleted-from-source handling**: a Drive item removed or trashed in scope retains its document
  row, versions, and history, and is marked `source_status = 'deleted_from_source'`; it is excluded
  from default listings and retrieval, and re-appearing creates a new active version.
- **Server REST + gateway UI**: implement the server side of the SDK's
  `/api/data-source-integrations/*` surface for the `google_drive` provider, and a gateway Data
  Sources UI (connect, test, trigger, monitor, source detail/delete) plus document version history.

## Capabilities

### New Capabilities

- `document-ingestion`: the provider-agnostic seam that upserts a document by source identity and
  detects changes by parsed-text hash, so external sources feed the existing pipeline.
- `document-versioning`: durable, supersedes-chained document versions with HEAD reads, a version
  history surface, and the source-attribution + deleted-from-source lifecycle.
- `google-drive-source`: the Google Drive connector plus the server REST surface the SDK/gateway
  call (provider catalog, source CRUD, connection test, sync trigger/monitor/cancel).

### Modified Capabilities

- `document-api`: adds version-history reads and head-only default listing to the gateway document
  API.

### Superseded changes

- `add-source-ingestion` (server framework `kb.sources` + MCP transport) — superseded.
- `add-external-document-ingestion` (URL ingestion + a conflicting attribution schema) — superseded.
  URL/SSRF ingestion is an explicit non-goal here and is not carried forward.

## Impact

- **DB** (`apps/server/migrations/`): new migration (`00226`) adding the document
  versioning/attribution columns and their indexes, `kb.sources`, and `kb.source_sync_jobs`.
- **Server** (`apps/server/domain/` + `internal/`): new `domain/sources` (source CRUD + sync
  orchestration + the `data-source-integrations` REST routes), a Google Drive transport client,
  version-aware `domain/documents` repository/service methods, worker + scheduler wiring, and
  credential encryption via `pkg/crypto` (the existing `provider.EncryptedCredential` pattern).
- **SDK/Gateway**: wire `apps/server/pkg/sdk/datasources` to the new server routes; add the
  gateway Data Sources UI and document version-history view.
- **Dependencies**: promote `google.golang.org/api` (Drive v3) from indirect to direct;
  `golang.org/x/oauth2` and `pkg/xberg` are already present.
- **Spec hygiene**: the two superseded changes get an `openspec:archive-hold` marker in their
  `tasks.md`; their delta specs are not synced (they were never implemented).
