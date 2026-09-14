## 1. Backend client and models

- [x] 1.1 Add per-agent share models + memory-backed client methods (create/list/revoke/rotate) in `gateway/agent_mcp_shares.go`, mirroring `gateway/mcp_shares.go`, with `X-Project-ID` scoping; verify unit tests assert the paths, methods, and one-time token handling.
- [x] 1.2 Extend `MemoryBackend` in `gateway/backend.go` with the per-agent methods and update fakes; verify `go build ./...` succeeds.

## 2. Gateway API routes and handlers

- [x] 2.1 Add JSON handlers + routes under `/api` (create, list, revoke, rotate) behind the existing auth boundary; verify handler unit tests cover success, 403 unauthenticated, and backend 409/422 pass-through.
- [x] 2.2 Ensure the raw key is returned only by create and rotate and never logged; verify a test asserts list responses contain no key and no secret reaches the logger.

## 3. Web UI

- [x] 3.1 Add a "Share as MCP tool" action on the agent view wired to create + reveal; verify a render test asserts the action and the reveal markup for a freshly created share.
- [x] 3.2 Add the agent shares list view with status/timestamps, empty state, and error state; verify render tests for each.
- [x] 3.3 Add the one-time reveal (endpoint URL, key + copy, client snippets + copy, "won't be shown again" warning) and revoke/rotate confirmation flows; verify tests assert the reveal and that the key is absent when not freshly created/rotated.

## 4. Verification

- [x] 4.1 Run `templ generate`, then `go build ./...` from `gateway/`; confirm no templ drift.
- [x] 4.2 Run `task lint` (or golangci-lint) and fix new findings; run `go test ./...` from `gateway/` with the new render/handler tests green.
- [ ] 4.3 (deferred — memory backend not running) Manually verify in the DevTools browser once the backend change is deployed: share an agent, copy the key, connect an MCP client to the per-agent endpoint, confirm only `call_agent` is listed and returns a reply, then rotate and revoke and confirm the old key stops working.
