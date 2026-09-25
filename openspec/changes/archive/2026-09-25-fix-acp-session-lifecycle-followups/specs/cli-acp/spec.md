## MODIFIED Requirements

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

## ADDED Requirements

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
