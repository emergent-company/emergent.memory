## ADDED Requirements

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
