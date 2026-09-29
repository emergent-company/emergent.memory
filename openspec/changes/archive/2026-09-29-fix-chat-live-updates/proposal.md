## Why

A `/chat` page that is reloaded or resumed while an agent run is executing stays frozen until the run's status changes: the conversation SSE hub only broadcasts a `refresh` frame when its fingerprint changes, and that fingerprint excluded persisted step/message/tool progress. A 5–6 minute run therefore looks hung. Separately, two chat surfaces have no live channel at all — a scheduled run's transcript (`resumeRun`) and a brand-new conversation open no `EventSource`, so out-of-band state changes never reach them.

## What Changes

- The hub's change-detection fingerprint includes the run's step/message/tool-call progress (a count plus a newest-item marker), so a run that stays `running` re-broadcasts as the executor persists steps. The poll cadence is unchanged, so this is at most one refresh per 1.5 s poll and none while the transcript is stable.
- A new run-scoped SSE endpoint (`GET /api/runs/:runId/events`) lets the hub fingerprint a scheduled run's synthesized timeline (runs are not `/api/chat` conversations); the client subscribes when a run transcript is opened.
- The chat client opens the conversation events stream for a brand-new conversation once its `meta` event supplies the id, and keeps at most one stream open across conversation/run scopes (switching closes the previous one).
- Approvals/questions decided elsewhere continue to refresh via the existing fingerprint fields.

## Capabilities

### New Capabilities

- `web-chat-live-refresh`: the SSE push channel that keeps an open chat page current with an in-flight run — step/message/tool progress on a resumed or reloaded conversation, and live step progress for a scheduled run transcript and a brand-new conversation.

### Modified Capabilities

<!-- None: web-chat-streaming covers raw token deltas + the authoritative markdown snapshot and is unchanged. -->

## Impact

- Gateway: `gateway/conversation_events.go` (fingerprint, run-scoped state, `/api/runs/:id/events`), `gateway/main.go` (route). Unit tests in `gateway/conversation_events_test.go`.
- Client: `gateway/webui/static/js/chat.js` (shared `openLiveStream` helper; new-conversation + run subscriptions).
- e2e: `tests/e2e/scenarios/chat/chat-run-control.spec.ts` asserts the new-conversation subscription.
