## Context

See `proposal.md` for motivation. Current state:

- `apps/server/domain/documents` supports upload (file, batch, raw content), list/get/download/delete, retry-extraction, recreate-chunks. `documents.source_url` (`entity.go:18`) and `parent_document_id` exist but are never populated; `content_hash` is set only for raw-content create (`service.go:94`), and `file_hash` for uploads.
- **No reusable SSRF guard exists.** `apps/server/pkg/utils/` does not exist; the only SSRF-adjacent code is a package-private, string-level `validatePublicHost` in `apps/server/domain/blueprints/github_fetch.go:250-264` that rejects IP literals/`localhost`/`.internal`/`.local` but does not block private ranges or resolve-then-check. A new guard must be built.
- The parse pipeline reads bytes from storage, so any ingested bytes can reuse it; `xberg` handles HTML MIME (`client.go:461`).
- The data-source SDK client (`pkg/sdk/datasources/client.go`) describes `/api/data-source-integrations/*` but no server domain or routes exist; connector CRUD/UI is the separate `add-data-sources` change.

## Goals / Non-Goals

**Goals:**
- A single server-side ingestion path for externally obtained bytes, reused by URL ingestion and by connectors.
- Safe, bounded fetching and deterministic dedup keyed by content and by source identity.
- Changes to a source propagate to document content and re-extraction.

**Non-Goals:**
- Connector CRUD, OAuth flows, provider UI, sync jobs, discovery, and scheduling — owned by `add-data-sources`. The server-side integration/sync/discovery domain is still unowned and not delivered here.
- JavaScript-rendered SPA crawling and multi-page site crawling (needs a headless browser); initial scope is server-rendered HTML and fetchable content.
- A web crawler / sitemap traversal.

## Decisions

### Build a new SSRF guard; do not assume one exists

Implement a guard that rejects loopback, private, link-local, and cloud-metadata addresses, resolves hostnames and checks resolved addresses, and re-validates every redirect hop with a bounded redirect count. The existing `blueprints/github_fetch.go` string check is not sufficient and must not be depended on.

- Why: no shared guard exists; the classic bypass is redirect-to-internal.
- Alternative: reuse the blueprints check — rejected (string-level only, package-private, allowlist-based).

### Fetch in Go, then reuse the parse pipeline

Fetch with a Go HTTP client using the new guard, store the bytes, and hand them to the existing parse → chunk → embed pipeline.

- Why: reuses the extractor and pipeline; no second parsing implementation.
- Alternative: a headless-browser fetch service (WeKnora uses Playwright) — deferred; the fetch interface is shaped so a headless backend can be added without a spec change.

### Dedup by source identity plus content hash

Add `(project_id, source_provider, source_external_id)` uniqueness plus `source_content_hash` (hash of fetched bytes). URL ingestion uses a normalized URL as the external id under a built-in `web` provider.

- Why: re-ingesting the same source must update, not duplicate; the content hash alone loses source identity needed for re-sync.
- Alternative: content-hash-only dedup (as raw-content create does) — rejected (cannot find a source item to refresh).

### URL normalization rules

Normalize scheme and host to lowercase, drop a default port, and drop the fragment; preserve path and query. These rules define the `web` source identity and are applied before the uniqueness check.

- Why: without rules, equivalent URLs either always or never dedup; the key is otherwise untestable.

### Change detection on fetched bytes

Compare the hash of freshly fetched bytes to `source_content_hash`: changed → replace bytes/content/hash and schedule re-extraction; unchanged → touch `source_synced_at`. Fetch aborts clean up any partial storage writes.

### Keep connectors out of this change

Expose only the ingestion seam (upsert-by-source + URL ingest). `add-data-sources` and a future server sync domain consume it; this change does not implement integration management/sync/discovery.

## Risks / Trade-offs

- [SSRF via redirects or DNS rebinding] → validate every redirect hop and resolve-then-check the target address; deny by default.
- [Large pages / slow origins] → hard max size (default 10 MiB) and timeout (default 30s), configurable; abort with cleanup.
- [SPA pages return little text] → acknowledged non-goal; fetch interface allows a headless backend later.
- [Source churn causing repeated re-extraction] → hash comparison limits work to real changes.
- [Concurrent duplicate ingestion] → uniqueness constraint plus conflict-safe upsert.
- [Partial storage orphans on abort] → cleanup-on-abort task.

## Migration Plan

- Additive migration adding `source_provider`, `source_external_id`, `source_synced_at`, `source_content_hash` and the uniqueness constraint; existing documents have null source fields and are unaffected. `source_url` already exists.
- Rollback: ingestion endpoints can be disabled without affecting uploads.

## Open Questions

- Whether URL ingestion should support authenticated/fragile pages (cookies, headers) now or only anonymous public pages. Default: public pages; headers deferred.
- Exact default size/timeout values may be tuned during implementation; the spec requires them to be configurable with defaults.
