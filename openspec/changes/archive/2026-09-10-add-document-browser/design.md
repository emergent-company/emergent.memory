## Context

Alfred's gateway (`gateway/`) is a single Go binary (echo + a-h/templ + go-daisy) that already proxies the Emergent Memory service through a `MemoryClient` HTTP helper (`gateway/memory.go`) and renders server-side templ pages (agents list, agent dashboard, chat, memories browser). The memories browser (`/agents/:id/memories`) and `agent-dashboard-ui`/`agent-memory-api` specs are the closest precedents.

The Memory service already exposes everything the document browser needs: `GET/POST /documents`, `GET /documents/{id}`, `GET /chunks?documentId=`, `POST /ingest/upload` (multipart), and `POST /admin/extraction-jobs`. The gateway has no document methods yet. See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**

- Add a minimal read/write document surface to the gateway that proxies Memory: list documents, get one, list its chunks, upload a file, and trigger extraction.
- Add a server-rendered "Documents" page (list + upload) and a document detail view (chunk preview + extraction trigger/status) matching the existing go-daisy layout and the memories-browser conventions.

**Non-Goals:**

- No chunk editing, document deletion, batch upload, or URL ingestion in this change (single-file upload only).
- No background extraction polling/streaming; status is surfaced by re-fetching the document's `extractionStatus` field.
- No cross-agent scoping: documents are project-wide (Memory has no per-agent document namespace), so the surface is global like the agents list, not nested under an agent.

## Decisions

### 1. Proxy through `MemoryClient`, one method per concern

Add typed methods on `MemoryClient` following the existing `do`/`doH` pattern: `ListDocuments`, `GetDocument`, `ListChunks`, `UploadDocument`, `CreateExtractionJob`. Each returns normalized Go structs (`Document`, `Chunk`) mirroring `DocumentDto`/`ChunkDto` fields the UI needs (id, name, mimeType, chunks, extractionStatus, extractionObjectsCount, createdAt; chunk index/size/hasEmbedding/text).

- **Alternative considered:** call Memory directly from handlers. Rejected — breaks the credential-hiding boundary (`memory.go` holds the token) and diverges from every other Memory call.
- **Alternative considered:** reuse the MCP JSON-RPC path (`ListMemories` does this). Rejected — documents/chunks/upload/extraction are plain REST endpoints with no MCP tool equivalent; REST is simpler and matches `ListAgentDefinitions`.

### 2. Document list uses cursor pagination

Memory's `GET /documents` returns a `DocumentDto[]` with an `x-next-cursor` response header and takes a `cursor` query param (the spec marks `cursor` required; the first page uses an empty cursor). The gateway `GET /api/documents` accepts an optional `?cursor=` and returns `{documents, nextCursor}`, forwarding the header value. The UI renders the first page and, when `nextCursor` is present, offers a "load more" link.

- **Alternative considered:** fetch all pages server-side and return one flat list. Rejected — unbounded and defeats Memory's paging; the browser only needs the first page initially.

### 3. Upload proxies multipart, not JSON

The browser posts a single file as `multipart/form-data` to `POST /api/documents`; the gateway reads the file and forwards it to Memory's `POST /ingest/upload` as multipart (filename + `projectId`). The gateway enforces a 10 MB cap (matching Memory's per-file limit) and rejects empty/unsupported uploads with a validation error before hitting Memory.

- **Alternative considered:** read the file into a JSON `content` string and use `POST /documents` (JSON create). Rejected — that path expects raw text content and would not carry binary/mime fidelity or trigger Memory's ingest chunking; the dedicated upload endpoint is the correct contract.

### 4. Extraction via the admin jobs endpoint, status via document field

`POST /api/documents/:id/extract` builds a `CreateExtractionJobDto` (`project_id`, `source_type: "document"`, `source_id: <docId>`, default `extraction_config`) and forwards it to `POST /admin/extraction-jobs`, returning the created job reference. The UI shows extraction state from the document's own `extractionStatus`/`extractionObjectsCount` (re-fetched after triggering) rather than polling the jobs list.

- **Alternative considered:** list/filter `GET /admin/extraction-jobs/projects/{projectId}` for per-document status. Rejected — `DocumentDto` already carries `extractionStatus` + `extractionObjectsCount`, so a single document re-fetch covers the UI need with far less plumbing.

### 5. Server-rendered templ pages, not a SPA

Add a `Documents` sidebar entry (Alfred group) and two new templ pages — a list/upload page and a document detail page — using `layout`/`render` exactly like the existing `uiAgentMemories` flow. New routes: `/documents` and `/documents/:id`; new JSON routes under `/api/documents*` for the list/detail/chunks/upload/extract handlers. No client-side framework; HTMX partials where the existing pages already use them.

- **Alternative considered:** extend the agent dashboard's memories subpage. Rejected — documents are project-global (not per-agent) and the memories subpage is scoped under `/agents/:id`.

## Risks / Trade-offs

- **Multipart buffering / memory pressure** → Cap uploads at 10 MB and read the single file into memory only after size checks; a single buffered file is acceptable at this scale.
- **Cursor contract ambiguity** (`cursor` marked required for the first page) → Send an empty cursor for the first request and treat a missing/empty `x-next-cursor` as end-of-list; verify against the live service during implementation.
- **Extraction is async** → The document's `extractionStatus` may lag right after triggering; the UI re-fetches after triggering and shows the current status with a clear "pending/running" label rather than assuming completion.
- **No per-agent scoping** → Documents are project-wide; a user browsing an agent's memories will not see documents scoped to that agent. This is an accepted limitation of the Memory data model, documented in the UI copy.
