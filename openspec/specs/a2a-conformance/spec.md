# a2a-conformance Specification

## Purpose

Define protocol conformance for the A2A v1.0 surface: `A2A-Version` negotiation, the `google.rpc.Status` error envelope with `a2a-protocol.org` reasons, explicit signalling for unimplemented operations, and shape assertions that keep internal vocabulary off the wire. Also records the official TCK integration as an explicitly deferred conformance goal rather than a shipping guarantee.

## Requirements

### Requirement: Protocol version negotiation
The system SHALL negotiate the A2A protocol version from the `A2A-Version` header or query parameter and SHALL support version `1.0`. Requests specifying an unsupported version SHALL be rejected with a `VersionNotSupported` error.

#### Scenario: Version 1.0 is accepted
- **WHEN** a client sends a request with `A2A-Version: 1.0`
- **THEN** the server processes the request normally

#### Scenario: Absent version defaults to 1.0
- **WHEN** a client sends an A2A request with no `A2A-Version` header or query parameter
- **THEN** the server treats the request as version `1.0`

#### Scenario: Unsupported version is rejected
- **WHEN** a client sends a request with `A2A-Version: 9.9`
- **THEN** the server responds with HTTP 400 and a reason of `VERSION_NOT_SUPPORTED`

### Requirement: A2A error envelope
All A2A error responses SHALL use the `google.rpc.Status` JSON shape `{"error": {"code", "status", "message", "details"}}` where each detail has an `@type`, a `reason` in UPPER_SNAKE_CASE, and `domain` equal to `a2a-protocol.org`.

#### Scenario: Not-found produces TASK_NOT_FOUND
- **WHEN** a client requests an unknown task
- **THEN** the error body contains `details[0].reason` = `TASK_NOT_FOUND` and `details[0].domain` = `a2a-protocol.org`

#### Scenario: Not-cancelable produces TASK_NOT_CANCELABLE
- **WHEN** a client cancels a terminal task
- **THEN** the error body contains `details[0].reason` = `TASK_NOT_CANCELABLE`

#### Scenario: HTTP code matches the A2A mapping
- **WHEN** a not-found, not-cancelable, or version-not-supported error occurs
- **THEN** the HTTP status is 404, 400, and 400 respectively

### Requirement: Unimplemented operations are declared, not silently ignored
Operations that are not implemented in this milestone — push-notification configs and subscription variants — SHALL return an `UnsupportedOperation` error rather than a generic 404 or a success response.

#### Scenario: Push notification config is unsupported
- **WHEN** a client calls a `pushNotificationConfigs` endpoint while `capabilities.pushNotifications` is false
- **THEN** the server responds with HTTP 400 and a reason of `PUSH_NOTIFICATION_NOT_SUPPORTED`

#### Scenario: Advertised capabilities match implementation
- **WHEN** the extended AgentCard advertises `capabilities.streaming` and `capabilities.extendedAgentCard`
- **THEN** both the streaming endpoint and the extended-card endpoint are implemented and functional

### Requirement: Conformance shape tests
The system SHALL ship tests asserting the exact A2A v1.0 JSON shapes so internal vocabulary cannot leak onto the wire. This change satisfies the requirement via inline assertions (DTO camelCase keys, `TaskState`/`Role` enum spelling, oneof serialization, AgentCard required fields, tenant-leak invariant). Checked-in golden fixture files are a deferred enhancement.

#### Scenario: Conformance test rejects internal status strings
- **WHEN** the conformance test suite renders task JSON for every internal status
- **THEN** no output contains `cancelling`, `skipped`, `resume_run_id`, or any internal status string

#### Scenario: Conformance test enforces camelCase keys
- **WHEN** DTOs are serialized
- **THEN** keys are camelCase (`taskId`, `contextId`, `artifactId`, `supportedInterfaces`, `defaultInputModes`) and never snake_case

#### Scenario: Conformance test validates the AgentCard
- **WHEN** the global and extended AgentCards are rendered
- **THEN** both contain all required A2A fields (`name`, `description`, `version`, `capabilities`, `supportedInterfaces`, `defaultInputModes`, `defaultOutputModes`, `skills`)

### Requirement: Official TCK conformance (deferred)
Running the official A2A Technology Compatibility Kit in CI is an explicitly deferred future conformance goal for this change, because the TCK currently targets the 0.3 wire format rather than v1.0. This change SHALL NOT claim TCK CI coverage, and SHALL record the 0.3-vs-1.0 coverage gap in the change documentation.

