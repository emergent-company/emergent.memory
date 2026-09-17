## Context

Verified against primary sources in this worktree.

- `POST /api/chat` → `chat()` (`apps/web-ui/gateway/handlers.go:161`), which pipes the upstream body through `rewriteChatStream` (`sse_markdown.go:53`) and streams the result (`handlers.go:187-191`).
- `rewriteChatStream` accumulates every `token` into a `strings.Builder` (`sse_markdown.go:57,80`) and re-emits `renderMarkdown(sb.String())` as a full `html` event per token (`sse_markdown.go:79-87`).
- Client: the dispatcher's `html` case (`chat-host.js:236-240`) drives `setBubbleHTML` → `updateBubbleText`, which assigns `el.innerHTML = ctx.bubbleHTML` (`chat-stream.js:306-328`). A full-document swap per frame.
- Upstream vocabulary is `meta`/`token`/`thinking`/`mcp_tool`/`error`/`approval`/`done` (`apps/server/pkg/sse/events.go:6-29`); one `token` per text delta (`apps/server/domain/chat/handler.go:1089-1091`).
- `renderMarkdown` is goldmark + `bluemonday.UGCPolicy()` (`markdown.go:22-28`). That sanitizer is the security boundary.
- `chat-stream.js` is shared by `/chat` (`chat.js`), the sidepanel (`sidepanel.js`), and the recorded-run transcript (`runs.templ:48-51`).
- No client-side markdown renderer exists; `gateway/package.json` is CSS tooling only, with no JS bundler and no markdown/DOMPurify dependency.

Two facts that shape the decision, both already true today:

1. **Live output already differs from history output.** `splitLeadingReasoning` runs only in `renderHistoryHTML` (`sse_markdown.go:310-318`), not in the live path (`:81`). The existing contract is therefore already "history is authoritative", and `/chat` already discards the live DOM at turn end by re-fetching history (`chat.js:904-907` → `refreshActiveTranscript`).
2. **The sidepanel has no history re-fetch.** `sidepanel.js:137` binds `onStreamFinish → recordHistory`, which persists the live `bubbleHTML` to `localStorage` (`:174-182`) and restores it with `innerHTML` (`:626-648`). Whatever the live path produces is what the sidepanel keeps.

Fact 2 is why a final authoritative snapshot is required rather than optional: without it the sidepanel would persist an empty reply.

## Options considered

**A. Server-side coalescing only.** Keep full-document `html` snapshots but emit them on a ~150ms threshold plus a final one. Smallest diff, zero client and zero XSS change, cuts T by roughly 15×. Rejected as the end state: the N factor is untouched, so a 100k-character answer at 150ms over a minute still means hundreds of growing-document renders and tens of megabytes. It makes long transcripts less awful rather than cheap.

**B. Raw deltas plus a client-side incremental markdown renderer.** Eliminates the quadratic term entirely, but requires vendoring a client markdown implementation and a client sanitizer. No bundler exists, and it would create a second, weaker, independently versioned security boundary that has to agree with goldmark+bluemonday to avoid live/history divergence — which it cannot do in general. Rejected as the over-engineering trap.

**C. Hybrid with throttled intermediate snapshots.** Raw deltas for cheap live text, plus periodic server snapshots for fidelity. Rejected: a tight cadence is barely better than A for long messages, and a loose cadence buys nothing because the turn-end swap already provides fidelity.

**D. Raw deltas plus one authoritative snapshot at `done`. CHOSEN.** Deltas append as plain text (`textContent`), no intermediate snapshots, exactly one server-rendered sanitized `html` snapshot at the end of the turn. This is the only option that makes long transcripts genuinely cheap (total wire bytes go from O(N·T) to O(N) deltas + one render), adds no new XSS surface (raw → `textContent`; the snapshot stays bluemonday-sanitized), and needs no client markdown renderer. It costs a bounded, transient UX change: the answer renders as plain text while it streams, then snaps to formatted markdown at turn end — which `/chat` already does today via the history re-fetch.

