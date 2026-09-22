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