#### Scenario: TCK CI coverage is not claimed
- **WHEN** a maintainer reads the conformance documentation for this change
- **THEN** no claim is made that the official TCK runs in CI as part of this change

#### Scenario: TCK gap is documented
- **WHEN** a maintainer reads the change documentation
- **THEN** the 0.3-wire TCK limitation is described as a deferred future goal alongside the current v1.0 shape-assertion coverage

### Requirement: A2A error reasons distinguish client errors from server faults
The A2A error-envelope mapping SHALL preserve the distinction between client mistakes and genuine server faults. A 4xx status SHALL map to its own reason — 400 to `INVALID_ARGUMENT`, 401 to `UNAUTHENTICATED`, 403 to `PERMISSION_DENIED`, 404 to `NOT_FOUND`, 405 to `METHOD_NOT_ALLOWED`, and any other 4xx to `INVALID_ARGUMENT` — and SHALL NOT be reported as `INVALID_AGENT_RESPONSE`. `INVALID_AGENT_RESPONSE` SHALL be reserved for 5xx failures. Streaming routes (`POST /message:stream`, `POST /tasks/{id}:subscribe`) SHALL surface handler errors in the A2A envelope rather than the platform's generic error shape, unless the response body has already been committed.

#### Scenario: 400 is not reported as a server fault
- **WHEN** a request fails with HTTP 400
- **THEN** the A2A envelope reason is `INVALID_ARGUMENT` and the code maps to HTTP 400

#### Scenario: 5xx stays INVALID_AGENT_RESPONSE
- **WHEN** a request fails with a 5xx status
- **THEN** the A2A envelope reason is `INVALID_AGENT_RESPONSE` and the code maps to HTTP 500

#### Scenario: Streaming route emits the A2A envelope
- **WHEN** an authenticated client calls `POST /message:stream` with no project selector
- **THEN** the response is HTTP 400 with an A2A envelope whose `details[0].reason` is `PROJECT_REQUIRED`, not the platform's generic 500 error shape

### Requirement: Client SSE event framing
The A2A SDK client SHALL parse `text/event-stream` bodies using SSE field framing rather than an exact `data: ` prefix match. A `data` field value SHALL have at most one leading space stripped; a leading tab is payload and SHALL NOT be stripped or treated as a separator. Consecutive `data` fields belonging to the same event SHALL be concatenated with `\n`, and the event SHALL be dispatched only when a blank line terminates it. Lines SHALL be terminated by CR, LF, or CRLF. Non-`data` fields (`event`, `id`, `retry`) and comment (`:`) lines SHALL be ignored without stalling the stream. A payload of `[DONE]` SHALL terminate the stream, and an event not terminated by a blank line SHALL be discarded.

#### Scenario: At most one leading space is stripped
- **WHEN** the stream contains `data:{...}`, `data: {...}`, or `data:  {...}`
- **THEN** the client strips at most one leading space, so `data:{...}` and `data: {...}` decode to the same value while `data:  {...}` keeps one leading space in the payload

#### Scenario: A leading tab is payload, not a separator
- **WHEN** the stream contains `data:\t{...}` or `data: \t{...}`
- **THEN** the client dispatches the event with the leading tab preserved in the field value (only one leading space, if present, is stripped)

#### Scenario: CR, LF, and CRLF framing are all accepted
- **WHEN** the stream terminates event lines with LF, CRLF, or a lone CR
- **THEN** the client dispatches the same decoded event for each framing, including when a CRLF or lone-CR boundary straddles a scanner buffer refill

#### Scenario: Multi-line data is concatenated
- **WHEN** one event's payload is split across consecutive `data:` lines and terminated by a blank line
- **THEN** the client joins the payload lines with `\n` and decodes the resulting event

#### Scenario: Non-data lines are ignored
- **WHEN** an event is preceded by `event:`, `id:`, `retry:`, or comment (`:`) lines
- **THEN** the client ignores them and still dispatches the subsequent `data:` event

#### Scenario: Event dispatched only on the terminating blank line
- **WHEN** a stream ends without a blank line terminating the final event
- **THEN** the incomplete event is discarded and the client reports end of stream

#### Scenario: Done sentinel terminates the stream
- **WHEN** a `data: [DONE]` payload is received
- **THEN** the client reports end of stream and does not dispatch any later payload