## Frozen interface

New gateway stream event:

```json
{"type":"token","token":"<raw text delta>"}
```

`html` is unchanged in shape and remains the authoritative, server-rendered, sanitized snapshot. Ordering guarantee per turn:

1. zero or more `token` deltas (raw),
2. exactly one `html` snapshot — emitted before `done` when the upstream sends `done`, or by the post-loop fallback when the stream ends without it,
3. `done` (unchanged) or the stream ends.

The fallback condition is: the accumulated buffer is non-empty and no snapshot has yet been emitted. It runs after the scan loop, before returning the scanner error.

Shared engine contract (additive; existing hosts keep working because unknown event types remain ignored):

- `chat-host.js` dispatcher gains `case "token"` delegating to an optional host hook.
- `chat-stream.js` gains an append-text path: raw deltas accumulate into a raw string and render via `textContent`. The `bubbleHTML` and raw-text paths are mutually exclusive by construction — once the authoritative `html` arrives, the `innerHTML` path owns the bubble.
- `chat.js` and `sidepanel.js` add the raw-text state and wire the hook.

## Non-goals and safety

- **No client-side markdown renderer.** It would duplicate goldmark and invite live/history divergence.
- **No client-side sanitizer.** A second, weaker boundary; the server sanitizer stays the only one.
- **No `last-event-id` or replay buffer.** The stream is a `fetch` POST body over `ReadableStream` (`chat-stream.js:830-882`), not an `EventSource`, so there is no `Last-Event-ID` plumbing and none is free; and re-opening the upstream stream would re-execute the agent turn. The existing repair is sufficient: `refreshActiveTranscript` re-fetches history at turn end, and if a stream drops mid-run the run continues server-side, `run_end` lands, the 1.5s poll hub fires `refresh` (`conversation_events.go:297-301`), and the client re-renders. The only residual gap is the bubble showing an interrupted state for the seconds until the run actually ends — cosmetic, and not worth a replay buffer.
- **No Paseo-style steady-visual-rate smoothing and no live 32k cap.** Polish, not correctness; if a cap is ever wanted it belongs in the server history render.
- **Safety rule, to be enforced in review:** the `token` path must never reach `innerHTML` or `insertAdjacentHTML`. Server sanitisation remains the single boundary, and the final snapshot is produced by the same renderMarkdown (goldmark + bluemonday) the history render uses, so it is never a weaker render. The two are not always byte-identical: `splitLeadingReasoning` still runs only in the history path (see "Fact 1" above), so a leading chain-of-thought line is present in the live snapshot and split out on the history re-render.

## Risks

- **Transient plain-text rendering.** The answer is unformatted while streaming and formats at turn end. Bounded and already the de-facto behaviour on `/chat`; the escape hatch if it proves unacceptable is option A's throttled snapshots, which should not be built speculatively.
- **Sidepanel regression if the final snapshot is missed.** Without it, `sidepanel.js` persists an empty reply. The fallback path exists specifically to guarantee it.
- **`textContent` discipline eroding.** A future change routing `token` through `innerHTML` reintroduces XSS. The requirement encodes the rule.
- **Test drift.** The gateway SSE tests and the deterministic chat test mode assert the current token→html behaviour and must be updated in the same change.
- **Merge surface.** `chat-stream.js` and `chat-host.js` are also touched by the unmerged `add-agentic-chat-control`. Regions differ but are adjacent; rebase onto its merge result.

## Lane split

| Lane | Owner | Scope | Files |
|---|---|---|---|
| A | `@fixer` | Gateway stream rewrite: delta forwarding, single final snapshot, post-loop fallback, and the Go test updates | `sse_markdown.go`, `sse_markdown_test.go`, `handlers_test.go` |
| B | `@fixer` | Shared client engine delta path and host wiring | `chat-stream.js`, `chat-host.js`, `chat.js`, `sidepanel.js` |

Disjoint file ownership; both code against the frozen interface above. Lane B does not need lane A to compile.
