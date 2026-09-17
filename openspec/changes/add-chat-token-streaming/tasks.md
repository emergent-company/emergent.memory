## 1. Gateway: forward token deltas

- [x] 1.1 In `rewriteChatStream` (`apps/web-ui/gateway/sse_markdown.go`), change the `token` case so it keeps accumulating the delta into the turn buffer but emits `{"type":"token","token":"<delta>"}` instead of a full markdown-rendered `html` event; verify a unit test feeds three token events and asserts three delta events carrying the exact deltas and zero `html` events — covered by `TestRewriteChatStreamTokenEvents`
- [x] 1.2 Assert the markdown render count for a turn is one, not one per token; verify by a test that counts `html` events across a multi-token turn (exactly one after the snapshot rule below is in place) — covered by `TestRewriteChatStreamRendersOncePerTurn` (20 tokens → exactly 1 `html` event)
- [x] 1.3 Preserve the existing behaviour for non-`token` events (`mcp_tool`, `ask_user` suppression, `approval`, `thinking`, `meta`, `error`) exactly as today; verify the existing tests for those paths still pass unmodified

## 2. Gateway: authoritative snapshot

- [x] 2.1 Add a `done` case that emits the single final `{"type":"html", …}` snapshot from the accumulated buffer immediately before passing the upstream `done` event through; verify a unit test asserts the order `…token, html, done…`
- [x] 2.2 Skip the snapshot when the accumulated buffer is empty; verify a test that a `done` with no preceding tokens emits no `html` event
- [x] 2.3 Add the post-loop fallback: if the buffer is non-empty and no snapshot has been emitted, emit it before returning the scanner error; verify tests for (a) an `error`-terminated stream and (b) an EOF without `done`. The helper is guarded by `sb.Len()==0 || snapshotEmitted`
- [x] 2.4 Prove the snapshot is emitted at most once per turn; verify a test asserting no second snapshot when `done` already triggered one
- [x] 2.5 Confirm the snapshot uses the same sanitized renderer as conversation history; verify by asserting the snapshot output equals the history render output for the same text

## 3. Shared client engine: raw-text delta path

- [x] 3.1 Add a raw-text accumulation and append path to the shared engine in `apps/web-ui/gateway/webui/static/js/chat-stream.js`, rendering via `textContent` and never `innerHTML`. Implemented as `ctx.bubbleText` plus `stream.appendToken(delta)`; `updateBubbleText` renders `innerHTML` only when `bubbleHTML` is set, otherwise `textContent`
- [x] 3.2 Make the raw-text path and the existing `html` snapshot path mutually exclusive by construction: before the snapshot the raw path owns the bubble, after it the snapshot path owns it. `appendToken` returns early when `bubbleHTML` is set, and the typing indicator now also requires an empty `bubbleText`
- [x] 3.3 Preserve the existing streaming caret and scroll behaviour on the raw-text path
- [x] 3.4 Confirm no delta content can reach an HTML sink. Raw deltas flow only through `String()` and `textContent`; the only `innerHTML` writes are the static typing-indicator literal and the server-sanitized snapshot, and the sidepanel persists only `bubbleHTML`, never `bubbleText`

## 4. Shared dispatcher and host wiring

- [x] 4.1 Add a `token` case to the shared dispatcher in `apps/web-ui/gateway/webui/static/js/chat-host.js` delegating to an optional host hook (`h.onToken`); unknown event types remain ignored and a host without the hook still renders the snapshot
- [x] 4.2 Wire the hook and raw-text state into `apps/web-ui/gateway/webui/static/js/chat.js`
- [x] 4.3 Wire the hook and raw-text state into `apps/web-ui/gateway/webui/static/js/sidepanel.js`; `recordHistory` persists only `bubbleHTML`, so the persisted reply is the rendered snapshot rather than raw text

## 5. Verification

- [x] 5.1 Run `templ generate` and confirm no unintended generated diff — clean
- [x] 5.2 Run `go build ./...` and `go test ./...` from `apps/web-ui/gateway`; all pass (`apps/web-ui` 20.4s, `components`, `webui`)
- [x] 5.3 Run `task lint` in `apps/web-ui` — `golangci-lint run ./...` reports 0 issues; `gofmt -l` clean; `task css` exit 0
- [x] 5.4 Run `node --check` on every JS file changed — all four pass
- [x] 5.5 Confirm the recorded-run transcript view still renders (shared engine consumer) and that no host is left showing raw text at turn end — confirmed by code path: the run view renders history via `addAssistantMessage`/`toolChip` and never calls `streamChat`, so the delta path is never invoked there; every streaming host ends on the snapshot
- [x] 5.6 Measure before/after on a long answer: the observed markdown render count is **1** per turn (measured by `TestRewriteChatStreamRendersOncePerTurn`), down from one per token. The wire cost reduction from O(N·T) to O(N) deltas plus one snapshot follows structurally from the event change; no byte-level benchmark was captured
- [x] 5.7 Run `openspec validate add-chat-token-streaming --strict` — valid

## 6. Remaining before merge

- [ ] 6.1 Rebase onto the merge result of `add-agentic-chat-control` (unmerged; touches adjacent regions of `chat-stream.js` and `chat-host.js` — the `assistant_message`/`finishStream` path and the `copyText`/`formatDuration` helpers)
- [ ] 6.2 Browser verification of the live delta path in both hosts: confirm plain text streams, the caret behaves, the turn ends on the rendered snapshot, and the sidepanel retains the formatted reply after a reload
- [ ] 6.3 Note for review: this repo has no JS unit harness, so the client-side assertions in 3.x and 4.x are verified by `node --check` plus code-path inspection rather than automated tests. Adding a JS test harness is out of scope for this change
