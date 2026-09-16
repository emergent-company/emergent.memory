## 1. Client + API surface (gateway ↔ memory admin API)

- [x] 1.1 Add the four missing methods to the `MemoryBackend` interface (`gateway/backend.go:42-46`): `SyncMCPServer`, `InspectMCPServer`, `ListMCPServerTools`, `SetMCPServerToolEnabled`; extend the fake in `gateway/handlers_test.go` so it still satisfies the interface, and verify `cd gateway && go test ./...` compiles
- [x] 1.2 Implement the four methods in a new `gateway/mcp_servers_client.go` as thin `m.do`/`doH` proxies (session `X-Project-ID` plumbing via `memory.go`) — `POST /api/admin/mcp-servers/:id/sync`, `POST .../inspect`, `GET .../:id/tools`, `PATCH .../:id/tools/:toolId`; verify with httptest-backed unit tests (mirroring `api_tokens_test.go`/`extras.go` client tests) asserting method/path/body and error propagation
- [x] 1.3 Register the new gateway routes in `gateway/main.go` near the existing MCP block (`:92-96`) plus thin handlers in `gateway/mcp_servers_handlers.go`; verify with handler tests asserting route → memory-call mapping and JSON shapes (`mcp_servers_handlers_test.go`, mirroring `api_tokens_handlers_test.go`)

## 2. Management UI (reuse api_tokens trio + existing components)

- [x] 2.1 Build `gateway/mcp_servers.templ` list page (`MCPPage`): table of servers (name, transport type badge, enabled toggle, tool count, actions) reusing go-daisy table pieces + `pageHeader`/`crumbsActive`/`flashToasts`/`pageError` and `ui.EmptyState` hero for an empty registry; verify via `renderHTML` UI tests (`mcp_servers_ui_test.go`, mirroring `api_tokens_ui_test.go`)
- [x] 2.2 Add the shared create/edit transport form page (radio choice `stdio|sse|http`; conditional URL + headers key/value rows vs command/args/env rows; pre-fill on edit; inline fieldset errors; no partial save on validation failure/409 duplicate; builtin servers non-editable/deletable); verify with UI tests for field rendering, pre-fill, and validation-error paths
- [x] 2.3 Add per-row actions wired to the new routes: delete via `confirmDeleteDialog`/`modalShell`, sync + inspect via JSON fetch with go-daisy toasts (surface prune/error copy, keep cached tools on failure), and per-row expandable `<details>` listing cached tools with per-tool `toggle toggle-sm` (reuse the `agentToolServerGroup` interaction style from `agent.templ:592-625`); verify with UI tests asserting action wiring and toggle state rendering
- [x] 2.4 Register the page: breadcrumb + `pageTestID` title + route in `gateway/main.go`, sidebar entry `{Label:"MCP Servers", Href:"/settings/mcp-servers"}` in `ui.go sidebarGroups` (`:97-104`); verify route renders and `page-mcp-servers` test-id anchor appears (UI test)
- [x] 2.5 Turn the agent tool picker empty-state CTA into a link to `/settings/mcp-servers` (`gateway/agent.templ:575`); verify with an existing agent UI test updated to assert the link
- [x] 2.6 Run `templ generate`, `cd gateway && go build ./...`, and `task lint`; verify all pass with the new/changed files

## 3. E2E — set up an example MCP server (live-dev mutation suite)

- [x] 3.1 Add `E2E_MCP_EXAMPLE_URL` (and optional `E2E_MCP_EXAMPLE_HEADER_NAME/VALUE`) to `tests/e2e/.env.e2e.example` with a comment that the dev memory backend must reach the URL; verify env keys load from `.env.e2e` in config
- [x] 3.2 Author `tests/e2e/specs/settings/mcp-servers-create-ui.spec.ts`: unique-named (`E2E MCP <ts>`) register of an `http` server via the new form → success toast + row; sync → tools appear; toggle one tool off; edit URL persists; delete via confirm dialog; `finally` cleanup through `page.request.delete('/api/mcp-servers/<id>')` (pattern: `skill-create-ui.spec.ts`); when `E2E_MCP_EXAMPLE_URL` is unset, env-gate sync/tool assertions to a documented skip — never fail on network state (pattern: `scenarios/provider-openai-litellm-config.spec.ts`)
- [x] 3.3 Run the new spec against the live dev gateway (`cd tests/e2e && npx playwright test specs/settings/mcp-servers-create-ui.spec.ts --project=mutations`); verify green, and verify no `E2E MCP` servers remain in the project after the run (self-cleanup proof)

## 4. Verification

- [x] 4.1 Run the full gateway unit/handler/UI-render test set plus `go build ./...` and `task lint`; verify all pass
- [x] 4.2 Browser smoke against the dev stack: sidebar entry → page renders (empty state) → register http server → sync shows tools → edit → delete round-trip, plus agent tool-picker empty-state link navigates; verify no console errors (follow `docs/sessions/*` precedent for recording the run)
- [x] 4.3 Commit finished unit + push (stage only this change's files — `gateway/mcp_servers*.go`, `gateway/main.go`, `gateway/ui.go`, `gateway/agent.templ`, `backend.go`, tests/e2e files, `.env.e2e.example`, change artifacts); verify `git status` clean of unrelated WIP before staging
