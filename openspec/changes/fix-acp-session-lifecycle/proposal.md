## Why

`Agent.sessions` in `apps/cli/internal/acp/agent.go` is only ever written — in `newSession` and the lazy creation inside `prompt` — and never pruned. A long-lived `memory acp` process (an IDE or desktop ACP client that spawns one agent and reuses it across many sessions) therefore leaks one `session` struct per session id for the lifetime of the process, including a stale `context.CancelFunc` from the last turn. Growth is unbounded and driven purely by client behaviour.

## What Changes

- Bound the session map: track at most `defaultMaxSessions` (256) sessions and evict the least-recently-used **idle** session when a new one would exceed the cap. A session with an in-flight turn is never evicted; the map may briefly exceed the cap rather than prune a running turn.
- Invoke the `context.CancelFunc` of any session that is dropped (evicted or deleted) so an in-flight turn cannot leak its goroutine.
- Clear a session's cancel function when its turn completes, so an idle session holds no stale cancellation state.
- Implement the ACP `session/delete` method (request and notification forms): cancel any in-flight turn and drop the session's state; deleting an unknown session succeeds silently.
- Advertise `agentCapabilities.sessionCapabilities.delete` in the `initialize` response, matching ACP's capability-gated session lifecycle.

Out of scope: `session/close`, `session/list`, and `session/load` remain unimplemented.

## Capabilities

### Modified Capabilities

- `cli-acp`: adds bounded session lifecycle management — the `session/delete` method and capability, LRU session eviction, and cancel-function cleanup.

## Impact

- `apps/cli/internal/acp/agent.go`: session cap/LRU/eviction, in-flight tracking, `deleteSession`, cancel cleanup on turn end.
- `apps/cli/internal/acp/wire.go`: `SessionCapabilities` on `AgentCapabilities`, `DeleteSessionParams`.
- `apps/cli/internal/acp/run.go`: dispatch and notification handling for `session/delete`.
- `apps/cli/internal/acp/acp_test.go`: tests for delete, cap enforcement, cancel-on-eviction, in-flight retention, and cancel cleanup.
