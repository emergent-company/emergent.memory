## ADDED Requirements

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
