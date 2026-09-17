## ADDED Requirements

### Requirement: Derive a run bucket for each conversation
The gateway SHALL compute, for every conversation shown in the chat session rail, a single run bucket using the priority order `needs_input` > `failed` > `running` > `done`. The bucket SHALL be derived from the conversation's newest run lifecycle item (`run_start`/`run_end`, whose `run_id` and `run_status` fields are already present) combined with any pending approval or pending `ask_user` question for that conversation. A pending approval or pending question SHALL force the `needs_input` bucket regardless of run status. A newest run status of `failed` SHALL yield `failed`; `submitted` or `working` SHALL yield `running`; `completed`, `cancelled`, or `skipped` SHALL yield `done`. A conversation with no run lifecycle item SHALL yield `done`.

#### Scenario: Pending approval outranks a failed run
- **WHEN** a conversation's newest run has status `failed` and it also has one pending tool approval
- **THEN** the computed bucket is `needs_input`

#### Scenario: Working run is running
- **WHEN** a conversation's newest run lifecycle item has `run_status` `working` and there are no pending approvals or questions
- **THEN** the computed bucket is `running`

#### Scenario: Conversation with no runs
- **WHEN** a conversation has no `run_start` or `run_end` item in its history
- **THEN** the computed bucket is `done` and no error is surfaced

#### Scenario: Bucket is computed without extra upstream calls
- **WHEN** the events hub polls conversation state
- **THEN** the bucket, active run id, active run status, pending approval ids, and pending question ids are all produced from the single history fetch that the poller already performs

### Requirement: Show run state in the session rail
Each row in the chat session rail SHALL carry its run bucket as a machine-readable attribute (`data-bucket`) and render a status badge whose appearance distinguishes the four buckets. Rows SHALL also expose pending work as `data-pending-approvals` and `data-pending-questions` counts. The rail SHALL NOT require a page reload to reflect a bucket change; the existing refresh event SHALL re-render the rail.

#### Scenario: Rail row reflects needs_input
- **WHEN** a conversation in the rail has a pending approval
- **THEN** its row has `data-bucket="needs_input"`, renders the needs-input badge, and `data-pending-approvals` is greater than zero

#### Scenario: Rail updates without reload
- **WHEN** a conversation transitions from `running` to `needs_input`
- **THEN** the refresh event re-renders the rail and the row shows the needs-input badge without a full page reload

### Requirement: Surface live turn status while streaming
While a turn is streaming, the chat header SHALL display the active run's status resolved from the conversation's newest run lifecycle item, and SHALL distinguish `working` from `input-required`. When the active run enters `input-required`, the UI SHALL present a waiting-on-you indication rather than a generic spinner.

#### Scenario: Live status reflects input-required
- **WHEN** the active run's `run_status` becomes `input-required`
- **THEN** the chat header shows a waiting-on-you indication and the rail row bucket is `needs_input`

#### Scenario: Status clears when the run ends
- **WHEN** the active run's newest lifecycle item changes to `run_end` with status `completed`
- **THEN** the header no longer shows an in-progress status and the rail row bucket is `done`

### Requirement: Interrupt cancels the server-side run
The stop action SHALL continue to abort the local stream and SHALL additionally request cancellation of the server-side run. The gateway SHALL expose a cancel route that proxies the existing upstream agent-run cancel endpoint using the active run's `run_id`. If the active run id cannot be resolved, the gateway SHALL still abort the local stream and SHALL report that the run could not be cancelled rather than failing silently or claiming success.

#### Scenario: Stop cancels the server run
- **WHEN** the user presses stop while a turn is streaming and an active run id is known
- **THEN** the gateway issues an upstream cancel for that run id, the local stream is aborted, and the UI reflects a `cancelling` or `cancelled` state once the history reports it

#### Scenario: No active run id
- **WHEN** the user presses stop but no active run id can be resolved
- **THEN** the stream is aborted, the user is told the run could not be cancelled, and no upstream request is issued

#### Scenario: Cancel is idempotent
- **WHEN** cancel is requested for a run that is already `completed` or `cancelled`
- **THEN** the gateway returns a non-error response and the UI state is unchanged

### Requirement: Dock pending approvals and questions above the composer
Pending tool approvals and pending `ask_user` questions for the active conversation SHALL be rendered in a dock anchored above the composer instead of inline in the transcript. The dock SHALL show a count when more than one item is pending, SHALL render each item with its existing approve/reject/answer controls, and SHALL refresh when the refresh event fires. Submitting a decision SHALL use the existing approval and question response routes; approval and question semantics SHALL be unchanged.

#### Scenario: Two pending approvals render in the dock
- **WHEN** the active conversation has two pending approvals
- **THEN** both appear in the dock above the composer, the dock shows a count of two, and each card has its own approve and reject controls

#### Scenario: Decision uses the existing route
- **WHEN** the user approves an item in the dock
- **THEN** the request goes to the existing approval response route and the item leaves the dock once the decision is recorded

