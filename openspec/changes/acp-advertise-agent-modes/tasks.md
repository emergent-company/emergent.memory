## 1. Wire types

- [x] 1.1 Add `Mode`, `SessionModeState`, `ConfigOption`, `ConfigOptionValue`, `SetModeParams`, `SetConfigOptionParams` to `wire.go`; extend `SessionNewResponse` with `modes` and `configOptions`. Verify `go build ./...`.

## 2. Agent

- [x] 2.1 Add `modes []Mode` to `Agent` and a per-session `skill` field; make `NewAgent` accept the mode list and guarantee the default skill is present (`ensureDefaultMode`). Verify `go build ./...`.
- [x] 2.2 Emit `modes` + `configOptions` from `newSession`. Verify `go build ./...`.
- [x] 2.3 Add `setMode` and `setConfigOption` (validate + record per-session skill + return updated state). Verify `go build ./...`.
- [x] 2.4 Route `session/prompt` through the per-session skill. Verify `go build ./...`.

## 3. Dispatch

- [x] 3.1 Add `session/set_mode` and `session/set_config_option` cases to `run.go` dispatch. Verify `go build ./...`.

## 4. Command

- [x] 4.1 In `acp.go`, fetch the extended AgentCard (short timeout, non-fatal) and pass the derived modes to `NewAgent`. Verify `go build ./...`.

## 5. Tests

- [x] 5.1 Unit-test modes on `session/new` (multi + single/default). Verify `go test ./internal/acp/...`.
- [x] 5.2 Unit-test `session/set_mode` routing + unknown-mode/missing-param errors. Verify `go test ./internal/acp/...`.
- [x] 5.3 Unit-test `session/set_config_option` routing + error cases. Verify `go test ./internal/acp/...`.

## 6. Specs + verify

- [x] 6.1 Write `proposal.md`, `tasks.md`, and the `cli-acp` delta spec. Verify `openspec validate acp-advertise-agent-modes --strict`.
- [x] 6.2 `go build ./... && go vet ./... && go test ./internal/acp/...` in `apps/cli/`. All pass.
