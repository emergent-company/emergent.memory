## ADDED Requirements

### Requirement: Feedback is submitted idempotently per message

A user SHALL be able to submit answer feedback (thumbs up or thumbs down, with an optional free-text comment) against a chat message/response. Submission SHALL be idempotent per (message, user): re-submitting by the same user on the same message SHALL overwrite the prior thumbs value rather than create a duplicate row, and clearing a vote SHALL remove the user's row.

#### Scenario: First submission creates a row

- **WHEN** a user submits a thumbs-up on a chat message for the first time
- **THEN** a feedback row is created for that message and user

#### Scenario: Re-submission updates in place

- **WHEN** the same user re-submits a thumbs-down on the same message after a prior thumbs-up
- **THEN** the existing row's thumbs value SHALL be updated to thumbs-down and no duplicate row is created

#### Scenario: Clearing removes the vote

- **WHEN** a user clears their feedback on a message
- **THEN** the user's feedback row for that message SHALL be removed

#### Scenario: Different users are independent

- **WHEN** two different users submit feedback on the same message
- **THEN** each user's feedback SHALL be stored independently

### Requirement: Feedback is scoped to project and user

Feedback SHALL always carry a project id and the submitting user id. A caller SHALL only be able to read or aggregate feedback for a project they can access, and SHALL only be able to submit feedback as themselves.

#### Scenario: Project scoping enforced on read

- **WHEN** a caller lists or aggregates feedback for a project
- **THEN** only feedback rows belonging to that project SHALL be returned

#### Scenario: Submission is as the authenticated user

- **WHEN** a user submits feedback
- **THEN** the submitting user id SHALL be derived from the authenticated context, not from a request field

### Requirement: Feedback is correlated with the retrieval trace and queryable by model/config

A feedback row SHALL reference the retrieval/query trace (and thereby the query) that produced the answer being rated. Feedback SHALL be queryable and aggregable by model and by embedding/model configuration, so a thumbs-down can be correlated to the model and config that produced the answer.

#### Scenario: Feedback references the trace

- **WHEN** feedback is submitted against a chat message that was produced from a persisted retrieval trace
- **THEN** the feedback row records the trace id and the query it represents

#### Scenario: Aggregation by model and config

- **WHEN** a caller requests feedback aggregates grouped by model and/or embedding/model config
- **THEN** thumbs-up and thumbs-down counts SHALL be returned per model and per config, scoped to the requested project and time range

### Requirement: Feedback aggregation counts are exposed

The API SHALL expose aggregate feedback counts (thumbs up, thumbs down, optional comment count) for a project over a time range, and SHALL support breakdowns by model and by config.

#### Scenario: Aggregate counts over a range

- **WHEN** a caller requests aggregate feedback for a project and time range
- **THEN** the response SHALL include total thumbs-up and thumbs-down counts within that range

#### Scenario: Empty range yields zero counts

- **WHEN** a project has no feedback in the requested range
- **THEN** the response SHALL return zero counts, not an error

### Requirement: Feedback is stored without a new rework subsystem

Answer feedback SHALL reuse the existing chat message identity (`kb.chat_messages`) and the existing retrieval trace store, and SHALL NOT introduce a new subsystem that overlaps with `kb.work_item_feedback` (append-only rework rounds for object-driven work).

#### Scenario: No work-item feedback reuse

- **WHEN** answer feedback is stored
- **THEN** it is written to `kb.answer_feedback`, keyed to a chat message and trace, and does not write to `kb.work_item_feedback`
