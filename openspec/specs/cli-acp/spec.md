# cli-acp Specification

## Purpose
Defines the `memory acp` stdio agent: it bridges the Agent Client Protocol (ACP) to Memory's A2A backend, mapping ACP session lifecycle onto A2A tasks, streaming agent output over SSE, resuming human-in-the-loop `TASK_STATE_INPUT_REQUIRED` turns, and answering malformed inbound lines with well-formed JSON-RPC errors instead of silently dropping them.

## Requirements

### Requirement: Malformed message handling

The CLI SHALL answer any inbound line it cannot decode into a valid JSON-RPC Request with a JSON-RPC error response, rather than silently dropping it. It SHALL return `-32700` Parse error for malformed JSON and `-32600` Invalid Request for structurally valid JSON that is not a valid Request object (bad or missing `jsonrpc`, missing or non-string `method`, or a non-object value). The response SHALL echo the request `id` when one was recoverable from the input and `id: null` otherwise. The CLI SHALL still log the failure to stderr for operator visibility.

#### Scenario: Malformed JSON

- **WHEN** the client sends a line that is not valid JSON
- **THEN** the CLI writes a `-32700` Parse error response with `id: null` and logs the failure to stderr

#### Scenario: Missing method

- **WHEN** the client sends a structurally valid JSON object that carries an `id` but no `method`
- **THEN** the CLI writes a `-32600` Invalid Request response echoing that `id`

#### Scenario: Unsupported version

- **WHEN** the client sends a request whose `jsonrpc` field is not `"2.0"`
- **THEN** the CLI writes a `-32600` Invalid Request response echoing the request `id`

#### Scenario: Non-Request JSON value

- **WHEN** the client sends valid JSON that is not an object (for example a string or an array)
- **THEN** the CLI writes a `-32600` Invalid Request response with `id: null`

#### Scenario: Loop continues after a bad line

- **WHEN** a malformed line is followed by a well-formed request
- **THEN** the CLI answers the bad line with the appropriate error frame and still serves the following request

### Requirement: Bounded session lifecycle

The CLI SHALL bound the number of ACP sessions it tracks so that a long-lived `memory acp` process does not grow without bound, and SHALL release a session's state — including every in-flight turn's cancel function — when the client deletes or closes it.

The CLI SHALL cap tracked sessions at `defaultMaxSessions` (256) and evict the least-recently-used **idle** session when a new session would exceed the cap. The bound SHALL be strict: the CLI SHALL never track more than `defaultMaxSessions` sessions. It SHALL NOT evict a session that has an in-flight turn, and SHALL invoke every in-flight turn's cancel function when it deletes or closes a session (an evicted session is by construction idle and so holds none).

When the cap is reached and no idle session can be evicted (every tracked session has an in-flight turn), the CLI SHALL refuse to add another session — `session/new` and the lazy creation performed by `session/prompt` for an unknown session id SHALL fail with a JSON-RPC error — rather than grow the session map. Losing an oldest idle session to LRU eviction remains preferred over refusing.

The CLI SHALL track one cancel function per in-flight turn for a session, and SHALL clear a turn's cancel function once that turn completes, so an idle session holds no stale cancellation state.

The CLI SHALL answer the `session/delete` and `session/close` methods by removing the session's state and cancelling every in-flight turn, and SHALL advertise `agentCapabilities.sessionCapabilities.delete` and `agentCapabilities.sessionCapabilities.close` on `initialize`. Deleting or closing a session the CLI does not track SHALL succeed silently with an empty result.

#### Scenario: Delete removes the session

- **WHEN** the client sends `session/delete` for an existing session
- **THEN** the CLI removes the session's state, cancels every in-flight turn, and responds with an empty result

#### Scenario: Delete of an unknown session succeeds silently

- **WHEN** the client sends `session/delete` for a session id the CLI does not track
- **THEN** the CLI responds with an empty result and no error

#### Scenario: Delete capability is advertised

- **WHEN** the client sends `initialize`
- **THEN** the response carries `agentCapabilities.sessionCapabilities.delete` and `agentCapabilities.sessionCapabilities.close`

#### Scenario: Session cap is enforced by LRU eviction

