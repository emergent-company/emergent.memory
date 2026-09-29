## Why

The web chat stream does not feel real-time:

- Thinking frames are fragmented and never marked in-progress. `chat/handler.go:1138-1140` mints a new `thinking` id for *every* delta, and `sse/events.go:99` hardcodes `done:true`. The client groups badges by id and treats `done:true` as terminal (`chat.js:1036-1063`), so every delta becomes its own already-finished badge and no in-progress animation can ever run. The same bug is in `agents/share_handler.go:268-270`.
- A reasoner's reasoning is emitted twice — once as partial `Thought` deltas (`executor.go:3041-3058`) and again as a whole non-partial block (`executor.go:3070-3086`).
- `mcp_tool` events carry no call id (`sse/events.go:104-110`), so the client matches chips by tool name only (`chat-stream.js:468-507`). Parallel or repeated same-name calls collide, and a `completed` with no matching running chip fabricates a finished chip — the "tool report arrives after the tool responded" gap.
- `ToolCallEnd` still fires for confirmation-paused and policy-blocked tools (`executor.go:2417-2422`, `2316-2334`), so a paused tool's chip flips green.
- A mid-run page reload loses all in-flight state: history returns only persisted rows (`agents/repository.go:3205,3238` via `gateway/extras_handlers.go`), so the transcript looks dead while the run is still working.

## What Changes

- **Thinking segments** — the executor decides segment boundaries and emits a stable segment id with `done:false` on open deltas and a single `done:true` at close; the partial-delta path and the whole-block path no longer both emit for one segment.
- **Tool call correlation** — `mcp_tool` gains an additive `id` (the ADK function-call id) so start/end correlate; clients match by id first, name as fallback.
- **Tool status fidelity** — a tool paused for confirmation or blocked by policy reports a distinct non-terminal status instead of `completed`.
- **In-flight replay** — the gateway retains the active run's open thinking segments and running tool calls per conversation and replays them as a `live_replay` frame when a client reconnects to `/api/conversations/:id/events`, so a reloaded page reconstructs what is happening now.
- **Animated affordances** — the thinking icon animates while a segment is open, and a tool chip animates from its `started` event until a terminal status (live and replayed), respecting `prefers-reduced-motion`.

## Impact

- Affected specs: `web-chat-streaming` (delta in this change).
- Affected code:
  - Server: `apps/server/pkg/sse/events.go`, `apps/server/domain/agents/executor.go`, `apps/server/domain/chat/handler.go`, `apps/server/domain/agents/share_handler.go`.
  - Gateway: `apps/web-ui/gateway/sse_markdown.go`, `apps/web-ui/gateway/handlers.go`, `apps/web-ui/gateway/conversation_events.go`, plus the live-state hub.
  - Web client: `apps/web-ui/gateway/webui/static/js/{chat,chat-stream,chat-components,sidepanel}.js`, `apps/web-ui/gateway/webui/css/app.css`.
- Additive JSON only: `thinking.done` may now be `false`; `mcp_tool.id` is new; the new `live_replay` gateway event is ignored by clients that do not handle it. iOS uses the LiveKit `lk.chat.events` protocol (not this SSE contract) and the Mac connector does not bridge HTTP SSE, so it is unaffected.
