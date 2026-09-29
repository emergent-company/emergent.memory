## Context

Two independent SSE paths feed the chat UI:

- **Per-turn live stream** — `POST /api/chat` (gateway `handlers.go:182` → `sse_markdown.go:rewriteChatStream`) proxying the memory server turn stream (`chat/handler.go:688`), carrying `token`/`thinking`/`mcp_tool`/`approval`/`ui`.
- **Refresh channel** — `GET /api/conversations/:id/events` (`conversation_events.go:449`), a 1500 ms poll-backed hub emitting `refresh` frames with run bucket/status.

The web `/chat` page and the global sidepanel share the client engine (`chat-stream.js`, `chat-components.js`) and dispatcher (`chat-host.js`).

## Goals / Non-Goals

- **Goals:** make in-progress thinking and tool activity visible, correctly correlated, and animated; restore in-flight state after a mid-run reload.
- **Non-Goals:** per-token persistence of reasoning; replaying completed (persisted) history; changing the iOS LiveKit protocol.

## Decisions

### D1 — Thinking segment boundaries live in the executor

The executor owns `currentStep`, role, `event.Partial`, and `IsFinalResponse()`; the handler only sees a flat event list. Add `ID` and `Done` to `StreamEvent`; the executor fills them and `chat/handler.go` / `agents/share_handler.go` copy them through unchanged.

Segment id = `fmt.Sprintf("step-%d-%s", currentStep, role)`. Close (`done:true`) on role change, step advance, tool start, final response, or run end.

**Duplicate suppression:** track per step whether reasoning partials were streamed. If yes, the whole non-partial block for the same role/step is suppressed (or emitted as `done:true` only). `operator` planning text is not partial-streamed and stays on the whole-block path. All thinking emission sites must participate: `executor.go` `167`, `184`, `3041-3058`, `3070-3086`.

### D2 — Tool call id is the ADK function-call id

Do **not** use a monotonic counter: `beforeToolCb` can fire concurrently for parallel calls (race + wrong pairing) and a re-invoked tool after approval gets a new id. `tCtx.FunctionCallID()` is stable across the before/after pair (`executor.go:2282` documents this). Add `ID` to `StreamEvent`, populate at `executor.go:2293-2299` and `2577-2588`, and add `ID string json:"id,omitempty"` to `MCPToolEvent`. Client matches by `data-call-id` first, tool name fallback.

### D3 — Status fidelity for non-executed tools

Confirmation-paused tools return `{status:"awaiting_confirmation",...}` (`executor.go:2418-2422`); policy-blocked and share-denied tools return `{error:..., policy:...}` (`2316-2334`). `afterToolCb` currently maps both to `completed` because `toolErr` is nil. Carry an explicit status on the `StreamEvent` so `awaiting_confirmation` is emitted (non-terminal, not green) and synthetic `error` results surface as `error`.

### D4 — Replay only the in-flight tail

The gateway keeps, per conversation, the active run's **open thinking segments** (`done:false`) and **running tool calls** (`status:"started"`, not yet terminal), keyed by run id. State is updated as rewritten frames pass through the chat proxy (a tee around `rewriteChatStream`), dropping a segment when it closes and a tool when it reaches a terminal status, and cleared when the run's bucket leaves `running`.

On `/api/conversations/:id/events` connect, immediately after the existing `refresh` frame, the gateway sends one additive frame:

```json
{"type":"live_replay","runId":"...","frames":[ /* thinking / mcp_tool / approval / question / ui frames in order */ ]}
```

Client renders the frames in order with the same handlers as the live path. Because only open/running frames are retained, persisted (completed) tools in history are never duplicated.

**Alternatives rejected:** a full ring buffer of every frame (stale-frame risk, history dedupe complexity); persisting in-flight reasoning per token (per-token DB writes, schema/migration, GC — YAGNI for an activity indicator).

### D5 — Animation is client CSS + state

`memory-badge-live` already drives a label shimmer; extend it to animate the leading thinking icon, and ensure a running tool chip shows the `loader-circle animate-spin` from its `started` frame (and from replay). `prefers-reduced-motion` disables both.

## Risks / Trade-offs

- `thinking.id` changes semantics from unique-per-event to stable-per-segment. Only the web client uses it as a grouping key (intended). The SDK ignores `done`; CLI ignores `thinking`.
- Buffered live state is in-memory and per-gateway-instance; a gateway restart loses it (the next `refresh` still reports the run bucket, so the page is not dead — just without the tail).
- `live_replay` must be sent before live tailing to preserve ordering.

## Migration Plan

Additive; no migration. Rollback = revert the change set.

## Open Questions

- None blocking; the whole-block/partial double-emission fix must be covered by a unit test in `executor_thinking_test.go`.
