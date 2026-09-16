## 1. Gateway — memory client + models

- [x] 1.1 Add `Memory{ID, Content, Category, Confidence}` and a `SearchMemories(query)` method calling `POST /api/search/unified` (normalize `results` → `[]Memory`)
- [x] 1.2 Add a `ListMemories()` method (MCP `entity-query` via `/api/mcp`, mirroring `agent/admin.py` `_memory_mcp_call`; prefer a REST list endpoint if found)
- [x] 1.3 Extend the `MemoryBackend` interface (`backend.go`) with `SearchMemories` and `ListMemories`, and update the test fake
- [x] 1.4 Add unit tests for the new memory decode/normalize paths

## 2. Gateway — agent dashboard

- [x] 2.1 Add `GET /agents/:id` route + handler (fetch `GetAgentDefinition` + `ListConversations`, filter conversations by `AgentDefinitionID`)
- [x] 2.2 Add `AgentDashboardPage` template: summary card (name, model, flow, visibility, tool count, description), configured tools (allowed + banned), recent chats (links to `/chat?c=`), memories link
- [x] 2.3 Link the agents-table name cell to `/agents/{id}`
- [x] 2.4 Add empty/error states for tools and recent chats (reuse the shared `pageError`)

## 3. Gateway — memories subpage

- [x] 3.1 Add `GET /agents/{id}/memories` route + handler (search via `?q=`, list on open, detail via `?memory=`)
- [x] 3.2 Add `MemoriesPage` template: search box, list (content, category badge, confidence), detail view with full content, back link
- [x] 3.3 Add empty/error states for list, search-no-match, and fetch failure

## 4. Gateway — tests

- [x] 4.1 Add render tests for the dashboard (summary + tools + recent chats + memories link) and memories page (list + search + detail + empty/error)
- [x] 4.2 Add a route smoke test asserting `/agents/:id` and `/agents/:id/memories` render against a fake backend

## 5. Remove superseded interfaces

- [x] 5.1 Delete the `ui/` directory
- [x] 5.2 In `agent/admin.py`, remove the `HTML` and `SESSIONS_HTML` page strings and their routes (`/`, `/sessions` HTML, `/api/prompt` GET/POST, `/api/catalog`); keep the iOS JSON endpoints (`/api/token`, `/api/qr-config`, `/api/qr.svg`, `/api/sessions`, `/api/session`, `/api/memories`, `/api/memories/capability`)
- [x] 5.3 Run `ruff check agent/admin.py` and confirm the remaining endpoints still resolve (no syntax/import errors)

## 6. Verification

- [x] 6.1 `templ generate` + `go build ./...` in `gateway/`
- [x] 6.2 `go test ./...` + `go vet ./...` in `gateway/`
- [x] 6.3 `task lint` at the repo root
- [x] 6.4 Restart gateway + browser-test: dashboard shows summary/tools/recent chats; memories subpage lists, searches, and opens detail
