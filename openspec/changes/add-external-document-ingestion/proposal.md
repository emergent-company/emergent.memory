## Why

Memory can only ingest files a user uploads and raw text bodies. There is no way to point it at a URL, and there is no server-side path for external sources to feed documents in: `documents.source_url` exists but is never set, the data-source SDK client has no matching server routes, and the gateway-only `add-data-sources` change has no server backend to call. This blocks the basic "ingest from a URL / keep a document in sync with its source" workflow.

## What Changes

- Add URL/web-page ingestion: fetch a page with SSRF protection and size/time limits, convert it to text through the existing extraction pipeline, and store it as a document with `source_url` provenance.
- Add a provider-agnostic external-source ingestion contract: upsert a document by `(provider, external_id)`, detect content change by hash, re-sync on change, and track sync state — the server seam the `add-data-sources` connectors need.
- Deduplicate repeated ingestion of the same URL or source item instead of creating duplicates.
- Expose ingestion and source-refresh through the gateway document API.

## Capabilities

### New Capabilities
- `document-ingestion`: ingests documents from URLs and from external sources (provider + external id) with SSRF-safe fetching, deduplication, change detection, and re-sync.

### Modified Capabilities
- `document-api`: adds URL-ingestion and source-document refresh operations to the gateway document API.

## Impact

- `apps/server/domain/documents/` plus a new ingestion service; a **new** SSRF guard (no reusable guard exists today — only a string-level `validatePublicHost` in `domain/blueprints/github_fetch.go:250`); `apps/server/pkg/xberg` reuse for HTML.
- Gateway document API and UI; CLI document commands; SDK/gateway client methods.
- Migration: add `source_provider`, `source_external_id`, `source_synced_at`, and `source_content_hash` (all new) plus a uniqueness constraint for source dedup. `source_url` already exists on `documents` and is not a new column.
- Relationship: connector-side CRUD, OAuth, sync **jobs**, and discovery/UI stay in the existing `add-data-sources` change. This change supplies only the document-ingestion seam (upsert-by-source + URL ingest); the server-side integration/sync/discovery domain that `add-data-sources` calls remains unowned and should be scoped separately.
