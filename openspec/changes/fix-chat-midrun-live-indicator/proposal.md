# Fix: opening a chat session mid-run shows no working indicator or agent bubble

## Why

Opening `/chat?c=<conversationId>` while an agent run is in flight showed a
frozen transcript: no agent "working" bubble in the message stream and no
"agent is working" indicator. Two independent gaps caused this:

1. **Gateway SSE initial frame carried no state.** `GET
   /api/conversations/:id/events` opened with a bare `{"type":"refresh"}`. The
   hub poller only broadcasts when the change-detection fingerprint changes, so
   a page connecting while another subscriber already held the conversation
   (the tab that started the run) never received a state-bearing frame — it
   stayed unaware the run was active.
2. **Client never derived the active run from history, and froze it.** The
   transcript renderer computes the newest run's status but never surfaced it,
   so the running indicator stayed off. Worse, once a refresh did report a
   working run the client set its single `streaming` flag, which then blocked
   every subsequent history re-render — the transcript froze mid-run and the
   composer stayed stuck ("stop") after the run ended. The same freeze affected
   the standalone `/runs/:runId` transcript.

## What Changes

- **Gateway:** the conversation events stream replays the conversation's last
  broadcast refresh frame (bucket/runId/runStatus/pending) as its first frame,
  so a page opened mid-run shows the current state immediately. Brand-new
  conversations (no prior broadcast) keep the bare refresh; run-scoped streams
  are unchanged.
- **Client (`chat.js` / `chat-stream.js`):** derive the active run from the
  rendered history and show the header working indicator plus a single inline
  agent "working" bubble; split the overloaded `streaming` flag into
  `streaming` (turn-busy: composer/queue/rail) and `liveTurn` (this page
  DOM-owns a live stream), so a page viewing a run it did not start keeps
  re-rendering as steps persist and releases cleanly when the run stops. A
  single engine hook raises `liveTurn` on every live turn start.

## Non-Goals

- Streaming another tab's in-flight thinking/tool detail to a viewing tab
  (`live_replay` remains connect-only; the viewing tab shows the placeholder +
  persisted history).
- Changing the side panel: it never had the refresh-render path and is
  unaffected (the new engine hook is optional and unset there).
- Any server/executor run-lifecycle change.

## Impact

- Capability: `web-chat-live-refresh`.
- Files: `apps/web-ui/gateway/conversation_events.go` (+ tests),
  `apps/web-ui/gateway/webui/static/js/chat.js`,
  `apps/web-ui/gateway/webui/static/js/chat-stream.js`, new hermetic spec
  `apps/web-ui/tests/e2e/specs/js/chat-midrun-working.spec.ts`.
