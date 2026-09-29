## 1. Server — SSE contract

- [x] 1.1 Add `ID`/`Done`/`Status` to `agents.StreamEvent`.
- [x] 1.2 Executor: stable thinking segment ids + `done` semantics; suppress partial/whole-block double emission across all five sites.
- [x] 1.3 Executor: set tool call `ID` from `tCtx.FunctionCallID()` on start/end; carry explicit status for paused/blocked tools.
- [x] 1.4 `pkg/sse/events.go`: `NewThinkingEvent` accepts `done`; add `ID` to `MCPToolEvent`; extend status vocabulary.
- [x] 1.5 `chat/handler.go` + `agents/share_handler.go`: map `ID`/`Done`/tool `ID`/status through.
- [x] 1.6 Tests: `executor_thinking_test.go`, `pkg/sse/events_test.go`.

## 2. Gateway — pass-through + in-flight replay

- [x] 2.1 `sse_markdown.go`: preserve `id`/`status`/`done` through `emitToolResultHTML` (verified; test locks it).
- [x] 2.2 Live-state hub: retain open thinking segments + running tools per conversation/run; tee the rewritten frames; drop/clear on close/terminal/run-end.
- [x] 2.3 `conversation_events.go`: emit `live_replay` after the initial `refresh` frame on connect.
- [x] 2.4 Tests: hub state transitions, replay frame ordering, no replay after run end.

## 3. Web client — correlation, grouping, hydration

- [x] 3.1 `chat-stream.js`: match tool chips by `data-call-id` first, name fallback; handle `awaiting_confirmation` as non-terminal.
- [x] 3.2 `chat.js`: group thinking deltas by stable id; start/stop live state from `done`.
- [x] 3.3 `chat-host.js`/`chat.js`: handle `live_replay` — render frames before live tail.
- [x] 3.4 Parity for the sidepanel and the recorded-run transcript view.

## 4. Design — animated affordances

- [x] 4.1 Thinking icon animates while the segment is open (live and replayed).
- [x] 4.2 Tool chip shows an animated spinner from `started` until terminal status (live and replayed).
- [x] 4.3 `prefers-reduced-motion` disables both; no layout shift.

## 5. Spec + verification

- [x] 5.1 Delta spec added; `openspec validate chat-realtime-tool-streaming --strict` clean.
- [x] 5.2 `apps/server`: `go build ./...`, `go test ./...` for touched packages.
- [x] 5.3 `apps/web-ui/gateway`: `go build ./...`, `go test ./...`, `task lint`.
- [ ] 5.4 Playwright: `chat-tool-correlation.spec.ts` written but not executed here (suite needs the dev stack + browsers); CI/dev run pending.
- [ ] 5.5 Manual browser check on dev: in-flight spinner, animated thinking, mid-run reload restores state.