- **WHEN** the number of tracked sessions is at the cap, at least one tracked session is idle, and a new session is created
- **THEN** the least-recently-used idle session is evicted and its cancel functions, if any, are invoked

#### Scenario: The cap is strict when every session is in-flight

- **WHEN** the number of tracked sessions is at the cap and every tracked session has an in-flight turn, and a new session is requested
- **THEN** no session is evicted and the request fails with a JSON-RPC error instead of growing the map

#### Scenario: An in-flight session is never pruned

- **WHEN** a new session would exceed the cap while a tracked session has an in-flight turn
- **THEN** that session is never evicted and its cancel functions are never invoked by eviction

#### Scenario: A completed turn leaves no stale cancel function

- **WHEN** a prompt turn completes
- **THEN** that turn's cancel function is removed and the session's in-flight count decreases, without disturbing other in-flight turns

#### Scenario: A dropped session does not admit a late turn

- **WHEN** a prompt has looked up a session but has not yet registered its cancel function, and the session is concurrently evicted, deleted, or closed
- **THEN** the session is marked as dropped before removal and every prompt that has already looked it up aborts without making a backend call

#### Scenario: Dropping a session cancels every in-flight turn

- **WHEN** a session with more than one in-flight turn is deleted or closed
- **THEN** the cancel function of every in-flight turn is invoked

### Requirement: ACP stdio command

The CLI SHALL provide a `memory acp` command that runs the process as an ACP v1 agent over stdio. It SHALL write only ACP JSON-RPC messages to stdout and route diagnostics to stderr.

#### Scenario: Command available

- **WHEN** a user runs `memory acp --help`
- **THEN** the command is listed under the `ai` command group with a `--agent` flag

#### Scenario: Missing agent selection

- **WHEN** `memory acp` runs with neither `--agent <skill-id>` nor the `MEMORY_AGENT` environment variable set
- **THEN** the command exits non-zero with an error explaining how to select an agent

### Requirement: initialize handshake

The CLI SHALL answer the ACP `initialize` method with `protocolVersion: 1`, an honest `agentCapabilities` object (all optional capabilities false), and `agentInfo` naming the agent `memory`.

#### Scenario: Initialize response

- **WHEN** the client sends `initialize`
- **THEN** the response carries `protocolVersion` 1, `agentCapabilities.loadSession` false, and `agentInfo.name` `"memory"`

### Requirement: Session and prompt bridge

The CLI SHALL answer `session/new` with a fresh `sessionId`, and `session/prompt` by running one Memory agent turn via A2A `message:stream` using the selected skill id, and returning `stopReason` `"end_turn"` when the turn completes.

#### Scenario: Prompt forwards to the selected agent

- **WHEN** the client sends `session/prompt` with text content
- **THEN** the CLI sends an A2A `message:stream` request carrying `metadata.skillId` equal to the selected skill id and the prompt text as a single text part

#### Scenario: Empty prompt

- **WHEN** the client sends `session/prompt` with no text content
- **THEN** the CLI returns `stopReason` `"end_turn"` without making a backend call

### Requirement: Streaming output

The CLI SHALL stream the agent's reply as incremental `session/update` `agent_message_chunk` notifications as A2A emits text deltas, deduplicating the repeated full text carried by artifact/message/task events.

#### Scenario: Reply is streamed incrementally

- **WHEN** the Memory agent turn streams text deltas
- **THEN** the CLI emits one `agent_message_chunk` notification per new text suffix, followed by a `session/prompt` response with `stopReason` `"end_turn"`

#### Scenario: Duplicate full text is not re-emitted

- **WHEN** A2A re-emits the full accumulated text (terminal message, last-chunk artifact, or final task snapshot)
- **THEN** the CLI does not emit a duplicate `agent_message_chunk` for already-streamed text

### Requirement: Human-in-the-loop resume

The CLI SHALL surface a paused turn's question when A2A reports `TASK_STATE_INPUT_REQUIRED`, remember the task id, and resume it on the next prompt in the same session by sending the A2A message with that `taskId`.

#### Scenario: Pause surfaces the question

- **WHEN** an A2A turn pauses at `TASK_STATE_INPUT_REQUIRED`
- **THEN** the CLI emits an `agent_message_chunk` with the question text and returns `stopReason` `"end_turn"`, recording the task id

