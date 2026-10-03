## ADDED Requirements

### Requirement: Feedback is submitted idempotently per message

A user SHALL be able to submit answer feedback (thumbs up or thumbs down, with an optional free-text comment) against a chat message/response. Thumbs SHALL be encoded as a smallint with value `1` (up) or `-1` (down), enforced by a `CHECK (thumbs IN (-1, 1))` constraint. Submission SHALL be idempotent per (message, user): re-submitting by the same user on the same message SHALL overwrite the prior thumbs value rather than create a duplicate row, and clearing a vote SHALL remove the user's row.

#### Scenario: First submission creates a row

- **WHEN** a user submits a thumbs-up on a chat message for the first time
- **THEN** a feedback row is created for that message and user with `thumbs = 1`

#### Scenario: Re-submission updates in place

- **WHEN** the same user re-submits a thumbs-down on the same message after a prior thumbs-up
- **THEN** the existing row's thumbs value SHALL be updated to `-1` and no duplicate row is created

#### Scenario: Clearing removes the vote

- **WHEN** a user clears their feedback on a message
- **THEN** the user's feedback row for that message SHALL be removed

#### Scenario: Different users are independent

- **WHEN** two different users submit feedback on the same message
- **THEN** each user's feedback SHALL be stored independently

#### Scenario: Thumbs values are constrained

- **WHEN** a feedback row is written
- **THEN** `thumbs` SHALL be exactly `1` or `-1`; any other value is rejected by the `CHECK (thumbs IN (-1, 1))` constraint

### Requirement: Feedback is scoped to project and user

Feedback SHALL always carry a project id and the submitting user id. The submitting user id SHALL reference `core.user_profiles(id)`. A caller SHALL only be able to read or aggregate feedback for a project they can access, and SHALL only be able to submit feedback as themselves.

#### Scenario: Project scoping enforced on read

- **WHEN** a caller lists or aggregates feedback for a project
- **THEN** only feedback rows belonging to that project SHALL be returned

#### Scenario: Submission is as the authenticated user

- **WHEN** a user submits feedback
- **THEN** the submitting user id SHALL be derived from the authenticated context, not from a request field

### Requirement: Feedback is linked to the retrieval trace end-to-end

A feedback row SHALL reference the retrieval trace that produced the answer being rated, via a nullable `retrieval_trace_id`. The linkage SHALL be threaded end-to-end: the chat response records the search's trace id on `kb.chat_messages.retrieval_trace_id` at answer time (from the search response `TraceID`), and the feedback row copies that value at submit time.

#### Scenario: Chat message records the search trace

- **WHEN** a chat response is produced from a unified search
- **THEN** `kb.chat_messages.retrieval_trace_id` SHALL be set to the search response's `TraceID`

#### Scenario: Feedback copies the message's trace

- **WHEN** feedback is submitted against a chat message that has a `retrieval_trace_id`
- **THEN** the feedback row's `retrieval_trace_id` SHALL equal that message's trace id

#### Scenario: No trace yields null

- **WHEN** feedback is submitted against a chat message with no retrieval trace
- **THEN** the feedback row's `retrieval_trace_id` SHALL be null and submission still succeeds

### Requirement: Feedback is queryable by model and by search-fusion config

Feedback SHALL be queryable and aggregable by model and by search-fusion configuration. Model SHALL be resolved via `run_id → kb.llm_usage_events.run_id`; because a run MAY emit multiple usage events, model aggregation SHALL be counts/sums grouped by model, not single-model attribution. Search-fusion config SHALL be resolved via `kb.retrieval_traces.filters` (resultTypes/fusionStrategy/weights), joined through `retrieval_trace_id`. Per-embedding-model aggregation SHALL NOT be available and SHALL be out of scope.

#### Scenario: Aggregation by model

- **WHEN** a caller requests feedback aggregates grouped by model
- **THEN** thumbs-up and thumbs-down counts SHALL be returned per model, resolved through `run_id → kb.llm_usage_events.run_id`, scoped to the requested project and time range

#### Scenario: Aggregation by search-fusion config

- **WHEN** a caller requests feedback aggregates grouped by search-fusion config
- **THEN** counts SHALL be returned per fusion config, resolved through `retrieval_trace_id → kb.retrieval_traces.filters`

#### Scenario: Missing provenance reports null, not an error

- **WHEN** a feedback row has no resolvable run or trace
- **THEN** the affected aggregation dimension SHALL report `NULL` and aggregation SHALL still succeed

### Requirement: Feedback aggregation counts are exposed

The API SHALL expose aggregate feedback counts (thumbs up, thumbs down, optional comment count) for a project over a time range, and SHALL support breakdowns by model and by search-fusion config.

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
