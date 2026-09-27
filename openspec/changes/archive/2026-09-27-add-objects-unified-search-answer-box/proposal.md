## Why

The objects browser (`GET /objects`) gained text search (full-text + hybrid) and a stats row earlier, but two gaps remained: (1) no single "unified" search that fuses lexical, vector, relationship context, and fusion scoring into one ranked list of graph objects, and (2) no way to ask a natural-language question against the project's knowledge graph and read a grounded answer in place. This change adds both as gateway-only extensions, consuming the already-membership-protected `domain/search` and `domain/chat` endpoints.

## What Changes

- **Unified search mode** — a third "Unified" option in the search mode selector. It calls `POST /api/search/unified` with `resultTypes=graph`, ranking graph objects via lexical + vector + relationship context + fusion. Unified results carry `fields`/`labels` but no `created_at`/`status`/`embedding_status`, so they render via a dedicated row (type icon + label + score + labels) that deliberately omits those badges rather than showing missing data.
- **Knowledge-search answer box** — an "Ask the graph" RAG box that HTMX-posts a natural-language question to `POST /objects/knowledge`, which calls `POST /api/projects/{id}/query` (SSE) and renders the grounded answer as sanitized markdown with a session id caption, plus loading and error states.
- **Authorization posture (unchanged, restated as contract)** — the gateway derives the project server-side from the signed session (`resolveDefaultProject`; no client input overrides it). The unified-search call forwards `X-Project-ID` to `POST /api/search/unified`, which enforces `RequireAuth` → `RequireProjectID` → `RequireProjectTokenScope` → `RequireProjectMember` → `search:read`. The answer-box call embeds the project in the path (`/api/projects/{id}/query`) and forwards `X-Project-ID` + bearer; the endpoint enforces `RequireAuth` → `RequireProjectTokenScope` → `RequireProjectMember` → `chat:use`. Neither path can return content from a project the caller is not a member of.

## Capabilities

### New Capabilities

<!-- None: both additions extend the existing objects-browser surface; no new capability is introduced. -->

### Modified Capabilities

- `object-browser`: new requirements are added for the unified search mode, the knowledge-search answer box, and the membership-enforced authorization posture behind `/api/search/unified` and `/api/projects/{id}/query`.

## Impact

- `apps/web-ui/gateway/memory_graph.go` — `SearchObjectsUnified` client method (+ `unifiedSearchResult` wire struct).
- `apps/web-ui/gateway/memory_knowledge.go` (new) — `QueryKnowledge` SSE client method.
- `apps/web-ui/gateway/backend.go` — extend `MemoryBackend` with both methods.
- `apps/web-ui/gateway/objects.go` — `uiObjects` dispatches `mode=unified`; new `uiObjectsKnowledge` POST handler.
- `apps/web-ui/gateway/main.go` — register `POST /objects/knowledge`.
- `apps/web-ui/gateway/objects.templ` — third "Unified" radio; `objectsKnowledge` box + answer/error partials; `objectUnifiedRow`; shared `objectScoreBadge`.
- Tests added across `objects_test.go`, `memory_test.go`, `handlers_test.go`.
- No server code changes: this is a gateway-only PR that consumes the existing membership-protected `domain/search` and `domain/chat` endpoints.
