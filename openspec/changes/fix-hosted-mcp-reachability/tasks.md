## 1. Specs first

- [x] 1.1 Delta `specs/mcp-connector/spec.md`: management API binds before serving, bounded bind retry, fail fast on permanent failure
- [x] 1.2 Delta `specs/mac-connector-app/spec.md`: restart sequences on child exit; hosted MCP page is availability-gated with generic messages

## 2. Mac app — engine restart sequencing

- [x] 2.1 `restartAction(hasLiveProcess:isBusy:)` pure decision + `RestartAction`
- [x] 2.2 `restart()` waits for the previous child to exit (`pendingRestart`) instead of spawning into the held port
- [x] 2.3 Bounded SIGKILL fallback for a hung child; cleared on stop/exit
- [x] 2.4 `stop()` cancels a queued restart
- [x] 2.5 Tests: restart decision; comment fixes in `ManagementPortProbe`/`EngineManagerPortProbeTests`

## 3. Connector engine — bind hardening

- [x] 3.1 `relay.go`: `net.Listen` with bounded retry, serve via `httpServer.Serve(ln)`, `return 1` if bind still fails
- [x] 3.2 Go test for the listener retry/fail path

## 4. Mac app — availability-gated page

- [x] 4.1 New `MCPServersAvailability` (pure) resolving engine state + connected-project into the page's state
- [x] 4.2 `MCPServersStore.userMessage(for:)` collapses `.unreachable` into a generic message; used on all surfaces
- [x] 4.3 `MCPServersPage`: gate load on availability, load on transition to available, disable Add/Browse when unavailable, render connecting/placeholder/generic-error states
- [x] 4.4 Tests: availability resolution; unreachable message mapping; keep existing store tests green

## 5. Verify

- [x] 5.1 `cd apps/connector.linux && go build ./... && go test ./cmd/memory-connector/...`
- [x] 5.2 `openspec validate fix-hosted-mcp-reachability --strict`
- [ ] 5.3 Swift: source review + `Scripts/lint-swift.sh` (macOS-only; cannot run on the Linux host)
- [x] 5.4 Independent `@oracle` review of the Swift diff (blocker found and fixed)
