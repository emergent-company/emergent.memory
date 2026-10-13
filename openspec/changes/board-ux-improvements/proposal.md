## Why

The object-driven Kanban board works end to end (reconciler → agent run → status
transitions), but the operator hit five closely-related usability gaps while
using it on a live project:

1. **The board does not refresh itself.** A board POST (`request-changes`,
   `approve`, drag) re-renders `#board` once, but the *asynchronous* transitions
   the agent performs afterwards (`revision → in_progress → review`) are never
   pushed, so the board looks stale until a manual reload. The operator read
   this as "the task is not processing".
2. **The board is width-constrained.** The page is wrapped in
   `layout.Container(ContainerLG)` (`max-w-6xl`) and the lanes are a
   `flex … overflow-x-auto` row of `min-w-64 shrink-0` columns, so the six
   status lanes force a horizontal scroll even on a wide screen.
3. **The preview has no change history.** The shared object preview drawer
   (`objectPreviewContent`) shows properties/details/labels but not the object's
   version chain, even though the server already exposes
   `GET /api/graph/objects/{id}/history`.
4. **The preview shows raw status keys.** `obj.Status` is rendered verbatim
   (`in_progress`) as a badge label and in the Details row, while the schema
   browser already humanises the same value (`boardStatusLabel` →
   `In progress`). The operator explicitly expected the formatted label.
5. **Board runs are not discoverable as sessions.** The chat rail can filter
   sessions by origin (manual / scheduled / shared) and can open a run
   transcript, but the runs the board itself triggers are not listed anywhere —
   so there is no way to watch what the agent actually did for a task.

## What Changes

- **Board live refresh**: the board page subscribes to the gateway's existing
  project event stream (the verbatim SSE proxy at
  `/api/notifications/stream?scope=project&project_id=…`) and re-fetches
  `#board` (debounced) when a `graph_object`/`agent_run` frame arrives, with
  guards for hidden tabs and in-flight drags.
- **Full-width board**: `layout.ContainerSize` gains a `ContainerFull`
  (`max-w-none`) variant (go-daisy PR), the board page uses it, the lanes become
  a responsive auto-fit grid that shares the available width, and the Tailwind
  candidate set explicitly includes `max-w-none`.
- **Preview change history**: the gateway `MemoryClient` exposes the object
  history endpoint, a new `GET /objects/:id/history` partial renders a
  newest-first version timeline, and the shared preview drawer lazy-loads it in
  a new "History" section.
- **Humanised work statuses**: the preview and the objects UI render statuses
  through `boardStatusLabel`/`boardStatusIntent` (raw value kept in attributes).
- **Board sessions in chat**: a new server endpoint lists a project's
  board-origin runs (`subject_object_id IS NOT NULL`), the gateway maps them to
  session summaries with origin `board` and joins the work item for context, and
  the chat rail gains a `Board` origin option whose rows open the run
  transcript.

## Capabilities

### New Capabilities

- `web-board-live-refresh`: the board reflects asynchronous work-item
  transitions without a manual reload.
- `web-board-layout`: the board uses the full available width instead of
  forcing a horizontal scroll.
- `web-object-preview-history`: the object preview drawer shows the object's
  version/change history.
- `web-work-status-labels`: work statuses are presented as humanised labels
  everywhere they are shown, never as raw keys.
- `web-board-sessions`: runs triggered by the board are discoverable and
  openable as sessions from the chat area.

### Modified Capabilities

- `object-driven-agent-work`: the Kanban projection gains a companion
  board-runs listing so a task's executions are addressable as sessions.

## Impact

- go-daisy: `components/layout/container.templ` (+ generated) — additive
  `ContainerFull` variant.
- Server: `apps/server/domain/agents` (new project board-runs listing +
  `AgentRunDTO.subjectObjectId`).
- Gateway: `board.templ`, `board.js` (new), `object_preview.templ/.go`,
  `objects.templ`, `memory_graph.go`, `memory_work.go`, `client_api.go`,
  `chat.templ`, `ui.go`, `backend.go`, `main.go`, `webui/css/app.css`.
- Tests: gateway unit tests for refresh wiring, label rendering, history
  partial, and session summarisation; server test for the new listing.
