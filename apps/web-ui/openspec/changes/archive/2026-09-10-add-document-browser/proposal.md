## Why

Alfred can ingest and extract structured knowledge from documents through the Emergent Memory service, but the gateway web UI only exposes the *memories* (extracted entities) — there is no way to see which source documents exist, upload new ones, inspect their chunks, or kick off extraction. A user who wants to feed documents into memory today must drive the Memory REST API by hand.

## What Changes

- Add a **document browser** to the gateway web UI (a new "Documents" nav surface) that lists ingested documents with their chunk counts, extraction status, and age.
- Add a **document detail view** that previews a document's chunks.
- Add an **upload flow** so a user can add a document from the UI (multipart file upload proxied through the gateway).
- Add an **extraction trigger** so a user can start extraction on a document from the UI and see the resulting status/counts.
- Add a backing **document API** on the gateway (`/api/documents*`) that proxies the Memory service and returns normalized document/chunk data, plus upload and extraction-job endpoints. This extends the existing authenticated `/api` trust boundary but is a new read/write surface (unlike the read-only memories API).
- Add an **extraction results** section to the document detail view listing the objects and relationships a document's extraction produced.
- Add a standalone **objects browser** (a new "Objects" nav surface) that lists the knowledge graph's objects, filterable by type, with a per-object detail view showing properties and relationships.

## Capabilities

### New Capabilities

- `document-api`: The gateway HTTP API for listing/fetching documents and chunks, uploading documents, and triggering extraction jobs, proxied through the Memory service under the shared API-key trust boundary.
- `document-browser`: The gateway web UI surface for browsing documents, uploading them, previewing chunks, and triggering/observing extraction (including the extracted objects/relationships).
- `object-browser`: The gateway web UI surface for browsing the knowledge graph's objects and their relationships.

### Modified Capabilities

<!-- none -->

## Impact

- **Code**: `gateway/memory.go` (new `MemoryClient` document/chunk/upload/extraction methods), `gateway/handlers.go` (new `/api/documents*` handlers), `gateway/ui.go` + new templ files (documents list/detail/upload views), `gateway/main.go` (route registration + nav entry), `gateway/webui` static assets if any client JS is added.
- **APIs**: new gateway endpoints `GET/POST /api/documents`, `GET /api/documents/:id`, `GET /api/documents/:id/chunks`, `POST /api/documents/:id/extract` (or equivalent); proxy to Memory `/documents`, `/chunks`, `/ingest/upload`, `/admin/extraction-jobs`.
- **Dependencies**: none new; reuses the existing `MemoryClient` HTTP client and the Memory service's document/ingest/extraction endpoints.
- **Tests**: unit tests for the new `MemoryClient` methods and handlers (TDD); templ render smoke tests for the new views.
