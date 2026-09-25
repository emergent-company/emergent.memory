## 1. Strict in-flight cap (TDD)

- [x] 1.1 Add failing tests: with the cap reached and every tracked session in-flight, `session/new` returns an error and the map stays at the cap; an in-flight session is retained and its cancel func is not invoked.
- [x] 1.2 Make the cap strict in `agent.go` — `newSession` and the `prompt` lazy-create refuse to add a session when `len(sessions) >= maxSessions` (and `maxSessions > 0`) after eviction. Verified by 1.1.

## 2. Per-turn cancel tracking (TDD)

- [x] 2.1 Add a failing test: two concurrent prompts on the same session id register independently, a single `session/cancel` cancels **both** turns, and the cancel map drains to empty (no leak).
- [x] 2.2 Replace `session.cancel`/`session.inFlight` with `cancels map[uint64]context.CancelFunc`; register at turn start, delete at turn end, cancel all on `session/cancel`, and cancel all on drop (evict/delete/close). Never hold `a.mu` across the cancel invocations in `cancel`/`deleteSession`; keep the lookup→register barrier intact.

## 3. session/close implemented, session/list declined

- [x] 3.1 Add failing tests: `initialize` advertises `sessionCapabilities.close` and **not** `list`; `session/close` through `Run` removes the session, cancels in-flight work, and returns an empty result; unknown close is silent; `session/list` returns `-32601`.
- [x] 3.2 Advertise `close` in `wire.go`, add `CloseSessionParams`, implement `Agent.closeSession`, and dispatch `session/close` (request + notification) in `run.go`. Document the `session/list` decline in code and keep it falling through to `-32601`.

## 4. Verification

- [x] 4.1 `cd apps/cli && go build ./...`
- [x] 4.2 `cd apps/cli && go test ./...`
- [x] 4.3 `cd apps/cli && go test -race -count=1 ./internal/acp/...`
- [x] 4.4 `golangci-lint run ./internal/acp/...`; `gofmt -l` on changed files
- [x] 4.5 `openspec validate fix-acp-session-lifecycle-followups --strict`