#### Scenario: Answered question leaves the dock
- **WHEN** a pending question is answered
- **THEN** the dock refreshes and no longer renders that question

#### Scenario: Empty dock is not rendered
- **WHEN** a conversation has no pending approvals and no pending questions
- **THEN** no dock is rendered and the composer occupies its normal position

### Requirement: Render a session todo card
The gateway SHALL resolve the active conversation's ACP session id and fetch that session's todos, and the transcript SHALL render a collapsible todo card showing each item's content and status. The card SHALL be collapsed by default, SHALL render nothing when the session has no todos, and SHALL refresh when the refresh event fires.

#### Scenario: Todos render with status
- **WHEN** the active conversation's ACP session has three todos with statuses `pending`, `in_progress`, and `completed`
- **THEN** the transcript renders one collapsed todo card listing all three with their statuses distinguished

#### Scenario: No session todos
- **WHEN** the conversation has no ACP session id, or its session has no todos
- **THEN** no todo card is rendered and no error is surfaced to the user

#### Scenario: Todo card refreshes on change
- **WHEN** a session todo is added or its status changes
- **THEN** the refresh event re-renders the todo card with the new state

### Requirement: Queue messages while a turn is running
The composer SHALL maintain a client-side queue that holds follow-up messages while a turn is running. Queued rows SHALL be editable and SHALL offer a "send next" action that promotes the row to the head of the queue. Queued messages SHALL NOT be sent to the server until released, SHALL be released automatically in order when the running turn ends, and SHALL survive a rail-driven conversation switch only for the conversation they were queued against. `Enter` SHALL follow the current turn state (send when idle, queue when running) and `Cmd/Ctrl+Enter` SHALL always queue.

#### Scenario: Queue a follow-up while running
- **WHEN** the user submits a message while a turn is streaming
- **THEN** the message appears as a queued row in the composer and no request is sent for it at that time

#### Scenario: Auto-release on turn end
- **WHEN** the running turn ends with two queued messages
- **THEN** the queued messages are sent in order and their rows are cleared

#### Scenario: Send next releases ahead of the queue
- **WHEN** the user activates "send next" on a queued row while a turn is still running
- **THEN** that message is promoted ahead of the rest of the queue and released first when the running turn ends

#### Scenario: Send next sends immediately when idle
- **WHEN** the user activates "send next" on a queued row while no turn is running
- **THEN** that message is sent immediately

#### Scenario: Cmd/Ctrl+Enter always queues
- **WHEN** the user presses `Cmd/Ctrl+Enter` while no turn is running
- **THEN** the message is queued rather than sent

#### Scenario: Queue is scoped to its conversation
- **WHEN** the user queues a message and then switches to another conversation in the rail
- **THEN** the queued message is not sent in the other conversation and is still present when the original conversation is reopened

### Requirement: Turn footer with duration and copy
Each completed assistant turn SHALL render a footer showing the turn's model, its wall-clock duration derived from the run's `completed_at − created_at` (or the run's `duration_ms` when present), and an end timestamp revealed on hover. The footer SHALL offer a copy action that copies that turn's assistant text. Duration SHALL NOT be shown for a turn whose run has not ended.

#### Scenario: Completed turn shows duration
- **WHEN** a run ended 92 seconds after it started
- **THEN** its turn footer shows a duration of one minute thirty-two seconds and hovering reveals the end time

#### Scenario: Running turn shows no duration
- **WHEN** a turn's run has not ended
- **THEN** no duration is shown for that turn

#### Scenario: Copy turn
- **WHEN** the user activates copy on a turn footer
- **THEN** that turn's assistant text is placed on the clipboard

### Requirement: Copy assistant messages and code blocks
Assistant messages SHALL offer a copy action, and each fenced code block SHALL offer a copy action that copies only that block's source text without surrounding markup.

#### Scenario: Copy a code block
- **WHEN** the user activates copy on a fenced code block
- **THEN** the clipboard contains only that block's source, unrendered and without syntax-highlight markup

#### Scenario: Copy affordance does not disturb layout
- **WHEN** a message renders with copy affordances available
- **THEN** the message layout and reading flow are unchanged from before the affordances were added

### Requirement: Typed run markers in the transcript
The transcript SHALL render `run_start` and `run_end` timeline items as turn boundaries rather than generic content. A `run_start` marker SHALL carry the run's model. A `run_end` marker SHALL render status-distinct states so that `failed` and `input-required` are visually and textually distinguishable from `completed`, and a `failed` run SHALL surface its `error_message`.

#### Scenario: Failed run renders its error
- **WHEN** a `run_end` item has `run_status` `failed` and a non-empty `error_message`
- **THEN** the transcript renders a failed turn boundary that includes that error message

#### Scenario: Paused run renders as waiting
- **WHEN** a `run_end` item has `run_status` `input-required`
- **THEN** the transcript renders that turn boundary as waiting on the user, not as completed or failed

#### Scenario: run_start carries the model
- **WHEN** a `run_start` item has a non-empty `run_model`
- **THEN** the turn boundary renders that model name
