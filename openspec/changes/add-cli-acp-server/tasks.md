## 1. ACP wire package (TDD)

- [x] 1.1 Add `apps/cli/internal/acp/wire.go` — newline-delimited JSON-RPC 2.0 stream (reader + mutex-serialized writer) and the ACP method types (initialize/session-new/prompt/cancel + session/update). Verify: `go build ./...` compiles.
- [x] 1.2 Add `apps/cli/internal/acp/agent.go` — the Agent bridging initialize/session-new/prompt/cancel to the Memory A2A client, with per-session `contextId` threading and cancellation. Verify: `go build ./...` compiles.
- [x] 1.3 Add `apps/cli/internal/acp/run.go` — the stdio dispatch loop with concurrent prompt handling and a `sync.WaitGroup` so in-flight prompts drain before EOF exit. Verify: `go build ./...` compiles.
- [x] 1.4 Add `apps/cli/internal/acp/acp_test.go` — httptest-backed tests for initialize, session/prompt round-trip (skill id + reply streaming), empty-prompt no-op, and unknown-method error. Verify: `go test ./internal/acp/...` passes.

## 2. CLI command

- [x] 2.1 Add `apps/cli/internal/cmd/acp.go` — `memory acp` command (group `ai`), `--agent` flag + `MEMORY_AGENT` env, reusing `getA2AClient` for config/auth, running `acp.Run` on os.Stdin/out/err. Verify: `go build ./...` compiles and `memory acp --help` lists the command.

## 3. Streaming (A2A message:stream SSE)

- [x] 3.1 Switch `Agent.prompt` from sync `SendMessage` to `StreamMessage` (SSE) and add `consumeStream` to drain events. Verify: `go build ./...` compiles.
- [x] 3.2 Add `textEmitter` to emit incremental `agent_message_chunk` deltas, deduplicating the repeated full text carried by artifact/message/task events. Verify: `go test ./internal/acp/...` passes.
- [x] 3.3 Capture `contextId` from stream events for multi-turn threading. Verify: `go test ./internal/acp/...` passes.

## 4. Human-in-the-loop resume

- [x] 4.1 Track a per-session `pendingTaskID`; on `TASK_STATE_INPUT_REQUIRED` emit the question and record the task id. Verify: `go build ./...` compiles.
- [x] 4.2 On the next prompt in the session, resume by sending `message.taskId` (without `metadata.skillId`). Verify: `go test ./internal/acp/...` passes.

## 5. Build, lint, and verification

- [x] 5.1 `go build ./...` (apps/cli) compiles. Verify: clean.
- [x] 5.2 `gofmt` + `go vet ./internal/acp/... ./internal/cmd/...` clean. Verify: clean.
- [x] 5.3 `golangci-lint run ./internal/acp/...` reports 0 issues. Verify: clean.
- [x] 5.4 `go test ./internal/acp/...` passes (streaming chunks + HITL resume covered). Verify: pass.
