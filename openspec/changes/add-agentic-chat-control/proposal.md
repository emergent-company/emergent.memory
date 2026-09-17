## Why

Web chat (`/chat`) is a turn-based bubble chat, but the agents it drives are long-running run-based processes. The gateway already fetches everything needed for run control — run lifecycle items with `run_id` and `run_status`, pending approvals, pending `ask_user` questions, per-run `duration_ms`/`completed_at`, and the conversation's ACP session id — yet almost none of it reaches the UI:

- Stop is client-side only. `chat.js:237-241` calls `AbortController.abort()`; the server-side run keeps executing. No gateway route cancels a run.
- Pending approvals and questions are rendered inline in the transcript with no count and no persistent position. `conversationState` (`conversation_events.go:310`) already computes `pendingApprovals`/`pendingQuestions`, but only as an SSE change-detection fingerprint (`:297`) — no template uses them.
- The session rail shows no run state. A conversation whose run is `input-required`, or `failed`, looks identical to an idle one.
- There is no way to park a follow-up while a turn is running, so users must wait or lose the message.
- Session todos exist server-side (`sessiontodos`, `kb`-backed, `/api/v1/agent/sessions/:sessionId/todos`) and the conversation already carries `acpSessionId` (`extras.go:25`), but the gateway never calls it — agent plans are invisible.
- Turns have no footer: no duration, no end time, no copy.

This change gives the chat the run-control surface it is missing, using only data the gateway already has or already proxies. It deliberately ships **without a server change**.

## What Changes

- **Run state in the session rail.** Gateway derives a run bucket per conversation — `needs_input` > `failed` > `running` > `done` — from the newest run lifecycle item plus pending approvals/questions, and renders it as a badge on each rail row. Buckets reuse the existing `AgentRunStatus` enum and reference `acp-run-lifecycle` rather than redefining it.
- **Live turn state.** While streaming, the rail row and chat header reflect the active run's status (`submitted`/`working`/`input-required`/`failed`) resolved from the newest `run_start`/`run_end` items, which already carry `run_id` and `run_status`.
- **Real interrupt.** The stop button keeps its local abort and additionally calls a new gateway route that proxies the existing upstream run-cancel endpoint, so the server-side run is actually cancelled.
- **Pending-work dock.** Pending approvals and questions move out of the transcript into a dock anchored above the composer, with counts surfaced on the rail row and header and a badge when more than one is pending. Approval/question semantics are unchanged — this is presentation and placement only.
- **Session todo card.** A collapsible todo card in the transcript, resolved through conversation → `acpSessionId` → session todos.
- **Composer queue.** A client-side queue lane: park a follow-up while a turn runs, edit it or promote it to the head with "Send next", auto-released when the turn ends. `Enter` follows turn state; `Cmd/Ctrl+Enter` always queues.
- **Turn footer.** Per-turn model, wall-clock duration from the run's `completed_at − created_at`, end timestamp on hover, and a copy-turn action.
- **Copy affordances.** Copy a whole assistant message and copy a fenced code block.
- **Typed run markers.** `run_start`/`run_end` render as turn boundaries with status; distinct treatment for `failed` and `input-required` ("waiting on you") runs.

Explicitly deferred (documented in `design.md`): steering a running turn (needs a server-side mid-turn injection endpoint that does not exist), `notification`/`compaction` timeline items (no server-side emission), per-turn token/cost (usage is project+time-window only; per-run tokens exist only as OTEL spans), attachments/@-mentions/slash commands, model picker, artifacts panel.

## Capabilities

### New Capabilities

- `web-agent-chat`: the web chat's run-control surface — run state in the session rail, live turn status, server-side interrupt, the pending-work dock, the session todo card, the composer queue, turn footers, copy affordances, and typed run markers.

### Modified Capabilities

None. Existing approval semantics (`tool-approval-policy`), run status enum and cancel/resume (`acp-run-lifecycle`), in-progress run preview (`agent-run-preview`), and usage reporting (`usage-dashboard`) are referenced, not restated or redefined.

## Impact

- `apps/web-ui/gateway/conversation_events.go` — extend the state computation to yield a run bucket, active run id/status, and pending counts; publish them in the refresh payload.
- `apps/web-ui/gateway/memory_runs.go` — add `CancelAgentRun` and `ListSessionTodos` gateway clients for existing upstream endpoints.
- `apps/web-ui/gateway/handlers.go`, `main.go` — new routes for run cancel, the dock partial, and the todos partial.
- `apps/web-ui/gateway/ui.go` — rail rows carry bucket and pending counts.
- New `apps/web-ui/gateway/chat_dock.templ` — dock, todo card, and rail status badge components.
- `apps/web-ui/gateway/chat.templ` — composer queue lane, turn footer, copy affordances, rail badge wiring.
- `apps/web-ui/gateway/webui/static/js/chat.js`, `chat-stream.js`, `chat-components.js`, `chat-host.js` — queue behaviour, footer rendering, copy actions, dock refresh, typed run markers, cancel-on-stop.
- `apps/web-ui/gateway/chat_templ.go` and gateway CSS bundle — regenerated.
- No server, migration, or API-contract change. Unit tests accompany every gateway behaviour; the existing Playwright chat e2e spec is extended for the dock and queue.
