# web-chat-live-refresh Specification

## Purpose
Keeps a chat transcript live for in-progress and scheduled agent runs and for
brand-new conversations: the conversation hub broadcasts a refresh frame when a
subscribed run persists new step, message, or tool-call progress (not only on a
status change), the gateway exposes a run-scoped server-sent events stream, and
the chat client subscribes to at most one live stream per active scope.

## Requirements

### Requirement: Refresh subscribers as a run advances

The conversation hub SHALL include a subscribed transcript's persisted step/message/tool-call progress (a monotonic count plus a newest-item marker) in its change-detection fingerprint, so a subscriber that reloaded or resumed mid-run receives a refresh frame within one poll interval of each persisted step, without waiting for the run to reach a new status.

#### Scenario: Step persisted during a running run

- **WHEN** a subscribed conversation's run is `running` and a message or tool call is persisted
- **THEN** the hub broadcasts a refresh frame on its next poll, even though the run bucket, run id, and run status are unchanged

#### Scenario: Idle transcript does not re-broadcast

- **WHEN** a subscribed transcript is unchanged between two polls
- **THEN** the hub broadcasts no refresh frame

#### Scenario: Decisions decided elsewhere still refresh

- **WHEN** a tool approval or `ask_user` question is decided out of band
- **THEN** the hub still broadcasts a refresh frame

### Requirement: Live channel for scheduled runs

The gateway SHALL expose a run-scoped server-sent events endpoint whose change-detection state is derived from the run's own DTO (so a run has a real id and status, not a permanently empty `done` state), and the chat client SHALL subscribe to it when a scheduled agent run's transcript is opened — in the chat workspace and on the standalone run page — so the run shows step progress and status changes live.

#### Scenario: Run transcript opened

- **WHEN** a scheduled run is opened in the chat workspace
- **THEN** the client opens the run's events stream and re-renders the run transcript on each refresh frame

#### Scenario: Standalone run page subscribes

- **WHEN** the standalone `/runs/:runId` page loads
- **THEN** the client opens the run's events stream (not a one-shot render)

#### Scenario: Run status transition without new steps

- **WHEN** a subscribed run's status changes (e.g. `working` → `completed`) with no new persisted step
- **THEN** the hub broadcasts a refresh frame carrying the run's id, status, and derived bucket

### Requirement: Live channel for new conversations

The chat client SHALL subscribe to the conversation events stream as soon as a brand-new conversation receives its id, and SHALL keep at most one events stream open across conversation and run scopes.

#### Scenario: New conversation receives its id

- **WHEN** the first turn on a new conversation emits the conversation id
- **THEN** the client opens the conversation events stream without a reload

#### Scenario: Scope switch closes the previous stream

- **WHEN** the client switches from a run to a conversation, or from one conversation to another
- **THEN** the previously open events stream is closed before the new one opens

#### Scenario: Stale transcript fetch is dropped after a switch

- **WHEN** a transcript refresh fetch is in flight and the user switches scope before it resolves
- **THEN** the resolved render is dropped unless the pane still shows the scope the fetch belongs to

### Requirement: Connecting subscriber receives the current run state

The conversation events stream SHALL send its first frame carrying the
conversation's current run bucket, run id, run status, and pending
approval/question counts whenever a prior refresh frame has been broadcast for
that conversation, so a page opened while a run is in flight shows the working
state immediately instead of waiting for the next change-driven broadcast. The
initial frame SHALL precede any `live_replay` frame. When no prior frame exists
(a brand-new conversation), the stream SHALL fall back to a bare
`{"type":"refresh"}` frame and the poller SHALL broadcast the state on its next
tick. Run-scoped event streams SHALL NOT replay a cached conversation frame.

#### Scenario: Second subscriber opened mid-run

- **WHEN** a client connects to a conversation's events stream while that conversation already has an active run and a prior broadcast exists
- **THEN** the first frame carries the run's bucket/runId/runStatus and pending counts, before the `live_replay` frame

#### Scenario: Brand-new conversation

- **WHEN** a client connects and the conversation has never been broadcast (no subscribers have polled yet)
- **THEN** the first frame is a bare `{"type":"refresh"}` and the poller broadcasts the current state on its next poll

#### Scenario: Run-scoped stream is unaffected

- **WHEN** a client connects to a run-scoped events stream (`/api/runs/:runId/events`)
- **THEN** no cached conversation frame is replayed and the stream still opens with a refresh frame

### Requirement: A mid-run open shows the active run and releases when it stops

The chat client SHALL, when a rendered transcript's newest run is still active
and this page does not own a live stream, surface that run as a working state: a
header working indicator and exactly one inline agent "working" bubble in the
message stream. The header SHALL reflect the run's bucket, so a run parked on a
decision (`needs_input`) reads "Waiting on you" even when its run status is still
`working`, and an empty-bucket transcript render SHALL NOT overwrite a bucket the
events stream has already reported. The client SHALL keep re-rendering the
transcript as the run persists new steps while that state is shown, SHALL keep
the working bubble single across re-renders, and SHALL release the working state
— clearing the header indicator, removing the bubble, and re-enabling the
composer — once the run is no longer active: on every render whose newest run is
stopped or absent, and whenever a conversation or run is (re)opened, so a stale
working state from a previous scope is never carried across. The release SHALL be
scoped to the conversation surface: a run-scope transcript carries no
run lifecycle items, so its newest-run status is always absent and the
refresh-reported bucket is authoritative — a render of an active run's own
transcript (bucket `running`, or any run scope) SHALL keep the working header,
bubble, busy composer, and cancelable run id, and SHALL NOT tear them down. A
page that owns a live stream SHALL NOT have its in-flight bubble clobbered by a
history re-render.

#### Scenario: Opening a session mid-run

- **WHEN** `/chat?c=<id>` loads while the conversation's newest run is active and the page does not own a live stream
- **THEN** the header shows a working indicator, one agent working bubble (with the typing indicator) renders at the end of the stream, and the composer is busy

#### Scenario: Transcript advances while viewing

- **WHEN** the run persists a new step while the transcript is shown
- **THEN** the transcript re-renders with the persisted step and the working bubble remains a single element

#### Scenario: Run stops

- **WHEN** a refresh reports the run is no longer active
- **THEN** the working bubble is removed, the working indicator clears, and the composer is released (send enabled)

#### Scenario: An active run-scope transcript is not released by its own refresh

- **WHEN** a run-scope transcript (which carries no run lifecycle items) re-renders after a refresh reporting bucket `running`
- **THEN** the working indicator, the working bubble, and the busy composer remain, and the run id stays set so the stop control can still cancel the run

#### Scenario: Opening an idle or run-less session

- **WHEN** a conversation or run is (re)opened and its newest run is stopped or absent (including an empty transcript)
- **THEN** no working indicator and no working bubble are shown, and the composer is released (send enabled, stop hidden) — a working state carried from the previous scope or a bare refresh is cleared

#### Scenario: Run parked on a decision

- **WHEN** the run is paused awaiting a human decision and the refresh reports bucket `needs_input` while the run status still reads `working`
- **THEN** the header reads "Waiting on you" and a following empty-bucket transcript render does not change it to "Working…"

#### Scenario: The owning page is not clobbered

- **WHEN** this page is rendering its own live turn's stream and a refresh arrives
- **THEN** the live in-flight bubble is not wiped by a history re-render
