## Context

The chat gateway runs a single global poller (`conversationHub.pollLoop`, 1.5 s tick) that fetches each subscribed conversation's timeline and its project-wide question/approval snapshots, fingerprints the derived state, and broadcasts `{"type":"refresh"}` only on change. The client re-renders the transcript, dock, todos, rail badge, and header status from those frames. Scheduled agent runs live in `agent_runs` (not `/api/chat` conversations) and had no subscription key or endpoint.

## Goals / Non-Goals

- Goal: a page reloaded mid-run tracks persisted steps within ~1.5 s.
- Goal: scheduled runs and brand-new conversations gain the same durable push channel.
- Goal: keep the single-poller design and the 1.5 s cadence; no refresh storm.
- Non-Goal: true push relay of executor events (the poller diff remains the mechanism).
- Non-Goal: changing token-delta streaming (`web-chat-streaming`).

## Decisions

- **Progress signal.** Add `progressCount` (number of `message` + `tool_call` items) and `progressMark` (newest item's `created_at`, falling back to id) to `conversationRunState`, both derived from the history fetch the poller already performs. Count alone is monotonic for appended steps; the marker additionally catches a changed newest item. Comparing these two strings per conversation per tick is negligible.
- **Cadence.** The fingerprint changes only when persisted state moves. During an active run that is at most once per 1.5 s poll (one frame per step-batch), and zero while idle — no storm.
- **Run scope.** Reuse the hub with a keyed scope: a run subscription is keyed `run:<runId>`, conversation keys stay unprefixed (conversation ids are UUIDs, so no collision). `broadcastConversationChanges` dispatches to `runState` (fetches `runTimeline`, matches approvals by `RunID`) or `conversationState`. A shared `runStateFromTimeline` keeps both in lockstep.
- **Client teardown.** One `EventSource` at a time, keyed by scope; `openLiveStream` reuses the same-scope source and closes any other, so switching surfaces never leaks or duplicates.

## Risks / Trade-offs

- A run whose timeline does not grow between polls (e.g. a long single tool call) still shows no new step — acceptable; no persistent state changed.
- Polling per subscribed conversation is unchanged in cost; the run scope adds one timeline fetch per subscribed run per tick.
