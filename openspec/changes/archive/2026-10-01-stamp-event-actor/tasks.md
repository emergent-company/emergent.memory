## 1. Events choke point

- [x] 1.1 `events.Service` resolves the event actor: explicit `opts.Actor` wins, else `auth.ActorFromContext(ctx)`; add `EmitCreated/Updated/Deleted/Batch` a `context.Context` parameter.
- [x] 1.2 Thread `ctx` through every emit call site (`domain/graph/service.go` emit helpers, `domain/notifications/service.go`, `domain/agents/a2a_stream.go`, `domain/agents/ask_user_tool.go`).

## 2. Tests

- [x] 2.1 `auth.WithActor(ctx, agent, id)` → non-nil `EntityEvent.Actor` with type+id; user/system likewise.
- [x] 2.2 No ctx actor and no `opts.Actor` → nil actor.
- [x] 2.3 Explicit `opts.Actor` still wins over a ctx actor.
- [x] 2.4 End-to-end-ish: with the actor stamped, an agent-originated event is **not** dispatched by default; a user-originated event still dispatches.

## 3. Verify

- [x] 3.1 `cd apps/server && go build ./...`.
- [x] 3.2 `go test -count=1 ./domain/events/... ./domain/agents/... ./domain/graph/...`.
- [x] 3.3 `bash scripts/lint-ratchet.sh` (no regression).
- [x] 3.4 `openspec validate stamp-event-actor --strict`.
