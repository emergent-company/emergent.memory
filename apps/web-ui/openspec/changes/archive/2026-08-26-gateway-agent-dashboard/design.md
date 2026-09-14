## Context

The gateway (`gateway/`) is a single Go binary (echo + templ + go-daisy) serving its own API and its own UI on `:8082`. It talks to the Emergent Memory service directly through `MemoryClient` (`memory.go`), which already covers agent-definition CRUD (`/api/projects/{id}/agent-definitions`), chat + conversations (`/api/chat/*`), MCP servers, and models. The UI surface today is two pages: `/agents` (list + create/edit/delete via `ui.templ`) and `/chat` (chat workspace via `chat.templ`). `Server`/`MemoryBackend` (`backend.go`, `handlers.go`) are the testable seam. See proposal.md for motivation.

Two other web surfaces exist and are superseded: `ui/` (a separate Go control-plane dashboard on `:8090`) and the Python `agent/admin.py` HTML pages (Overview, Sessions, prompt editor, catalog).

## Goals / Non-Goals

**Goals:**
- Add a per-agent dashboard and a memories subpage to the gateway.
- Remove `ui/`.
- Strip the HTML frontend from `admin.py`, keeping its iOS JSON endpoints.

**Non-Goals:**
- No memory editing/creation from the UI (read-only browser).
- No change to the agent runtime / bridge / voice worker.
- No re-pointing of the iOS client away from `admin.py`'s JSON endpoints (out of scope).

## Decisions

### 1. Dashboard route `/agents/{id}`, memories route `/agents/{id}/memories`

New echo routes alongside `/agents` and `/chat`. The sidebar stays `Agents` / `Chat`. The agents table's name cell becomes a link to `/agents/{id}` (the row already keys off `a.ID`).

### 2. Dashboard data comes from the memory service, not admin.py

- Summary + tools: `GetAgentDefinition(id)` → `AgentDefinition` (`Model.Name`, `FlowType`, `Visibility`, `DispatchMode`, `Description`, `Tools`, `BannedTools`, `ToolCount`). Configured tools = `Tools` (+ `BannedTools` shown as muted/red chips).
- Recent chats: `ListConversations()` filtered client-side to `Conversation.AgentDefinitionID == id`, most recent first, each linking to `/chat?c=<conversationId>`.

### 3. Memories browser reads the memory service directly

Add two methods to `MemoryClient` (+ `MemoryBackend` interface):
- `SearchMemories(query)` → `POST /api/search/unified` (confirmed working; returns `{results: [{canonical_id, object_type, fields:{content,category,confidence}, key}]}`). Query is required by the service.
- `ListMemories()` → memory objects without a query. The service has no REST list endpoint (probed), so list goes through the MCP `entity-query` tool (`/api/mcp`), reusing the same `initialize` + `tools/call` approach as `agent/admin.py`'s `_memory_mcp_call`. If a simpler REST list is discovered during implementation, prefer it.

Both normalize to a `Memory{ID, Content, Category, Confidence}` shape (mirroring the iOS browser). Memories are project-level (the memory service is the shared store), so the browser is not per-agent-gated.

### 4. Memories subpage mirrors the iOS browser

`/agents/{id}/memories` renders a search box (`?q=` → `SearchMemories`) and a list (content line-clamped, category badge, confidence). On open (no query) it lists via `ListMemories`. `?memory=<id>` renders the full content (looked up from the fetched list). Back link to `/agents/{id}`.

### 5. Remove `ui/` and strip admin.py's HTML

- Delete the `ui/` directory (untracked, superseded).
- In `admin.py`, remove the `HTML` and `SESSIONS_HTML` page strings and the routes that serve them (`/`, `/sessions` HTML, `/api/prompt` GET/POST, `/api/catalog`). Keep `/api/token`, `/api/qr-config`, `/api/qr.svg`, `/api/sessions`, `/api/session`, `/api/memories`, `/api/memories/capability`.

## Risks / Trade-offs

- **MCP list endpoint adds a second protocol to the gateway** (currently REST-only). → Mitigation: isolate it in one `listEntities` helper; prefer a REST list if found.
- **Conversation scoping** relies on `AgentDefinitionID` being populated on conversations. → Mitigation: conversations without a matching agent simply won't appear on any dashboard (acceptable; the chat rail already shows them globally).
- **`admin.py` HTML removal touches the iOS onboarding surface.** → Mitigation: only HTML routes are removed; the JSON endpoints the iOS app calls are untouched. Deploy admin.py and gateway independently.

## Migration Plan

1. Add `SearchMemories`/`ListMemories` to `MemoryClient` + `MemoryBackend` (+ fake).
2. Add `/agents/{id}` dashboard and `/agents/{id}/memories` subpage; link agents table.
3. Delete `ui/`.
4. Strip `admin.py` HTML routes; keep JSON endpoints. Restart `alfred-admin.service`.
5. Rollback: revert `admin.py` (additive removal) and redeploy the previous gateway binary independently.

## Open Questions

None — the memory browser shape is fixed by the iOS browser it mirrors; the list-vs-MCP detail above is an implementation choice that does not change the specs.
