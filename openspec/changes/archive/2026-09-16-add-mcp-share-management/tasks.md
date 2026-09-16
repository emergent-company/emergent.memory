## 1. Backend client and models

- [x] 1.1 Add share-instance and tool-catalog models to `gateway/` (mirroring the backend JSON: id, name, description, tools, status, timestamps, one-time token) and verify with a unit test that JSON round-trips the fields the UI needs.
- [x] 1.2 Add `gateway/mcp_shares.go` client methods (list/create/get/update/revoke/rotate/tool-catalog) backed by the shared memory client with `X-Project-ID` scoping, and verify unit tests against `httptest` stubs assert the correct paths, methods, and bodies.
- [x] 1.3 Extend the `MemoryBackend` interface in `gateway/backend.go` with the share methods and update fakes; verify `go build ./...` succeeds.

## 2. Gateway API routes and handlers

- [x] 2.1 Add `gateway/mcp_shares_handlers.go` JSON handlers and routes under `/api` (list, create, get, update, delete, rotate, tools) behind the existing auth boundary; verify handler unit tests cover success, 403 unauthenticated, and backend 409/422 pass-through.
- [x] 2.2 Ensure raw token values are returned only by create and rotate responses and never logged; verify a unit test asserts list/get responses contain no token field and no secret is written to logs.

## 3. Web UI — list, create, edit

- [x] 3.1 Add `gateway/mcp_shares.templ` list view rendering each instance's name, tool count, status, and timestamps, with an empty state and a load-error state; verify a render test asserts the elements, empty state, and error state.
- [x] 3.2 Add the create form (name, description, searchable category-grouped multi-select tool picker populated from the catalog) with inline validation for required name, ≥1 tool, and duplicate name; verify render tests cover the fields, groups, and each inline error.
- [x] 3.3 Add the edit form pre-filled with the instance's current scoping and the one-time key reveal modal (key + copy controls + snippets + "won't be shown again" warning); verify render tests assert the reveal markup and that the key is absent when not freshly created/rotated.
- [x] 3.4 Add revoke and rotate confirmation flows wired to the API, re-fetching the list on completion; verify handler tests assert the actions call the expected client method and surface errors inline.

## 4. Entry points

- [x] 4.1 Add an entry-point link from the MCP Servers page (`gateway/mcp_servers.templ`) to the sharing page; verify a render test asserts the link and target.

## 5. Verification

- [x] 5.1 Run `templ generate`, then `go build ./...` from `gateway/`; confirm it compiles with no templ drift.
- [x] 5.2 Run `task lint` (or golangci-lint) and fix all new findings.
  *Note: `task lint` wraps lefthook, which is not installed on this host. Ran `golangci-lint run ./...` from `gateway/` directly → 0 issues; `go vet ./...` clean.*
- [x] 5.3 Run the gateway unit test suite (`go test ./...` from `gateway/`) including the new render and handler tests; all green.
- [ ] 5.4 (deferred — requires a running backend and browser tooling; not available in this environment) Manually verify in the DevTools browser with the backend change deployed: create an instance with a subset of tools, copy the key, connect an MCP client, confirm only selected tools are listed and a non-selected call is rejected, then rotate and revoke and confirm the old key stops working.
