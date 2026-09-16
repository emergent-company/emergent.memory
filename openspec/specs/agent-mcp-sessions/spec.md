# agent-mcp-sessions Specification

## Purpose
Lets an external MCP client opt into a persistent conversation with an agent: start a session, then continue it across calls, while the default one-shot behavior and every session id remain scoped to the authorizing key.

## Requirements

### Requirement: Start a session

The system SHALL expose a `start_session` tool that accepts an optional `message` string. When `message` is present the tool MUST run the first turn of a new session and return the reply. When `message` is absent the tool MUST create an empty session and return its session id without running a turn. The result SHALL be a uniform envelope containing at least `session_id` and, when a turn ran, `reply`, `run_id`, and `status`.

#### Scenario: Start with a message runs turn one

- **WHEN** an authorized client calls `start_session` with a `message`
- **THEN** a new session is created, the agent runs one turn, and the envelope contains `session_id`, the `reply`, `run_id`, and `status`

#### Scenario: Start without a message creates an empty session

- **WHEN** an authorized client calls `start_session` without a `message`
- **THEN** a session is created and the envelope contains a `session_id` and no reply

#### Scenario: All session ids are new

- **WHEN** an authorized client calls `start_session` more than once
- **THEN** each call returns a distinct session id

### Requirement: Continue a session

The system SHALL expose a `continue_session` tool that requires both `session_id` and `message`. The turn MUST run with the session's prior conversation context, and the result SHALL be a uniform envelope containing the new `reply`, the `run_id`, and the session `status`.

#### Scenario: Continue shares prior context

- **WHEN** an authorized client continues a session whose earlier turns established context
- **THEN** the run uses that prior context and the envelope contains the agent's new reply

#### Scenario: Missing arguments are rejected

- **WHEN** `continue_session` is called without a `session_id` or without a `message`
- **THEN** the system returns an invalid-params error and starts no run

#### Scenario: Unknown session id is rejected

- **WHEN** `continue_session` is called with a session id that does not exist
- **THEN** the system returns a not-found error and starts no run

### Requirement: Read and list sessions

The system SHALL expose `get_session` (required `session_id`) returning the session's `status`, `created_at`, `last_active_at`, and `turn_count`, and `list_sessions` (no parameters) returning each session owned by the authorizing key with the same fields. Both results SHALL be uniform envelopes.

#### Scenario: Get a session returns its metadata

- **WHEN** an authorized client calls `get_session` for one of its sessions
- **THEN** the envelope contains that session's status and timestamps and turn count

#### Scenario: List returns only the authorizing key's sessions

- **WHEN** an authorized client calls `list_sessions`
- **THEN** the envelope contains only the sessions created by that key, and no other key's sessions

#### Scenario: Get an unknown session is rejected

- **WHEN** an authorized client calls `get_session` with an id it does not own
- **THEN** the system returns a not-found/forbidden error and no session details

### Requirement: Session ids are scoped to a key and are not capabilities

Session ids SHALL be scoped to the key that created them. Every session tool MUST re-authorize the presented key and MUST verify that the session row belongs to that key; a session id presented by any other key MUST NOT resolve. Revoking one key MUST NOT invalidate or reveal another key's sessions.

#### Scenario: Another key's session id does not resolve

- **WHEN** a client uses key B to continue, get, or list a session created by key A
- **THEN** the system returns not-found/forbidden and exposes no session data

#### Scenario: Revoking a key does not strand another key's sessions

- **WHEN** key A is revoked and key B has its own sessions
- **THEN** key B's sessions remain continuable and readable

#### Scenario: Session ids are not bearer tokens

- **WHEN** a request presents a valid but differently scoped key alongside a session id
- **THEN** the session id alone grants no access

### Requirement: A session survives its own key's rotation

Rotating a key's token SHALL preserve that key's sessions so they remain continuable, because rotation keeps the key identity and changes only the credential.

#### Scenario: Continue after rotation

