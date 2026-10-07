## ADDED Requirements

### Requirement: Emergency kill switch SHALL gate every agent execution entry point

When `AGENT_EXECUTION_ENABLED` is false (`AgentSafeguardsConfig.ExecutionEnabled`
false), the system SHALL refuse to execute an agent run at every execution entry
point — `Execute`, `ExecuteWithRun`, and `Resume` — and SHALL return a stable
`ErrExecutionDisabled` sentinel whose message is
`agent execution is disabled system-wide`. The executor SHALL refuse **before** it
creates or mutates any agent run row. There SHALL be no `TrustedInternal`
exemption: trusted and external callers are blocked alike.

#### Scenario: Execute is blocked when disabled
- **WHEN** the kill switch is off
- **AND** `Execute` is called
- **THEN** it SHALL return an error satisfying `errors.Is(err, ErrExecutionDisabled)`
- **AND** it SHALL NOT create or update an agent run row

#### Scenario: ExecuteWithRun is blocked before mutating the run
- **WHEN** the kill switch is off
- **AND** `ExecuteWithRun` is called with a pre-created run
- **THEN** it SHALL return `ErrExecutionDisabled`
- **AND** it SHALL NOT persist the trust marker or otherwise mutate the run row

#### Scenario: Resume is blocked when disabled
- **WHEN** the kill switch is off
- **AND** `Resume` is called for a paused run
- **THEN** it SHALL return `ErrExecutionDisabled`
- **AND** it SHALL NOT create or update any run row

#### Scenario: Trusted internal callers are not exempt
- **WHEN** the kill switch is off
- **AND** a request carrying `TrustedInternal: true` reaches any entry point
- **THEN** execution SHALL still be refused with `ErrExecutionDisabled`

#### Scenario: Kill-switch block is logged
- **WHEN** execution is refused by the kill switch
- **THEN** an error-level log entry SHALL be emitted with `project_id` and `agent_id`

### Requirement: Callers SHALL NOT leave a pre-created run stuck `running` when execution is disabled

Callers that create an agent run row before invoking the executor (the HTTP
trigger endpoint, the A2A send/resume paths) SHALL refuse **before** creating the
run when execution is disabled. Where a run row already exists because it was
pre-created (a resume run), the caller SHALL mark it failed with the disabled
reason so it reaches a terminal state instead of remaining `running` until the
stale-run reaper reports a misleading idle-timeout.

#### Scenario: HTTP trigger is refused before run creation
- **WHEN** the kill switch is off
- **AND** the HTTP trigger endpoint is invoked
- **THEN** it SHALL return an explicit error and SHALL NOT create a run row
- **AND** it SHALL NOT report a false `success: true`

#### Scenario: A2A send is refused before run creation on both surfaces
- **WHEN** the kill switch is off
- **AND** an A2A send is invoked via either the JSON `/message:send` or the SSE `/message:stream` surface
- **THEN** it SHALL return a disabled A2A error and SHALL NOT create a run row

#### Scenario: A refused A2A stream run is failed terminal
- **WHEN** a run reaches `ExecuteWithRun`/`Resume` on the SSE `/message:stream` surface while the kill switch is off (including a switch flipped after the pre-check)
- **THEN** the surface SHALL mark the run failed with the disabled reason
- **AND** SHALL emit a `TASK_STATE_FAILED` status update
- **AND** the run SHALL NOT remain `running`

#### Scenario: A pre-created resume run is marked failed
- **WHEN** a pre-created resume run exists
- **AND** `Resume` returns `ErrExecutionDisabled`
- **THEN** the caller SHALL mark that pre-created run failed with the disabled reason
- **AND** it SHALL reach a terminal state rather than remaining `running`

### Requirement: Question-consuming responders SHALL check executability before consuming the question

Responders that claim a pending question before resuming the run (the MCP
respond-to-agent-question tool and the A2A resume path) SHALL check the kill
switch **before** claiming or answering the question. This prevents a disabled
system from leaving a run paused with its question already consumed, which would
make the run permanently unresumable once the switch is re-enabled. The
in-executor guard SHALL remain as defence in depth.

#### Scenario: MCP respond tool does not consume the question when disabled
- **WHEN** the kill switch is off
- **AND** respond-to-agent-question is invoked for a pending question
- **THEN** it SHALL return the disabled error
- **AND** the question SHALL remain pending (`pending`)

#### Scenario: A2A resume does not consume the question when disabled on both surfaces
- **WHEN** the kill switch is off
- **AND** an A2A resume is requested via either the JSON `/message:send` or the SSE `/message:stream` surface for a paused task with a pending question
- **THEN** it SHALL return a disabled error
- **AND** the question SHALL remain pending (no claim UPDATE is issued)
- **AND** the paused run SHALL remain resumable once the switch is re-enabled

### Requirement: query_knowledge SHALL surface the kill switch without double-wrapping

When the agent query endpoint returns an SSE `error` event whose message is the
canonical `ErrExecutionDisabled` message, the `query_knowledge` MCP tool SHALL
return the error `query_knowledge: agent execution is disabled system-wide` and
SHALL NOT wrap it as `query_knowledge: agent error: …`. The canonical message SHALL
be sourced from a single shared definition, not duplicated as a literal.

#### Scenario: Kill-switch error event is not double-wrapped
- **WHEN** the stream emits `{"type":"error","error":"agent execution is disabled system-wide"}`
- **THEN** `query_knowledge` SHALL return exactly
  `query_knowledge: agent execution is disabled system-wide`

#### Scenario: Other agent errors keep the generic wrapper
- **WHEN** the stream emits an unrelated error message
- **THEN** `query_knowledge` SHALL return `query_knowledge: agent error: <message>`

### Requirement: Internal same-server HTTP base URL SHALL be configurable

The `mcp` domain's internal same-server HTTP calls (query, remember, forget) SHALL
use a base URL taken from `INTERNAL_BASE_URL` when set, and SHALL fall back to
`http://localhost:<SERVER_PORT>` when it is empty. Trailing slashes SHALL be
normalised so the resulting URL never contains a doubled slash before `api/`.

#### Scenario: Configured base URL is used and normalised
- **WHEN** `INTERNAL_BASE_URL` is set (with or without a trailing slash)
- **THEN** internal calls SHALL be built from that value with trailing slashes trimmed

#### Scenario: Empty base URL falls back to loopback
- **WHEN** `INTERNAL_BASE_URL` is unset or empty
- **THEN** internal calls SHALL be built from `http://localhost:<SERVER_PORT>`
