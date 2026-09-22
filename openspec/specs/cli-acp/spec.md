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

The CLI SHALL bound the number of ACP sessions it tracks so that a long-lived `memory acp` process does not grow without bound, and SHALL release a session's state — including any in-flight turn's cancel function — when the client deletes it.

The CLI SHALL answer the `session/delete` method by removing the session's state and cancelling any in-flight turn, and SHALL advertise `agentCapabilities.sessionCapabilities.delete` on `initialize`. Deleting a session the CLI does not track SHALL succeed silently with an empty result.

The CLI SHALL cap tracked sessions (default 256) and evict the least-recently-used idle session when a new session would exceed the cap. It SHALL NOT evict a session that has an in-flight turn, and SHALL invoke the cancel function of any session it drops.

The CLI SHALL clear a session's cancel function once its prompt turn completes, so an idle session holds no stale cancellation state.

#### Scenario: Delete removes the session

- **WHEN** the client sends `session/delete` for an existing session
- **THEN** the CLI removes the session's state, cancels any in-flight turn, and responds with an empty result

#### Scenario: Delete of an unknown session succeeds silently

- **WHEN** the client sends `session/delete` for a session id the CLI does not track
- **THEN** the CLI responds with an empty result and no error

#### Scenario: Delete capability is advertised

- **WHEN** the client sends `initialize`
- **THEN** the response carries `agentCapabilities.sessionCapabilities.delete`

#### Scenario: Session cap is enforced by LRU eviction

- **WHEN** the number of tracked sessions is at the cap and a new session is created
- **THEN** the least-recently-used idle session is evicted and its cancel function, if any, is invoked

#### Scenario: An in-flight session is never pruned

- **WHEN** a new session would exceed the cap while every tracked session has an in-flight turn
- **THEN** no session is evicted and the new session is still created

#### Scenario: A completed turn leaves no stale cancel function

- **WHEN** a prompt turn completes
- **THEN** the session's cancel function is cleared and its in-flight count returns to zero

#### Scenario: A dropped session does not admit a late turn

- **WHEN** a prompt has looked up a session but has not yet registered its cancel function, and the session is concurrently evicted or deleted
- **THEN** the session is marked as dropped before removal and every prompt that has already looked it up aborts without making a backend call

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

The CLI SHALL handle the `session/cancel` notification by interrupting any in-flight A2A request for that session and resolving the pending `session/prompt` with `stopReason` `"cancelled"`.

#### Scenario: Cancel interrupts a running turn

- **WHEN** a `session/cancel` notification arrives while a `session/prompt` for that session is in flight
- **THEN** the in-flight request is aborted and the prompt resolves with `stopReason` `"cancelled"`

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
