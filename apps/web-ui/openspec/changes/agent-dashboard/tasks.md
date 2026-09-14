## 1. Backend — agent-scoped sessions (agent/admin.py)

- [x] 1.1 Add a pure helper `_sessions_for_agent(name)` that filters a session summary list to rooms matching the agent's prefix (`room.startswith(name + "-")`), reusing the existing room-prefix convention
- [x] 1.2 Add `GET /api/sessions` support for an optional `agent` query param (filters to that agent's rooms; empty/absent param returns all), guarded by `_authorized()`
- [x] 1.3 Add a stdlib `unittest` test (no new deps) covering `_sessions_for_agent` (`python -m unittest`)
- [ ] 1.4 Verify with curl on home2: `?agent=` sessions filter matches the prefix; no param returns all; bad key → 401; restart `alfred-admin.service`

## 2. Go UI — models + client

- [x] 2.1 Add `AgentDetail` (backend, tools, sub_agents, routing), `McpServerSummary` (id + name), and `Memory` (id, content, category, confidence) structs with tolerant decoders in `ui/models.go`
- [x] 2.2 Add client methods in `ui/client.go`: `FetchAgent(id)`, `FetchMcpServers()`, `FetchSessionsForAgent(name)`, `FetchMemoryCapability(name)`, `FetchMemories(agent, query)`
- [x] 2.3 Add Go unit tests for the new decode paths using golden JSON fixtures in `ui/smoke_test.go`

## 3. Go UI — dashboard + memories subpage

- [x] 3.1 Add `/agents/{id}` route and the dashboard template: summary card (backend type, model, enabled, version), configured tools (MCP servers by name + allowlist, built-in functions), and recent chats (agent-scoped sessions linking to `/sessions?room=`)
- [x] 3.2 Link each agent row in the agents table to `/agents/{id}`
- [x] 3.3 Gate the memories link on `GET /api/memories/capability` (show only when `hasMemory`)
- [x] 3.4 Add `/agents/{id}/memories` route and the memories subpage mirroring the iOS browser: a search box (calls `/api/memories?agent=&query=`), a list of memories (content, category, confidence), and a detail view with full content
- [x] 3.5 Add empty/error states for tools, recent chats, and memories sections (reuse the shared `pageError` pattern)
- [x] 3.6 Extend `ui/smoke_test.go` with route/render smoke tests for the dashboard and memories subpage

## 4. Verification

- [x] 4.1 Run `templ generate` in `ui/` (pages.templ changed) and `go build ./...`
- [x] 4.2 Run `go test ./...` in `ui/` (unit + smoke) and `go vet ./...`
- [x] 4.3 Run `ruff check agent/` and the new `python -m unittest` for the admin.py changes
- [x] 4.4 Run `task lint` at the repo root (lefthook: ruff, golangci-lint, go vet/test, templ, gitleaks)
- [ ] 4.5 Restart the UI and admin server, then browser-test: dashboard shows summary/tools/recent chats; memories subpage lists, searches, and opens detail; a non-memory agent hides the memories link
