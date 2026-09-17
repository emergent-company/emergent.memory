## Why

The gateway re-renders the **entire** markdown document on **every** streamed token and ships the whole document to the client, which replaces `innerHTML` with it each frame (`apps/web-ui/gateway/sse_markdown.go:79-87` → `chat-stream.js:306-328`). For an N-character answer delivered in T token events that is ~T goldmark renders and O(N·T) bytes per turn — for a long agentic answer, tens of megabytes and hundreds of redundant document renders. It is the single largest structural cost in the chat path, and it grows quadratically with answer length, which is exactly the direction agentic transcripts move.

Two smaller consequences of the same code path:

- The client cannot render anything until the first full-document `html` frame arrives, so the live view is a sequence of whole-document swaps rather than an append.
- There is no stream resume, and there cannot be a cheap one: the stream is a `fetch` POST body, and re-opening it would re-execute the agent turn.

The fix is to stop rendering markdown per token: forward the raw token deltas the upstream already emits, append them client-side as plain text through `textContent` (no HTML interpretation, so no new XSS surface), and emit exactly **one** server-rendered, sanitized markdown snapshot at the end of the turn as the authoritative render.

This is gateway + web-asset work only. `apps/server` already emits one `token` event per text delta (`apps/server/pkg/sse/events.go:61-64`, `apps/server/domain/chat/handler.go:1089-1091`); the gateway is the single place that turns them into quadratic work.

## What Changes

- **Forward token deltas.** `rewriteChatStream` stops accumulating-and-rendering per token. Each upstream `token` becomes a `{"type":"token","token":"<delta>"}` event on the gateway stream. Server-side markdown renders per turn drop from T to 1.
- **One authoritative snapshot.** Immediately before passing the upstream `done` event through, the gateway emits a single `{"type":"html", …}` carrying the full markdown render of the accumulated turn text. A post-loop fallback emits it if the stream ends without `done` (error or EOF), so a client always ends on formatted markdown.
- **Client delta path.** The shared streaming engine gains an append-text path that accumulates raw deltas and renders them with `textContent` — never `innerHTML`. The existing `html` path is retained unchanged for the authoritative snapshot.
- **All three hosts, one implementation.** The change lands in the shared engine and event dispatcher, so the `/chat` page, the global sidepanel, and the recorded-run transcript view all gain the delta path additively. Unknown event types remain ignored, so hosts that do not wire the new hook keep working.
- **Sidepanel correctness.** The sidepanel has no history re-fetch on turn end (it persists the live bubble to `localStorage`), so the final server-rendered snapshot is load-bearing for it, not a nicety.
- **Explicit safety rule.** The delta path must never reach `innerHTML` or `insertAdjacentHTML`; server-side sanitisation (`bluemonday` via `renderMarkdown`) remains the single security boundary.

Deliberately **not** built, with reasons recorded in `design.md`: a client-side markdown renderer, a client-side sanitizer (DOMPurify), `last-event-id` plus a replay buffer, intermediate throttled `html` snapshots, and Paseo's steady-visual-rate smoothing and 32k live-render cap.

## Capabilities

### New Capabilities

- `web-chat-streaming`: how the web chat transports and renders streamed assistant text — raw token deltas appended client-side as plain text, a single authoritative server-rendered markdown snapshot per turn, and the safety rule that raw deltas never reach an HTML sink.

### Modified Capabilities

None. The existing `html` event contract is extended, not replaced, and no other capability's requirements change.

## Impact

- `apps/web-ui/gateway/sse_markdown.go` — `rewriteChatStream`: `token` case forwards a delta; `done` case emits the single final snapshot; post-loop fallback. `renderMarkdown` call count per turn goes from T to 1.
- `apps/web-ui/gateway/sse_markdown_test.go`, `apps/web-ui/gateway/handlers_test.go` — assertions move from per-token `html` frames to delta frames plus one final snapshot (including the deterministic chat test mode).
- `apps/web-ui/gateway/webui/static/js/chat-stream.js` — raw-text accumulation and a `textContent`-based append path in the shared engine; the two render paths are mutually exclusive by construction.
- `apps/web-ui/gateway/webui/static/js/chat-host.js` — a `token` case in the shared event dispatcher, as an optional host hook.
- `apps/web-ui/gateway/webui/static/js/chat.js`, `apps/web-ui/gateway/webui/static/js/sidepanel.js` — wire the append hook and carry the raw-text state.
- No `apps/server` change, no migration, no API-contract change, no new dependency.
- Merge-conflict note: an unmerged change (`add-agentic-chat-control`) touches adjacent regions of `chat-stream.js` and `chat-host.js`. This change is designed to be rebased onto its merge result rather than against its unmerged state.