#### Scenario: Next prompt resumes the paused task

- **WHEN** a later `session/prompt` arrives on the same session after a pause
- **THEN** the CLI sends an A2A `message:stream` request carrying `message.taskId` equal to the paused task id (without `metadata.skillId`)

### Requirement: Multi-turn threading

The CLI SHALL thread consecutive prompts within a session by carrying the A2A `contextId` returned by a prior turn on the next turn's message.

#### Scenario: Context carried across turns

- **WHEN** a prompt returns an A2A task with a non-empty `contextId`, and a later prompt arrives on the same session
- **THEN** the later A2A message carries that `contextId`

### Requirement: Cancellation

The CLI SHALL handle the `session/cancel` notification by interrupting every in-flight A2A request for that session and resolving each pending `session/prompt` with `stopReason` `"cancelled"`, and SHALL record a cancel that arrives before a turn registers so that turn does not start un-cancellable.

#### Scenario: Cancel interrupts a running turn

- **WHEN** a `session/cancel` notification arrives while a `session/prompt` for that session is in flight
- **THEN** the in-flight request is aborted and the prompt resolves with `stopReason` `"cancelled"`

#### Scenario: Concurrent turns on one session are each cancellable

- **WHEN** two or more prompts are in flight on the same session id and a `session/cancel` arrives
- **THEN** the CLI cancels every in-flight turn — none is orphaned — and each prompt resolves with `stopReason` `"cancelled"`

#### Scenario: A cancel with no registered turn still takes effect

- **WHEN** a `session/cancel` notification arrives before the next prompt on that session registers its cancel function
- **THEN** that prompt resolves with `stopReason` `"cancelled"` without making a backend call

### Requirement: Unknown methods

The CLI SHALL return a JSON-RPC error for unknown methods and ignore unknown notifications.

#### Scenario: Unknown method

- **WHEN** the client sends a method the CLI does not implement
- **THEN** the CLI returns a JSON-RPC error response carrying the request id

### Requirement: Failure reporting

The CLI SHALL report a failed or rejected Memory turn as `stopReason` `"refusal"` (and a server-cancelled turn as `"cancelled"`), rather than `"end_turn"`, so ACP clients can distinguish failure from success.

#### Scenario: Failed turn

- **WHEN** the A2A stream reports `TASK_STATE_FAILED` or `TASK_STATE_REJECTED`
- **THEN** the CLI streams any failure message and returns a `session/prompt` response with `stopReason` `"refusal"`

### Requirement: Session close

The CLI SHALL implement the ACP `session/close` method: cancel any ongoing work for the session, free the session's tracked state, and respond with an empty result. It SHALL advertise `agentCapabilities.sessionCapabilities.close` before accepting calls, and SHALL treat an unknown session id as a silent no-op so a client closing an already-dropped session is not left with an error.

#### Scenario: Close cancels work and frees the session

- **WHEN** the client sends `session/close` for a session with an in-flight turn
- **THEN** every in-flight turn is cancelled, the session's state is removed, and the CLI responds with an empty result

#### Scenario: Close of an unknown session succeeds silently

- **WHEN** the client sends `session/close` for a session id the CLI does not track
- **THEN** the CLI responds with an empty result and no error

### Requirement: Session list is deliberately unsupported

The CLI SHALL NOT advertise `agentCapabilities.sessionCapabilities.list` and SHALL answer an attempted `session/list` with `-32601` method not found. This agent is an ephemeral in-memory bridge: it advertises `loadSession: false`, persists no sessions across process exit, may evict tracked sessions under its LRU cap, and does not model the per-session absolute `cwd` that ACP's `SessionInfo` requires. Listing sessions would therefore advertise state that cannot be loaded or resumed and that may already have been evicted, so the capability is deliberately declined rather than implemented.

#### Scenario: List capability is not advertised

- **WHEN** the client sends `initialize`
- **THEN** `agentCapabilities.sessionCapabilities` does not contain `list`

#### Scenario: An unadvertised session/list is refused

- **WHEN** the client sends `session/list` even though the CLI does not advertise it
- **THEN** the CLI responds with a `-32601` method not found JSON-RPC error
