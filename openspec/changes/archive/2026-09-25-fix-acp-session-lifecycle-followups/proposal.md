## Why

Independent review of #724 (which bounded the ACP session map) left three residual gaps in the session lifecycle of `apps/cli/internal/acp`:

1. The LRU cap bounds *idle* sessions only. In-flight sessions are deliberately never evicted, so the map could exceed `defaultMaxSessions` by the number of concurrently in-flight turns — not a strict bound.
2. `session.cancel` is a single slot per session, so two concurrent prompts on the *same* session id leave one turn's cancel function unreachable: a `session/cancel` orphans that turn (and its `CancelFunc` reference is later dropped without being invoked).
3. `session/close` and `session/list` are unimplemented; only `session/delete` is advertised. It is worth checking what ACP v1 actually requires of an agent before implementing or declining.

Refs #718, #724.

## What Changes

- **Strict session cap.** Tracked sessions never exceed `defaultMaxSessions` (256). When the cap is reached and no idle session can be evicted (every tracked session has an in-flight turn), `session/new` — and the lazy create in `session/prompt` for an unknown session id — now fail with a server error instead of growing the map. The guarantee that an in-flight session is never evicted is preserved; the map is hard-bounded because the only way to add a session past the cap is refused. This replaces the previous "the map may exceed the cap by the number of in-flight turns" behaviour with a strict bound and an explicit, tested refusal.
- **Per-turn cancel functions.** A session now tracks one `context.CancelFunc` per in-flight turn (`cancels map[uint64]context.CancelFunc`, keyed by turn generation) instead of a single slot, and `inFlight` is derived from that map. A `session/cancel` notification cancels *every* in-flight turn for the session, and every turn deletes its own entry when it finishes. Dropping a session (evict/delete/close) invokes all of its outstanding cancel functions, so no turn is orphaned and no `CancelFunc` leaks.
- **`session/close` implemented.** Advertise `agentCapabilities.sessionCapabilities.close`, dispatch the `session/close` method (and its notification form), and free the session's state while cancelling in-flight turns — matching ACP's "cancel any ongoing work … then free the resources" semantics. Closing an unknown session succeeds silently, mirroring `session/delete`.
- **`session/list` explicitly declined.** ACP v1 makes `list` optional and meaningful only alongside session persistence and `session/load`/`resume`. This agent is an ephemeral in-memory bridge with `loadSession: false`, no session persistence, and an LRU map whose entries can be evicted; it also does not model the required absolute `cwd` per session. Advertising `list` would misrepresent evicted sessions as listable-and-loadable. The capability is therefore deliberately not advertised, a compliant client MUST NOT call it, and an unadvertised `session/list` returns `-32601` method not found. The decline is documented in code, in the delta spec, and covered by a test.

## Capabilities

### Modified Capabilities

- `cli-acp`: the bounded session lifecycle is now a strict bound with an explicit refusal instead of a documented overshoot; cancellation is tracked per in-flight turn; `session/close` is advertised and dispatched; `session/list` is explicitly out of scope with a documented rationale.

## Impact

- `apps/cli/internal/acp/agent.go`: strict cap and refusal in `newSession`/`prompt`; `session.cancels` map replaces `cancel`/`inFlight`; `closeSession`; per-turn cancel tracking and cleanup.
- `apps/cli/internal/acp/wire.go`: advertise `sessionCapabilities.close`; add `CloseSessionParams`; note why `list` is not advertised.
- `apps/cli/internal/acp/run.go`: dispatch `session/close` (request + notification); document that `session/list` intentionally falls through to `-32601`.
- `apps/cli/internal/acp/acp_test.go`: strict-cap refusal, in-flight retention, per-turn cancellation (both concurrent turns cancelled, no leak), close round-trip/capability, and the unadvertised `session/list` decline.