- **WHEN** a key is rotated and its session is then continued with the new token
- **THEN** the session's prior context is available and the turn proceeds

### Requirement: A second concurrent continue is rejected as busy

The system SHALL serialize turns within one session. If a `continue_session` (or `start_session` turn) is already running for a session, a second concurrent turn on that session MUST be rejected rather than allowed to interleave, and MUST NOT mutate the session.

#### Scenario: Concurrent continue is rejected

- **WHEN** two `continue_session` calls for one session overlap
- **THEN** exactly one turn runs and the other is rejected with a busy error

#### Scenario: Rejected turn does not corrupt history

- **WHEN** a concurrent turn is rejected as busy
- **THEN** the session's stored history and counters reflect only the turn that ran

### Requirement: Cumulative session work is bounded

A session SHALL accumulate a total step count across its turns, and the system MUST enforce a maximum total step count and a maximum turn count. When the cumulative step cap is exceeded the session tools MUST return an envelope with `ok:false` and a `budget_exceeded` kind rather than running unbounded work. The per-call step and time budget MUST remain enforced per turn, independently of the cumulative cap.

#### Scenario: Cumulative step cap is enforced

- **WHEN** a session's accumulated steps would exceed the maximum total step count
- **THEN** the turn is stopped and the envelope reports `budget_exceeded`

#### Scenario: Turn cap is enforced

- **WHEN** a session has reached its maximum turn count
- **THEN** a further continue is rejected without running

#### Scenario: Per-call budget still applies

- **WHEN** a single turn would exceed the per-call step or time budget
- **THEN** that turn stops and reports a budget/timeout error as before

### Requirement: Sessions expire

A session SHALL carry an expiry and MUST NOT be continuable after it expires. The system SHALL mark or remove expired sessions so that `list_sessions` no longer reports them as active and `get_session` reports a non-active status.

#### Scenario: Expired session is not continuable

- **WHEN** a client continues a session whose expiry has passed
- **THEN** the system rejects the request as not-active and starts no run

#### Scenario: Expired sessions are reaped

- **WHEN** the reaper runs and finds idle sessions past expiry
- **THEN** those sessions are marked expired and no longer reported as active

### Requirement: A canceled turn is reported as interrupted

A turn that is canceled mid-run SHALL leave the session in an `interrupted` status so that `get_session` and `list_sessions` reflect it, and a later continue MUST remain possible. The system MUST NOT report a canceled turn as a completed success.

#### Scenario: Canceled turn is visible

- **WHEN** a turn is canceled mid-run and the client then calls `get_session`
- **THEN** the session status is `interrupted`

#### Scenario: An interrupted session can be resumed by continuing

- **WHEN** a client continues an interrupted session
- **THEN** a new turn runs and the session returns to an active status

### Requirement: Session tool results use the uniform envelope

Every session tool (`start_session`, `continue_session`, `get_session`, `list_sessions`) SHALL return the uniform result envelope `{ ok, error, data, meta }`. A successful call MUST return `ok:true` with the session payload in `data`, and a failed call MUST return `ok:false` with a non-empty `error` and a `meta.kind` mapped from the run error (`agent_unavailable`, `run_failed`, `input_required`, `budget_exceeded`). `meta` SHOULD carry `steps` and `run_id` when a turn ran. The one-shot `call_agent` tool MUST NOT be re-enveloped.

#### Scenario: Success is an ok envelope

- **WHEN** a session tool completes successfully
- **THEN** the result is a single JSON object with `ok:true` and the session payload inside `data`

#### Scenario: Failure carries a kind

- **WHEN** a turn fails because the agent is unavailable, the run failed, human input is required, or the budget was exceeded
- **THEN** the result has `ok:false`, a non-empty `error`, and a `meta.kind` naming the failure

#### Scenario: call_agent stays bare text

- **WHEN** a client calls `call_agent`
- **THEN** the result is the unchanged bare text reply, not an envelope
