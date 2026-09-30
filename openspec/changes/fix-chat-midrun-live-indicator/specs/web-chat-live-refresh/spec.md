# web-chat-live-refresh — delta (fix-chat-midrun-live-indicator)

## ADDED Requirements

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
message stream. The client SHALL keep re-rendering the transcript as the run
persists new steps while that state is shown, SHALL keep the working bubble
single across re-renders, and SHALL release the working state — removing the
bubble, clearing the indicator, and re-enabling the composer — once the run is
no longer active. A page that owns a live stream SHALL NOT have its in-flight
bubble clobbered by a history re-render.

#### Scenario: Opening a session mid-run

- **WHEN** `/chat?c=<id>` loads while the conversation's newest run is active and the page does not own a live stream
- **THEN** the header shows a working indicator, one agent working bubble (with the typing indicator) renders at the end of the stream, and the composer is busy

#### Scenario: Transcript advances while viewing

- **WHEN** the run persists a new step while the transcript is shown
- **THEN** the transcript re-renders with the persisted step and the working bubble remains a single element

#### Scenario: Run stops

- **WHEN** a refresh reports the run is no longer active
- **THEN** the working bubble is removed, the working indicator clears, and the composer is released (send enabled)

#### Scenario: The owning page is not clobbered

- **WHEN** this page is rendering its own live turn's stream and a refresh arrives
- **THEN** the live in-flight bubble is not wiped by a history re-render
