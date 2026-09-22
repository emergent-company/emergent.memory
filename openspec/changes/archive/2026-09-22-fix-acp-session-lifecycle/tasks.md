## 1. Bounded session lifecycle (TDD)

- [x] 1.1 Add failing tests in `apps/cli/internal/acp/acp_test.go`: session delete removes the entry; `session/delete` round-trips through `Run` with an empty result; the cap evicts the LRU session; eviction invokes a dropped cancel func; an in-flight session is not pruned; delete cancels an in-flight turn; a completed turn clears the cancel func. Verify: `go test ./internal/acp/...` fails on the new cases.
- [x] 1.2 Cap the session map in `agent.go` with LRU eviction of idle sessions, in-flight tracking, and cancel-func invocation on eviction. Verify: `go test ./internal/acp/...` passes.
- [x] 1.3 Implement `Agent.deleteSession` and clear a session's cancel func at turn end. Verify: `go test ./internal/acp/...` passes.
- [x] 1.4 Advertise `sessionCapabilities.delete` in `wire.go` and handle `session/delete` in `run.go` (request + notification). Verify: `go test ./internal/acp/...` passes.

## 2. Verification

- [x] 2.1 `cd apps/cli && go build ./...` compiles. Verify: clean.
- [x] 2.2 `gofmt -l ./internal/acp/` and `go vet ./internal/acp/...` clean. Verify: clean.
- [x] 2.3 `go test -race ./internal/acp/...` passes. Verify: pass.
- [x] 2.4 `golangci-lint run ./...` (apps/cli) reports 0 issues. Verify: clean.
- [x] 2.5 `openspec validate fix-acp-session-lifecycle --strict` passes. Verify: clean.
